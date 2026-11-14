//! `engine_inspect`: which container engines the host has (ticket 039.1).
//! Read-only. Runs as root because rootful engine sockets and the users'
//! runtime directories are not readable by the agent.
//!
//! What it looks at, all under `sysroot`:
//! - programs in the standard locations, and their `--version`;
//! - Docker CLI plugins (compose, buildx) and their `version`;
//! - the engines' systemd units (`systemctl show`, fixed property list);
//! - rootful sockets (`/var/run/docker.sock`, `/run/docker.sock`,
//!   `/run/podman/podman.sock`) and every user's
//!   `/run/user/<uid>/{docker.sock,podman/podman.sock}`; each one that
//!   exists is asked `GET /version` and `GET /info`, and its kind is what
//!   it answers (ENV-002), not its path;
//! - K3s by its program, units and containerd socket.
//!
//! Sockets belong to whoever created them, and a user's socket may be
//! served by any program of that user, so answers are size- and
//! time-bounded and every string is cut to [`MAX_TEXT`] characters.

use crate::command::{CommandRunner, CommandSpec, OutputSink};
use crate::executor::ActionResult;
use hazelminx_protocol::engines::{
    Engine, EngineReport, Environment, MIN_DOCKER_API, MIN_PODMAN, Plugin, Uninspected, Unit, kind,
    state,
};
use std::collections::HashSet;
use std::os::unix::fs::{FileTypeExt, MetadataExt};
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::Duration;
use tokio::io::{AsyncReadExt, AsyncWriteExt};

pub const SYSTEMCTL: &str = "/usr/bin/systemctl";
/// Longest string kept from a program or socket answer.
pub const MAX_TEXT: usize = 256;
/// Largest HTTP answer read from a socket.
pub const MAX_ANSWER_BYTES: usize = 1 << 20;
/// Bound of one socket round trip and of one program run.
pub const PROBE_TIMEOUT: Duration = Duration::from_secs(2);
/// Bound of all socket probes together, under the Core's 60 s wait;
/// sockets not reached in time are listed in `uninspected`.
pub const SOCKETS_DEADLINE: Duration = Duration::from_secs(30);
/// Most user runtime directories looked at.
pub const MAX_USERS: usize = 256;

const DOCKER_BINS: &[&str] = &["/usr/bin/docker", "/usr/local/bin/docker"];
const PODMAN_BINS: &[&str] = &["/usr/bin/podman", "/usr/local/bin/podman"];
const K3S_BINS: &[&str] = &["/usr/local/bin/k3s", "/usr/bin/k3s"];
const PLUGIN_DIRS: &[&str] = &[
    "/usr/libexec/docker/cli-plugins",
    "/usr/lib/docker/cli-plugins",
    "/usr/local/lib/docker/cli-plugins",
    "/usr/local/libexec/docker/cli-plugins",
];
const DOCKER_UNITS: &[&str] = &["docker.service", "docker.socket", "containerd.service"];
const PODMAN_UNITS: &[&str] = &["podman.service", "podman.socket"];
const K3S_UNITS: &[&str] = &["k3s.service", "k3s-agent.service"];
const ROOTFUL_SOCKETS: &[(&str, &str)] = &[
    ("/var/run/docker.sock", kind::DOCKER),
    ("/run/docker.sock", kind::DOCKER),
    ("/run/podman/podman.sock", kind::PODMAN),
];
const K3S_CONTAINERD: &str = "/run/k3s/containerd/containerd.sock";

/// Runs the whole inspection.
pub async fn run(runner: Arc<dyn CommandRunner>, sysroot: &Path) -> ActionResult {
    let report = inspect(runner, sysroot).await;
    Ok(serde_json::to_value(report).expect("EngineReport serializes"))
}

