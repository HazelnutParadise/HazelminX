// Package github keeps the GitHub Apps and GitHub connections HazelminX may
// use to read deployment sources (spec 15.4～15.9, ticket 047.1).
//
// Shape of the data:
//   - App: a GitHub App the administrator created through the manifest flow
//     or entered by hand (GIT-013). Its private key, client secret and
//     webhook secret live in the secret store and never leave Core.
//   - Connection: one authorization — a PAT, an App user authorization or
//     an App installation — owned by the HazelminX account that made it.
//     GitHub's stable account id identifies who is behind it; several
//     connections may share an account and none overwrites another
//     (GIT-003, GIT-004). Connecting GitHub never changes HazelminX roles.
//   - Authorization request: one browser round trip to GitHub, bound to the
//     account, login session, callback address and a deadline, usable once
//     (GIT-015).
//
// Every call to GitHub goes through Client, so tests replace it.
package github

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
)

// Vault is the part of the secret store this package uses.
type Vault interface {
	Create(ctx context.Context, in secrets.CreateInput) (secrets.Secret, error)
	Rotate(ctx context.Context, id string, value []byte, actor string) (secrets.Secret, error)
	Reveal(ctx context.Context, req secrets.RevealRequest) ([]byte, int, error)
	Delete(ctx context.Context, id, actor string) error
}

// Auditor writes audit rows inside the transaction of the change.
type Auditor interface {
	RecordTx(ctx context.Context, tx *sql.Tx, ev iam.AuditEvent) error
}

// Options builds a Service.
type Options struct {
	DB     *sql.DB
	Vault  Vault
	Audit  Auditor
	Client Client
	// PublicOrigin is the configured panel address. Browser flows send
	// GitHub back to PublicOrigin+CallbackPath and nowhere else; with no
	// origin they are not available (PAT connections still are).
	PublicOrigin string
	// CloneBase is where repositories are fetched from with Git
	// ("https://github.com" when empty; tests point it at a local server).
	CloneBase string
	Now       func() time.Time
	Logger    *slog.Logger
}

// CallbackPath is the web page GitHub sends the browser back to. The page
// posts code and state to the management API.
const CallbackPath = "/sources/github/callback"

// RequestTTL is how long a browser authorization request can be finished.
const RequestTTL = 10 * time.Minute

// Service is the GitHub source store.
type Service struct {
	db      *sql.DB
	vault   Vault
	audit   Auditor
	client  Client
	origin  string
	clone   string
	now     func() time.Time
	log     *slog.Logger
	pkceKey []byte

	// refreshMu serializes token refreshes per connection: GitHub refresh
	// tokens are single-use, two refreshes at once would lose the second.
	refreshMu sync.Map // connection id → *sync.Mutex
	// backoff limits refresh retries after a failure GitHub did not
	// explain (connection id → backoffState).
	backoffMu sync.Mutex
	backoff   map[string]backoffState
	// installTokens caches installation tokens in memory only, keyed by
	// connection and requested scope.
	tokMu         sync.Mutex
	installTokens map[string]Token
}

type backoffState struct {
	failures int
	until    time.Time
}

