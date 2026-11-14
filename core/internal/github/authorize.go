package github

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
)

// Authorization purposes.
const (
	PurposeManifest  = "manifest"
	PurposeAuthorize = "authorize"
	PurposeInstall   = "install"
)

// Optional capabilities an App may ask for beyond reading (GIT-014). The
// manifest asks only for what is listed.
const (
	CapabilityWebhook     = "webhook"
	CapabilityStatusWrite = "status-write"
)

// ManifestInput starts the guided App creation (GIT-013 引導設定).
type ManifestInput struct {
	Name string `json:"name"`
	// Organization creates the App under this organization instead of the
	// signed-in GitHub user.
	Organization string `json:"organization"`
	// Capabilities beyond reading: "webhook" (push events to WebhookURL),
	// "status-write" (deployment status on commits).
	Capabilities []string `json:"capabilities"`
	// WebhookURL is the public address GitHub sends events to; required
	// with the webhook capability. The panel itself need not be public.
	WebhookURL string `json:"webhookUrl"`
}

// AuthorizeInput starts a browser authorization with a registered App.
type AuthorizeInput struct {
	AppID string `json:"appId"`
	// Mode is "authorize" (sign in with GitHub and grant the App's user
	// access) or "install" (install the App on an account, then authorize).
	Mode string `json:"mode"`
	// Name for the new connection; GitHub's login when empty.
	Name string `json:"name"`
	// ConnectionID authorizes an existing app-user connection again; the
	// result must be the same GitHub account.
	ConnectionID string `json:"connectionId"`
}

// Started is what the browser needs to go to GitHub.
type Started struct {
	ID      string `json:"id"`
	Purpose string `json:"purpose"`
	// State is the one-time value GitHub sends back; the callback page
	// posts it with the code. It is shown only in this answer.
	State string `json:"state"`
	// URL is where the browser goes. For the manifest flow the browser
	// POSTs Manifest as form field "manifest" to it.
	URL         string          `json:"url"`
	Method      string          `json:"method"` // GET | POST
	Manifest    json.RawMessage `json:"manifest,omitempty"`
	RedirectURI string          `json:"redirectUri"`
	ExpiresAt   time.Time       `json:"expiresAt"`
}

// Callback is what GitHub sent back to the callback page.
type Callback struct {
	State          string `json:"state"`
	Code           string `json:"code"`
	InstallationID int64  `json:"installationId"`
	SetupAction    string `json:"setupAction"`
	// Error is GitHub's error parameter (access_denied when the user
	// cancelled on GitHub).
	Error string `json:"error"`
}

// Completed is the result of a finished authorization.
type Completed struct {
	Purpose     string       `json:"purpose"`
	App         *App         `json:"app,omitempty"`
	Connections []Connection `json:"connections"`
}

func (s *Service) redirectURI() (string, error) {
	if s.origin == "" {
		return "", fmt.Errorf("%w: browser authorization needs the panel's public origin (--public-origin); without one, use a PAT connection", ErrNotAvailable)
	}
	return s.origin + CallbackPath, nil
}

