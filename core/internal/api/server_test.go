package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

type fakeHealth struct{ err error }

func (f fakeHealth) Ping(context.Context) error { return f.err }

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHealthzOK(t *testing.T) {
	rec := get(t, New(fakeHealth{}, nil).Handler(), "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body %v", body)
	}
}

func TestHealthzDegradedWhenStoreFails(t *testing.T) {
	rec := get(t, New(fakeHealth{err: errors.New("locked")}, nil).Handler(), "/healthz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestOverviewListsOperations(t *testing.T) {
	rec := get(t, New(fakeHealth{}, nil).Handler(), "/management/v1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body struct {
		Operations []struct{ Path string } `json:"operations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Operations) < 2 {
		t.Fatalf("overview must list operations, got %+v", body.Operations)
	}
}

func TestUIMissingIsExplicit(t *testing.T) {
	rec := get(t, New(fakeHealth{}, nil).Handler(), "/")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestUISPAFallback(t *testing.T) {
	ui := fstest.MapFS{
		"index.html":  {Data: []byte("<html>app</html>")},
		"assets/a.js": {Data: []byte("js")},
	}
	h := New(fakeHealth{}, ui).Handler()
	if rec := get(t, h, "/assets/a.js"); rec.Code != http.StatusOK || rec.Body.String() != "js" {
		t.Fatalf("asset: %d %q", rec.Code, rec.Body)
	}
	if rec := get(t, h, "/nodes/abc"); rec.Code != http.StatusOK || rec.Body.String() != "<html>app</html>" {
		t.Fatalf("spa fallback: %d %q", rec.Code, rec.Body)
	}
	if rec := get(t, h, "/management/v1/nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("api 404 must not fall back to index.html: %d", rec.Code)
	}
}