pub async fn inspect(runner: Arc<dyn CommandRunner>, sysroot: &Path) -> EngineReport {
    let mut uninspected = Vec::new();
    let users = passwd(sysroot);

    // Endpoints: rootful first, so a user's link to a root socket is
    // listed as the root one.
    let mut candidates: Vec<(String, &'static str)> = ROOTFUL_SOCKETS
        .iter()
        .map(|(p, k)| (p.to_string(), *k))
        .collect();
    let run_user = sysroot.join("run/user");
    match std::fs::read_dir(&run_user) {
        Ok(rd) => {
            let mut uids: Vec<u32> = rd
                .filter_map(|e| e.ok())
                .filter_map(|e| e.file_name().to_str()?.parse::<u32>().ok())
                .collect();
            uids.sort_unstable();
            if uids.len() > MAX_USERS {
                uninspected.push(Uninspected {
                    path: "/run/user".into(),
                    reason: format!(
                        "{} user runtime directories; only the first {MAX_USERS} were looked at",
                        uids.len()
                    ),
                });
                uids.truncate(MAX_USERS);
            }
            for uid in uids {
                candidates.push((format!("/run/user/{uid}/docker.sock"), kind::DOCKER));
                candidates.push((format!("/run/user/{uid}/podman/podman.sock"), kind::PODMAN));
            }
        }
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => {}
        Err(e) => uninspected.push(Uninspected {
            path: "/run/user".into(),
            reason: cut(&format!("cannot list user runtime directories: {e}")),
        }),
    }

    let mut seen = HashSet::new();
    let mut environments = Vec::new();
    let started = std::time::Instant::now();
    for (endpoint, usual) in candidates {
        if started.elapsed() > SOCKETS_DEADLINE {
            uninspected.push(Uninspected {
                path: endpoint.clone(),
                reason: format!(
                    "not reached within {}s of socket probes",
                    SOCKETS_DEADLINE.as_secs()
                ),
            });
            continue;
        }
        let host_path = sysroot.join(endpoint.trim_start_matches('/'));
        let Ok(resolved) = std::fs::canonicalize(&host_path) else {
            continue; // absent (or a dangling link): nothing there
        };
        let meta = match std::fs::metadata(&resolved) {
            Ok(m) => m,
            Err(_) => continue,
        };
        if !meta.file_type().is_socket() {
            continue;
        }
        if seen.contains(&resolved) {
            continue; // already listed through another path (rootful first)
        }
        let shown = shown_path(sysroot, &resolved);
        // A user's path must stay that user's: a link out of their runtime
        // directory, or a socket someone else owns, would make root talk
        // to a socket the user chose.
        if let Some(uid) = user_of(&endpoint)
            && (meta.uid() != uid || !shown.starts_with(&format!("/run/user/{uid}/")))
        {
            uninspected.push(Uninspected {
                path: endpoint.clone(),
                reason: cut(&format!(
                    "leads to {shown} owned by uid {}; only the user's own sockets in their runtime directory are asked",
                    meta.uid()
                )),
            });
            continue;
        }
        seen.insert(resolved.clone());
        // The usual kind follows the socket the path leads to, so a link
        // does not change it between reports.
        let usual = if shown.ends_with("podman.sock") {
            kind::PODMAN
        } else {
            usual
        };
        environments
            .push(probe_socket(&endpoint, &shown, &resolved, usual, meta.uid(), &users).await);
    }

    // K3s: its containerd speaks gRPC, not the engine API, so the
    // environment's state comes from its unit.
    let k3s_units = units(&runner, K3S_UNITS).await;
    let k3s_sock = sysroot.join(K3S_CONTAINERD.trim_start_matches('/'));
    if let Ok(meta) = std::fs::metadata(&k3s_sock)
        && meta.file_type().is_socket()
    {
        let active = k3s_units.as_ref().map(|u| any_active(u));
        environments.push(Environment {
            id: format!("env:{K3S_CONTAINERD}"),
            kind: kind::K3S.into(),
            kind_confirmed: false,
            endpoint: K3S_CONTAINERD.into(),
            endpoint_resolved: K3S_CONTAINERD.into(),
            mode: mode(meta.uid()).into(),
            owner_uid: meta.uid(),
            owner_name: users
                .iter()
                .find(|(u, _)| *u == meta.uid())
                .map(|(_, n)| n.clone()),
            state: match active {
                Some(true) => state::RUNNING,
                Some(false) => state::STOPPED,
                None => state::UNKNOWN,
            }
            .into(),
            version: None,
            api_version: None,
            cgroup_driver: None,
            cgroup_version: None,
            swarm: None,
            containers: None,
            containers_running: None,
            error: None,
        });
    }

    let docker_units = units(&runner, DOCKER_UNITS).await;
    let podman_units = units(&runner, PODMAN_UNITS).await;
    if docker_units.is_none() || podman_units.is_none() || k3s_units.is_none() {
        uninspected.push(Uninspected {
            path: SYSTEMCTL.into(),
            reason: "systemctl is missing or failed; whether services run is not known".into(),
        });
    }
    if !environments.iter().any(|e| e.mode == "rootless") {
        // Rootless engines without an API socket (Podman is daemonless)
        // leave no trace this action can read without entering each
        // user's session.
        uninspected.push(Uninspected {
            path: "/run/user/*".into(),
            reason: "rootless engines are found by their API socket only; a user's Podman without podman.socket is not seen".into(),
        });
    }

    let mut plugins = Vec::new();
    for dir in PLUGIN_DIRS {
        for name in ["compose", "buildx"] {
            if plugins.iter().any(|p: &Plugin| p.name == name) {
                continue;
            }
            let path = format!("{dir}/docker-{name}");
            if is_file(sysroot, &path) {
                let version = first_line(&runner, sysroot, &path, "version").await;
                plugins.push(Plugin {
                    name: name.into(),
                    path,
                    version,
                });
            }
        }
    }

    let engines = vec![
        engine(
            &runner,
            sysroot,
            kind::DOCKER,
            DOCKER_BINS,
            docker_units,
            plugins,
            &environments,
        )
        .await,
        engine(
            &runner,
            sysroot,
            kind::PODMAN,
            PODMAN_BINS,
            podman_units,
            Vec::new(),
            &environments,
        )
        .await,
        engine(
            &runner,
            sysroot,
            kind::K3S,
            K3S_BINS,
            k3s_units,
            Vec::new(),
            &environments,
        )
        .await,
    ];
    EngineReport {
        engines,
        environments,
        uninspected,
    }
}

async fn engine(
    runner: &Arc<dyn CommandRunner>,
    sysroot: &Path,
    k: &str,
    bins: &[&str],
    units: Option<Vec<Unit>>,
    plugins: Vec<Plugin>,
    envs: &[Environment],
) -> Engine {
    let binary = bins
        .iter()
        .find(|b| is_file(sysroot, b))
        .map(|b| b.to_string());
    let binary_version = match &binary {
        Some(b) => first_line(runner, sysroot, b, "--version").await,
        None => None,
    };
    // podman-docker installs a `docker` that is Podman: not Docker.
    let is_other = k == kind::DOCKER
        && binary_version
            .as_deref()
            .is_some_and(|v| v.to_ascii_lowercase().contains("podman"));
    let state = engine_state(k, binary.is_some() && !is_other, units.as_deref(), envs);
    Engine {
        kind: k.into(),
        state: state.into(),
        binary,
        binary_version,
        units: units.unwrap_or_default(),
        plugins,
    }
}

/// The engine's state from its environments, program and units: an
/// environment that answers wins, then the most specific problem, then
/// installation and units.
pub fn engine_state(
    k: &str,
    has_binary: bool,
    units: Option<&[Unit]>,
    envs: &[Environment],
) -> &'static str {
    let mine: Vec<&Environment> = envs.iter().filter(|e| e.kind == k).collect();
    for s in [
        state::RUNNING,
        state::INCOMPATIBLE,
        state::PERMISSION_DENIED,
        state::NOT_RESPONDING,
    ] {
        if mine.iter().any(|e| e.state == s) {
            return s;
        }
    }
    let loaded = units.is_some_and(|u| u.iter().any(|u| u.load_state.as_deref() == Some("loaded")));
    if !has_binary && !loaded && mine.is_empty() {
        return state::NOT_INSTALLED;
    }
    match units {
        Some(u) if any_active(u) => state::RUNNING,
        Some(_) => state::STOPPED,
        None => state::UNKNOWN,
    }
}

