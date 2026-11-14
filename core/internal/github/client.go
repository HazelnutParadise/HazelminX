package github

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Client is every call Core makes to GitHub. The service only talks to
// GitHub through it, so tests replace it and never reach the real site.
// Tokens and keys passed in are opaque: nothing here infers validity from
// their length or prefix (GIT-016).
type Client interface {
	// ConvertManifest exchanges the code GitHub returns after the App
	// manifest flow for the new App's credentials (POST /app-manifests/{code}/conversions).
	ConvertManifest(ctx context.Context, code string) (ManifestApp, error)
	// GetApp reads the App the JWT was signed for (GET /app).
	GetApp(ctx context.Context, appJWT string) (AppInfo, error)
	// GetInstallation reads one installation of the App (GET /app/installations/{id}).
	GetInstallation(ctx context.Context, appJWT string, installationID int64) (Installation, error)
	// CreateInstallationToken mints a short-lived installation token,
	// narrowed to the given repositories and permissions when set
	// (POST /app/installations/{id}/access_tokens).
	CreateInstallationToken(ctx context.Context, appJWT string, installationID int64, req InstallationTokenRequest) (Token, error)
	// GetAppHookConfig reads the App's webhook address (GET /app/hook/config).
	GetAppHookConfig(ctx context.Context, appJWT string) (HookConfig, error)
	// LastAppHookDelivery reads the newest webhook delivery, if any
	// (GET /app/hook/deliveries?per_page=1).
	LastAppHookDelivery(ctx context.Context, appJWT string) (*HookDelivery, error)
	// ExchangeCode turns a user authorization code into a user access token
	// (POST https://github.com/login/oauth/access_token). verifier is the
	// PKCE code verifier, empty when the flow did not send a challenge.
	ExchangeCode(ctx context.Context, clientID, clientSecret, code, redirectURI, verifier string) (UserToken, error)
	// RefreshUserToken uses a refresh token (grant_type=refresh_token).
	RefreshUserToken(ctx context.Context, clientID, clientSecret, refreshToken string) (UserToken, error)
	// GetUser reads the identity behind a token (GET /user), with the
	// headers GitHub sends about the token itself.
	GetUser(ctx context.Context, token string) (User, TokenFacts, error)
	// UserInstallations lists the installations of this App the user can
	// access with a user access token (GET /user/installations, every page).
	UserInstallations(ctx context.Context, userToken string) ([]Installation, error)
	// CanListRepositories reports whether the token can list at least the
	// repositories endpoint (GET /user/repos?per_page=1).
	CanListRepositories(ctx context.Context, token string) (bool, error)
}

// Account is a GitHub user or organization. ID is GitHub's stable id; Login
// is only its current name.
type Account struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Type  string `json:"type"` // User | Organization | Bot
}

// User is the identity behind a token.
type User = Account

// TokenFacts are what GitHub says about a token in response headers.
type TokenFacts struct {
	// Scopes is X-OAuth-Scopes; nil when GitHub sent no such header
	// (fine-grained PATs and App tokens).
	Scopes []string
	// ExpiresAt is github-authentication-token-expiration; nil when absent.
	ExpiresAt *time.Time
}

// AppInfo is the public description of a GitHub App.
type AppInfo struct {
	ID          int64             `json:"id"`
	Slug        string            `json:"slug"`
	ClientID    string            `json:"client_id"`
	Name        string            `json:"name"`
	Owner       Account           `json:"owner"`
	HTMLURL     string            `json:"html_url"`
	Permissions map[string]string `json:"permissions"`
	Events      []string          `json:"events"`
}

// ManifestApp is the answer of the manifest conversion: the App plus its
// secrets, which GitHub shows only this once.
type ManifestApp struct {
	AppInfo
	ClientSecret  string `json:"client_secret"`
	WebhookSecret string `json:"webhook_secret"`
	PEM           string `json:"pem"`
}

// Installation is one account's installation of an App.
type Installation struct {
	ID                  int64             `json:"id"`
	AppID               int64             `json:"app_id"`
	Account             Account           `json:"account"`
	RepositorySelection string            `json:"repository_selection"`
	Permissions         map[string]string `json:"permissions"`
	Events              []string          `json:"events"`
	SuspendedAt         *time.Time        `json:"suspended_at"`
}

// InstallationTokenRequest narrows an installation token.
type InstallationTokenRequest struct {
	RepositoryIDs []int64           `json:"repository_ids,omitempty"`
	Permissions   map[string]string `json:"permissions,omitempty"`
}

// Token is a short-lived credential with the expiry GitHub returned.
type Token struct {
	Value     string
	ExpiresAt time.Time
}

// UserToken is a user access token, with a refresh token when the App has
// expiring user tokens. Zero times mean GitHub sent no expiry.
type UserToken struct {
	AccessToken      string
	ExpiresAt        time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
}

// HookConfig is the App webhook configuration.
type HookConfig struct {
	URL string `json:"url"`
}

// HookDelivery is one webhook delivery attempt.
type HookDelivery struct {
	DeliveredAt time.Time `json:"delivered_at"`
	StatusCode  int       `json:"status_code"`
	Event       string    `json:"event"`
}

// Error kinds a Client reports. Messages never contain the token or code.
var (
	// ErrRejected: GitHub refused the credential, code or request (401,
	// 403 without rate limit, 404, 422, or an OAuth error answer).
	ErrRejected = errors.New("github: request refused by GitHub")
	// ErrUnavailable: GitHub could not be reached, answered 5xx or rate
	// limited the request.
	ErrUnavailable = errors.New("github: GitHub is not reachable right now")
)

// APIError carries what GitHub answered. It wraps ErrRejected or
// ErrUnavailable.
type APIError struct {
	Kind   error
	Status int
	// Reason is GitHub's OAuth error code (bad_verification_code,
	// bad_refresh_token, …) or a short description; never a secret.
	Reason string
	// RetryAfter is set for rate limits.
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	s := e.Kind.Error()
	if e.Status != 0 {
		s += fmt.Sprintf(" (HTTP %d)", e.Status)
	}
	if e.Reason != "" {
		s += ": " + e.Reason
	}
	return s
}

func (e *APIError) Unwrap() error { return e.Kind }

// reasonOf returns the Reason of an APIError, or "".
func reasonOf(err error) string {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Reason
	}
	return ""
}