func (s *Service) newRequest(ctx context.Context, actor Actor, purpose, appRef, connID, name, redirect string, doc any) (id, state string, expires time.Time, err error) {
	if actor.SessionID == "" {
		return "", "", time.Time{}, fmt.Errorf("%w: a browser authorization must start from a signed-in browser session, not an API token", ErrNotAvailable)
	}
	id, state = newID("ghr_"), randomToken()
	now := s.now().UTC()
	expires = now.Add(RequestTTL).Truncate(time.Second)
	var app, conn any
	if appRef != "" {
		app = appRef
	}
	if connID != "" {
		conn = connID
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", time.Time{}, err
	}
	defer tx.Rollback()
	// Finished or expired requests older than a day are only clutter.
	if _, err := tx.ExecContext(ctx, `DELETE FROM github_auth_requests WHERE expires_at < ?`, now.Add(-24*time.Hour).Format(time.RFC3339)); err != nil {
		return "", "", time.Time{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO github_auth_requests(id, state_hash, user_id, session_id, purpose, app_ref, connection_id, name, redirect_uri, doc, created_at, expires_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, hashState(state), actor.UserID, actor.SessionID, purpose, app, conn, name, redirect, jsonOf(doc),
		now.Format(time.RFC3339), expires.Format(time.RFC3339)); err != nil {
		return "", "", time.Time{}, err
	}
	if err := s.auditTx(ctx, tx, actor, "github.authorization.start", "github-authorization", id, nil,
		map[string]any{"purpose": purpose, "appId": appRef, "connectionId": connID, "expiresAt": expires}); err != nil {
		return "", "", time.Time{}, err
	}
	return id, state, expires, tx.Commit()
}

// StartManifest prepares the App manifest flow. The manifest asks for
// metadata and contents read only, plus the optional capabilities named.
func (s *Service) StartManifest(ctx context.Context, in ManifestInput, actor Actor) (Started, error) {
	redirect, err := s.redirectURI()
	if err != nil {
		return Started{}, err
	}
	name := cleanName(in.Name)
	if name == "" || len(name) > 34 {
		return Started{}, invalid("name", "1 to 34 characters (GitHub's App name limit)")
	}
	if in.Organization != "" && !orgName.MatchString(in.Organization) {
		return Started{}, invalid("organization", "not a GitHub organization name")
	}
	perms := map[string]string{"metadata": "read", "contents": "read"}
	manifest := map[string]any{
		"name": name, "url": s.origin, "redirect_url": redirect, "callback_urls": []string{redirect}, "setup_url": redirect,
		"public": false, "request_oauth_on_install": true, "setup_on_update": false, "default_permissions": perms,
		"default_events": []string{}, "hook_attributes": map[string]any{"url": s.origin + "/integrations/github/webhook", "active": false},
	}
	for _, c := range in.Capabilities {
		switch c {
		case CapabilityWebhook:
			u, err := url.Parse(in.WebhookURL)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return Started{}, invalid("webhookUrl", "a public https:// address without credentials or query, required with the webhook capability")
			}
			manifest["hook_attributes"] = map[string]any{"url": u.String(), "active": true}
			manifest["default_events"] = []string{"push"}
		case CapabilityStatusWrite:
			perms["statuses"], perms["deployments"] = "write", "write"
		default:
			return Started{}, invalid("capabilities", "unknown capability "+c)
		}
	}
	doc, _ := json.Marshal(manifest)
	id, state, exp, err := s.newRequest(ctx, actor, PurposeManifest, "", "", name, redirect, manifest)
	if err != nil {
		return Started{}, err
	}
	target := "https://github.com/settings/apps/new"
	if in.Organization != "" {
		target = "https://github.com/organizations/" + url.PathEscape(in.Organization) + "/settings/apps/new"
	}
	return Started{ID: id, Purpose: PurposeManifest, State: state, URL: target + "?state=" + url.QueryEscape(state), Method: "POST",
		Manifest: doc, RedirectURI: redirect, ExpiresAt: exp}, nil
}

// StartAuthorization prepares a user authorization or an installation.
func (s *Service) StartAuthorization(ctx context.Context, in AuthorizeInput, actor Actor) (Started, error) {
	redirect, err := s.redirectURI()
	if err != nil {
		return Started{}, err
	}
	app, err := s.GetApp(ctx, in.AppID)
	if errors.Is(err, ErrNotFound) {
		return Started{}, invalid("appId", "no such App")
	}
	if err != nil {
		return Started{}, err
	}
	purpose := in.Mode
	if purpose == "" {
		purpose = PurposeAuthorize
	}
	if purpose != PurposeAuthorize && purpose != PurposeInstall {
		return Started{}, invalid("mode", "authorize or install")
	}
	if in.ConnectionID != "" {
		c, err := s.GetConnection(ctx, in.ConnectionID, actor)
		if errors.Is(err, ErrNotFound) {
			return Started{}, invalid("connectionId", "no such connection")
		}
		if err != nil {
			return Started{}, err
		}
		if c.Kind != KindAppUser || c.AppID != app.ID || purpose != PurposeAuthorize {
			return Started{}, invalid("connectionId", "only an App user connection of this App can be authorized again, with mode authorize")
		}
	}
	id, state, exp, err := s.newRequest(ctx, actor, purpose, app.ID, in.ConnectionID, cleanName(in.Name), redirect, map[string]any{})
	if err != nil {
		return Started{}, err
	}
	var target string
	if purpose == PurposeInstall {
		// The App asks for user authorization during installation
		// (request_oauth_on_install), so GitHub returns a code with the
		// installation id. That flow has no PKCE parameter.
		target = app.InstallURL + "?state=" + url.QueryEscape(state)
	} else {
		q := url.Values{"client_id": {app.ClientID}, "redirect_uri": {redirect}, "state": {state},
			"code_challenge": {pkceChallenge(s.pkceVerifier(id))}, "code_challenge_method": {"S256"}, "allow_signup": {"false"}}
		target = "https://github.com/login/oauth/authorize?" + q.Encode()
	}
	return Started{ID: id, Purpose: purpose, State: state, URL: target, Method: "GET", RedirectURI: redirect, ExpiresAt: exp}, nil
}

type authRequest struct {
	id, userID, sessionID, purpose, appRef, connID, name, redirect string
	expires                                                        time.Time
	finished                                                       bool
}

// claim finds the request for a state and marks it finished, once. Only
// the account and session that started it can claim it; for anyone else
// it does not exist.
func (s *Service) claim(ctx context.Context, state string, actor Actor, outcome string) (authRequest, error) {
	if state == "" {
		return authRequest{}, &AuthorizationError{Reason: "unknown"}
	}
	var r authRequest
	var app, conn, finished sql.NullString
	var exp string
	err := s.db.QueryRowContext(ctx, `SELECT id, user_id, session_id, purpose, app_ref, connection_id, name, redirect_uri, expires_at, finished_at
		FROM github_auth_requests WHERE state_hash=?`, hashState(state)).Scan(&r.id, &r.userID, &r.sessionID, &r.purpose, &app, &conn, &r.name, &r.redirect, &exp, &finished)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && r.userID != actor.UserID) {
		return authRequest{}, &AuthorizationError{Reason: "unknown"}
	}
	if err != nil {
		return authRequest{}, err
	}
	r.appRef, r.connID, r.expires, r.finished = app.String, conn.String, parseTS(exp), finished.Valid
	switch {
	case r.finished:
		return authRequest{}, &AuthorizationError{Reason: "used"}
	case r.sessionID != actor.SessionID:
		return authRequest{}, &AuthorizationError{Reason: "session-mismatch"}
	}
	if !s.now().Before(r.expires) {
		outcome = "expired"
	}
	res, err := s.db.ExecContext(ctx, `UPDATE github_auth_requests SET finished_at=?, outcome=? WHERE id=? AND finished_at IS NULL`, s.ts(), outcome, r.id)
	if err != nil {
		return authRequest{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return authRequest{}, &AuthorizationError{Reason: "used"}
	}
	if outcome == "expired" {
		return authRequest{}, &AuthorizationError{Reason: "expired"}
	}
	return r, nil
}

func (s *Service) setOutcome(ctx context.Context, id, outcome string) {
	if _, err := s.db.ExecContext(context.WithoutCancel(ctx), `UPDATE github_auth_requests SET outcome=? WHERE id=?`, outcome, id); err != nil {
		s.log.Error("github: authorization outcome could not be saved", "request", id, "error", err)
	}
}

// Cancel ends a request the user abandoned. Nothing else changes.
func (s *Service) Cancel(ctx context.Context, state string, actor Actor) error {
	_, err := s.claim(ctx, state, actor, "cancelled")
	return err
}

// Complete finishes a browser authorization. The code is exchanged and
// every id GitHub sent back is checked with GitHub before anything is
// saved; a refused callback changes nothing but the request, which cannot
// be used again (GIT-015).
func (s *Service) Complete(ctx context.Context, cb Callback, actor Actor) (Completed, error) {
	r, err := s.claim(ctx, cb.State, actor, "completed")
	if err != nil {
		return Completed{}, err
	}
	out, err := s.complete(ctx, r, cb, actor)
	if err != nil {
		s.setOutcome(ctx, r.id, "failed")
		return Completed{}, err
	}
	return out, nil
}

func (s *Service) complete(ctx context.Context, r authRequest, cb Callback, actor Actor) (Completed, error) {
	if cb.Error != "" {
		return Completed{}, &AuthorizationError{Reason: "cancelled-by-github"}
	}
	if cb.Code == "" {
		return Completed{}, invalid("code", "required")
	}
	if r.purpose == PurposeManifest {
		return s.completeManifest(ctx, r, cb, actor)
	}
	app, err := s.GetApp(ctx, r.appRef)
	if err != nil {
		return Completed{}, err
	}
	if r.purpose == PurposeAuthorize && cb.InstallationID != 0 {
		return Completed{}, &AuthorizationError{Reason: "installation-mismatch"}
	}
	if r.purpose == PurposeInstall && cb.InstallationID == 0 {
		return Completed{}, invalid("installationId", "GitHub sends installation_id after an installation")
	}
	clientSecret, err := s.reveal(ctx, app.ID, app.clientSecretSecret)
	if err != nil {
		return Completed{}, err
	}
	verifier := ""
	if r.purpose == PurposeAuthorize {
		verifier = s.pkceVerifier(r.id)
	}
	tok, err := s.client.ExchangeCode(ctx, app.ClientID, clientSecret, cb.Code, r.redirect, verifier)
	if err != nil {
		return Completed{}, err
	}
	user, _, err := s.client.GetUser(ctx, tok.AccessToken)
	if err != nil {
		return Completed{}, err
	}
	if r.connID != "" {
		c, err := s.reauthorize(ctx, r, app, user, tok, actor)
		if err != nil {
			return Completed{}, err
		}
		return Completed{Purpose: r.purpose, Connections: []Connection{c}}, nil
	}

	var inst *Installation
	if r.purpose == PurposeInstall {
		// The id in the callback is only a claim: the App must own the
		// installation, and the user who authorized must be able to see it.
		jwt, err := s.appSigner(ctx, app)
		if err != nil {
			return Completed{}, err
		}
		got, err := s.client.GetInstallation(ctx, jwt, cb.InstallationID)
		if errors.Is(err, ErrRejected) {
			return Completed{}, &AuthorizationError{Reason: "installation-mismatch"}
		}
		if err != nil {
			return Completed{}, err
		}
		if got.ID != cb.InstallationID || got.AppID != app.AppID {
			return Completed{}, &AuthorizationError{Reason: "installation-mismatch"}
		}
		mine, err := s.client.UserInstallations(ctx, tok.AccessToken)
		if err != nil {
			return Completed{}, err
		}
		seen := false
		for _, m := range mine {
			if m.ID == got.ID && m.AppID == app.AppID {
				seen = true
			}
		}
		if !seen {
			return Completed{}, &AuthorizationError{Reason: "installation-mismatch"}
		}
		inst = &got
	}

	userConn := Connection{ID: newID("ghc_"), Name: r.name, Kind: KindAppUser, AppID: app.ID, Account: user, TokenType: "app-user",
		Permissions: app.Permissions, Events: app.Events}
	if userConn.Name == "" {
		userConn.Name = user.Login
	}
	var created []string
	var tx *sql.Tx
	fail := func(err error) (Completed, error) {
		if tx != nil {
			tx.Rollback() // the secret store needs the connection the transaction holds
		}
		s.dropSecrets(ctx, actor, created...)
		return Completed{}, err
	}
	if err := s.storeUserToken(ctx, &userConn, tok, actor, &created); err != nil {
		return fail(err)
	}
	tx, err = s.db.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	if err := s.insertConnTx(ctx, tx, userConn, actor, "github.connection.create"); err != nil {
		return fail(err)
	}
	ids := []string{userConn.ID}
	if inst != nil {
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT id FROM github_connections WHERE owner_user_id=? AND app_ref=? AND installation_id=?`, actor.UserID, app.ID, inst.ID).Scan(&existing)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			ic := Connection{ID: newID("ghc_"), Name: inst.Account.Login + " (" + app.Name + ")", Kind: KindAppInstallation, AppID: app.ID, Account: inst.Account,
				InstallationID: inst.ID, RepositorySelection: inst.RepositorySelection, TokenType: "app-installation", Permissions: inst.Permissions, Events: inst.Events}
			if err := s.insertConnTx(ctx, tx, ic, actor, "github.connection.create"); err != nil {
				return fail(err)
			}
			ids = append(ids, ic.ID)
		case err != nil:
			return fail(err)
		default:
			// Installing again (e.g. to change repositories) refreshes what
			// GitHub granted on the existing connection.
			if _, err := tx.ExecContext(ctx, `UPDATE github_connections SET account_login=?, repository_selection=?, permissions=?, events=?, version=version+1, updated_at=? WHERE id=?`,
				inst.Account.Login, inst.RepositorySelection, jsonOf(nonNilMap(inst.Permissions)), jsonOf(nonNilSlice(inst.Events)), s.ts(), existing); err != nil {
				return fail(err)
			}
			if err := s.auditTx(ctx, tx, actor, "github.connection.refresh", "github-connection", existing, nil,
				map[string]any{"installationId": inst.ID, "permissions": inst.Permissions, "repositorySelection": inst.RepositorySelection}); err != nil {
				return fail(err)
			}
			ids = append(ids, existing)
		}
	}
	if err := tx.Commit(); err != nil {
		return fail(err)
	}
	out := Completed{Purpose: r.purpose}
	for _, id := range ids {
		c, err := s.GetConnection(ctx, id, actor)
		if err != nil {
			return Completed{}, err
		}
		out.Connections = append(out.Connections, c)
	}
	return out, nil
}

func (s *Service) storeUserToken(ctx context.Context, c *Connection, tok UserToken, actor Actor, created *[]string) error {
	var exp *time.Time // the stored value outlives the token: refreshing needs the row, not the value
	if !tok.ExpiresAt.IsZero() {
		e := tok.ExpiresAt.UTC().Truncate(time.Second)
		c.AccessExpiresAt = &e
	}
	id, err := s.storeSecret(ctx, c.ID, "access-token", secrets.KindAPIToken, tok.AccessToken, exp, actor)
	if err != nil {
		return err
	}
	*created = append(*created, id)
	c.accessSecret = id
	if tok.RefreshToken != "" {
		rid, err := s.storeSecret(ctx, c.ID, "refresh-token", secrets.KindOAuthRefresh, tok.RefreshToken, nil, actor)
		if err != nil {
			return err
		}
		*created = append(*created, rid)
		c.refreshSecret = rid
		if !tok.RefreshExpiresAt.IsZero() {
			e := tok.RefreshExpiresAt.UTC().Truncate(time.Second)
			c.RefreshExpiresAt = &e
		}
	}
	return nil
}

// reauthorize replaces the tokens of an App user connection. A different
// GitHub account is refused and the connection is left as it was.
func (s *Service) reauthorize(ctx context.Context, r authRequest, app App, user User, tok UserToken, actor Actor) (Connection, error) {
	c, err := s.GetConnection(ctx, r.connID, actor)
	if err != nil {
		return Connection{}, err
	}
	if c.AppID != app.ID || c.Kind != KindAppUser {
		return Connection{}, &AuthorizationError{Reason: "app-mismatch"}
	}
	if c.Account.ID != user.ID {
		return Connection{}, &AuthorizationError{Reason: "wrong-account"}
	}
	unlock := s.lockConn(c.ID)
	defer unlock()
	if err := s.writeUserToken(ctx, c, tok, actor); err != nil {
		return Connection{}, err
	}
	return s.mutate(ctx, c.ID, 0, actor, "github.connection.reauthorize", func(Connection) (map[string]any, string, []any, error) {
		return map[string]any{"accountLogin": user.Login},
			"account_login=?, state=CASE WHEN state='disabled' THEN 'disabled' ELSE 'active' END, state_reason=CASE WHEN state='disabled' THEN state_reason ELSE '' END, last_checked_at=?",
			[]any{user.Login, s.ts()}, nil
	})
}

// writeUserToken saves new user tokens over a connection's stored ones:
// the secret values first, then the expiry columns.
func (s *Service) writeUserToken(ctx context.Context, c Connection, tok UserToken, actor Actor) error {
	if _, err := s.vault.Rotate(ctx, c.accessSecret, []byte(tok.AccessToken), actor.UserID); err != nil {
		return fmt.Errorf("github: store access token: %w", err)
	}
	refresh := c.refreshSecret
	switch {
	case tok.RefreshToken != "" && refresh != "":
		if _, err := s.vault.Rotate(ctx, refresh, []byte(tok.RefreshToken), actor.UserID); err != nil {
			return fmt.Errorf("github: store refresh token: %w", err)
		}
	case tok.RefreshToken != "":
		id, err := s.storeSecret(ctx, c.ID, "refresh-token", secrets.KindOAuthRefresh, tok.RefreshToken, nil, actor)
		if err != nil {
			return err
		}
		refresh = id
	}
	var refreshCol any
	if refresh != "" {
		refreshCol = refresh
	}
	_, err := s.db.ExecContext(context.WithoutCancel(ctx), `UPDATE github_connections SET refresh_secret=?, access_expires_at=?, refresh_expires_at=COALESCE(?, refresh_expires_at),
		version=version+1, updated_at=? WHERE id=?`, refreshCol, tsOrNil(tok.ExpiresAt), tsOrNil(tok.RefreshExpiresAt), s.ts(), c.ID)
	return err
}

func (s *Service) completeManifest(ctx context.Context, r authRequest, cb Callback, actor Actor) (Completed, error) {
	if !actor.CanManageApps {
		return Completed{}, ErrForbidden
	}
	if cb.InstallationID != 0 {
		return Completed{}, &AuthorizationError{Reason: "installation-mismatch"}
	}
	got, err := s.client.ConvertManifest(ctx, cb.Code)
	if err != nil {
		return Completed{}, err
	}
	if got.ID <= 0 || got.ClientID == "" || got.ClientSecret == "" || got.PEM == "" {
		return Completed{}, fmt.Errorf("%w: manifest conversion answer is incomplete", ErrUnavailable)
	}
	if _, err := parsePrivateKey([]byte(got.PEM)); err != nil {
		return Completed{}, fmt.Errorf("%w: manifest conversion returned an unusable key", ErrUnavailable)
	}
	name := r.name
	if strings.TrimSpace(name) == "" {
		name = got.Name
	}
	app, err := s.insertApp(ctx, name, "manifest", got.AppInfo, got.ClientSecret, got.PEM, got.WebhookSecret, actor)
	if err != nil {
		return Completed{}, err
	}
	return Completed{Purpose: PurposeManifest, App: &app, Connections: []Connection{}}, nil
}