fn any_active(units: &[Unit]) -> bool {
    units
        .iter()
        .any(|u| u.active_state.as_deref() == Some("active"))
}

/// The uid of a `/run/user/<uid>/...` path.
fn user_of(endpoint: &str) -> Option<u32> {
    endpoint
        .strip_prefix("/run/user/")?
        .split('/')
        .next()?
        .parse()
        .ok()
}

fn mode(uid: u32) -> &'static str {
    if uid == 0 { "rootful" } else { "rootless" }
}

async fn probe_socket(
    endpoint: &str,
    shown: &str,
    resolved: &Path,
    usual: &str,
    uid: u32,
    users: &[(u32, String)],
) -> Environment {
    let mut env = Environment {
        id: format!("env:{shown}"),
        kind: usual.into(),
        kind_confirmed: false,
        endpoint: endpoint.into(),
        endpoint_resolved: shown.into(),
        mode: mode(uid).into(),
        owner_uid: uid,
        owner_name: users
            .iter()
            .find(|(u, _)| *u == uid)
            .map(|(_, n)| n.clone()),
        state: state::NOT_RESPONDING.into(),
        version: None,
        api_version: None,
        cgroup_driver: None,
        cgroup_version: None,
        swarm: None,
        containers: None,
        containers_running: None,
        error: None,
    };
    let version = match get_json(resolved, "/version", uid).await {
        Ok(v) => v,
        Err(ProbeError::Denied(m)) => {
            env.state = state::PERMISSION_DENIED.into();
            env.error = Some(cut(&m));
            return env;
        }
        Err(ProbeError::Other(m)) => {
            env.error = Some(cut(&m));
            return env;
        }
    };
    let Some(answered) = answered_kind(&version) else {
        env.kind = kind::UNKNOWN.into();
        env.kind_confirmed = true;
        env.error = Some("the endpoint answered /version without an engine version".into());
        return env;
    };
    env.kind = answered.into();
    env.kind_confirmed = true;
    env.version = text(&version, "Version");
    env.api_version = text(&version, "ApiVersion");
    let supported = match answered {
        kind::PODMAN => env
            .version
            .as_deref()
            .and_then(major_minor)
            .map(|v| v >= MIN_PODMAN),
        _ => env
            .api_version
            .as_deref()
            .and_then(major_minor)
            .map(|v| v >= MIN_DOCKER_API),
    };
    env.state = match supported {
        Some(false) => state::INCOMPATIBLE,
        _ => state::RUNNING,
    }
    .into();
    match get_json(resolved, "/info", uid).await {
        Ok(info) => {
            env.cgroup_driver = text(&info, "CgroupDriver");
            env.cgroup_version = text(&info, "CgroupVersion");
            env.containers = info.get("Containers").and_then(|v| v.as_u64());
            env.containers_running = info.get("ContainersRunning").and_then(|v| v.as_u64());
            env.swarm = info
                .get("Swarm")
                .and_then(|s| s.get("LocalNodeState"))
                .and_then(|s| s.as_str())
                .filter(|s| !s.is_empty())
                .map(cut);
        }
        Err(ProbeError::Denied(m) | ProbeError::Other(m)) => {
            env.error = Some(cut(&format!("/info: {m}")));
        }
    }
    env
}

