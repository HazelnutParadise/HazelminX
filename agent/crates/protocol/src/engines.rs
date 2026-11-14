//! `host.engines` v1: what container engines a host has (ticket 039.1).
//!
//! The privileged executor's read-only `engine_inspect` action produces an
//! [`EngineReport`]; the agent relays it to the Core in `engines_report`.
//! A value the host did not provide is `null` (or an empty list); it is
//! never guessed.

use serde::{Deserialize, Serialize};

/// Engine kinds this version looks for.
pub mod kind {
    pub const DOCKER: &str = "docker";
    pub const PODMAN: &str = "podman";
    pub const K3S: &str = "k3s";
    /// An endpoint answered, but not as Docker or Podman.
    pub const UNKNOWN: &str = "unknown";
}

/// States of an engine and of an environment.
pub mod state {
    /// The endpoint answered (environment), or one of the engine's
    /// environments did (engine).
    pub const RUNNING: &str = "running";
    /// Installed (binary or unit present), but no unit is active and no
    /// endpoint answers.
    pub const STOPPED: &str = "stopped";
    /// No binary in the standard locations, no unit, no endpoint.
    pub const NOT_INSTALLED: &str = "not_installed";
    /// The endpoint answered with a version below the supported minimum.
    pub const INCOMPATIBLE: &str = "incompatible";
    /// The endpoint exists but refused the connection for lack of
    /// permission.
    pub const PERMISSION_DENIED: &str = "permission_denied";
    /// The endpoint exists but did not answer, or answered something that
    /// is not the engine API.
    pub const NOT_RESPONDING: &str = "not_responding";
    /// Installed, but whether it runs could not be read (no systemd, or
    /// systemctl failed).
    pub const UNKNOWN: &str = "unknown";
}

/// Lowest supported versions: Docker Engine API 1.41 (Docker 20.10) and
/// Podman 4.0.
pub const MIN_DOCKER_API: (u32, u32) = (1, 41);
pub const MIN_PODMAN: (u32, u32) = (4, 0);

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct EngineReport {
    /// One entry per kind this version looks for (docker, podman, k3s),
    /// even when not installed.
    pub engines: Vec<Engine>,
    /// One entry per endpoint found; several of one kind stay separate
    /// (ENV-001). The same socket reached through two paths is listed once.
    pub environments: Vec<Environment>,
    /// What could not be looked at, so a missing environment is not read
    /// as "none there".
    pub uninspected: Vec<Uninspected>,
}

/// An engine as installed on the host.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Engine {
    pub kind: String,
    pub state: String,
    /// Path of the program found, or null when none of the standard
    /// locations has it.
    pub binary: Option<String>,
    /// First line of `<binary> --version`, or null.
    pub binary_version: Option<String>,
    pub units: Vec<Unit>,
    /// Docker CLI plugins (compose, buildx) found next to the engine.
    pub plugins: Vec<Plugin>,
}

/// A systemd unit as `systemctl show` reports it.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Unit {
    pub name: String,
    /// `loaded`, `not-found`, ...
    pub load_state: Option<String>,
    /// `active`, `inactive`, `failed`, ...
    pub active_state: Option<String>,
    pub sub_state: Option<String>,
    /// `enabled`, `disabled`, `masked`, ...
    pub unit_file_state: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Plugin {
    /// `compose` or `buildx`.
    pub name: String,
    pub path: String,
    /// First line of `<plugin> version`, or null.
    pub version: Option<String>,
}

/// One engine endpoint and who owns it.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Environment {
    /// `env:<resolved endpoint>`; stable while the endpoint stays, whatever
    /// answers on it.
    pub id: String,
    /// Decided by what the endpoint answered, not by its path (ENV-002):
    /// a `docker.sock` served by Podman is `podman`. When the endpoint did
    /// not answer, the kind its path is the usual place for.
    pub kind: String,
    /// True when `kind` comes from the endpoint's answer.
    pub kind_confirmed: bool,
    /// The path looked at, e.g. `/var/run/docker.sock`.
    pub endpoint: String,
    /// The same path with symbolic links resolved.
    pub endpoint_resolved: String,
    /// `rootful` (the socket belongs to root) or `rootless`.
    pub mode: String,
    pub owner_uid: u32,
    /// From `/etc/passwd`; null when the uid has no entry.
    pub owner_name: Option<String>,
    pub state: String,
    /// Engine version the endpoint reported.
    pub version: Option<String>,
    pub api_version: Option<String>,
    /// `systemd` or `cgroupfs`.
    pub cgroup_driver: Option<String>,
    /// `1` or `2`.
    pub cgroup_version: Option<String>,
    /// Docker Swarm `LocalNodeState` (`inactive`, `active`, ...).
    pub swarm: Option<String>,
    /// Containers the endpoint counts, all states.
    pub containers: Option<u64>,
    pub containers_running: Option<u64>,
    /// Why the endpoint could not be read; null when it answered.
    pub error: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Uninspected {
    pub path: String,
    pub reason: String,
}