// New returns a Service.
func New(o Options) (*Service, error) {
	if o.DB == nil || o.Vault == nil || o.Audit == nil || o.Client == nil {
		return nil, errors.New("github: DB, Vault, Audit and Client are required")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if o.CloneBase == "" {
		o.CloneBase = "https://github.com"
	}
	return &Service{db: o.DB, vault: o.Vault, audit: o.Audit, client: o.Client, origin: strings.TrimSuffix(o.PublicOrigin, "/"), clone: strings.TrimSuffix(o.CloneBase, "/"),
		now: o.Now, log: o.Logger, pkceKey: key, backoff: map[string]backoffState{}, installTokens: map[string]Token{}}, nil
}

// Actor is who asks, for ownership checks and audit rows.
type Actor struct {
	UserID    string
	Username  string
	SessionID string
	TraceID   string
	SourceIP  string
	// CanManageApps: the actor may register GitHub Apps (platform:write on
	// the global scope); checked again when a manifest flow finishes.
	CanManageApps bool
}

// Errors reported by the service. Messages never contain secret values.
var (
	ErrNotFound        = errors.New("github: not found")
	ErrValidation      = errors.New("github: validation failed")
	ErrVersionMismatch = errors.New("github: version mismatch")
	ErrNotAvailable    = errors.New("github: not available")
	ErrReferenced      = errors.New("github: still used")
	ErrNeedsReauth     = errors.New("github: the connection must be authorized again")
	ErrDisabled        = errors.New("github: the connection is disabled")
	ErrRefreshBackoff  = errors.New("github: refreshing the token failed recently; waiting before the next try")
	ErrForbidden       = errors.New("github: not allowed")
)

// ValidationError names the field that is wrong.
type ValidationError struct {
	Field, Message string
}

func (e *ValidationError) Error() string { return "github: " + e.Field + ": " + e.Message }
func (e *ValidationError) Unwrap() error { return ErrValidation }

func invalid(field, msg string) error { return &ValidationError{Field: field, Message: msg} }

// AuthorizationError refuses a callback or an authorization result.
type AuthorizationError struct {
	// Reason: unknown, expired, used, session-mismatch, wrong-account,
	// installation-mismatch, app-mismatch, cancelled-by-github.
	Reason string
}

func (e *AuthorizationError) Error() string { return "github: authorization refused: " + e.Reason }

// ReferencedError lists what still uses an App.
type ReferencedError struct{ Connections int }

func (e *ReferencedError) Error() string {
	return fmt.Sprintf("github: %d connection(s) still use this App", e.Connections)
}
func (e *ReferencedError) Unwrap() error { return ErrReferenced }

// App is a GitHub App registered in this HazelminX. It never carries a secret.
type App struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Host             string            `json:"host"`
	AppID            int64             `json:"appId"`
	Slug             string            `json:"slug"`
	ClientID         string            `json:"clientId"`
	Owner            Account           `json:"owner"`
	HTMLURL          string            `json:"htmlUrl"`
	Permissions      map[string]string `json:"permissions"`
	Events           []string          `json:"events"`
	Source           string            `json:"source"` // manifest | manual
	HasWebhookSecret bool              `json:"hasWebhookSecret"`
	// InstallURL is where an account installs the App.
	InstallURL string    `json:"installUrl"`
	Version    int64     `json:"version"`
	CreatedBy  string    `json:"createdBy"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`

	privateKeySecret, clientSecretSecret, webhookSecretSecret string
}

// ManualApp is an App the administrator created on GitHub and enters here.
type ManualApp struct {
	Name          string `json:"name"`
	AppID         int64  `json:"appId"`
	ClientID      string `json:"clientId"`
	ClientSecret  string `json:"clientSecret"`
	PrivateKey    string `json:"privateKey"`
	WebhookSecret string `json:"webhookSecret"`
}

func (s *Service) ts() string { return s.now().UTC().Format(time.RFC3339) }

func parseTS(v string) time.Time {
	t, _ := time.Parse(time.RFC3339, v)
	return t
}

func parseTSPtr(v sql.NullString) *time.Time {
	if !v.Valid || v.String == "" {
		return nil
	}
	t := parseTS(v.String)
	return &t
}

func tsOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func newID(prefix string) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b)
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashState(state string) string {
	sum := sha256.Sum256([]byte(state))
	return hex.EncodeToString(sum[:])
}

