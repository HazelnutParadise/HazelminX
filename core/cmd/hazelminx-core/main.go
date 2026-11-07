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
	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
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

	st, err := store.Open(ctx, cfg.DatabasePath())
	if err != nil {
		if errors.Is(err, store.ErrNewerSchema) {
			// REL-006 / T-009: never open or recreate newer data; leave it untouched.
			return fmt.Errorf("refusing to start: %w (restore a matching backup or upgrade hazelminx-core)", err)
		}
		return err
	}
	defer st.Close()
	v, _ := st.SchemaVersion(ctx)
	slog.Info("store ready", "path", st.Path(), "schemaVersion", v)

	identity, err := iam.New(st.DB, iam.Config{})
	if err != nil {
		return err
	}
	router := api.New(st, web.FS())
	router.MountIAM(identity, api.IAMOptions{})
	if err := announceSetup(ctx, identity, os.Stdout); err != nil {
		return err
	}

	srv := &http.Server{
		Handler:           router.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
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

func setupLogging(format string) {
	var h slog.Handler
	if format == "text" {
		h = slog.NewTextHandler(os.Stderr, nil)
	} else {
		h = slog.NewJSONHandler(os.Stderr, nil)
	}
	slog.SetDefault(slog.New(h))
}
