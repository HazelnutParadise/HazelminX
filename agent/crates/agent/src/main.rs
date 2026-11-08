//! hazelminx-agent: the per-host executor. Runs as a low-privilege systemd
//! service, keeps the node identity, dials the Core over TLS (mutual
//! certificates plus a signed handshake) and runs authorized job steps,
//! journaling each one before reporting it.

mod actions;
mod config;
mod connection;
mod identity;
mod journal;
mod steps;

use anyhow::Result;
use clap::{Parser, Subcommand};
use config::{Config, DEFAULT_CONFIG_PATH, Overrides};
use identity::Identity;
use std::path::PathBuf;
use tracing::{info, warn};

#[derive(Parser, Debug)]
#[command(name = "hazelminx-agent", version, about = "HazelminX host agent")]
struct Cli {
    /// Configuration file (TOML).
    #[arg(long, env = "HAZELMINX_AGENT_CONFIG", default_value = DEFAULT_CONFIG_PATH)]
    config: PathBuf,
    /// State directory override.
    #[arg(long, env = "HAZELMINX_AGENT_STATE_DIR")]
    state_dir: Option<PathBuf>,
    /// Core URL override.
    #[arg(long, env = "HAZELMINX_AGENT_CORE_URL")]
    core_url: Option<String>,
    /// Log format override: json or text.
    #[arg(long, env = "HAZELMINX_AGENT_LOG_FORMAT")]
    log_format: Option<String>,
    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand, Debug)]
enum Command {
    /// Run the agent service (what systemd starts).
    Run,
    /// Print the node identity fingerprint and public key location.
    Identity,
    /// Load and print the effective configuration, then exit.
    CheckConfig,
}

fn main() {
    if let Err(err) = real_main() {
        eprintln!("hazelminx-agent: {err:#}");
        std::process::exit(1);
    }
}

fn real_main() -> Result<()> {
    let cli = Cli::parse();
    let cfg = Config::load(
        &cli.config,
        Overrides {
            state_dir: cli.state_dir,
            core_url: cli.core_url,
            node_name: None,
            log_format: cli.log_format,
        },
    )?;
    init_tracing(&cfg.log_format);

    match cli.command {
        Command::CheckConfig => {
            println!("{cfg:#?}");
            Ok(())
        }
        Command::Identity => {
            let id = Identity::load_or_create(&cfg.state_dir)?;
            println!("fingerprint: {}", id.fingerprint());
            println!(
                "public key:  {}",
                id.dir().join(identity::PUBLIC_KEY_FILE).display()
            );
            Ok(())
        }
        Command::Run => run(cfg),
    }
}

#[tokio::main]
async fn run(cfg: Config) -> Result<()> {
    let id = Identity::load_or_create(&cfg.state_dir)?;
    info!(fingerprint = %id.fingerprint(), state_dir = %cfg.state_dir.display(), "agent identity ready");

    let Some(url) = cfg.core_url.clone() else {
        info!("no core_url configured; waiting for enrollment (ticket 019)");
        shutdown_signal().await;
        info!("shutting down");
        return Ok(());
    };

    // The journal is opened before connecting: steps that were running when
    // the agent stopped are reported as interrupted, never as done, and are
    // not re-run until the Core reconciles them (ENG-003).
    let journal = journal::Journal::open(&cfg.state_dir.join("jobs"))?;
    let interrupted = journal.interrupted_steps();
    if !interrupted.is_empty() {
        warn!(count = interrupted.len(), steps = ?interrupted, "steps were interrupted by the last stop; they will be reconciled with the core, not re-run");
    }
    let steps = steps::Steps::new(
        journal,
        id.fingerprint(),
        actions::specs(),
        actions::capabilities(),
    );
    let opts = connection::Options {
        core_url: url,
        transport: cfg.core_transport,
        proxy_ca_file: cfg.proxy_ca_file.clone(),
        identity_dir: id.dir().to_path_buf(),
        agent_version: env!("CARGO_PKG_VERSION").to_string(),
    };
    let shared = connection::Shared::new(steps, id, opts.agent_version.clone());
    connection::run(opts, shared, shutdown_signal()).await;
    info!("shutting down");
    Ok(())
}

#[cfg(unix)]
async fn shutdown_signal() {
    use tokio::signal::unix::{SignalKind, signal};
    let mut term = signal(SignalKind::terminate()).expect("install SIGTERM handler");
    let mut int = signal(SignalKind::interrupt()).expect("install SIGINT handler");
    tokio::select! {
        _ = term.recv() => {},
        _ = int.recv() => {},
    }
}

#[cfg(not(unix))]
async fn shutdown_signal() {
    let _ = tokio::signal::ctrl_c().await;
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
