package github

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
)

// Connection kinds.
const (
	KindPAT             = "pat"
	KindAppUser         = "app-user"
	KindAppInstallation = "app-installation"
)

// Connection states.
const (
	StateActive      = "active"
	StateDisabled    = "disabled"
	StateNeedsReauth = "needs-reauth"
)

// Capability answers.
const (
	Yes     = "yes"
	No      = "no"
	Unknown = "unknown"
)

// Capabilities are what a connection lets HazelminX do, each on its own
// (GIT-014): basic reading never needs the others.
type Capabilities struct {
	// RepoRead: list repositories and read their content.
	RepoRead string `json:"repoRead"`
	// Webhook: GitHub can send push events (App events or a repository
	// webhook the token may create). Whether they reach HazelminX is the
	// reachability check's answer.
	Webhook string `json:"webhook"`
	// StatusWrite: write deployment status back to commits.
	StatusWrite string `json:"statusWrite"`
	// ContentsWrite: push to repositories. HazelminX never needs it.
	ContentsWrite string `json:"contentsWrite"`
	// OrgAdmin: manage organizations. HazelminX never needs it.
	OrgAdmin string `json:"orgAdmin"`
	// Polling is the deployment trigger that works with read access only.
	Polling bool `json:"polling"`
}

// Connection is one GitHub authorization. It never carries a secret.
type Connection struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Host  string `json:"host"`
	AppID string `json:"appId,omitempty"` // HazelminX App id
	// Account is who GitHub says is behind the authorization: the token's
	// user, or the account the App is installed on. Its id is stable.
	Account             Account           `json:"account"`
	InstallationID      int64             `json:"installationId,omitempty"`
	RepositorySelection string            `json:"repositorySelection,omitempty"`
	TokenType           string            `json:"tokenType"`
	Scopes              []string          `json:"scopes"`
	Permissions         map[string]string `json:"permissions"`
	Events              []string          `json:"events"`
	Capabilities        Capabilities      `json:"capabilities"`
	State               string            `json:"state"`
	StateReason         string            `json:"stateReason,omitempty"`
	AccessExpiresAt     *time.Time        `json:"accessExpiresAt,omitempty"`
	RefreshExpiresAt    *time.Time        `json:"refreshExpiresAt,omitempty"`
	LastCheckedAt       *time.Time        `json:"lastCheckedAt,omitempty"`
	OwnerUserID         string            `json:"ownerUserId"`
	Version             int64             `json:"version"`
	CreatedAt           time.Time         `json:"createdAt"`
	UpdatedAt           time.Time         `json:"updatedAt"`

	accessSecret, refreshSecret string
}

const connColumns = `id, owner_user_id, name, kind, host, app_ref, account_id, account_login, account_type, installation_id, repository_selection,
	token_type, scopes, permissions, events, state, state_reason, access_secret, refresh_secret, access_expires_at, refresh_expires_at,
	last_checked_at, version, created_at, updated_at`