/// `podman` when the answer names Podman (its Docker-compatible API does,
/// in `Components` or `Platform`), `docker` when it has an engine version
/// otherwise, `None` when it is not the engine API.
pub fn answered_kind(v: &serde_json::Value) -> Option<&'static str> {
    let names_podman =
        |s: Option<&str>| s.is_some_and(|s| s.to_ascii_lowercase().contains("podman"));
    let platform = v
        .get("Platform")
        .and_then(|p| p.get("Name"))
        .and_then(|n| n.as_str());
    let component = v
        .get("Components")
        .and_then(|c| c.as_array())
        .is_some_and(|cs| {
            cs.iter()
                .any(|c| names_podman(c.get("Name").and_then(|n| n.as_str())))
        });
    if names_podman(platform) || component {
        return Some(kind::PODMAN);
    }
    text(v, "Version")
        .map(|_| kind::DOCKER)
        .filter(|_| text(v, "ApiVersion").is_some())
}

fn text(v: &serde_json::Value, key: &str) -> Option<String> {
    v.get(key)
        .and_then(|s| s.as_str())
        .filter(|s| !s.is_empty())
        .map(cut)
}

/// `"1.47"` → (1, 47); `"v4.9.3"` → (4, 9).
pub fn major_minor(s: &str) -> Option<(u32, u32)> {
    let s = s.trim().trim_start_matches('v');
    let mut it = s.split(['.', '-', '+']);
    let major = it.next()?.parse().ok()?;
    let minor = it.next().and_then(|m| m.parse().ok()).unwrap_or(0);
    Some((major, minor))
}

