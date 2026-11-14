package github

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// refreshMargin: a token closer than this to its expiry is renewed before
// use. Short-lived tokens use half their remaining life instead, so a
// token's real lifetime decides, not a fixed length (GIT-016).
const refreshMargin = 5 * time.Minute

func (s *Service) lockConn(id string) func() {
	m, _ := s.refreshMu.LoadOrStore(id, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// fresh reports whether a token that expires at exp, issued at issued (zero
// when unknown), can still be used now.
func fresh(now, exp time.Time) bool {
	if exp.IsZero() {
		return true
	}
	return now.Add(refreshMargin).Before(exp)
}

// Credential is a token a later step (repository listing, fetching) uses
// on behalf of a connection. Value is opaque.
type Credential struct {
	Value     string
	ExpiresAt *time.Time
}

// Scope narrows an installation token to repositories and permissions.
type Scope struct {
	RepositoryIDs []int64
	Permissions   map[string]string
}

func (sc Scope) key() string {
	ids := append([]int64(nil), sc.RepositoryIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var b strings.Builder
	for _, id := range ids {
		b.WriteString(strconv.FormatInt(id, 10) + ",")
	}
	b.WriteString("|")
	keys := make([]string, 0, len(sc.Permissions))
	for k := range sc.Permissions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(k + "=" + sc.Permissions[k] + ",")
	}
	return b.String()
}

// Credential returns a usable token for a connection the actor owns:
//   - PAT: the stored token while GitHub's expiry is ahead; an expired PAT
//     marks the connection needs-reauth (it is never "renewed").
//   - App user: the stored token, renewed with the refresh token when it
//     is about to expire; a refused refresh marks needs-reauth, other
//     failures wait before the next try.
//   - App installation: a short-lived installation token minted for the
//     scope asked, cached in memory until close to its returned expiry.
func (s *Service) Credential(ctx context.Context, id string, scope Scope, actor Actor) (Credential, error) {
	c, err := s.GetConnection(ctx, id, actor)
	if err != nil {
		return Credential{}, err
	}
	switch c.State {
	case StateDisabled:
		return Credential{}, ErrDisabled
	case StateNeedsReauth:
		return Credential{}, ErrNeedsReauth
	}
	switch c.Kind {
	case KindPAT:
		if c.AccessExpiresAt != nil && !s.now().Before(*c.AccessExpiresAt) {
			s.setState(ctx, c.ID, StateNeedsReauth, "token expired")
			return Credential{}, ErrNeedsReauth
		}
		v, err := s.reveal(ctx, c.ID, c.accessSecret)
		if err != nil {
			return Credential{}, err
		}
		return Credential{Value: v, ExpiresAt: c.AccessExpiresAt}, nil
	case KindAppUser:
		return s.userCredential(ctx, c, actor)
	default:
		return s.installationCredential(ctx, c, scope)
	}
}

func (s *Service) userCredential(ctx context.Context, c Connection, actor Actor) (Credential, error) {
	unlock := s.lockConn(c.ID)
	defer unlock()
	// Read again under the lock: another caller may have refreshed.
	c, err := s.GetConnection(ctx, c.ID, actor)
	if err != nil {
		return Credential{}, err
	}
	if c.AccessExpiresAt == nil || fresh(s.now(), *c.AccessExpiresAt) {
		v, err := s.reveal(ctx, c.ID, c.accessSecret)
		if err != nil {
			return Credential{}, err
		}
		return Credential{Value: v, ExpiresAt: c.AccessExpiresAt}, nil
	}
	if c.refreshSecret == "" || (c.RefreshExpiresAt != nil && !s.now().Before(*c.RefreshExpiresAt)) {
		s.setState(ctx, c.ID, StateNeedsReauth, "authorization expired")
		return Credential{}, ErrNeedsReauth
	}
	if err := s.waitBackoff(c.ID); err != nil {
		return Credential{}, err
	}
	app, err := s.GetApp(ctx, c.AppID)
	if err != nil {
		return Credential{}, err
	}
	secret, err := s.reveal(ctx, app.ID, app.clientSecretSecret)
	if err != nil {
		return Credential{}, err
	}
	refresh, err := s.reveal(ctx, c.ID, c.refreshSecret)
	if err != nil {
		return Credential{}, err
	}
	tok, err := s.client.RefreshUserToken(ctx, app.ClientID, secret, refresh)
	switch {
	case errors.Is(err, ErrRejected):
		s.clearBackoff(c.ID)
		s.setState(ctx, c.ID, StateNeedsReauth, "GitHub refused the refresh: "+reasonOf(err))
		return Credential{}, ErrNeedsReauth
	case err != nil:
		s.noteFailure(c.ID)
		return Credential{}, err
	}
	s.clearBackoff(c.ID)
	if err := s.writeUserToken(ctx, c, tok, actor); err != nil {
		return Credential{}, err
	}
	var exp *time.Time
	if !tok.ExpiresAt.IsZero() {
		e := tok.ExpiresAt.UTC()
		exp = &e
	}
	return Credential{Value: tok.AccessToken, ExpiresAt: exp}, nil
}

func (s *Service) installationCredential(ctx context.Context, c Connection, scope Scope) (Credential, error) {
	key := c.ID + "#" + scope.key()
	s.tokMu.Lock()
	t, ok := s.installTokens[key]
	s.tokMu.Unlock()
	if ok && fresh(s.now(), t.ExpiresAt) {
		exp := t.ExpiresAt
		return Credential{Value: t.Value, ExpiresAt: &exp}, nil
	}
	app, err := s.GetApp(ctx, c.AppID)
	if err != nil {
		return Credential{}, err
	}
	jwt, err := s.appSigner(ctx, app)
	if err != nil {
		return Credential{}, err
	}
	t, err = s.client.CreateInstallationToken(ctx, jwt, c.InstallationID, InstallationTokenRequest{RepositoryIDs: scope.RepositoryIDs, Permissions: scope.Permissions})
	if errors.Is(err, ErrRejected) && (strings.Contains(strings.ToLower(reasonOf(err)), "suspend") || isStatus(err, 404)) {
		s.setState(ctx, c.ID, StateNeedsReauth, "installation removed or suspended on GitHub")
		return Credential{}, ErrNeedsReauth
	}
	if err != nil {
		return Credential{}, err
	}
	s.tokMu.Lock()
	s.installTokens[key] = t
	s.tokMu.Unlock()
	exp := t.ExpiresAt
	return Credential{Value: t.Value, ExpiresAt: &exp}, nil
}

func isStatus(err error, code int) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == code
}

func (s *Service) forgetTokens(connID string) {
	s.tokMu.Lock()
	defer s.tokMu.Unlock()
	for k := range s.installTokens {
		if strings.HasPrefix(k, connID+"#") {
			delete(s.installTokens, k)
		}
	}
}

// Refresh retries after an unexplained failure wait 30 s, 60 s, … up to 10
// minutes, so a GitHub outage is not hammered.
func (s *Service) waitBackoff(id string) error {
	s.backoffMu.Lock()
	defer s.backoffMu.Unlock()
	if b, ok := s.backoff[id]; ok && s.now().Before(b.until) {
		return ErrRefreshBackoff
	}
	return nil
}

func (s *Service) noteFailure(id string) {
	s.backoffMu.Lock()
	defer s.backoffMu.Unlock()
	b := s.backoff[id]
	b.failures++
	wait := 30 * time.Second << min(b.failures-1, 5)
	if wait > 10*time.Minute {
		wait = 10 * time.Minute
	}
	b.until = s.now().Add(wait)
	s.backoff[id] = b
}

func (s *Service) clearBackoff(id string) {
	s.backoffMu.Lock()
	defer s.backoffMu.Unlock()
	delete(s.backoff, id)
}

// Check asks GitHub about a connection now: who it is (the login may have
// changed; the account id must not), whether it still works, and records
// the state. A connection whose identity changed is marked needs-reauth.
func (s *Service) Check(ctx context.Context, id string, actor Actor) (Connection, error) {
	c, err := s.GetConnection(ctx, id, actor)
	if err != nil {
		return Connection{}, err
	}
	if c.State == StateDisabled {
		return c, nil
	}
	// A connection marked needs-reauth is checked again: a PAT renewed on
	// GitHub with the same value, or an installation unsuspended, recovers.
	if c.State == StateNeedsReauth && c.Kind != KindAppUser {
		s.setState(ctx, c.ID, StateActive, "")
	}
	cred, err := s.Credential(ctx, id, Scope{}, actor)
	if errors.Is(err, ErrNeedsReauth) {
		return s.GetConnection(ctx, id, actor)
	}
	if err != nil {
		return Connection{}, err
	}
	switch c.Kind {
	case KindAppInstallation:
		app, err := s.GetApp(ctx, c.AppID)
		if err != nil {
			return Connection{}, err
		}
		jwt, err := s.appSigner(ctx, app)
		if err != nil {
			return Connection{}, err
		}
		inst, err := s.client.GetInstallation(ctx, jwt, c.InstallationID)
		if errors.Is(err, ErrRejected) {
			s.setState(ctx, c.ID, StateNeedsReauth, "installation not found on GitHub")
			return s.GetConnection(ctx, id, actor)
		}
		if err != nil {
			return Connection{}, err
		}
		if inst.Account.ID != c.Account.ID {
			s.setState(ctx, c.ID, StateNeedsReauth, "installation now belongs to another account")
			return s.GetConnection(ctx, id, actor)
		}
		if inst.SuspendedAt != nil {
			s.setState(ctx, c.ID, StateNeedsReauth, "installation suspended on GitHub")
			return s.GetConnection(ctx, id, actor)
		}
		return s.recordCheck(ctx, c, inst.Account.Login, map[string]any{"permissions": nonNilMap(inst.Permissions)}, "permissions=?", jsonOf(nonNilMap(inst.Permissions)))
	default:
		user, facts, err := s.client.GetUser(ctx, cred.Value)
		if errors.Is(err, ErrRejected) && isStatus(err, 401) {
			s.setState(ctx, c.ID, StateNeedsReauth, "GitHub no longer accepts this credential")
			return s.GetConnection(ctx, id, actor)
		}
		if err != nil {
			return Connection{}, err
		}
		if user.ID != c.Account.ID {
			s.setState(ctx, c.ID, StateNeedsReauth, "credential now belongs to another GitHub account")
			return s.GetConnection(ctx, id, actor)
		}
		if c.Kind == KindPAT {
			return s.recordCheck(ctx, c, user.Login, map[string]any{"expiresAt": facts.ExpiresAt}, "access_expires_at=?", tsOrNil(ptrTime(facts.ExpiresAt)))
		}
		return s.recordCheck(ctx, c, user.Login, nil, "")
	}
}

func (s *Service) recordCheck(ctx context.Context, c Connection, login string, after map[string]any, set string, args ...any) (Connection, error) {
	if after == nil {
		after = map[string]any{}
	}
	after["accountLogin"] = login
	stmt := "account_login=?, state=CASE WHEN state='disabled' THEN 'disabled' ELSE 'active' END, state_reason=CASE WHEN state='disabled' THEN state_reason ELSE '' END, last_checked_at=?"
	all := []any{login, s.ts()}
	if set != "" {
		stmt += ", " + set
		all = append(all, args...)
	}
	return s.mutate(ctx, c.ID, 0, Actor{UserID: c.OwnerUserID}, "github.connection.check", func(Connection) (map[string]any, string, []any, error) {
		return after, stmt, all, nil
	})
}

// Reachability answers the two separate questions of GIT-031: can a
// browser come back to the panel after GitHub, and can GitHub deliver
// webhooks. Nothing is opened or published to make either work.
type Reachability struct {
	BrowserCallback CallbackCheck `json:"browserCallback"`
	Webhook         WebhookCheck  `json:"webhook"`
	// Polling works without either: periodic checks over read access.
	PollingAvailable bool `json:"pollingAvailable"`
}

// CallbackCheck: configured means GitHub is told to send the browser to
// CallbackURL. Only the browser can tell whether it reaches that address;
// RequestHostMatches says whether this request came through it.
type CallbackCheck struct {
	Status             string `json:"status"` // configured | not-configured
	CallbackURL        string `json:"callbackUrl,omitempty"`
	RequestHostMatches bool   `json:"requestHostMatches"`
	Message            string `json:"message"`
}

// WebhookCheck is what GitHub reports about the App's webhook.
type WebhookCheck struct {
	// Status: not-configured | no-deliveries | delivered | failing | unknown.
	Status       string        `json:"status"`
	URL          string        `json:"url,omitempty"`
	LastDelivery *HookDelivery `json:"lastDelivery,omitempty"`
	Message      string        `json:"message"`
}

// CheckReachability reports both checks for an App. requestHost is the
// Host of the asking request, compared only for the message.
func (s *Service) CheckReachability(ctx context.Context, appID, requestHost string) (Reachability, error) {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return Reachability{}, err
	}
	out := Reachability{PollingAvailable: true}
	if u, err := s.redirectURI(); err != nil {
		out.BrowserCallback = CallbackCheck{Status: "not-configured", Message: "no public origin is configured; browser authorization is unavailable, PAT connections and periodic checks still work"}
	} else {
		host := strings.TrimPrefix(strings.TrimPrefix(s.origin, "https://"), "http://")
		out.BrowserCallback = CallbackCheck{Status: "configured", CallbackURL: u, RequestHostMatches: strings.EqualFold(host, requestHost),
			Message: "GitHub sends the browser back to this address; it only has to be reachable from the browser, not from the internet"}
	}
	jwt, err := s.appSigner(ctx, app)
	if err != nil {
		return Reachability{}, err
	}
	cfg, err := s.client.GetAppHookConfig(ctx, jwt)
	if err != nil {
		out.Webhook = WebhookCheck{Status: "unknown", Message: fmt.Sprintf("GitHub did not answer about the webhook: %v", errors.Unwrap(err))}
		return out, nil
	}
	active := false
	for _, e := range app.Events {
		active = active || e == "push"
	}
	if cfg.URL == "" || !active {
		out.Webhook = WebhookCheck{Status: "not-configured", URL: cfg.URL, Message: "the App receives no push events; deployments use periodic checks"}
		return out, nil
	}
	out.Webhook.URL = cfg.URL
	last, err := s.client.LastAppHookDelivery(ctx, jwt)
	switch {
	case err != nil:
		out.Webhook.Status, out.Webhook.Message = "unknown", "GitHub did not list webhook deliveries"
	case last == nil:
		out.Webhook.Status, out.Webhook.Message = "no-deliveries", "GitHub has not delivered an event yet; reachability from GitHub is not confirmed"
	case last.StatusCode >= 200 && last.StatusCode < 300:
		out.Webhook.Status, out.Webhook.LastDelivery, out.Webhook.Message = "delivered", last, "GitHub's last delivery reached HazelminX"
	default:
		out.Webhook.Status, out.Webhook.LastDelivery, out.Webhook.Message = "failing", last, "GitHub's last delivery did not reach HazelminX; periodic checks still work"
	}
	return out, nil
}
