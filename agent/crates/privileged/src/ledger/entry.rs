//! What one ledger line says (spec 55.1): the target, the kind of change,
//! the state before and after, when, which job caused it, and who owns the
//! target. Every type here is part of the on-disk format and of the batch
//! Core keeps a copy of, so fields are only ever added, never renamed.

use crate::params::{HostPath, PackageName, UnitName, UserName};
use serde::{Deserialize, Serialize};
use std::path::PathBuf;
use time::OffsetDateTime;

/// A host object the platform changes. One variant per row of the 55.1
/// table that an action currently touches; tickets that add actions on other
/// objects (firewall, containers, sysctl, config keys, …) add a variant here
/// together with its probe in [`super::probe`].
#[derive(Debug, Clone, PartialEq, Eq, Hash, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case", deny_unknown_fields)]
pub enum Target {
    /// A file, directory or symlink. The link itself is the target; links
    /// are never followed.
    Path { path: HostPath },
    SystemdUnit { unit: UnitName },
    Package { name: PackageName },
    User { name: UserName },
    Group { name: UserName },
}

impl Target {
    pub fn describe(&self) -> String {
        match self {
            Target::Path { path } => format!("path {}", path.as_path().display()),
            Target::SystemdUnit { unit } => format!("unit {}", unit.as_str()),
            Target::Package { name } => format!("package {}", name.as_str()),
            Target::User { name } => format!("user {}", name.as_str()),
            Target::Group { name } => format!("group {}", name.as_str()),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ChangeKind {
    Create,
    Modify,
    Delete,
    Install,
    Upgrade,
    Remove,
    Enable,
    Disable,
    Start,
    Stop,
    Restart,
}

impl ChangeKind {
    /// Changes that act on running state only and leave files, packages and
    /// enablement as they were.
    pub fn is_runtime_only(self) -> bool {
        matches!(self, ChangeKind::Start | ChangeKind::Stop | ChangeKind::Restart)
    }
}

/// Who a target belongs to, from the uninstaller's point of view (55.1).
/// Ordered so that `max` keeps the strongest claim the ledger has seen.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Ownership {
    /// Existed before the platform touched it; the platform did not change
    /// it, or only read it.
    Existing,
    /// Existed before the platform touched it; the platform changed its
    /// content or state.
    PlatformModified,
    /// Did not exist before the platform created it.
    PlatformInstalled,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Phase {
    /// Written and synced before the change runs. An intent without a
    /// result means the outcome is unknown (crash or lost write).
    Intent,
    /// Written after the change, with the observed state.
    Result,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "status", rename_all = "snake_case")]
pub enum Outcome {
    Succeeded,
    Failed { error: String },
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum PathKind {
    File,
    Directory,
    Symlink,
    Other,
}

/// Whether the original content of a file was kept.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "status", rename_all = "snake_case")]
pub enum Backup {
    /// Stored as `backups/<sha256>` in the ledger directory.
    Stored,
    /// Hash recorded, content not kept.
    TooLarge { limit_bytes: u64 },
}

#[derive(Debug, Clone, PartialEq, Eq, Default, Serialize, Deserialize)]
pub struct PathState {
    pub exists: bool,
    pub kind: Option<PathKind>,
    /// Permission bits including setuid/setgid/sticky (`mode & 0o7777`).
    pub mode: Option<u32>,
    pub uid: Option<u32>,
    pub gid: Option<u32>,
    /// Regular files only.
    pub size: Option<u64>,
    /// Hex SHA-256 of the content, regular files only.
    pub sha256: Option<String>,
    /// Symlinks only.
    pub link_target: Option<PathBuf>,
    /// Set on the state captured before a change, regular files only.
    pub backup: Option<Backup>,
}

#[derive(Debug, Clone, PartialEq, Eq, Default, Serialize, Deserialize)]
pub struct UnitState {
    /// `false` when systemd reports `LoadState=not-found`.
    pub exists: bool,
    pub load_state: Option<String>,
    /// `enabled`, `disabled`, `static`, `masked`, …
    pub unit_file_state: Option<String>,
    pub active_state: Option<String>,
    pub sub_state: Option<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum PackageDb {
    Dpkg,
    Rpm,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct PackageState {
    pub db: PackageDb,
    pub installed: bool,
    pub version: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Default, Serialize, Deserialize)]
pub struct UserState {
    pub exists: bool,
    pub uid: Option<u32>,
    pub gid: Option<u32>,
    /// Names of the primary and supplementary groups, sorted.
    pub groups: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Default, Serialize, Deserialize)]
pub struct GroupState {
    pub exists: bool,
    pub gid: Option<u32>,
    /// Supplementary members from `/etc/group`, sorted.
    pub members: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum TargetState {
    Path(PathState),
    SystemdUnit(UnitState),
    Package(PackageState),
    User(UserState),
    Group(GroupState),
}

impl TargetState {
    pub fn exists(&self) -> bool {
        match self {
            TargetState::Path(s) => s.exists,
            TargetState::SystemdUnit(s) => s.exists,
            TargetState::Package(s) => s.installed,
            TargetState::User(s) => s.exists,
            TargetState::Group(s) => s.exists,
        }
    }

    /// Same observable host state, ignoring the backup bookkeeping.
    pub fn same_host_state(&self, other: &TargetState) -> bool {
        match (self, other) {
            (TargetState::Path(a), TargetState::Path(b)) => {
                PathState {
                    backup: None,
                    ..a.clone()
                } == PathState {
                    backup: None,
                    ..b.clone()
                }
            }
            _ => self == other,
        }
    }
}

/// Ownership of a target after one change, given what the ledger already
/// knew (`prior`), the state before, and the state after (`None` at intent
/// time, or when the after state could not be read).
pub fn classify(
    prior: Option<Ownership>,
    change: ChangeKind,
    before: &TargetState,
    after: Option<&TargetState>,
) -> Ownership {
    let this = if !before.exists() {
        Ownership::PlatformInstalled
    } else {
        match after {
            Some(after) if before.same_host_state(after) => Ownership::Existing,
            Some(_) => Ownership::PlatformModified,
            None if change.is_runtime_only() => Ownership::Existing,
            None => Ownership::PlatformModified,
        }
    };
    prior.map_or(this, |p| p.max(this))
}

/// One ledger record. Serialized as the `entry` of a line
/// `{"hash":"<hex sha256 of the entry bytes>","entry":{…}}`.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Entry {
    /// 1, 2, 3, … with no gaps.
    pub seq: u64,
    /// Hash of the previous line; the first line chains to the genesis hash
    /// derived from the ledger id.
    pub prev_hash: String,
    #[serde(with = "time::serde::rfc3339")]
    pub recorded_at: OffsetDateTime,
    pub phase: Phase,
    /// Shared by the intent and result lines of one target change.
    pub change_id: String,
    /// Job that caused the change (spec 33), when the caller supplied one.
    pub job_id: Option<String>,
    pub request_id: Option<String>,
    pub grant_id: Option<String>,
    pub subject: Option<String>,
    /// Action kind (`restart_service`) or installer step name.
    pub action: String,
    pub target: Target,
    pub change: ChangeKind,
    pub ownership: Ownership,
    pub before: TargetState,
    /// Result lines only; `None` with `after_error` when it could not be read.
    pub after: Option<TargetState>,
    pub after_error: Option<String>,
    /// Result lines only.
    pub outcome: Option<Outcome>,
    /// Result lines only: the matching intent.
    pub intent_seq: Option<u64>,
}

#[cfg(test)]
mod tests {
    use super::*;

    fn unit(exists: bool, active: &str) -> TargetState {
        TargetState::SystemdUnit(UnitState {
            exists,
            load_state: Some(if exists { "loaded" } else { "not-found" }.into()),
            unit_file_state: Some("enabled".into()),
            active_state: Some(active.into()),
            sub_state: None,
        })
    }

    #[test]
    fn ownership_follows_55_1() {
        let absent = unit(false, "inactive");
        let running = unit(true, "active");
        let stopped = unit(true, "inactive");
        // Created by the platform.
        assert_eq!(
            classify(None, ChangeKind::Install, &absent, Some(&running)),
            Ownership::PlatformInstalled
        );
        // Existing unit restarted, still running: platform did not change it.
        assert_eq!(
            classify(None, ChangeKind::Restart, &running, Some(&running)),
            Ownership::Existing
        );
        // Existing stopped unit that the restart started: state changed.
        assert_eq!(
            classify(None, ChangeKind::Restart, &stopped, Some(&running)),
            Ownership::PlatformModified
        );
        // Intent time: persistent change to an existing target is a modification.
        assert_eq!(
            classify(None, ChangeKind::Modify, &running, None),
            Ownership::PlatformModified
        );
        assert_eq!(
            classify(None, ChangeKind::Restart, &running, None),
            Ownership::Existing
        );
        // Later changes never weaken an earlier claim.
        assert_eq!(
            classify(
                Some(Ownership::PlatformInstalled),
                ChangeKind::Restart,
                &running,
                Some(&running)
            ),
            Ownership::PlatformInstalled
        );
    }

    #[test]
    fn backup_bookkeeping_is_not_a_host_change() {
        let a = TargetState::Path(PathState {
            exists: true,
            kind: Some(PathKind::File),
            sha256: Some("ab".into()),
            backup: Some(Backup::Stored),
            ..Default::default()
        });
        let b = TargetState::Path(PathState {
            exists: true,
            kind: Some(PathKind::File),
            sha256: Some("ab".into()),
            backup: None,
            ..Default::default()
        });
        assert!(a.same_host_state(&b));
        assert_ne!(a, b);
    }

    #[test]
    fn target_wire_shape_validates_names() {
        let t: Target =
            serde_json::from_str(r#"{"type":"systemd_unit","unit":"nginx.service"}"#).unwrap();
        assert_eq!(t.describe(), "unit nginx.service");
        assert!(
            serde_json::from_str::<Target>(r#"{"type":"systemd_unit","unit":"x; id"}"#).is_err()
        );
        assert!(serde_json::from_str::<Target>(r#"{"type":"path","path":"rel"}"#).is_err());
        assert!(serde_json::from_str::<Target>(r#"{"type":"shell","cmd":"id"}"#).is_err());
    }
}