pub(crate) enum ProbeError {
    Denied(String),
    Other(String),
}

/// One HTTP/1.0 request over the socket; the body when the status is 200
/// and it is JSON.
/// `owner` is the uid the socket file belonged to when it was checked; the
/// process that accepts must run as that uid, so a path swapped for a link
/// after the check cannot lead root to another socket.
pub(crate) async fn get_json(sock: &Path, path: &str, owner: u32) -> Result<serde_json::Value, ProbeError> {
    let fut = async {
        let mut s = tokio::net::UnixStream::connect(sock).await.map_err(|e| {
            if e.kind() == std::io::ErrorKind::PermissionDenied {
                ProbeError::Denied(format!("connect: {e}"))
            } else {
                ProbeError::Other(format!("connect: {e}"))
            }
        })?;
        let req =
            format!("GET {path} HTTP/1.0\r\nHost: localhost\r\nAccept: application/json\r\n\r\n");
        let peer = s
            .peer_cred()
            .map_err(|e| ProbeError::Other(format!("peer credentials: {e}")))?;
        if peer.uid() != owner {
            return Err(ProbeError::Other(format!(
                "the process answering runs as uid {}, not the socket owner {owner}",
                peer.uid()
            )));
        }
        s.write_all(req.as_bytes())
            .await
            .map_err(|e| ProbeError::Other(format!("write: {e}")))?;
        let mut buf = Vec::new();
        (&mut s)
            .take(MAX_ANSWER_BYTES as u64 + 1)
            .read_to_end(&mut buf)
            .await
            .map_err(|e| ProbeError::Other(format!("read: {e}")))?;
        if buf.len() > MAX_ANSWER_BYTES {
            return Err(ProbeError::Other(format!(
                "answer to {path} is larger than {MAX_ANSWER_BYTES} bytes"
            )));
        }
        parse_http(&buf).map_err(ProbeError::Other)
    };
    match tokio::time::timeout(PROBE_TIMEOUT, fut).await {
        Ok(r) => r,
        Err(_) => Err(ProbeError::Other(format!(
            "no answer to {path} within {}s",
            PROBE_TIMEOUT.as_secs()
        ))),
    }
}

/// Status 200 and a JSON body (no chunked encoding: the request is
/// HTTP/1.0).
pub fn parse_http(buf: &[u8]) -> Result<serde_json::Value, String> {
    let split = buf
        .windows(4)
        .position(|w| w == b"\r\n\r\n")
        .ok_or("answer is not HTTP")?;
    let head = String::from_utf8_lossy(&buf[..split]);
    let status = head.lines().next().unwrap_or("");
    let code = status.split_whitespace().nth(1).unwrap_or("");
    if !status.starts_with("HTTP/") {
        return Err("answer is not HTTP".into());
    }
    if code != "200" {
        return Err(format!("HTTP status {}", cut(code)));
    }
    serde_json::from_slice(&buf[split + 4..]).map_err(|e| format!("body is not JSON: {e}"))
}

