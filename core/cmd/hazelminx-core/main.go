// hazelminx-core is the HazelminX control plane: management API, permissions,
// job coordination and the embedded web interface.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/api"
	"github.com/HazelnutParadise/hazelminx/core/internal/config"
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

	srv := &http.Server{
		Handler:           api.New(st, web.FS()).Handler(),
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

func setupLogging(format string) {
	var h slog.Handler
	if format == "text" {
		h = slog.NewTextHandler(os.Stderr, nil)
	} else {
		h = slog.NewJSONHandler(os.Stderr, nil)
	}
	slog.SetDefault(slog.New(h))
}
