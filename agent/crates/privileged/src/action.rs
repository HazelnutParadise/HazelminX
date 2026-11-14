//! The closed set of actions the executor performs. Adding one means adding a
//! variant here (with typed parameters from [`crate::params`]), a permission
//! name, its ledger declaration in [`Action::ledger_decl`] (what it changes,
//! or that it only reads; spec 55.1), and a handler arm in
//! [`crate::executor::HostExecutor`]. There is
//! deliberately no shell, script or "raw command" variant; if one is ever
//! added it must require [`PERM_SHELL`], which no container-level grant
//! carries (SEC-005).

use crate::compose_manifest::ComposeManifest;
use crate::ledger::entry::{ChangeKind, Target};
use crate::ledger::recorder::{Declared, LedgerDecl};
use crate::params::{HostPath, UnitName};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

pub const PERM_HOST_INFO_READ: &str = "host.info.read";
pub const PERM_HOST_PACKAGES_READ: &str = "host.packages.read";
pub const PERM_HOST_SERVICES_MANAGE: &str = "host.services.manage";
pub const PERM_HOST_LEDGER_READ: &str = "host.ledger.read";
pub const PERM_HOST_ENGINES_READ: &str = "host.engines.read";
pub const PERM_HOST_STORAGE_READ: &str = "host.storage.read";
pub const PERM_HOST_COMPOSE_CHECK: &str = "host.compose.check";
pub const PERM_HOST_COMPOSE_DEPLOY: &str = "host.compose.deploy";
/// The platform's Compose directory: `<project>/revisions/<revision>/` per
/// deployment and `<project>/data/` for persistent data (decision 0035).
pub const COMPOSE_ROOT: &str = "/var/lib/hazelminx/compose";
/// Reserved for a future terminal/script action; listed so grant issuers and
/// reviewers treat it as a separate high-risk permission (spec 32.2).
pub const PERM_SHELL: &str = "host.shell";

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case", deny_unknown_fields)]
pub enum Action {
    /// Hostname, OS release, kernel and uptime from the host's own files.
    /// (Struct variants, not unit variants: serde only enforces
    /// `deny_unknown_fields` on the former.)
    SystemInfo {},
    /// Packages with a newer candidate version, via the detected package manager.
    ListPackageUpdates {},
    /// `systemctl restart <unit>` followed by `systemctl is-active <unit>`.
    RestartService { unit: UnitName },
    /// Raw host-ledger lines after `after_seq`, for Core's copy (55.1).
    LedgerExport {
        after_seq: u64,
        #[serde(default)]
        limit: Option<u32>,
        /// Stop before the page's lines exceed this many bytes (at least one
        /// line is always returned), so a page fits one channel message.
        #[serde(default)]
        max_bytes: Option<u32>,
    },
    /// Which container engines the host has: programs, units, sockets and
    /// what each socket answers (ticket 039.1).
    EngineInspect {},
    /// How much of each local filesystem a fixed set of directories holds,
    /// by category (ticket 030.1). No parameters: roots and limits are fixed.
    StorageUsage {},
    /// Host pre-checks of one Compose revision (ticket 044.2): the files are
    /// laid out in a private temporary directory, read with the native
    /// `docker compose config`, and checked; nothing on the host changes.
    ComposeCheck { manifest: ComposeManifest },
    /// Writes the revision under [`COMPOSE_ROOT`] and runs
    /// `docker compose up` for it, after the same checks.
    ComposeDeploy { manifest: ComposeManifest },
}