/// `systemctl show` of the given units, in order; `None` when systemctl
/// is missing or fails.
async fn units(runner: &Arc<dyn CommandRunner>, names: &[&str]) -> Option<Vec<Unit>> {
    let mut spec = CommandSpec::new(SYSTEMCTL)
        .arg("show")
        .arg("--property=Id,LoadState,ActiveState,SubState,UnitFileState")
        .arg("--");
    for n in names {
        spec = spec.arg(n);
    }
    let out = tokio::time::timeout(PROBE_TIMEOUT, runner.run(spec, OutputSink::discard()))
        .await
        .ok()?
        .ok()?;
    if !out.success() {
        return None;
    }
    let parsed = parse_show(&out.stdout_lossy());
    Some(
        names
            .iter()
            .map(|n| {
                parsed
                    .iter()
                    .find(|u| u.name == *n)
                    .cloned()
                    .unwrap_or(Unit {
                        name: n.to_string(),
                        load_state: None,
                        active_state: None,
                        sub_state: None,
                        unit_file_state: None,
                    })
            })
            .collect(),
    )
}

/// Blocks of `Key=Value` lines separated by blank lines, one per unit.
pub fn parse_show(out: &str) -> Vec<Unit> {
    let mut units = Vec::new();
    for block in out.split("\n\n") {
        let mut u = Unit {
            name: String::new(),
            load_state: None,
            active_state: None,
            sub_state: None,
            unit_file_state: None,
        };
        for line in block.lines() {
            let Some((k, v)) = line.split_once('=') else {
                continue;
            };
            let v = (!v.trim().is_empty()).then(|| cut(v.trim()));
            match k {
                "Id" => u.name = v.unwrap_or_default(),
                "LoadState" => u.load_state = v,
                "ActiveState" => u.active_state = v,
                "SubState" => u.sub_state = v,
                "UnitFileState" => u.unit_file_state = v,
                _ => {}
            }
        }
        if !u.name.is_empty() {
            units.push(u);
        }
    }
    units
}

/// First non-empty line of `<program> <arg>`, or `None`.
async fn first_line(
    runner: &Arc<dyn CommandRunner>,
    sysroot: &Path,
    program: &str,
    arg: &str,
) -> Option<String> {
    let spec = CommandSpec::new(sysroot.join(program.trim_start_matches('/'))).arg(arg);
    let out = tokio::time::timeout(PROBE_TIMEOUT, runner.run(spec, OutputSink::discard()))
        .await
        .ok()?
        .ok()?;
    if !out.success() {
        return None;
    }
    out.stdout_lossy()
        .lines()
        .map(str::trim)
        .find(|l| !l.is_empty())
        .map(cut)
}

fn is_file(sysroot: &Path, abs: &str) -> bool {
    sysroot.join(abs.trim_start_matches('/')).is_file()
}

/// The resolved path as the host sees it (without the test sysroot).
fn shown_path(sysroot: &Path, resolved: &Path) -> String {
    let root = std::fs::canonicalize(sysroot).unwrap_or_else(|_| PathBuf::from(sysroot));
    match resolved.strip_prefix(&root) {
        Ok(rel) => format!("/{}", rel.display()),
        Err(_) => resolved.display().to_string(),
    }
}

/// uid → name from `/etc/passwd`.
pub fn passwd(sysroot: &Path) -> Vec<(u32, String)> {
    std::fs::read_to_string(sysroot.join("etc/passwd"))
        .unwrap_or_default()
        .lines()
        .filter_map(|l| {
            let mut f = l.split(':');
            let name = f.next()?;
            let uid = f.nth(1)?.parse().ok()?;
            (!name.is_empty()).then(|| (uid, cut(name)))
        })
        .collect()
}

fn cut(s: &str) -> String {
    s.chars()
        .filter(|c| !c.is_control() && !is_format(*c))
        .take(MAX_TEXT)
        .collect()
}

/// Invisible and direction-changing characters (Unicode Cf) that would let
/// a user's socket make a value look like something else.
fn is_format(c: char) -> bool {
    matches!(c, '\u{00AD}' | '\u{061C}' | '\u{180E}' | '\u{200B}'..='\u{200F}' | '\u{202A}'..='\u{202E}' | '\u{2060}'..='\u{2064}' | '\u{2066}'..='\u{206F}' | '\u{FEFF}' | '\u{FFF9}'..='\u{FFFB}')
}

#[cfg(test)]
mod tests;
