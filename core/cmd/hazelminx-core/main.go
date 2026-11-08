// hazelminx-core is the HazelminX control plane: management API, permissions,
// job coordination and the embedded web interface.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/api"
	"github.com/HazelnutParadise/hazelminx/core/internal/config"
	"github.com/HazelnutParadise/hazelminx/core/internal/confirm"
	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
	"github.com/HazelnutParadise/hazelminx/core/web"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("hazelminx-core exited", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load(args, config.EnvGetter())
	if err != nil {
		return err
	}
	setupLogging(cfg.LogFormat)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, kr, sec, err := openPlatformData(ctx, cfg)
	if err != nil {
		return err
	}
	defer st.Close()
	defer kr.Close()
	v, _ := st.SchemaVersion(ctx)
	slog.Info("store ready", "path", st.Path(), "schemaVersion", v)
	slog.Info("secrets ready", "keyring", kr.Path(), "activeKey", sec.ActiveKeyID())

	// Job engine (ticket 008). Handlers for host-changing kinds arrive with
	// the Agent channel (tickets 004/007); until then no kind is registered.
	engine := jobs.New(jobs.NewSQLStore(st.DB), jobs.Options{FreeBytes: jobs.FreeBytesOf(cfg.DataDir)})
	identity, err := iam.New(st.DB, iam.Config{Secrets: sec})
	if err != nil {
		return err
	}
	// Background list queries are a Core-side job kind; handlers register
	// before Recover so an interrupted query is reconciled, not orphaned.
	listQueries := api.NewListQueries(st.DB, identity, engine)
	if err := engine.Register(listQueries); err != nil {
		return err
	}
	rec, err := engine.Recover(ctx)
	if err != nil {
		return fmt.Errorf("recover jobs: %w", err)
	}
	slog.Info("job engine recovered", "markedUnknown", len(rec.MarkedUnknown), "locksRebuilt", rec.LocksRebuilt)
	engine.Start(ctx)
	defer engine.Stop()

	router := api.New(st, web.FS())
	router.MountIAM(identity, api.IAMOptions{PublicOrigin: cfg.PublicOrigin})
	// Jobs mount on the authenticated group, so MountIAM must run first.
	router.RegisterJobs(engine)
	router.MountListQueries(listQueries)
	go listQueries.RunPurger(ctx, time.Hour)
	router.MountConfirmations(confirm.New(st.DB, nil))
	if err := announceSetup(ctx, identity, os.Stdout); err != nil {
		return err
	}

	srv := &http.Server{
		Handler:           router.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// Records when each connection opened, for ROB-010 confirmations.
		ConnContext: api.ConnContext,
	}
	ln, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.ListenAddr, err)
	}
	slog.Info("management API listening", "addr", ln.Addr().String(), "version", api.Version)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// announceSetup prints the one-time owner setup code to the installer's
// stdout when no owner exists yet (39.1, INS-005). It deliberately bypasses
// slog so the code never lands in shared log files.
func announceSetup(ctx context.Context, identity *iam.Service, out io.Writer) error {
	code, expires, err := identity.SetupCode(ctx)
	if err != nil {
		return fmt.Errorf("setup code: %w", err)
	}
	if code == "" {
		return nil
	}
	_, err = fmt.Fprintf(out, "HazelminX first-run setup\n  POST /management/v1/setup with {\"setupCode\": \"%s\", \"username\": ..., \"password\": ...}\n  code expires at %s\n", code, expires.UTC().Format(time.RFC3339))
	return err
}

// openPlatformData opens the database and the master key file, in that order
// of trust: an existing key file proves this directory was installed, so a
// missing or damaged database is a recovery situation and never replaced by a
// blank one (DAT-002, T-012). Secrets are checked against the key file before
// anything is served, so a key generation lost since the last backup is found
// now rather than at restore time (DAT-004).
func openPlatformData(ctx context.Context, cfg config.Config) (*store.Store, *secrets.Keyring, *secrets.Service, error) {
	dbPath, keyPath := cfg.DatabasePath(), cfg.MasterKeyPath()
	var st *store.Store
	var err error
	if secrets.KeyringExists(keyPath) {
		st, err = store.OpenExisting(ctx, dbPath)
	} else {
		st, err = store.Open(ctx, dbPath)
	}
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNewerSchema):
			// REL-006 / T-009: never open or recreate newer data; leave it untouched.
			return nil, nil, nil, fmt.Errorf("refusing to start: %w (restore a matching backup or upgrade hazelminx-core)", err)
		case errors.Is(err, store.ErrCorrupt), errors.Is(err, store.ErrMissing):
			return nil, nil, nil, fmt.Errorf("refusing to start, recovery needed: %w (no blank database was created; restore %s from a backup or use the rescue tool)", err, dbPath)
		}
		return nil, nil, nil, err
	}
	kr, err := secrets.LoadOrCreateKeyring(keyPath)
	if err != nil {
		st.Close()
		return nil, nil, nil, fmt.Errorf("refusing to start: %w", err)
	}
	sec, err := secrets.Open(ctx, st, kr)
	if err != nil {
		st.Close()
		kr.Close()
		return nil, nil, nil, fmt.Errorf("refusing to start, recovery needed: %w", err)
	}
	return st, kr, sec, nil
}

func setupLogging(format string) {
	var h slog.Handler
	if format == "text" {
		h = slog.NewTextHandler(os.Stderr, nil)
	} else {
		h = slog.NewJSONHandler(os.Stderr, nil)
	}
	slog.SetDefault(slog.New(h))
}
