//! hazelminx-privileged: root-side executor for the HazelminX agent.
//! `run` binds the socket and serves until SIGTERM; `check-config` prints
//! the effective configuration.

use anyhow::{Context, Result};
use clap::{Parser, Subcommand};
use ed25519_dalek::VerifyingKey;
use ed25519_dalek::pkcs8::DecodePublicKey;
use hazelminx_privileged::audit::TracingLog;
use hazelminx_privileged::command::HostRunner;
use hazelminx_privileged::config::{Config, DEFAULT_CONFIG_PATH, Overrides};
use hazelminx_privileged::executor::HostExecutor;
use hazelminx_privileged::server::{DEFAULT_REQUEST_READ_TIMEOUT, Server, ServerConfig};
use std::path::PathBuf;
use std::sync::Arc;
use tracing::info;

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
    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand, Debug)]
enum Command {
    /// Bind the socket and serve actions (what systemd starts).
    Run,
    /// Load and print the effective configuration, then exit.
    CheckConfig,
}

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
        },
    )?;
    init_tracing(&cfg.log_format);
    match cli.command {
        Command::CheckConfig => {
            println!("{cfg:#?}");
            Ok(())
        }
        Command::Run => run(cfg),
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

    let server = Server::new(
        ServerConfig {
            socket_path: cfg.socket_path.clone(),
            allowed_uid,
            issuer_key,
            max_concurrent_actions: cfg.max_concurrent_actions,
            request_read_timeout: DEFAULT_REQUEST_READ_TIMEOUT,
        },
        Arc::new(HostExecutor::new(Arc::new(HostRunner), "/")),
        Arc::new(TracingLog),
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