impl Action {
    /// All kind names, in the wire spelling. Used to tell "unknown action"
    /// from "bad parameters".
    pub const KINDS: &'static [&'static str] = &[
        "system_info",
        "list_package_updates",
        "restart_service",
        "ledger_export",
        "engine_inspect",
        "storage_usage",
        "compose_check",
        "compose_deploy",
    ];

    pub fn kind(&self) -> &'static str {
        match self {
            Action::SystemInfo {} => "system_info",
            Action::ListPackageUpdates {} => "list_package_updates",
            Action::RestartService { .. } => "restart_service",
            Action::LedgerExport { .. } => "ledger_export",
            Action::EngineInspect {} => "engine_inspect",
            Action::StorageUsage {} => "storage_usage",
            Action::ComposeCheck { .. } => "compose_check",
            Action::ComposeDeploy { .. } => "compose_deploy",
        }
    }

    /// What this action changes on the host. Read-only actions say so;
    /// every other action names each target and the kind of change, and the
    /// executor records it in the host ledger before and after running.
    pub fn ledger_decl(&self) -> LedgerDecl {
        match self {
            Action::SystemInfo {}
            | Action::ListPackageUpdates {}
            | Action::LedgerExport { .. }
            | Action::EngineInspect {}
            | Action::StorageUsage {}
            | Action::ComposeCheck { .. } => LedgerDecl::ReadOnly,
            Action::ComposeDeploy { manifest } => {
                let project = manifest.project.as_str();
                let path = |p: String| Target::Path {
                    path: HostPath::try_from(p).expect("validated names make a valid host path"),
                };
                LedgerDecl::Changes(vec![
                    Declared {
                        target: path(COMPOSE_ROOT.to_string()),
                        change: ChangeKind::Create,
                    },
                    Declared {
                        target: path(format!("{COMPOSE_ROOT}/{project}")),
                        change: ChangeKind::Create,
                    },
                    Declared {
                        target: path(format!(
                            "{COMPOSE_ROOT}/{project}/revisions/{}",
                            manifest.revision.as_str()
                        )),
                        change: ChangeKind::Create,
                    },
                    Declared {
                        target: path(format!("{COMPOSE_ROOT}/{project}/data")),
                        change: ChangeKind::Create,
                    },
                    Declared {
                        target: Target::ComposeProject {
                            project: manifest.project.clone(),
                            environment: manifest.environment.clone(),
                        },
                        change: ChangeKind::Modify,
                    },
                ])
            }
            Action::RestartService { unit } => LedgerDecl::Changes(vec![Declared {
                target: Target::SystemdUnit { unit: unit.clone() },
                change: ChangeKind::Restart,
            }]),
        }
    }

    /// The digest a grant's `binding` must equal for this action, when the
    /// action is bound to its content (decision 0035).
    pub fn binding(&self) -> Option<String> {
        match self {
            Action::ComposeCheck { manifest } | Action::ComposeDeploy { manifest } => {
                let text = format!("{}:{}", self.kind(), manifest.digest());
                Some(hex::encode(Sha256::digest(text.as_bytes())))
            }
            _ => None,
        }
    }

    /// The single permission a grant must list for this action.
    pub fn required_permission(&self) -> &'static str {
        Self::permission_for_kind(self.kind()).expect("every variant is in KINDS")
    }

    /// Permission for a kind name, or `None` when the kind does not exist.
    pub fn permission_for_kind(kind: &str) -> Option<&'static str> {
        match kind {
            "system_info" => Some(PERM_HOST_INFO_READ),
            "list_package_updates" => Some(PERM_HOST_PACKAGES_READ),
            "restart_service" => Some(PERM_HOST_SERVICES_MANAGE),
            "ledger_export" => Some(PERM_HOST_LEDGER_READ),
            "engine_inspect" => Some(PERM_HOST_ENGINES_READ),
            "storage_usage" => Some(PERM_HOST_STORAGE_READ),
            "compose_check" => Some(PERM_HOST_COMPOSE_CHECK),
            "compose_deploy" => Some(PERM_HOST_COMPOSE_DEPLOY),
            _ => None,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn every_kind_has_a_permission_and_round_trips() {
        for kind in Action::KINDS {
            assert!(Action::permission_for_kind(kind).is_some(), "{kind}");
        }
        let a: Action =
            serde_json::from_value(json!({"kind": "restart_service", "unit": "nginx.service"}))
                .unwrap();
        assert_eq!(a.kind(), "restart_service");
        assert_eq!(a.required_permission(), PERM_HOST_SERVICES_MANAGE);
        let back = serde_json::to_value(&a).unwrap();
        assert_eq!(
            back,
            json!({"kind": "restart_service", "unit": "nginx.service"})
        );
        let s: Action = serde_json::from_value(json!({"kind": "system_info"})).unwrap();
        assert_eq!(s.required_permission(), PERM_HOST_INFO_READ);
    }

    #[test]
    fn every_action_declares_its_host_changes() {
        let unit = UnitName::try_from("nginx.service".to_string()).unwrap();
        assert_eq!(Action::SystemInfo {}.ledger_decl(), LedgerDecl::ReadOnly);
        assert_eq!(
            Action::ListPackageUpdates {}.ledger_decl(),
            LedgerDecl::ReadOnly
        );
        assert_eq!(
            Action::LedgerExport {
                after_seq: 0,
                limit: None,
                max_bytes: None
            }
            .ledger_decl(),
            LedgerDecl::ReadOnly
        );
        assert_eq!(
            Action::RestartService { unit: unit.clone() }.ledger_decl(),
            LedgerDecl::Changes(vec![Declared {
                target: Target::SystemdUnit { unit },
                change: ChangeKind::Restart
            }])
        );
        let e: Action =
            serde_json::from_value(json!({"kind": "ledger_export", "after_seq": 3})).unwrap();
        assert_eq!(e.required_permission(), PERM_HOST_LEDGER_READ);
        assert_eq!(Action::EngineInspect {}.ledger_decl(), LedgerDecl::ReadOnly);
        let i: Action = serde_json::from_value(json!({"kind": "engine_inspect"})).unwrap();
        assert_eq!(i.required_permission(), PERM_HOST_ENGINES_READ);
        assert!(
            serde_json::from_value::<Action>(json!({"kind": "engine_inspect", "socket": "/x"}))
                .is_err()
        );
    }

    #[test]
    fn there_is_no_shell_script_or_raw_command_action() {
        for kind in ["shell", "script", "exec", "run", "command", "terminal"] {
            assert!(!Action::KINDS.contains(&kind));
            assert!(Action::permission_for_kind(kind).is_none());
            let r = serde_json::from_value::<Action>(json!({"kind": kind, "cmd": "id"}));
            assert!(r.is_err(), "{kind}");
        }
        assert_ne!(PERM_SHELL, PERM_HOST_SERVICES_MANAGE);
    }

    #[test]
    fn extra_or_invalid_parameters_are_rejected() {
        assert!(
            serde_json::from_value::<Action>(json!({"kind": "system_info", "cmd": "id"})).is_err()
        );
        assert!(
            serde_json::from_value::<Action>(
                json!({"kind": "restart_service", "unit": "nginx.service; id"})
            )
            .is_err()
        );
        assert!(serde_json::from_value::<Action>(json!({"kind": "restart_service"})).is_err());
    }
}
