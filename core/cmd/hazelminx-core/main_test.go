package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/api"
	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
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
	identity, err := iam.New(st.DB, iam.Config{PasswordParams: iam.TestPasswordParams})
	if err != nil {
		t.Fatal(err)
	}
	router := api.New(st, web.FS())
	router.MountIAM(identity, api.IAMOptions{})
	var stdout bytes.Buffer
	if err := announceSetup(ctx, identity, &stdout); err != nil {
		t.Fatal(err)
	}
	code, _, _ := identity.SetupCode(ctx)
	if code == "" || !strings.Contains(stdout.String(), code) {
		t.Fatalf("setup code must be announced on stdout: %q", stdout.String())
	}
	srv := &http.Server{Handler: router.Handler(), ReadHeaderTimeout: time.Second}
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

	// Ticket 009: nothing under /management/v1 answers without a session,
	// and the first-run setup endpoint is reachable.
	resp, err = http.Get("http://" + ln.Addr().String() + "/management/v1/users")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous management call: status %d", resp.StatusCode)
	}
	resp, err = http.Get("http://" + ln.Addr().String() + "/management/v1/setup")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var setup struct {
		NeedsSetup bool `json:"needsSetup"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&setup); err != nil || !setup.NeedsSetup {
		t.Fatalf("setup status: %v %+v", err, setup)
	}
}
