// hazelminx-core is the HazelminX control plane: management API, permissions,
// job coordination and the embedded web interface.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/api"
	"github.com/HazelnutParadise/hazelminx/core/internal/config"
	"github.com/HazelnutParadise/hazelminx/core/internal/confirm"
	"github.com/HazelnutParadise/hazelminx/core/internal/engines"
	"github.com/HazelnutParadise/hazelminx/core/internal/github"
	"github.com/HazelnutParadise/hazelminx/core/internal/grants"
	"github.com/HazelnutParadise/hazelminx/core/internal/hostledger"
	"github.com/HazelnutParadise/hazelminx/core/internal/hostmetrics"
	"github.com/HazelnutParadise/hazelminx/core/internal/hoststorage"
	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodereg"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/plans"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
	"github.com/HazelnutParadise/hazelminx/core/web"
)

// IssuerPublicKeyFile is written to the data directory at every start: the
// public key the privileged executor on each host must trust.
const IssuerPublicKeyFile = "privileged-issuer.pub"

// LedgerSyncInterval is how often connected nodes' ledgers are copied
// again during a session (they are also copied when a session starts).
const LedgerSyncInterval = 5 * time.Minute

// EngineProbeInterval is how often connected nodes' container engines are
// read again (ticket 039.1); a refresh from the API reads them at once.
const EngineProbeInterval = 5 * time.Minute