func scanConn(r scanner) (Connection, error) {
	var c Connection
	var app, access, refresh, accessExp, refreshExp, checked sql.NullString
	var inst sql.NullInt64
	var scopes, perms, events, created, updated string
	err := r.Scan(&c.ID, &c.OwnerUserID, &c.Name, &c.Kind, &c.Host, &app, &c.Account.ID, &c.Account.Login, &c.Account.Type, &inst, &c.RepositorySelection,
		&c.TokenType, &scopes, &perms, &events, &c.State, &c.StateReason, &access, &refresh, &accessExp, &refreshExp, &checked, &c.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	if err != nil {
		return Connection{}, err
	}
	c.AppID, c.InstallationID = app.String, inst.Int64
	c.accessSecret, c.refreshSecret = access.String, refresh.String
	_ = json.Unmarshal([]byte(scopes), &c.Scopes)
	_ = json.Unmarshal([]byte(perms), &c.Permissions)
	_ = json.Unmarshal([]byte(events), &c.Events)
	if c.Scopes == nil {
		c.Scopes = []string{}
	}
	if c.Permissions == nil {
		c.Permissions = map[string]string{}
	}
	if c.Events == nil {
		c.Events = []string{}
	}
	c.AccessExpiresAt, c.RefreshExpiresAt, c.LastCheckedAt = parseTSPtr(accessExp), parseTSPtr(refreshExp), parseTSPtr(checked)
	c.CreatedAt, c.UpdatedAt = parseTS(created), parseTS(updated)
	c.Capabilities = capabilitiesOf(c)
	return c, nil
}

func level(perm string, want string) string {
	switch {
	case perm == "write" || perm == "admin":
		return Yes
	case perm == "read" && want == "read":
		return Yes
	}
	return No
}

// capabilitiesOf derives what a connection can do from what GitHub granted.
// A fine-grained PAT's permissions are not readable through the API, so
// only the part HazelminX verified (listing repositories) is known.
func capabilitiesOf(c Connection) Capabilities {
	var k Capabilities
	switch c.Kind {
	case KindAppUser, KindAppInstallation:
		p := c.Permissions
		k.RepoRead = level(p["contents"], "read")
		k.ContentsWrite = level(p["contents"], "write")
		k.StatusWrite = No
		if level(p["statuses"], "write") == Yes || level(p["deployments"], "write") == Yes {
			k.StatusWrite = Yes
		}
		k.OrgAdmin = level(p["administration"], "write")
		if level(p["organization_administration"], "write") == Yes {
			k.OrgAdmin = Yes
		}
		k.Webhook = No
		for _, e := range c.Events {
			if e == "push" {
				k.Webhook = Yes
			}
		}
	case KindPAT:
		if c.Scopes == nil || c.TokenType == "fine-grained" {
			k = Capabilities{RepoRead: Unknown, Webhook: Unknown, StatusWrite: Unknown, ContentsWrite: Unknown, OrgAdmin: Unknown}
			if c.Permissions["repositories"] == "read" {
				k.RepoRead = Yes
			}
			break
		}
		has := map[string]bool{}
		for _, s := range c.Scopes {
			has[s] = true
		}
		k.RepoRead, k.ContentsWrite, k.StatusWrite, k.Webhook, k.OrgAdmin = No, No, No, No, No
		if has["repo"] {
			k.RepoRead, k.ContentsWrite, k.StatusWrite = Yes, Yes, Yes
		}
		if has["repo:status"] || has["repo_deployment"] {
			k.StatusWrite = Yes
		}
		if has["admin:repo_hook"] || has["write:repo_hook"] {
			k.Webhook = Yes
		}
		if has["admin:org"] || has["write:org"] {
			k.OrgAdmin = Yes
		}
		if !has["repo"] && c.Permissions["repositories"] == "read" {
			// public_repo or no repo scope: public repositories only.
			k.RepoRead = Yes
		}
	}
	k.Polling = k.RepoRead != No
	return k
}

// GetConnection returns a connection the actor owns; others are ErrNotFound.
func (s *Service) GetConnection(ctx context.Context, id string, actor Actor) (Connection, error) {
	c, err := scanConn(s.db.QueryRowContext(ctx, `SELECT `+connColumns+` FROM github_connections WHERE id=?`, id))
	if err != nil {
		return Connection{}, err
	}
	if c.OwnerUserID != actor.UserID {
		return Connection{}, ErrNotFound
	}
	return c, nil
}

func (s *Service) getConn(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (Connection, error) {
	return scanConn(q.QueryRowContext(ctx, `SELECT `+connColumns+` FROM github_connections WHERE id=?`, id))
}

// ListConnections returns the actor's connections, oldest first.
func (s *Service) ListConnections(ctx context.Context, actor Actor) ([]Connection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+connColumns+` FROM github_connections WHERE owner_user_id=? ORDER BY created_at, id`, actor.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Connection
	for rows.Next() {
		c, err := scanConn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PATInput creates a PAT connection.
type PATInput struct {
	Name  string `json:"name"`
	Token string `json:"token"`
}

// patFacts asks GitHub who the token is and what it can do.
func (s *Service) patFacts(ctx context.Context, token string) (User, TokenFacts, bool, error) {
	user, facts, err := s.client.GetUser(ctx, token)
	if err != nil {
		return User{}, TokenFacts{}, false, err
	}
	canList, err := s.client.CanListRepositories(ctx, token)
	if err != nil {
		return User{}, TokenFacts{}, false, err
	}
	return user, facts, canList, nil
}

func patShape(facts TokenFacts, canList bool) (tokenType string, scopes []string, perms map[string]string) {
	tokenType, perms = "fine-grained", map[string]string{}
	if facts.Scopes != nil {
		tokenType, scopes = "classic", nonNilSlice(facts.Scopes)
	}
	if canList {
		perms["repositories"] = "read"
	}
	return tokenType, scopes, perms
}

// CreatePAT verifies a personal access token with GitHub and saves it as a
// reusable connection (GIT-002). The token is stored encrypted and never
// returned.
func (s *Service) CreatePAT(ctx context.Context, in PATInput, actor Actor) (Connection, error) {
	token := strings.TrimSpace(in.Token)
	if token == "" {
		return Connection{}, invalid("token", "required")
	}
	user, facts, canList, err := s.patFacts(ctx, token)
	if err != nil {
		return Connection{}, err
	}
	if facts.ExpiresAt != nil && !facts.ExpiresAt.After(s.now()) {
		return Connection{}, invalid("token", "this token has expired on GitHub")
	}
	tokenType, scopes, perms := patShape(facts, canList)
	id := newID("ghc_")
	secID, err := s.storeSecret(ctx, id, "token", secrets.KindAPIToken, token, nil, actor)
	if err != nil {
		return Connection{}, err
	}
	name := cleanName(in.Name)
	if name == "" {
		name = user.Login
	}
	c := Connection{ID: id, Name: name, Kind: KindPAT, Account: user, TokenType: tokenType, Scopes: scopes, Permissions: perms, accessSecret: secID}
	if facts.ExpiresAt != nil {
		c.AccessExpiresAt = facts.ExpiresAt
	}
	if err := s.insertConn(ctx, c, actor, "github.connection.create"); err != nil {
		s.dropSecrets(ctx, actor, secID)
		return Connection{}, err
	}
	return s.GetConnection(ctx, id, actor)
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func ptrTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func (s *Service) insertConnTx(ctx context.Context, tx *sql.Tx, c Connection, actor Actor, action string) error {
	now := s.ts()
	var app, inst, access, refresh any
	if c.AppID != "" {
		app = c.AppID
	}
	if c.Kind == KindAppInstallation {
		inst = c.InstallationID
	}
	if c.accessSecret != "" {
		access = c.accessSecret
	}
	if c.refreshSecret != "" {
		refresh = c.refreshSecret
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO github_connections(id, owner_user_id, name, kind, host, app_ref, account_id, account_login, account_type,
		installation_id, repository_selection, token_type, scopes, permissions, events, state, state_reason, access_secret, refresh_secret,
		access_expires_at, refresh_expires_at, last_checked_at, version, created_at, updated_at)
		VALUES(?, ?, ?, ?, 'github.com', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', '', ?, ?, ?, ?, ?, 1, ?, ?)`,
		c.ID, actor.UserID, c.Name, c.Kind, app, c.Account.ID, c.Account.Login, c.Account.Type, inst, c.RepositorySelection, c.TokenType,
		jsonOf(nonNilSlice(c.Scopes)), jsonOf(nonNilMap(c.Permissions)), jsonOf(nonNilSlice(c.Events)), access, refresh,
		tsOrNil(ptrTime(c.AccessExpiresAt)), tsOrNil(ptrTime(c.RefreshExpiresAt)), now, now, now); err != nil {
		return fmt.Errorf("github: save connection: %w", err)
	}
	return s.auditTx(ctx, tx, actor, action, "github-connection", c.ID, nil, map[string]any{"name": c.Name, "kind": c.Kind,
		"accountId": c.Account.ID, "accountLogin": c.Account.Login, "installationId": c.InstallationID, "tokenType": c.TokenType,
		"scopes": nonNilSlice(c.Scopes), "permissions": nonNilMap(c.Permissions)})
}

func (s *Service) insertConn(ctx context.Context, c Connection, actor Actor, action string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.insertConnTx(ctx, tx, c, actor, action); err != nil {
		return err
	}
	return tx.Commit()
}

// Patch changes the name of a connection.
type Patch struct {
	Name *string `json:"name"`
}

// UpdateConnection renames a connection at the given version. The GitHub
// identity is not touched (GIT-003).
func (s *Service) UpdateConnection(ctx context.Context, id string, version int64, p Patch, actor Actor) (Connection, error) {
	if p.Name == nil {
		return Connection{}, invalid("name", "nothing to change")
	}
	name := cleanName(*p.Name)
	if name == "" || len(name) > 100 {
		return Connection{}, invalid("name", "1 to 100 characters")
	}
	return s.mutate(ctx, id, version, actor, "github.connection.rename", func(c Connection) (map[string]any, string, []any, error) {
		return map[string]any{"name": name}, "name=?", []any{name}, nil
	})
}

// SetEnabled disables a connection (nothing uses it until enabled) or
// enables it again. A connection that needs authorization stays so.
func (s *Service) SetEnabled(ctx context.Context, id string, enabled bool, actor Actor) (Connection, error) {
	action := "github.connection.disable"
	if enabled {
		action = "github.connection.enable"
	}
	return s.mutate(ctx, id, 0, actor, action, func(c Connection) (map[string]any, string, []any, error) {
		switch {
		case !enabled:
			return map[string]any{"state": StateDisabled}, "state='disabled', state_reason='disabled by user'", nil, nil
		case c.State != StateDisabled:
			return map[string]any{"state": c.State}, "state=state", nil, nil
		default:
			return map[string]any{"state": StateActive}, "state='active', state_reason=''", nil, nil
		}
	})
}

// mutate applies one change to a connection the actor owns, with its audit
// row in the same transaction. version 0 skips the version condition.
func (s *Service) mutate(ctx context.Context, id string, version int64, actor Actor, action string,
	change func(Connection) (after map[string]any, set string, args []any, err error)) (Connection, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Connection{}, err
	}
	defer tx.Rollback()
	c, err := s.getConn(ctx, tx, id)
	if err != nil {
		return Connection{}, err
	}
	if c.OwnerUserID != actor.UserID {
		return Connection{}, ErrNotFound
	}
	if version != 0 && c.Version != version {
		return Connection{}, ErrVersionMismatch
	}
	after, set, args, err := change(c)
	if err != nil {
		return Connection{}, err
	}
	args = append(args, s.ts(), id, c.Version)
	res, err := tx.ExecContext(ctx, `UPDATE github_connections SET `+set+`, version=version+1, updated_at=? WHERE id=? AND version=?`, args...)
	if err != nil {
		return Connection{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return Connection{}, ErrVersionMismatch
	}
	if err := s.auditTx(ctx, tx, actor, action, "github-connection", id, map[string]any{"name": c.Name, "state": c.State}, after); err != nil {
		return Connection{}, err
	}
	if err := tx.Commit(); err != nil {
		return Connection{}, err
	}
	return s.GetConnection(ctx, id, actor)
}

// RotatePAT replaces the token of one PAT connection (GIT-004: no other
// connection changes). The new token must belong to the same GitHub
// account, so a rotation can never switch identities quietly.
func (s *Service) RotatePAT(ctx context.Context, id, token string, actor Actor) (Connection, error) {
	c, err := s.GetConnection(ctx, id, actor)
	if err != nil {
		return Connection{}, err
	}
	if c.Kind != KindPAT {
		return Connection{}, invalid("token", "only PAT connections take a token; authorize App connections again instead")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return Connection{}, invalid("token", "required")
	}
	user, facts, canList, err := s.patFacts(ctx, token)
	if err != nil {
		return Connection{}, err
	}
	if user.ID != c.Account.ID {
		return Connection{}, &AuthorizationError{Reason: "wrong-account"}
	}
	if facts.ExpiresAt != nil && !facts.ExpiresAt.After(s.now()) {
		return Connection{}, invalid("token", "this token has expired on GitHub")
	}
	tokenType, scopes, perms := patShape(facts, canList)
	// The value is replaced first: if the row update fails afterwards, the
	// connection already uses the verified new token of the same account.
	if _, err := s.vault.Rotate(ctx, c.accessSecret, []byte(token), actor.UserID); err != nil {
		return Connection{}, fmt.Errorf("github: store token: %w", err)
	}
	return s.mutate(ctx, id, 0, actor, "github.connection.rotate", func(Connection) (map[string]any, string, []any, error) {
		return map[string]any{"accountLogin": user.Login, "tokenType": tokenType, "scopes": nonNilSlice(scopes), "expiresAt": facts.ExpiresAt},
			"account_login=?, token_type=?, scopes=?, permissions=?, access_expires_at=?, state=CASE WHEN state='disabled' THEN 'disabled' ELSE 'active' END, state_reason=CASE WHEN state='disabled' THEN state_reason ELSE '' END, last_checked_at=?",
			[]any{user.Login, tokenType, jsonOf(nonNilSlice(scopes)), jsonOf(perms), tsOrNil(ptrTime(facts.ExpiresAt)), s.ts()}, nil
	})
}

// DeleteConnection removes a connection and its stored credentials. Nothing
// on GitHub is revoked; the PAT or App authorization stays valid there.
func (s *Service) DeleteConnection(ctx context.Context, id string, actor Actor) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	c, err := s.getConn(ctx, tx, id)
	if err != nil {
		return err
	}
	if c.OwnerUserID != actor.UserID {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM github_auth_requests WHERE connection_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM github_connections WHERE id=?`, id); err != nil {
		return err
	}
	if err := s.auditTx(ctx, tx, actor, "github.connection.delete", "github-connection", id,
		map[string]any{"name": c.Name, "kind": c.Kind, "accountId": c.Account.ID, "accountLogin": c.Account.Login}, nil); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.forgetTokens(id)
	s.dropSecrets(ctx, actor, c.accessSecret, c.refreshSecret)
	return nil
}

// setState records a state GitHub's answer revealed (needs-reauth, back to
// active) without an actor-driven change; it keeps a disabled connection
// disabled.
func (s *Service) setState(ctx context.Context, id, state, reason string) {
	_, err := s.db.ExecContext(context.WithoutCancel(ctx), `UPDATE github_connections SET state=?, state_reason=?, last_checked_at=?, updated_at=?, version=version+1
		WHERE id=? AND state<>'disabled' AND (state<>? OR state_reason<>?)`, state, reason, s.ts(), s.ts(), id, state, reason)
	if err != nil {
		s.log.Error("github: connection state could not be saved", "connection", id, "state", state, "error", err)
	}
}
