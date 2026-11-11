//! hazelminx-agent: the per-host executor. Runs as a low-privilege systemd
//! service, keeps the node identity, and (from ticket 007 on) dials the Core
//! over mTLS to receive authorized jobs.

mod config;
mod identity;

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

    match &cfg.core_url {
        None => info!("no core_url configured; waiting for enrollment (ticket 019)"),
        Some(url) => {
            warn!(core_url = %url, "core connection is not implemented yet (ticket 007); staying idle")
        }
    }

    shutdown_signal().await;
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
