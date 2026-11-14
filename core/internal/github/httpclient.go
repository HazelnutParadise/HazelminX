package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// HTTPClient is the Client that talks to GitHub over HTTPS.
type HTTPClient struct {
	// APIBase is https://api.github.com; WebBase is https://github.com.
	// Tests point them at a local server.
	APIBase, WebBase string
	HTTP             *http.Client
	UserAgent        string
}

// NewHTTPClient returns a client for github.com. It does not follow
// redirects, so a credential is never sent to another address.
func NewHTTPClient() *HTTPClient {
	return &HTTPClient{
		APIBase: "https://api.github.com",
		WebBase: "https://github.com",
		HTTP: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		UserAgent: "HazelminX",
	}
}

const maxBody = 1 << 20

// call sends one request. auth is the Authorization header value ("" for
// none). It decodes a 2xx JSON answer into out (when not nil) and maps every
// other answer to an *APIError.
func (c *HTTPClient) call(ctx context.Context, method, rawURL, auth string, body any, form url.Values, out any) (http.Header, error) {
	h, _, err := c.callH(ctx, method, rawURL, auth, body, form, nil, out)
	return h, err
}

// callH is call with extra request headers. A 304 answer is not an error:
// it returns status 304 and leaves out untouched.
func (c *HTTPClient) callH(ctx context.Context, method, rawURL, auth string, body any, form url.Values, extra http.Header, out any) (http.Header, int, error) {
	var rd io.Reader
	ctype := ""
	switch {
	case form != nil:
		rd, ctype = strings.NewReader(form.Encode()), "application/x-www-form-urlencoded"
	case body != nil:
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rd, ctype = bytes.NewReader(b), "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rd)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range extra {
		req.Header[k] = v
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.UserAgent)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// The URL may hold a manifest code; report only the method and host.
		return nil, 0, &APIError{Kind: ErrUnavailable, Reason: fmt.Sprintf("%s %s: request failed", method, req.URL.Host)}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, resp.StatusCode, &APIError{Kind: ErrUnavailable, Status: resp.StatusCode, Reason: "reading the answer failed"}
	}
	if len(data) > maxBody {
		return resp.Header, resp.StatusCode, &APIError{Kind: ErrUnavailable, Status: resp.StatusCode, Reason: "answer larger than 1 MiB; not read"}
	}
	if resp.StatusCode == http.StatusNotModified && extra.Get("If-None-Match") != "" {
		return resp.Header, resp.StatusCode, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.Header, resp.StatusCode, statusError(resp, data)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.Header, resp.StatusCode, &APIError{Kind: ErrUnavailable, Status: resp.StatusCode, Reason: "answer is not the expected JSON"}
		}
	}
	return resp.Header, resp.StatusCode, nil
}

func statusError(resp *http.Response, data []byte) error {
	var msg struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(data, &msg)
	e := &APIError{Kind: ErrRejected, Status: resp.StatusCode, Reason: truncate(msg.Message)}
	limited := resp.StatusCode == http.StatusTooManyRequests ||
		(resp.StatusCode == http.StatusForbidden && (resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != ""))
	switch {
	case limited:
		e.Kind, e.Reason = ErrUnavailable, "rate limited"
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			e.RetryAfter = time.Duration(s) * time.Second
		} else if r, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			e.RetryAfter = time.Until(time.Unix(r, 0))
		}
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout:
		e.Kind = ErrUnavailable
	}
	return e
}