// StorageUsageCheckInterval is how often Core looks for connected nodes
// whose storage usage measurement is due.
const StorageUsageCheckInterval = 5 * time.Minute

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

	// Agent channel (ticket 007): node PKI in the secrets store, the hub
	// agents dial, and the step runner job handlers use. Reconnecting agents
	// are reconciled before anything else reaches them.
	pki, err := nodes.LoadOrCreatePKI(ctx, sec, nil)
	if err != nil {
		return fmt.Errorf("refusing to start, node PKI: %w", err)
	}
	// Grants for the privileged executor (ticket 012). The installer and
	// node enrollment (tickets 013, 019) copy the public key to each host
	// as /etc/hazelminx/privileged/issuer.pub (enrollment hands it to the
	// agent, which saves it as identity/privileged-issuer.pub).
	issuer, err := grants.LoadOrCreate(ctx, sec, nil)
	if err != nil {
		return fmt.Errorf("refusing to start, grant issuer: %w", err)
	}
	issuerPub := filepath.Join(cfg.DataDir, IssuerPublicKeyFile)
	if err := os.WriteFile(issuerPub, issuer.PublicKeyPEM(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", issuerPub, err)
	}
	engine := jobs.New(jobs.NewSQLStore(st.DB), jobs.Options{FreeBytes: jobs.FreeBytesOf(cfg.DataDir)})
	// Node registry (ticket 019): which nodes may connect, enrollment on the
	// agent listener, certificate rotation, revocation and maintenance.
	registry, err := nodereg.New(nodereg.Options{DB: st.DB, PKI: pki, IssuerKeyPEM: issuer.PublicKeyPEM(), Jobs: engine,
		SubmitRecheck: func(ctx context.Context, fp, requestID string) (string, error) {
			j, _, err := engine.Submit(ctx, jobs.SubmitRequest{RequestID: requestID, Kind: nodes.KindNodeRecheck, Node: fp, Requester: "system:nodes"})
			if err != nil {
				return "", err
			}
			return j.ID, nil
		}})
	if err != nil {
		return err
	}
	var reconciler *nodes.Reconciler
	var ledgers *hostledger.Syncer
	var engineProber *engines.Prober
	// Host facts and metrics (ticket 020.1): agents report every 10 s;
	// raw points are kept 48 hours and 5-minute rollups 90 days.
	hostMetrics := hostmetrics.NewStore(st.DB, nil)
	go hostMetrics.RunPruner(ctx, time.Hour)
	// Storage inventory and usage (ticket 030.1).
	hostStorage := hoststorage.NewStore(st.DB, nil)
	hub := nodes.NewHub(nodes.HubOptions{PKI: pki, CoreVersion: api.Version,
		Authorizer: registry, OnClosed: registry.OnClosed, OnConflict: registry.OnConflict,
		HostReports:    hostmetrics.Reporter{Store: hostMetrics, NodeID: registry.NodeIDByFingerprint},
		StorageReports: hoststorage.Reporter{Store: hostStorage, NodeID: registry.NodeIDByFingerprint},
		OnReady: func(s *nodes.Session, inFlight []string) {
			go registry.OnReady(s)
			// Core's copy of the host change ledger (spec 55.1) catches up
			// as soon as a node connects.
			go func() { _, _ = ledgers.SyncNode(ctx, s.NodeID()) }()
			// Container engines (ticket 039.1) are read on every connect.
			go func() { _ = engineProber.Probe(ctx, s.NodeID()) }()
			reconciler.OnReady(s, inFlight)
		}})
	ledgers = &hostledger.Syncer{Store: hostledger.NewStore(st.DB, nil), Grants: issuer, Pull: hub}
	go ledgers.Run(ctx, func() []string {
		conns, _ := hub.Connections()
		ids := make([]string, len(conns))
		for i, c := range conns {
			ids[i] = c.NodeID
		}
		return ids
	}, LedgerSyncInterval)
	engineStore := engines.NewStore(st.DB, nil)
	engineProber = &engines.Prober{Store: engineStore, Grants: issuer, Hub: hub, NodeID: registry.NodeIDByFingerprint}
	go engineProber.Run(ctx, func() []string {
		conns, _ := hub.Connections()
		fps := make([]string, len(conns))
		for i, c := range conns {
			fps[i] = c.NodeID
		}
		return fps
	}, EngineProbeInterval)
	// Storage usage (ticket 030.1): each connected node is asked for a
	// measurement by the privileged executor when its latest is 6 hours old.
	usageProber := &hoststorage.Prober{Store: hostStorage, Grants: issuer, Hub: hub, NodeID: registry.NodeIDByFingerprint}
	go usageProber.Run(ctx, func() []string {
		conns, _ := hub.Connections()
		fps := make([]string, len(conns))
		for i, c := range conns {
			fps[i] = c.NodeID
		}
		return fps
	}, StorageUsageCheckInterval)
	registry.AttachChannel(hub)
	runner := nodes.NewStepRunner(hub)
	runner.Gate = registry.GateJob
	reconciler = &nodes.Reconciler{Engine: engine, Runner: runner}
	if err := engine.Register(&nodes.InspectHandler{Runner: runner}); err != nil {
		return err
	}
	if err := engine.Register(&nodes.RecheckHandler{Runner: runner, Record: registry.RecordRecheck}); err != nil {
		return err
	}

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
	router.RegisterAgentConnections(hub)
	router.RegisterNodes(registry)
	router.RegisterHostMetrics(registry, hostMetrics)
	router.RegisterHostStorage(registry, hostStorage, hostMetrics)
	router.RegisterHostEngines(registry, engineStore, engineProber)
	router.MountListQueries(listQueries)
	go listQueries.RunPurger(ctx, time.Hour)
	router.MountConfirmations(confirm.New(st.DB, nil))
	// Change plans (ticket 016.1). Resource tickets register their plan
	// kinds on this service; until then it has none.
	router.MountChangePlans(plans.New(plans.Options{DB: st.DB, Audit: identity, Jobs: engine}))
	// GitHub Apps and connections (ticket 047.1). Their secrets stay in
	// the secret store; only Core talks to GitHub.
	sources, err := github.New(github.Options{DB: st.DB, Vault: sec, Audit: identity, Client: github.NewHTTPClient(), PublicOrigin: cfg.PublicOrigin})
	if err != nil {
		return err
	}
	router.MountGitHub(sources)
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

	agentSrv := &http.Server{
		Handler:           registry.AgentHandler(hub),
		TLSConfig:         pki.ServerTLSConfig(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	agentLn, err := net.Listen("tcp", cfg.AgentListenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.AgentListenAddr, err)
	}
	slog.Info("agent channel listening", "addr", agentLn.Addr().String(), "path", "/agent/v1/connect", "enroll", "/agent/v1/enroll")

	errCh := make(chan error, 2)
	go func() { errCh <- srv.Serve(ln) }()
	go func() { errCh <- agentSrv.Serve(tls.NewListener(agentLn, agentSrv.TLSConfig)) }()

	shutdown := func() error {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		hub.Close()
		return errors.Join(agentSrv.Shutdown(shutdownCtx), srv.Shutdown(shutdownCtx))
	}
	select {
	case <-ctx.Done():
		slog.Info("shutting down")
		return shutdown()
	case err := <-errCh:
		_ = shutdown()
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
