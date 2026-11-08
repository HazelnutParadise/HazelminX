//! "使用者後來改過" (55.1): a file the platform wrote whose current state no
//! longer matches the last state the ledger recorded. Complete removal (037)
//! lists these for the user to decide instead of restoring or deleting them.

use super::entry::{Ownership, PathState, Target, TargetState};
use super::probe::{Capture, Probe};
use super::store::Known;
use serde::Serialize;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum Difference {
    /// Existed when recorded and is gone, or the reverse.
    Existence,
    Kind,
    Content,
    LinkTarget,
    Mode,
    Owner,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct Drift {
    pub target: Target,
    pub ownership: Ownership,
    /// The ledger line whose after-state no longer holds.
    pub recorded_seq: u64,
    pub job_id: Option<String>,
    pub recorded: PathState,
    pub current: PathState,
    pub differences: Vec<Difference>,
}

#[derive(Debug, Clone, PartialEq, Eq, Default, Serialize)]
pub struct DriftReport {
    /// Paths compared.
    pub checked: usize,
    pub modified: Vec<Drift>,
    /// Paths whose current state could not be read, with the reason.
    pub unreadable: Vec<(Target, String)>,
}

pub fn differences(recorded: &PathState, current: &PathState) -> Vec<Difference> {
    let mut d = Vec::new();
    if recorded.exists != current.exists {
        d.push(Difference::Existence);
        return d;
    }
    if recorded.kind != current.kind {
        d.push(Difference::Kind);
        return d;
    }
    if recorded.sha256 != current.sha256 {
        d.push(Difference::Content);
    }
    if recorded.link_target != current.link_target {
        d.push(Difference::LinkTarget);
    }
    if recorded.mode != current.mode {
        d.push(Difference::Mode);
    }
    if recorded.uid != current.uid || recorded.gid != current.gid {
        d.push(Difference::Owner);
    }
    d
}

/// Compares every path the platform installed or modified with its last
/// recorded after-state. Paths the platform only read (`Existing`) are not
/// the platform's to report.
pub async fn detect(known: &[(Target, Known)], probe: &dyn Probe) -> DriftReport {
    let mut report = DriftReport::default();
    for (target, k) in known {
        if !matches!(target, Target::Path { .. }) || k.ownership == Ownership::Existing {
            continue;
        }
        let Some(last) = &k.last_result else {
            continue;
        };
        let Some(TargetState::Path(recorded)) = &last.after else {
            continue;
        };
        report.checked += 1;
        match probe.probe(target, Capture::After).await {
            Ok(TargetState::Path(current)) => {
                let differences = differences(recorded, &current);
                if !differences.is_empty() {
                    report.modified.push(Drift {
                        target: target.clone(),
                        ownership: k.ownership,
                        recorded_seq: last.seq,
                        job_id: last.job_id.clone(),
                        recorded: recorded.clone(),
                        current,
                        differences,
                    });
                }
            }
            Ok(_) => report
                .unreadable
                .push((target.clone(), "probe returned a non-path state".into())),
            Err(e) => report.unreadable.push((target.clone(), e)),
        }
    }
    report
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::command::RecordingRunner;
    use crate::ledger::entry::ChangeKind;
    use crate::ledger::probe::HostProbe;
    use crate::ledger::recorder::{ChangeContext, Declared, Recorder};
    use crate::ledger::store::Ledger;
    use crate::ledger::store::tests::{sec_for, tight_tempdir};
    use crate::params::HostPath;
    use std::os::unix::fs::PermissionsExt;
    use std::sync::Arc;

    fn path(p: &str) -> Target {
        Target::Path {
            path: HostPath::try_from(p.to_string()).unwrap(),
        }
    }

    /// The platform writes a file through the recorder; the user edits it,
    /// another platform file is left alone, a third is deleted.
    #[tokio::test]
    async fn user_edits_after_the_platform_wrote_are_reported() {
        let root = tight_tempdir();
        let host = root.path().join("host");
        std::fs::create_dir_all(host.join("etc/ssh/sshd_config.d")).unwrap();
        let (ledger, _) = Ledger::open(&root.path().join("ledger"), &sec_for(root.path())).unwrap();
        let ledger = Arc::new(ledger);
        let probe = Arc::new(HostProbe::new(
            Arc::new(RecordingRunner::default()),
            &host,
            Some(ledger.backup_dir()),
        ));
        let rec = Recorder::new(Ok(ledger.clone()), probe.clone());
        let conf = "/etc/ssh/sshd_config.d/50-hazelminx.conf";
        let other = "/etc/ssh/sshd_config.d/60-hazelminx.conf";
        let gone = "/etc/ssh/sshd_config.d/70-hazelminx.conf";
        for (p, body) in [
            (conf, "PasswordAuthentication no\n"),
            (other, "X 1\n"),
            (gone, "Y\n"),
        ] {
            let decl = [Declared {
                target: path(p),
                change: ChangeKind::Create,
            }];
            let pending = rec
                .begin(
                    &ChangeContext {
                        job_id: Some("job-1".into()),
                        action: "write_sshd_dropin".into(),
                        ..Default::default()
                    },
                    &decl,
                )
                .await
                .unwrap();
            std::fs::write(host.join(p.trim_start_matches('/')), body).unwrap();
            rec.finish(pending, &Ok(())).await.unwrap();
        }
        let known = ledger.known_all();
        assert!(
            known
                .iter()
                .all(|(_, k)| k.ownership == Ownership::PlatformInstalled)
        );
        assert!(detect(&known, probe.as_ref()).await.modified.is_empty());

        // The user edits one file and deletes another.
        let full = host.join(conf.trim_start_matches('/'));
        std::fs::write(&full, "PasswordAuthentication yes\n").unwrap();
        std::fs::set_permissions(&full, std::fs::Permissions::from_mode(0o600)).unwrap();
        std::fs::remove_file(host.join(gone.trim_start_matches('/'))).unwrap();

        let report = detect(&known, probe.as_ref()).await;
        assert_eq!(report.checked, 3);
        assert_eq!(report.modified.len(), 2, "{report:?}");
        let edited = report
            .modified
            .iter()
            .find(|d| d.target == path(conf))
            .unwrap();
        assert!(edited.differences.contains(&Difference::Content));
        assert_eq!(edited.job_id.as_deref(), Some("job-1"));
        assert_ne!(edited.recorded.sha256, edited.current.sha256);
        let deleted = report
            .modified
            .iter()
            .find(|d| d.target == path(gone))
            .unwrap();
        assert_eq!(deleted.differences, vec![Difference::Existence]);
    }
}
