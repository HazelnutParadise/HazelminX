package github

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The HTTP client is checked against a local server playing GitHub.
func localGitHub(t *testing.T, h http.HandlerFunc) *HTTPClient {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := NewHTTPClient()
	c.APIBase, c.WebBase = srv.URL, srv.URL
	return c
}

func TestHTTPClientReadsTokenFacts(t *testing.T) {
	c := localGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("X-GitHub-Api-Version") == "" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("X-OAuth-Scopes", "repo, read:org")
		w.Header().Set("GitHub-Authentication-Token-Expiration", "2026-12-01 10:00:00 UTC")
		w.Write([]byte(`{"id":5,"login":"octo","type":"User"}`))
	})
	u, f, err := c.GetUser(ctx, "tok")
	if err != nil || u.ID != 5 || len(f.Scopes) != 2 || f.Scopes[1] != "read:org" || f.ExpiresAt == nil || f.ExpiresAt.Hour() != 10 {
		t.Fatalf("%+v %+v %v", u, f, err)
	}
	if _, _, err := c.GetUser(ctx, "bad"); !errors.Is(err, ErrRejected) || strings.Contains(err.Error(), "bad") {
		t.Fatalf("refusal: %v", err)
	}
}

func TestHTTPClientMapsRateLimitAndOAuthErrors(t *testing.T) {
	c := localGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("Retry-After", "42")
			w.WriteHeader(403)
		case "/login/oauth/access_token":
			r.ParseForm()
			if r.Form.Get("code_verifier") != "v" {
				w.Write([]byte(`{"error":"bad_verification_code"}`))
				return
			}
			w.Write([]byte(`{"access_token":"ghu_x","expires_in":28800,"refresh_token":"ghr_x","refresh_token_expires_in":15897600}`))
		case "/redirect":
			http.Redirect(w, r, "https://elsewhere.example/steal", http.StatusFound)
		}
	})
	_, _, err := c.GetUser(ctx, "t")
	var ae *APIError
	if !errors.As(err, &ae) || !errors.Is(err, ErrUnavailable) || ae.RetryAfter != 42*time.Second {
		t.Fatalf("rate limit: %v", err)
	}
	if _, err := c.ExchangeCode(ctx, "id", "sec", "code", "https://p/cb", "wrong"); !errors.Is(err, ErrRejected) || reasonOf(err) != "bad_verification_code" {
		t.Fatalf("oauth error with 200: %v", err)
	}
	tok, err := c.ExchangeCode(ctx, "id", "sec", "code", "https://p/cb", "v")
	if err != nil || tok.AccessToken != "ghu_x" || tok.ExpiresAt.IsZero() || tok.RefreshExpiresAt.Sub(tok.ExpiresAt) < 24*time.Hour {
		t.Fatalf("exchange: %+v %v", tok, err)
	}
	// Redirects are not followed, so a credential never goes elsewhere.
	if _, err := c.call(ctx, http.MethodGet, c.APIBase+"/redirect", "Bearer secret", nil, nil, nil); !errors.Is(err, ErrRejected) {
		t.Fatalf("redirect must not be followed: %v", err)
	}
}

func TestHTTPClientInstallationToken(t *testing.T) {
	c := localGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/app/installations/7/access_tokens" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(201)
		w.Write([]byte(`{"token":"any-format-token","expires_at":"2026-11-14T09:00:00Z"}`))
	})
	tok, err := c.CreateInstallationToken(ctx, "jwt", 7, InstallationTokenRequest{RepositoryIDs: []int64{1}})
	if err != nil || tok.Value != "any-format-token" || tok.ExpiresAt.Hour() != 9 {
		t.Fatalf("%+v %v", tok, err)
	}
}