// pkceVerifier is derived from the request id with a per-process key, so
// no verifier is stored; a restart only invalidates requests in flight.
func (s *Service) pkceVerifier(requestID string) string {
	m := hmac.New(sha256.New, s.pkceKey)
	m.Write([]byte("pkce:" + requestID))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// consumer is the secret-store consumer name of a row.
func consumer(rowID string) string { return "github:" + rowID }

func (s *Service) storeSecret(ctx context.Context, rowID, part string, kind secrets.Kind, value string, expires *time.Time, actor Actor) (string, error) {
	sec, err := s.vault.Create(ctx, secrets.CreateInput{Kind: kind, Name: "github/" + rowID + "/" + part, Value: []byte(value),
		Scope: []string{consumer(rowID)}, ExpiresAt: expires, Actor: actor.UserID})
	if err != nil {
		return "", fmt.Errorf("github: store %s: %w", part, err)
	}
	return sec.ID, nil
}

func (s *Service) reveal(ctx context.Context, rowID, secretID string) (string, error) {
	v, _, err := s.vault.Reveal(ctx, secrets.RevealRequest{ID: secretID, Consumer: consumer(rowID)})
	if err != nil {
		return "", fmt.Errorf("github: read stored credential: %w", err)
	}
	return string(v), nil
}

// dropSecrets removes secrets that a failed or undone change left behind.
// A failure is logged: the value stays encrypted and unreferenced.
func (s *Service) dropSecrets(ctx context.Context, actor Actor, ids ...string) {
	for _, id := range ids {
		if id == "" {
			continue
		}
		if err := s.vault.Delete(context.WithoutCancel(ctx), id, actor.UserID); err != nil && !errors.Is(err, secrets.ErrNotFound) {
			s.log.Error("github: unreferenced secret could not be deleted", "secret", id, "error", err)
		}
	}
}

func (s *Service) auditTx(ctx context.Context, tx *sql.Tx, actor Actor, action, kind, id string, before, after map[string]any) error {
	return s.audit.RecordTx(ctx, tx, iam.AuditEvent{Action: action, Scope: iam.Global, ResourceKind: kind, ResourceID: id,
		ActorID: actor.UserID, ActorName: actor.Username, Result: iam.ResultOK, Before: before, After: after, TraceID: actor.TraceID, SourceIP: actor.SourceIP})
}

// --- Apps -----------------------------------------------------------------

const appColumns = `id, name, host, app_id, slug, client_id, owner_login, owner_id, owner_type, html_url, permissions, events, source,
	private_key_secret, client_secret_secret, webhook_secret_secret, version, created_by, created_at, updated_at`

type scanner interface{ Scan(...any) error }

func scanApp(r scanner) (App, error) {
	var a App
	var perms, events, created, updated string
	var hook sql.NullString
	err := r.Scan(&a.ID, &a.Name, &a.Host, &a.AppID, &a.Slug, &a.ClientID, &a.Owner.Login, &a.Owner.ID, &a.Owner.Type, &a.HTMLURL, &perms, &events, &a.Source,
		&a.privateKeySecret, &a.clientSecretSecret, &hook, &a.Version, &a.CreatedBy, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return App{}, ErrNotFound
	}
	if err != nil {
		return App{}, err
	}
	_ = json.Unmarshal([]byte(perms), &a.Permissions)
	_ = json.Unmarshal([]byte(events), &a.Events)
	if a.Permissions == nil {
		a.Permissions = map[string]string{}
	}
	if a.Events == nil {
		a.Events = []string{}
	}
	a.webhookSecretSecret, a.HasWebhookSecret = hook.String, hook.Valid
	a.CreatedAt, a.UpdatedAt = parseTS(created), parseTS(updated)
	a.InstallURL = "https://" + a.Host + "/apps/" + a.Slug + "/installations/new"
	return a, nil
}

// GetApp returns one App.
func (s *Service) GetApp(ctx context.Context, id string) (App, error) {
	return scanApp(s.db.QueryRowContext(ctx, `SELECT `+appColumns+` FROM github_apps WHERE id=?`, id))
}

// ListApps returns every App, by name.
func (s *Service) ListApps(ctx context.Context) ([]App, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+appColumns+` FROM github_apps ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []App
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Service) appSigner(ctx context.Context, a App) (string, error) {
	pemText, err := s.reveal(ctx, a.ID, a.privateKeySecret)
	if err != nil {
		return "", err
	}
	key, err := parsePrivateKey([]byte(pemText))
	if err != nil {
		return "", fmt.Errorf("github: stored App private key: %w", err)
	}
	return appJWT(key, a.AppID, s.now())
}

func cleanName(name string) string { return strings.TrimSpace(name) }

// CreateManualApp registers an App the administrator created on GitHub
// (GIT-013 手動輸入). The key is checked by asking GitHub which App it
// signs for; a key of another App, or a wrong App id, is refused.
func (s *Service) CreateManualApp(ctx context.Context, in ManualApp, actor Actor) (App, error) {
	switch {
	case in.AppID <= 0:
		return App{}, invalid("appId", "the App ID from the App's settings page")
	case strings.TrimSpace(in.ClientID) == "":
		return App{}, invalid("clientId", "required")
	case in.ClientSecret == "":
		return App{}, invalid("clientSecret", "required")
	case in.PrivateKey == "":
		return App{}, invalid("privateKey", "required")
	}
	key, err := parsePrivateKey([]byte(in.PrivateKey))
	if err != nil {
		return App{}, invalid("privateKey", err.Error())
	}
	jwt, err := appJWT(key, in.AppID, s.now())
	if err != nil {
		return App{}, err
	}
	info, err := s.client.GetApp(ctx, jwt)
	if err != nil {
		return App{}, err
	}
	if info.ID != in.AppID {
		return App{}, invalid("privateKey", "this key belongs to another App")
	}
	if info.ClientID != "" && info.ClientID != strings.TrimSpace(in.ClientID) {
		return App{}, invalid("clientId", "GitHub reports another client ID for this App")
	}
	info.ClientID = strings.TrimSpace(in.ClientID)
	name := cleanName(in.Name)
	if name == "" {
		name = info.Name
	}
	return s.insertApp(ctx, name, "manual", info, in.ClientSecret, in.PrivateKey, in.WebhookSecret, actor)
}

func (s *Service) insertApp(ctx context.Context, name, source string, info AppInfo, clientSecret, pemText, webhookSecret string, actor Actor) (App, error) {
	if name == "" {
		name = info.Slug
	}
	id := newID("gha_")
	var created []string
	var tx *sql.Tx
	fail := func(err error) (App, error) {
		if tx != nil {
			tx.Rollback() // the secret store needs the connection the transaction holds
		}
		s.dropSecrets(ctx, actor, created...)
		return App{}, err
	}
	keyID, err := s.storeSecret(ctx, id, "private-key", secrets.KindPrivateKey, pemText, nil, actor)
	if err != nil {
		return fail(err)
	}
	created = append(created, keyID)
	csID, err := s.storeSecret(ctx, id, "client-secret", secrets.KindPassword, clientSecret, nil, actor)
	if err != nil {
		return fail(err)
	}
	created = append(created, csID)
	var hookID any
	if webhookSecret != "" {
		h, err := s.storeSecret(ctx, id, "webhook-secret", secrets.KindGeneric, webhookSecret, nil, actor)
		if err != nil {
			return fail(err)
		}
		created, hookID = append(created, h), h
	}
	perms, _ := json.Marshal(nonNilMap(info.Permissions))
	events, _ := json.Marshal(nonNilSlice(info.Events))
	now := s.ts()
	tx, err = s.db.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	var taken int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM github_apps WHERE host='github.com' AND app_id=?`, info.ID).Scan(&taken); err != nil {
		return fail(err)
	}
	if taken > 0 {
		return fail(invalid("appId", "this App is already registered"))
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO github_apps(id, name, host, app_id, slug, client_id, owner_login, owner_id, owner_type, html_url, permissions, events, source,
		private_key_secret, client_secret_secret, webhook_secret_secret, version, created_by, created_at, updated_at)
		VALUES(?, ?, 'github.com', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		id, name, info.ID, info.Slug, info.ClientID, info.Owner.Login, info.Owner.ID, info.Owner.Type, info.HTMLURL, string(perms), string(events), source,
		keyID, csID, hookID, actor.UserID, now, now); err != nil {
		return fail(err)
	}
	if err := s.auditTx(ctx, tx, actor, "github.app.create", "github-app", id, nil, map[string]any{"name": name, "appId": info.ID, "slug": info.Slug,
		"source": source, "permissions": info.Permissions, "owner": info.Owner.Login}); err != nil {
		return fail(err)
	}
	if err := tx.Commit(); err != nil {
		return fail(err)
	}
	return s.GetApp(ctx, id)
}

// DeleteApp removes an App no connection uses. It removes the App here
// only; the App stays on GitHub until its owner deletes it there.
func (s *Service) DeleteApp(ctx context.Context, id string, actor Actor) error {
	a, err := s.GetApp(ctx, id)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM github_connections WHERE app_ref=?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return &ReferencedError{Connections: n}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM github_auth_requests WHERE app_ref=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM github_apps WHERE id=?`, id); err != nil {
		return err
	}
	if err := s.auditTx(ctx, tx, actor, "github.app.delete", "github-app", id, map[string]any{"name": a.Name, "appId": a.AppID}, nil); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.dropSecrets(ctx, actor, a.privateKeySecret, a.clientSecretSecret, a.webhookSecretSecret)
	return nil
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func nonNilSlice(v []string) []string {
	if v == nil {
		return []string{}
	}
	out := append([]string(nil), v...)
	sort.Strings(out)
	return out
}

var orgName = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
