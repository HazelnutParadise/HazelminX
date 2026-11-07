//! Dispatch from a validated [`Action`] to its handler. `Executor` is the
//! seam the server tests replace (ENG.md: "動作處理器以 trait 替換").

use crate::BoxFuture;
use crate::action::Action;
use crate::command::{CommandRunner, OutputSink};
use crate::handlers;
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
    fn execute(&self, action: Action, sink: OutputSink) -> BoxFuture<'static, ActionResult>;
}

/// The real dispatcher. `sysroot` is `/` in production and a temp dir in
/// tests; handlers that read host files or probe for binaries join it.
pub struct HostExecutor {
    runner: Arc<dyn CommandRunner>,
    sysroot: PathBuf,
}

impl HostExecutor {
    pub fn new(runner: Arc<dyn CommandRunner>, sysroot: impl Into<PathBuf>) -> Self {
        Self {
            runner,
            sysroot: sysroot.into(),
        }
    }
}

impl Executor for HostExecutor {
    fn execute(&self, action: Action, sink: OutputSink) -> BoxFuture<'static, ActionResult> {
        let runner = self.runner.clone();
        let sysroot = self.sysroot.clone();
        Box::pin(async move {
            match action {
                Action::SystemInfo {} => handlers::system_info::run(&sysroot).await,
                Action::ListPackageUpdates {} => {
                    handlers::packages::run(runner, &sysroot, sink).await
                }
                Action::RestartService { unit } => {
                    handlers::systemd::restart(runner, sink, unit).await
                }
            }
        })
    }
}
