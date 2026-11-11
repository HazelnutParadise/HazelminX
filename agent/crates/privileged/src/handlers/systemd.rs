//! `restart_service`: `systemctl restart <unit>` then `systemctl is-active
//! <unit>`. The unit name is a validated [`UnitName`] and is one argv entry.

use crate::command::{CommandRunner, CommandSpec, OutputSink};
use crate::executor::{ActionError, ActionResult};
use crate::params::UnitName;
use serde::Serialize;
use std::sync::Arc;

pub const SYSTEMCTL: &str = "/usr/bin/systemctl";

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct RestartResult {
    pub unit: String,
    /// Output of `systemctl is-active`, e.g. `active`, `activating`, `failed`.
    pub active_state: String,
}

pub async fn restart(
    runner: Arc<dyn CommandRunner>,
    sink: OutputSink,
    unit: UnitName,
) -> ActionResult {
    let spec = CommandSpec::new(SYSTEMCTL)
        .arg("restart")
        .arg("--")
        .arg(unit.as_str());
    let out = runner
        .run(spec, sink.clone())
        .await
        .map_err(|e| ActionError::new(format!("spawn systemctl: {e}")))?;
    if !out.success() {
        return Err(ActionError::new(format!(
            "systemctl restart exited with {:?}: {}",
            out.exit_code,
            out.stderr_lossy().trim()
        )));
    }
    let spec = CommandSpec::new(SYSTEMCTL)
        .arg("is-active")
        .arg("--")
        .arg(unit.as_str());
    let state = runner
        .run(spec, sink)
        .await
        .map_err(|e| ActionError::new(format!("spawn systemctl: {e}")))?;
    let result = RestartResult {
        unit: unit.as_str().to_string(),
        active_state: state.stdout_lossy().trim().to_string(),
    };
    Ok(serde_json::to_value(result).expect("RestartResult serializes"))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::command::{RecordingRunner, output};
    use std::ffi::OsString;

    fn unit(s: &str) -> UnitName {
        UnitName::try_from(s.to_string()).unwrap()
    }

    #[tokio::test]
    async fn restart_then_is_active_as_separate_argv() {
        let runner = Arc::new(RecordingRunner::with_replies([
            output(0, ""),
            output(0, "active\n"),
        ]));
        let v = restart(
            runner.clone(),
            OutputSink::discard(),
            unit("getty@tty1.service"),
        )
        .await
        .unwrap();
        assert_eq!(v["unit"], "getty@tty1.service");
        assert_eq!(v["active_state"], "active");
        let calls = runner.calls();
        assert_eq!(calls.len(), 2);
        assert_eq!(calls[0].program.to_str().unwrap(), SYSTEMCTL);
        assert_eq!(
            calls[0].args,
            ["restart", "--", "getty@tty1.service"].map(OsString::from)
        );
        assert_eq!(
            calls[1].args,
            ["is-active", "--", "getty@tty1.service"].map(OsString::from)
        );
        assert!(calls.iter().all(|c| c.env.is_empty()));
    }

    #[tokio::test]
    async fn failed_restart_reports_stderr_and_skips_is_active() {
        let mut failed = output(1, "");
        failed.stderr =
            b"Job for nginx.service failed because the control process exited with error code.\n"
                .to_vec();
        let runner = Arc::new(RecordingRunner::with_replies([failed]));
        let err = restart(runner.clone(), OutputSink::discard(), unit("nginx.service"))
            .await
            .unwrap_err();
        assert!(
            err.message.contains("control process exited"),
            "{}",
            err.message
        );
        assert_eq!(runner.calls().len(), 1);
    }
}