func truncate(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

func bearer(token string) string { return "Bearer " + token }

func (c *HTTPClient) ConvertManifest(ctx context.Context, code string) (ManifestApp, error) {
	var out ManifestApp
	_, err := c.call(ctx, http.MethodPost, c.APIBase+"/app-manifests/"+url.PathEscape(code)+"/conversions", "", nil, nil, &out)
	return out, err
}

func (c *HTTPClient) GetApp(ctx context.Context, appJWT string) (AppInfo, error) {
	var out AppInfo
	_, err := c.call(ctx, http.MethodGet, c.APIBase+"/app", bearer(appJWT), nil, nil, &out)
	return out, err
}

func (c *HTTPClient) GetInstallation(ctx context.Context, appJWT string, id int64) (Installation, error) {
	var out Installation
	_, err := c.call(ctx, http.MethodGet, fmt.Sprintf("%s/app/installations/%d", c.APIBase, id), bearer(appJWT), nil, nil, &out)
	return out, err
}

func (c *HTTPClient) CreateInstallationToken(ctx context.Context, appJWT string, id int64, req InstallationTokenRequest) (Token, error) {
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	_, err := c.call(ctx, http.MethodPost, fmt.Sprintf("%s/app/installations/%d/access_tokens", c.APIBase, id), bearer(appJWT), req, nil, &out)
	if err != nil {
		return Token{}, err
	}
	if out.Token == "" || out.ExpiresAt.IsZero() {
		return Token{}, &APIError{Kind: ErrUnavailable, Reason: "installation token answer has no token or expiry"}
	}
	return Token{Value: out.Token, ExpiresAt: out.ExpiresAt}, nil
}

func (c *HTTPClient) GetAppHookConfig(ctx context.Context, appJWT string) (HookConfig, error) {
	var out HookConfig
	_, err := c.call(ctx, http.MethodGet, c.APIBase+"/app/hook/config", bearer(appJWT), nil, nil, &out)
	return out, err
}

func (c *HTTPClient) LastAppHookDelivery(ctx context.Context, appJWT string) (*HookDelivery, error) {
	var out []HookDelivery
	if _, err := c.call(ctx, http.MethodGet, c.APIBase+"/app/hook/deliveries?per_page=1", bearer(appJWT), nil, nil, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return &out[0], nil
}

// oauthAnswer is the token endpoint's JSON. GitHub answers OAuth errors with
// 200 and an error field.
type oauthAnswer struct {
	AccessToken           string `json:"access_token"`
	ExpiresIn             int64  `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
	Error                 string `json:"error"`
}

func (c *HTTPClient) token(ctx context.Context, form url.Values) (UserToken, error) {
	var out oauthAnswer
	if _, err := c.call(ctx, http.MethodPost, c.WebBase+"/login/oauth/access_token", "", nil, form, &out); err != nil {
		return UserToken{}, err
	}
	if out.Error != "" || out.AccessToken == "" {
		reason := out.Error
		if reason == "" {
			reason = "no access token in the answer"
		}
		return UserToken{}, &APIError{Kind: ErrRejected, Reason: truncate(reason)}
	}
	now := time.Now().UTC()
	t := UserToken{AccessToken: out.AccessToken, RefreshToken: out.RefreshToken}
	if out.ExpiresIn > 0 {
		t.ExpiresAt = now.Add(time.Duration(out.ExpiresIn) * time.Second)
	}
	if out.RefreshTokenExpiresIn > 0 {
		t.RefreshExpiresAt = now.Add(time.Duration(out.RefreshTokenExpiresIn) * time.Second)
	}
	return t, nil
}

func (c *HTTPClient) ExchangeCode(ctx context.Context, clientID, clientSecret, code, redirectURI, verifier string) (UserToken, error) {
	f := url.Values{"client_id": {clientID}, "client_secret": {clientSecret}, "code": {code}, "redirect_uri": {redirectURI}}
	if verifier != "" {
		f.Set("code_verifier", verifier)
	}
	return c.token(ctx, f)
}

func (c *HTTPClient) RefreshUserToken(ctx context.Context, clientID, clientSecret, refreshToken string) (UserToken, error) {
	return c.token(ctx, url.Values{"client_id": {clientID}, "client_secret": {clientSecret}, "grant_type": {"refresh_token"}, "refresh_token": {refreshToken}})
}

func (c *HTTPClient) GetUser(ctx context.Context, token string) (User, TokenFacts, error) {
	var out User
	h, err := c.call(ctx, http.MethodGet, c.APIBase+"/user", bearer(token), nil, nil, &out)
	if err != nil {
		return User{}, TokenFacts{}, err
	}
	var f TokenFacts
	if v, ok := h["X-Oauth-Scopes"]; ok {
		f.Scopes = []string{}
		for _, s := range strings.Split(strings.Join(v, ","), ",") {
			if s = strings.TrimSpace(s); s != "" {
				f.Scopes = append(f.Scopes, s)
			}
		}
	}
	if v := h.Get("Github-Authentication-Token-Expiration"); v != "" {
		for _, layout := range []string{"2006-01-02 15:04:05 MST", "2006-01-02 15:04:05 -0700", time.RFC3339} {
			if t, err := time.Parse(layout, v); err == nil {
				t = t.UTC()
				f.ExpiresAt = &t
				break
			}
		}
	}
	return out, f, nil
}

func (c *HTTPClient) UserInstallations(ctx context.Context, userToken string) ([]Installation, error) {
	var all []Installation
	for page := 1; page <= 50; page++ {
		var out struct {
			TotalCount    int            `json:"total_count"`
			Installations []Installation `json:"installations"`
		}
		u := fmt.Sprintf("%s/user/installations?per_page=100&page=%d", c.APIBase, page)
		if _, err := c.call(ctx, http.MethodGet, u, bearer(userToken), nil, nil, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Installations...)
		if len(out.Installations) < 100 || len(all) >= out.TotalCount {
			return all, nil
		}
	}
	return all, nil
}

func (c *HTTPClient) UserInstallationRepositories(ctx context.Context, userToken string, id int64) ([]int64, error) {
	var ids []int64
	for page := 1; page <= 50; page++ {
		var out struct {
			TotalCount   int `json:"total_count"`
			Repositories []struct {
				ID int64 `json:"id"`
			} `json:"repositories"`
		}
		u := fmt.Sprintf("%s/user/installations/%d/repositories?per_page=100&page=%d", c.APIBase, id, page)
		if _, err := c.call(ctx, http.MethodGet, u, bearer(userToken), nil, nil, &out); err != nil {
			return nil, err
		}
		for _, r := range out.Repositories {
			ids = append(ids, r.ID)
		}
		if len(out.Repositories) < 100 || len(ids) >= out.TotalCount {
			return ids, nil
		}
	}
	return nil, &APIError{Kind: ErrUnavailable, Reason: "more than 5000 repositories; list not complete"}
}

func (c *HTTPClient) CanListRepositories(ctx context.Context, token string) (bool, error) {
	_, err := c.call(ctx, http.MethodGet, c.APIBase+"/user/repos?per_page=1", bearer(token), nil, nil, nil)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrRejected) {
		return false, nil
	}
	return false, err
}
