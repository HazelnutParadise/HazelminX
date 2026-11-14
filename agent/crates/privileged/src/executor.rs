//! Dispatch from a validated [`Action`] to its handler. `Executor` is the
//! seam the server tests replace (engineering/architecture.md: "動作處理器以 trait 替換").
//!
//! An executor must say, per action, what that action changes on the host
//! ([`Executor::declare`]). The server records declared changes in the host
//! ledger around `execute` and refuses actions with no declaration.

use crate::BoxFuture;
use crate::action::Action;
use crate::command::{CommandRunner, OutputSink};
use crate::handlers;
use crate::ledger::recorder::LedgerDecl;
use crate::ledger::store::{Ledger, MAX_EXPORT_LINES};
use std::path::PathBuf;
use std::sync::Arc;

/// A handler failure (non-zero exit, unsupported host, parse failure). The
/// message may include command stderr; handlers never put secrets in it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ActionError {
    pub message: String,
}

impl ActionError {
    pub fn new(message: impl Into<String>) -> Self {
        Self {
            message: message.into(),
        }
    }
}

impl std::fmt::Display for ActionError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.message)
    }
}

pub type ActionResult = Result<serde_json::Value, ActionError>;

pub trait Executor: Send + Sync + 'static {
    /// The host changes `action` makes, or `None` when this executor has no
    /// ledger description for it (the server then refuses the action).
    fn declare(&self, action: &Action) -> Option<LedgerDecl>;
    fn execute(&self, action: Action, sink: OutputSink) -> BoxFuture<'static, ActionResult>;
}

/// The real dispatcher. `sysroot` is `/` in production and a temp dir in
/// tests; handlers that read host files or probe for binaries join it.
pub struct HostExecutor {
    runner: Arc<dyn CommandRunner>,
    sysroot: PathBuf,
    /// For `ledger_export`; `None` when the ledger could not be opened.
    ledger: Option<Arc<Ledger>>,
    compose: handlers::compose::ComposeConfig,
}

impl HostExecutor {
    pub fn new(
        runner: Arc<dyn CommandRunner>,
        sysroot: impl Into<PathBuf>,
        ledger: Option<Arc<Ledger>>,
    ) -> Self {
        Self {
            runner,
            sysroot: sysroot.into(),
            ledger,
            compose: handlers::compose::ComposeConfig {
                spool_dir: PathBuf::from(crate::config::DEFAULT_COMPOSE_SPOOL_DIR),
                agent_uid: None,
            },
        }
    }

    /// Where `compose_*` actions read the agent's spool and whose it is.
    pub fn with_compose(mut self, compose: handlers::compose::ComposeConfig) -> Self {
        self.compose = compose;
        self
    }
}

impl Executor for HostExecutor {
    fn declare(&self, action: &Action) -> Option<LedgerDecl> {
        Some(action.ledger_decl())
    }

    fn execute(&self, action: Action, sink: OutputSink) -> BoxFuture<'static, ActionResult> {
        let runner = self.runner.clone();
        let sysroot = self.sysroot.clone();
        let ledger = self.ledger.clone();
        let compose = self.compose.clone();
        Box::pin(async move {
            match action {
                Action::SystemInfo {} => handlers::system_info::run(&sysroot).await,
                Action::ListPackageUpdates {} => {
                    handlers::packages::run(runner, &sysroot, sink).await
                }
                Action::RestartService { unit } => {
                    handlers::systemd::restart(runner, sink, unit).await
                }
                Action::EngineInspect {} => handlers::engines::run(runner, &sysroot).await,
                Action::StorageUsage {} => handlers::storage_usage::run(&sysroot).await,
                Action::ComposeCheck { manifest } => {
                    handlers::compose::check_only(runner, &sysroot, &compose, manifest).await
                }
                Action::ComposeDeploy { manifest } => {
                    handlers::compose::deploy(runner, &sysroot, &compose, manifest, sink).await
                }
                Action::LedgerExport {
                    after_seq,
                    limit,
                    max_bytes,
                } => {
                    let ledger =
                        ledger.ok_or_else(|| ActionError::new("host ledger is unavailable"))?;
                    let limit = limit.map_or(MAX_EXPORT_LINES, |l| l as usize);
                    let batch = tokio::task::spawn_blocking(move || {
                        ledger.export(after_seq, limit, max_bytes.map(|b| b as usize))
                    })
                    .await
                    .map_err(|e| ActionError::new(format!("ledger task: {e}")))?
                    .map_err(|e| ActionError::new(e.to_string()))?;
                    Ok(serde_json::to_value(batch).expect("LedgerBatch serializes"))
                }
            }
        })
    }
}
