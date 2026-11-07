//! The closed set of actions the executor performs. Adding one means adding a
//! variant here (with typed parameters from [`crate::params`]), a permission
//! name, and a handler arm in [`crate::executor::HostExecutor`]. There is
//! deliberately no shell, script or "raw command" variant; if one is ever
//! added it must require [`PERM_SHELL`], which no container-level grant
//! carries (SEC-005).

use crate::params::UnitName;
use serde::{Deserialize, Serialize};

pub const PERM_HOST_INFO_READ: &str = "host.info.read";
pub const PERM_HOST_PACKAGES_READ: &str = "host.packages.read";
pub const PERM_HOST_SERVICES_MANAGE: &str = "host.services.manage";
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
}

impl Action {
    /// All kind names, in the wire spelling. Used to tell "unknown action"
    /// from "bad parameters".
    pub const KINDS: &'static [&'static str] =
        &["system_info", "list_package_updates", "restart_service"];

    pub fn kind(&self) -> &'static str {
        match self {
            Action::SystemInfo {} => "system_info",
            Action::ListPackageUpdates {} => "list_package_updates",
            Action::RestartService { .. } => "restart_service",
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
