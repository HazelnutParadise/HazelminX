package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/api"
	"github.com/HazelnutParadise/hazelminx/core/internal/config"
	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
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
	engine := jobs.New(jobs.NewSQLStore(st.DB), jobs.Options{})
	if _, err := engine.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	engine.Start(ctx)
	defer engine.Stop()
	router := api.New(st, web.FS())
	router.MountIAM(identity, api.IAMOptions{})
	router.RegisterJobs(engine)
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

	// Tickets 009/008: nothing under /management/v1, jobs included, answers without a session,
	// and the first-run setup endpoint is reachable.
	for _, path := range []string{"/management/v1/users", "/management/v1/jobs"} {
		resp, err = http.Get("http://" + ln.Addr().String() + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous GET %s: status %d", path, resp.StatusCode)
		}
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

// TestPlatformDataRecoveryStates is the acceptance flow for ticket 011
// (DAT-002, T-012): a fresh directory installs; a directory whose master key
// file exists but whose database is gone, or damaged, refuses to start and
// leaves the directory as it was.
func TestPlatformDataRecoveryStates(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.Defaults
	cfg.DataDir = dir

	st, kr, sec, err := openPlatformData(ctx, cfg)
	if err != nil {
		t.Fatalf("fresh install: %v", err)
	}
	if sec.ActiveKeyID() != kr.ActiveKeyID() {
		t.Fatal("service and keyring disagree on the active generation")
	}
	st.Close()
	kr.Close()
	if !secrets.KeyringExists(cfg.MasterKeyPath()) {
		t.Fatal("first start must create the master key file")
	}

	// Second start on the same directory is a normal start.
	st, kr, _, err = openPlatformData(ctx, cfg)
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	st.Close()
	kr.Close()

	// Database gone, key file present: recovery, not a fresh install.
	if err := os.Rename(cfg.DatabasePath(), cfg.DatabasePath()+".lost"); err != nil {
		t.Fatal(err)
	}
	_, _, _, err = openPlatformData(ctx, cfg)
	if !errors.Is(err, store.ErrMissing) {
		t.Fatalf("missing database: %v", err)
	}
	if _, statErr := os.Stat(cfg.DatabasePath()); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("a blank database was created over a lost one")
	}
	if err := os.Rename(cfg.DatabasePath()+".lost", cfg.DatabasePath()); err != nil {
		t.Fatal(err)
	}

	// Database damaged: recovery, file untouched.
	if err := os.WriteFile(cfg.DatabasePath(), []byte("this is not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, _, err = openPlatformData(ctx, cfg)
	if !errors.Is(err, store.ErrCorrupt) {
		t.Fatalf("damaged database: %v", err)
	}
	data, _ := os.ReadFile(cfg.DatabasePath())
	if string(data) != "this is not a database" {
		t.Fatal("damaged database was rewritten")
	}
}
