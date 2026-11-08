//! hazelminx-privileged: root-side executor for the HazelminX agent.
//! `run` binds the socket and serves until SIGTERM; `check-config` prints
//! the effective configuration; `snapshot` and `ledger` take and compare the
//! pre-install snapshot and check the host change ledger (spec 55.1).

use anyhow::{Context, Result};
use clap::{Parser, Subcommand};
use ed25519_dalek::VerifyingKey;
use ed25519_dalek::pkcs8::DecodePublicKey;
use hazelminx_privileged::audit::TracingLog;
use hazelminx_privileged::command::HostRunner;
use hazelminx_privileged::config::{Config, DEFAULT_CONFIG_PATH, Overrides};
use hazelminx_privileged::executor::HostExecutor;
use hazelminx_privileged::ledger::drift;
use hazelminx_privileged::ledger::probe::HostProbe;
use hazelminx_privileged::ledger::recorder::Recorder;
use hazelminx_privileged::ledger::snapshot::{self, DEFAULT_PLATFORM_PATHS, SNAPSHOT_FILE};
use hazelminx_privileged::ledger::store::{self, Ledger, Security};
use hazelminx_privileged::server::{DEFAULT_REQUEST_READ_TIMEOUT, Server, ServerConfig};
use std::path::{Path, PathBuf};
use std::sync::Arc;
use tracing::{error, info, warn};

#[derive(Parser, Debug)]
#[command(
    name = "hazelminx-privileged",
    version,
    about = "HazelminX privileged executor"
)]
struct Cli {
    /// Configuration file (TOML).
    #[arg(long, env = "HAZELMINX_PRIVILEGED_CONFIG", default_value = DEFAULT_CONFIG_PATH)]
    config: PathBuf,
    /// Unix socket path override.
    #[arg(long, env = "HAZELMINX_PRIVILEGED_SOCKET")]
    socket: Option<PathBuf>,
    /// Issuer public key (PEM) override.
    #[arg(long, env = "HAZELMINX_PRIVILEGED_ISSUER_KEY")]
    issuer_key: Option<PathBuf>,
    /// uid of the only account allowed to connect.
    #[arg(long, env = "HAZELMINX_PRIVILEGED_ALLOWED_UID")]
    allowed_uid: Option<u32>,
    /// Log format override: json or text.
    #[arg(long, env = "HAZELMINX_PRIVILEGED_LOG_FORMAT")]
    log_format: Option<String>,
    /// Host change ledger directory override (root-only).
    #[arg(long, env = "HAZELMINX_PRIVILEGED_LEDGER_DIR")]
    ledger_dir: Option<PathBuf>,
    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand, Debug)]
enum Command {
    /// Bind the socket and serve actions (what systemd starts).
    Run,
    /// Load and print the effective configuration, then exit.
    CheckConfig,
    /// Pre-install snapshot of the host (spec 55.1).
    #[command(subcommand)]
    Snapshot(SnapshotCommand),
    /// Host change ledger checks.
    #[command(subcommand)]
    Ledger(LedgerCommand),
}

#[derive(Subcommand, Debug)]
enum SnapshotCommand {
    /// Record the host before the platform writes anything. Stores
    /// `<ledger_dir>/preinstall-snapshot.json` once; refuses to replace it.
    Capture {
        /// Extra platform path whose presence to record (repeatable); the
        /// built-in list is always included.
        #[arg(long = "path")]
        paths: Vec<PathBuf>,
        /// Print the snapshot instead of storing it.
        #[arg(long)]
        print: bool,
    },
    /// Compare a stored snapshot with the host now (or with another
    /// snapshot). Prints JSON; exits 0 when identical, 3 when not.
    Compare {
        /// Default: `<ledger_dir>/preinstall-snapshot.json`.
        #[arg(long)]
        before: Option<PathBuf>,
        /// Default: capture the host now.
        #[arg(long)]
        after: Option<PathBuf>,
        /// Extra platform path for the live capture (repeatable).
        #[arg(long = "path")]
        paths: Vec<PathBuf>,
    },
}

#[derive(Subcommand, Debug)]
enum LedgerCommand {
    /// Verify ownership, permissions and the hash chain. Prints the head.
    Verify,
    /// List platform-written files the user changed afterwards.
    Drift,
}

/// Exit status of `snapshot compare` when the host differs.
const EXIT_DIFFERENT: i32 = 3;

fn main() {
    if let Err(err) = real_main() {
        eprintln!("hazelminx-privileged: {err:#}");
        std::process::exit(1);
    }
}

fn real_main() -> Result<()> {
    let cli = Cli::parse();
    let cfg = Config::load(
        &cli.config,
        Overrides {
            socket_path: cli.socket,
            issuer_public_key: cli.issuer_key,
            allowed_uid: cli.allowed_uid,
            log_format: cli.log_format,
            ledger_dir: cli.ledger_dir,
        },
    )?;
    init_tracing(&cfg.log_format);
    match cli.command {
        Command::CheckConfig => {
            println!("{cfg:#?}");
            Ok(())
        }
        Command::Run => run(cfg),
        Command::Snapshot(cmd) => snapshot_command(&cfg, cmd),
        Command::Ledger(cmd) => ledger_command(&cfg, cmd),
    }
}

fn platform_paths(extra: Vec<PathBuf>) -> Vec<PathBuf> {
    DEFAULT_PLATFORM_PATHS
        .iter()
        .map(PathBuf::from)
        .chain(extra)
        .collect()
}

fn print_json(v: &impl serde::Serialize) {
    println!(
        "{}",
        serde_json::to_string_pretty(v).expect("output serializes")
    );
}

