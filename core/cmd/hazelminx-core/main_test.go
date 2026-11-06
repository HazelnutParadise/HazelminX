package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/api"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
	"github.com/HazelnutParadise/hazelminx/core/web"
)

// TestEndToEndHealth is the acceptance flow for ticket 003: start the real
// store and router on a free port, hit /healthz, and get a healthy answer with
// the schema version recorded.
func TestEndToEndHealth(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: api.New(st, web.FS()).Handler(), ReadHeaderTimeout: time.Second}
	go srv.Serve(ln)
	defer srv.Close()

	resp, err := http.Get("http://" + ln.Addr().String() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ok" || body.Checks["store"] != "ok" {
		t.Fatalf("unexpected health %+v", body)
	}
}
