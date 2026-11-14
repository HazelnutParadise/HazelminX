//! The closed set of actions the executor performs. Adding one means adding a
//! variant here (with typed parameters from [`crate::params`]), a permission
//! name, its ledger declaration in [`Action::ledger_decl`] (what it changes,
//! or that it only reads; spec 55.1), and a handler arm in
//! [`crate::executor::HostExecutor`]. There is
//! deliberately no shell, script or "raw command" variant; if one is ever
//! added it must require [`PERM_SHELL`], which no container-level grant
//! carries (SEC-005).

use crate::ledger::entry::{ChangeKind, Target};
use crate::ledger::recorder::{Declared, LedgerDecl};
use crate::params::UnitName;
use serde::{Deserialize, Serialize};

pub const PERM_HOST_INFO_READ: &str = "host.info.read";
pub const PERM_HOST_PACKAGES_READ: &str = "host.packages.read";
pub const PERM_HOST_SERVICES_MANAGE: &str = "host.services.manage";
pub const PERM_HOST_LEDGER_READ: &str = "host.ledger.read";
pub const PERM_HOST_ENGINES_READ: &str = "host.engines.read";
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
    ];

    pub fn kind(&self) -> &'static str {
        match self {
            Action::SystemInfo {} => "system_info",
            Action::ListPackageUpdates {} => "list_package_updates",
            Action::RestartService { .. } => "restart_service",
            Action::LedgerExport { .. } => "ledger_export",
            Action::EngineInspect {} => "engine_inspect",
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
            | Action::EngineInspect {} => {
                LedgerDecl::ReadOnly
            }
            Action::RestartService { unit } => LedgerDecl::Changes(vec![Declared {
                target: Target::SystemdUnit { unit: unit.clone() },
                change: ChangeKind::Restart,
            }]),
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