#[tokio::main]
async fn snapshot_command(cfg: &Config, cmd: SnapshotCommand) -> Result<()> {
    let runner = Arc::new(HostRunner);
    match cmd {
        SnapshotCommand::Capture { paths, print } => {
            let snap = snapshot::capture(runner, Path::new("/"), &platform_paths(paths)).await;
            if print {
                print_json(&snap);
                return Ok(());
            }
            // Directories are created only after the host was read, so the
            // snapshot shows them as absent on a fresh host.
            if let Some(parent) = cfg.ledger_dir.parent() {
                use std::os::unix::fs::DirBuilderExt;
                std::fs::DirBuilder::new()
                    .recursive(true)
                    .mode(0o755)
                    .create(parent)
                    .with_context(|| format!("create {}", parent.display()))?;
            }
            store::ensure_dir(&cfg.ledger_dir, &Security::root())?;
            let path = snapshot::store(&cfg.ledger_dir, &snap).map_err(anyhow::Error::msg)?;
            eprintln!("stored {}", path.display());
            Ok(())
        }
        SnapshotCommand::Compare {
            before,
            after,
            paths,
        } => {
            let before = before.unwrap_or_else(|| cfg.ledger_dir.join(SNAPSHOT_FILE));
            let before = snapshot::load(&before).map_err(anyhow::Error::msg)?;
            let after = match after {
                Some(p) => snapshot::load(&p).map_err(anyhow::Error::msg)?,
                None => snapshot::capture(runner, Path::new("/"), &platform_paths(paths)).await,
            };
            let cmp = snapshot::compare(&before, &after);
            print_json(&cmp);
            if !cmp.identical {
                std::process::exit(EXIT_DIFFERENT);
            }
            Ok(())
        }
    }
}

#[tokio::main]
async fn ledger_command(cfg: &Config, cmd: LedgerCommand) -> Result<()> {
    let chain = store::verify(&cfg.ledger_dir, &Security::root())?;
    match cmd {
        LedgerCommand::Verify => {
            print_json(&serde_json::json!({
                "ledger_id": chain.meta.ledger_id,
                "entries": chain.entries.len(),
                "head": chain.head,
                "torn_tail_bytes": chain.torn_tail,
            }));
            Ok(())
        }
        LedgerCommand::Drift => {
            let probe = HostProbe::new(Arc::new(HostRunner), "/", None);
            let report = drift::detect(&store::known_from(&chain.entries), &probe).await;
            print_json(&report);
            Ok(())
        }
    }
}

#[tokio::main]
async fn run(cfg: Config) -> Result<()> {
    let allowed_uid = cfg.allowed_uid.context(
        "allowed_uid is not set (config, --allowed-uid or HAZELMINX_PRIVILEGED_ALLOWED_UID)",
    )?;
    let pem = std::fs::read_to_string(&cfg.issuer_public_key)
        .with_context(|| format!("read issuer key {}", cfg.issuer_public_key.display()))?;
    let issuer_key = VerifyingKey::from_public_key_pem(&pem).with_context(|| {
        format!(
            "parse issuer key {} (SPKI PEM Ed25519)",
            cfg.issuer_public_key.display()
        )
    })?;

    let runner = Arc::new(HostRunner);
    let ledger = match Ledger::open(&cfg.ledger_dir, &Security::root()) {
        Ok((ledger, report)) => {
            if report.torn_tail_cut > 0 {
                warn!(
                    bytes = report.torn_tail_cut,
                    "host ledger ended in a torn write; cut it off"
                );
            }
            info!(
                dir = %cfg.ledger_dir.display(),
                entries = report.entries,
                created = report.created,
                "host ledger open"
            );
            Ok(Arc::new(ledger))
        }
        Err(e) => {
            error!(
                dir = %cfg.ledger_dir.display(),
                error = %e,
                "host ledger unavailable; host-changing actions will be refused"
            );
            Err(e.to_string())
        }
    };
    let probe = HostProbe::new(
        runner.clone(),
        "/",
        ledger.as_ref().ok().map(|l| l.backup_dir()),
    );
    let executor = HostExecutor::new(runner, "/", ledger.as_ref().ok().cloned());
    let recorder = Recorder::new(ledger, Arc::new(probe));
    let server = Server::new(
        ServerConfig {
            socket_path: cfg.socket_path.clone(),
            allowed_uid,
            issuer_key,
            max_concurrent_actions: cfg.max_concurrent_actions,
            request_read_timeout: DEFAULT_REQUEST_READ_TIMEOUT,
        },
        Arc::new(executor),
        Arc::new(TracingLog),
        Arc::new(recorder),
    );
    let listener = server.bind()?;
    info!(socket = %cfg.socket_path.display(), allowed_uid, "privileged executor listening");
    server.serve(listener, shutdown_signal()).await;
    info!("shutting down");
    Ok(())
}

async fn shutdown_signal() {
    use tokio::signal::unix::{SignalKind, signal};
    let mut term = signal(SignalKind::terminate()).expect("install SIGTERM handler");
    let mut int = signal(SignalKind::interrupt()).expect("install SIGINT handler");
    tokio::select! {
        _ = term.recv() => {},
        _ = int.recv() => {},
    }
}

fn init_tracing(format: &str) {
    let filter = tracing_subscriber::EnvFilter::try_from_default_env()
        .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info"));
    if format == "text" {
        tracing_subscriber::fmt().with_env_filter(filter).init();
    } else {
        tracing_subscriber::fmt()
            .json()
            .with_env_filter(filter)
            .init();
    }
}
