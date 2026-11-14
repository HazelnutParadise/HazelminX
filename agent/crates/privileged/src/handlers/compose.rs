//! `compose_check` and `compose_deploy` (ticket 044.2, decision 0035).
//!
//! The files of one revision come from the agent's spool as
//! `<spool>/<revision>/<sha256>`; nothing else of the agent's is read, every
//! component is opened without following links, and a file is used only when
//! its size and hash match the signed manifest. The executor lays the files
//! out in a directory of its own, asks the native `docker compose config`
//! what the project means, and checks the host against that answer, never
//! against paths, ports or images a caller sent. `compose_deploy` then writes
//! the revision under `/var/lib/hazelminx/compose/<project>/revisions/` and
//! runs `docker compose up`.
//!
//! The output of `config` and of registry lookups can contain interpolated
//! values (secrets among them), so it is parsed here and never forwarded;
//! only `up` streams its output.

use crate::action::COMPOSE_ROOT;
use crate::command::{CommandRunner, CommandSpec, OutputSink, StreamKind};
use crate::compose_manifest::{ComposeManifest, ComposeProjectName, EnvironmentId};
use crate::executor::{ActionError, ActionResult};
use crate::handlers::engines::{ProbeError, get_json};
use crate::ledger::entry::{ComposeContainer, ComposeState};
use crate::ledger::probe::under;
use crate::ledger::snapshot::listening;
use serde::Serialize;
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::collections::BTreeSet;
use std::ffi::CString;
use std::fs::{File, OpenOptions};
use std::io::{Read, Write};
use std::os::fd::{AsRawFd, FromRawFd};
use std::os::unix::ffi::OsStrExt;
use std::os::unix::fs::{DirBuilderExt, MetadataExt, OpenOptionsExt, PermissionsExt};
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::Duration;

pub const DOCKER_BINS: &[&str] = &["/usr/bin/docker", "/usr/local/bin/docker"];
/// Free space below this on the engine's or the platform's filesystem stops
/// the deployment; below [`DISK_WARN_BYTES`] it is a warning.
pub const DISK_MIN_BYTES: u64 = 1 << 30;
pub const DISK_WARN_BYTES: u64 = 5 << 30;
pub const CONFIG_TIMEOUT: Duration = Duration::from_secs(60);
pub const IMAGE_TIMEOUT: Duration = Duration::from_secs(30);
/// Images looked up in registries per check; the rest are reported unchecked.
pub const MAX_REMOTE_IMAGES: usize = 50;
/// Longest text kept from a command's error output.
pub const MAX_ERROR_TEXT: usize = 2000;

/// Where the spool is and whose it must be (from the executor's config).
#[derive(Debug, Clone)]
pub struct ComposeConfig {
    /// Absolute; the agent's `<state_dir>/compose-spool`.
    pub spool_dir: PathBuf,
    /// The agent's uid; the spool must belong to it. `None` refuses.
    pub agent_uid: Option<u32>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct Check {
    /// `error` blocks, `warning` does not, `ok` passed.
    pub severity: &'static str,
    pub code: &'static str,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub service: Option<String>,
    pub detail: String,
}

fn check(
    severity: &'static str,
    code: &'static str,
    service: Option<&str>,
    detail: String,
) -> Check {
    Check {
        severity,
        code,
        service: service.map(str::to_string),
        detail,
    }
}

fn fail(m: impl Into<String>) -> ActionError {
    ActionError::new(m)
}

// ---- engine -------------------------------------------------------------

/// The engine socket of a root-owned environment, checked: a socket (not a
/// link), owned by root.
pub fn engine_socket(sysroot: &Path, env: &EnvironmentId) -> Result<PathBuf, String> {
    let abs = env
        .as_str()
        .strip_prefix("env:")
        .expect("validated environment id");
    let p = under(sysroot, Path::new(abs));
    let md = std::fs::symlink_metadata(&p)
        .map_err(|e| format!("engine socket {abs}: {e}; is the engine running?"))?;
    use std::os::unix::fs::FileTypeExt;
    if !md.file_type().is_socket() {
        return Err(format!("{abs} is not a socket"));
    }
    if md.uid() != 0 {
        return Err(format!("{abs} is owned by uid {}, not root", md.uid()));
    }
    Ok(p)
}

fn probe_err(e: ProbeError) -> String {
    match e {
        ProbeError::Denied(m) | ProbeError::Other(m) => m,
    }
}

/// The containers labelled with `project` (the ledger's `ComposeProject`
/// state).
pub async fn project_state(
    sysroot: &Path,
    env: &EnvironmentId,
    project: &ComposeProjectName,
) -> Result<ComposeState, String> {
    let sock = engine_socket(sysroot, env)?;
    // The name is [a-z0-9-]: nothing in it needs escaping beyond the JSON
    // punctuation encoded here.
    let path = format!(
        "/containers/json?all=1&filters=%7B%22label%22%3A%5B%22com.docker.compose.project%3D{}%22%5D%7D",
        project.as_str()
    );
    let v = get_json(&sock, &path, 0).await.map_err(probe_err)?;
    let mut state = parse_containers(&v)?;
    let label = format!(
        "filters=%7B%22label%22%3A%5B%22com.docker.compose.project%3D{}%22%5D%7D",
        project.as_str()
    );
    let vols = get_json(&sock, &format!("/volumes?{label}"), 0)
        .await
        .map_err(probe_err)?;
    let nets = get_json(&sock, &format!("/networks?{label}"), 0)
        .await
        .map_err(probe_err)?;
    state.volumes = names(vols.get("Volumes").unwrap_or(&Value::Null));
    state.networks = names(&nets);
    state.exists = state.exists || !state.volumes.is_empty() || !state.networks.is_empty();
    state.removal = format!(
        "docker compose --project-name {} down --volumes --rmi local (on {}), then remove {COMPOSE_ROOT}/{}",
        project.as_str(),
        env.as_str(),
        project.as_str()
    );
    Ok(state)
}

fn names(v: &Value) -> Vec<String> {
    let mut out: Vec<String> = v
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(|x| x.get("Name").and_then(Value::as_str).map(str::to_string))
        .collect();
    out.sort();
    out
}

pub fn parse_containers(v: &Value) -> Result<ComposeState, String> {
    let list = v.as_array().ok_or("container list is not an array")?;
    let s = |c: &Value, k: &str| c.get(k).and_then(Value::as_str).unwrap_or("").to_string();
    let mut containers: Vec<ComposeContainer> = list
        .iter()
        .map(|c| ComposeContainer {
            id: s(c, "Id"),
            name: c
                .get("Names")
                .and_then(|n| n.get(0))
                .and_then(Value::as_str)
                .unwrap_or("")
                .trim_start_matches('/')
                .to_string(),
            service: c
                .pointer("/Labels/com.docker.compose.service")
                .and_then(Value::as_str)
                .unwrap_or("")
                .to_string(),
            image: s(c, "Image"),
            image_id: s(c, "ImageID"),
            state: s(c, "State"),
        })
        .collect();
    containers.sort_by(|a, b| a.name.cmp(&b.name));
    Ok(ComposeState {
        exists: !containers.is_empty(),
        containers,
        ..Default::default()
    })
}

/// Host ports published by the project's own containers, which a
/// redeployment takes over rather than conflicts with.
fn own_ports(v: &Value) -> BTreeSet<(String, u16)> {
    let mut out = BTreeSet::new();
    for c in v.as_array().into_iter().flatten() {
        for p in c
            .get("Ports")
            .and_then(Value::as_array)
            .into_iter()
            .flatten()
        {
            if let (Some(port), Some(proto)) = (
                p.get("PublicPort").and_then(Value::as_u64),
                p.get("Type").and_then(Value::as_str),
            ) {
                out.insert((proto.to_string(), port as u16));
            }
        }
    }
    out
}

fn docker_bin(sysroot: &Path) -> Result<PathBuf, String> {
    DOCKER_BINS
        .iter()
        .find(|b| under(sysroot, Path::new(b)).is_file())
        .map(PathBuf::from)
        .ok_or_else(|| {
            "the docker CLI is not installed; installing it is not available yet (ticket 039.2)"
                .into()
        })
}

// ---- spool --------------------------------------------------------------

fn cstr(s: &str) -> CString {
    CString::new(s).expect("validated names have no NUL")
}

/// Opens `name` under `dir` with `flags | O_NOFOLLOW | O_CLOEXEC`.
fn open_at(dir: &File, name: &str, flags: libc::c_int) -> std::io::Result<File> {
    let c = cstr(name);
    // SAFETY: dir is an open descriptor, c a valid C string; the returned
    // descriptor is owned by the File.
    let fd = unsafe {
        libc::openat(
            dir.as_raw_fd(),
            c.as_ptr(),
            flags | libc::O_NOFOLLOW | libc::O_CLOEXEC,
        )
    };
    if fd < 0 {
        return Err(std::io::Error::last_os_error());
    }
    Ok(unsafe { File::from_raw_fd(fd) })
}

/// A directory of the agent's: its own, writable by nobody else.
fn agent_dir(f: &File, uid: u32, what: &str) -> Result<(), String> {
    let md = f.metadata().map_err(|e| format!("{what}: {e}"))?;
    if !md.is_dir() {
        return Err(format!("{what} is not a directory"));
    }
    if md.uid() != uid {
        return Err(format!(
            "{what} is owned by uid {}, not the agent",
            md.uid()
        ));
    }
    if md.mode() & 0o022 != 0 {
        return Err(format!("{what} is writable by others"));
    }
    Ok(())
}

/// The revision's files from the spool, checked against the manifest.
pub fn read_spool(cfg: &ComposeConfig, m: &ComposeManifest) -> Result<Vec<Vec<u8>>, String> {
    let uid = cfg
        .agent_uid
        .ok_or("the executor has no agent uid configured; it cannot trust a spool")?;
    let spool = OpenOptions::new()
        .read(true)
        .custom_flags(libc::O_DIRECTORY | libc::O_NOFOLLOW)
        .open(&cfg.spool_dir)
        .map_err(|e| {
            format!(
                "compose spool {}: {e}; compose_spool_dir in the executor's config must be the agent's <state_dir>/compose-spool",
                cfg.spool_dir.display()
            )
        })?;
    agent_dir(&spool, uid, "the compose spool")?;
    let rev = open_at(
        &spool,
        m.revision.as_str(),
        libc::O_RDONLY | libc::O_DIRECTORY,
    )
    .map_err(|e| format!("spool of revision {}: {e}", m.revision.as_str()))?;
    agent_dir(&rev, uid, "the revision's spool")?;
    let mut out = Vec::with_capacity(m.files.len());
    for f in &m.files {
        let what = format!("staged file {}", f.path.as_str());
        let mut file = open_at(&rev, f.sha256.as_str(), libc::O_RDONLY | libc::O_NONBLOCK)
            .map_err(|e| format!("{what}: {e}"))?;
        let md = file.metadata().map_err(|e| format!("{what}: {e}"))?;
        if !md.is_file() {
            return Err(format!("{what} is not a regular file"));
        }
        if md.len() != f.size {
            return Err(format!(
                "{what} is {} bytes, the manifest says {}",
                md.len(),
                f.size
            ));
        }
        let mut buf = Vec::with_capacity(f.size as usize);
        (&mut file)
            .take(f.size + 1)
            .read_to_end(&mut buf)
            .map_err(|e| format!("{what}: {e}"))?;
        if buf.len() as u64 != f.size || hex::encode(Sha256::digest(&buf)) != f.sha256.as_str() {
            return Err(format!("{what} does not match the signed manifest"));
        }
        out.push(buf);
    }
    Ok(out)
}

// ---- layout -------------------------------------------------------------

/// A directory that root owns and nobody else can write, created with
/// `mode` if missing. A link, another owner or a writable mode is refused.
fn root_dir(p: &Path, mode: u32) -> Result<(), String> {
    match std::fs::symlink_metadata(p) {
        Ok(md) => {
            if !md.is_dir() {
                return Err(format!("{} exists and is not a directory", p.display()));
            }
            if md.uid() != 0 && unsafe { libc::geteuid() } == 0 {
                return Err(format!("{} is not owned by root", p.display()));
            }
            if md.mode() & 0o022 != 0 {
                return Err(format!("{} is writable by others", p.display()));
            }
            Ok(())
        }
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
            std::fs::DirBuilder::new()
                .mode(mode)
                .create(p)
                .map_err(|e| format!("create {}: {e}", p.display()))?;
            // The umask may have narrowed the mode; set it as asked.
            std::fs::set_permissions(p, std::fs::Permissions::from_mode(mode))
                .map_err(|e| format!("chmod {}: {e}", p.display()))
        }
        Err(e) => Err(format!("stat {}: {e}", p.display())),
    }
}

/// Every directory from `base` (which must exist and be checked) down to
/// `rel`'s parent, then the file itself, created exclusively.
fn write_files(base: &Path, m: &ComposeManifest, contents: &[Vec<u8>]) -> Result<(), String> {
    for (f, data) in m.files.iter().zip(contents) {
        let rel = f.path.as_str();
        let mut dir = base.to_path_buf();
        if let Some((parents, _)) = rel.rsplit_once('/') {
            for seg in parents.split('/') {
                dir.push(seg);
                root_dir(&dir, 0o700)?;
            }
        }
        let target = base.join(rel);
        let mut out = OpenOptions::new()
            .write(true)
            .create_new(true)
            .mode(0o600)
            .custom_flags(libc::O_NOFOLLOW)
            .open(&target)
            .map_err(|e| format!("write {rel}: {e}"))?;
        out.write_all(data)
            .and_then(|_| out.sync_all())
            .map_err(|e| format!("write {rel}: {e}"))?;
    }
    Ok(())
}

/// True when `dir`'s files match the manifest exactly (a step retried after
/// its revision was written).
fn same_files(dir: &Path, m: &ComposeManifest) -> bool {
    m.files.iter().all(|f| {
        let p = dir.join(f.path.as_str());
        let Ok(md) = std::fs::symlink_metadata(&p) else {
            return false;
        };
        md.is_file()
            && md.len() == f.size
            && std::fs::read(&p).is_ok_and(|b| hex::encode(Sha256::digest(&b)) == f.sha256.as_str())
    })
}

/// A private temporary directory for `compose_check`.
struct TempDir(PathBuf);

impl TempDir {
    fn new() -> Result<Self, String> {
        let mut tmpl = b"/tmp/hazelminx-compose-check-XXXXXX\0".to_vec();
        // SAFETY: tmpl is a writable NUL-terminated template.
        let p = unsafe { libc::mkdtemp(tmpl.as_mut_ptr().cast()) };
        if p.is_null() {
            return Err(format!("mkdtemp: {}", std::io::Error::last_os_error()));
        }
        tmpl.pop();
        Ok(TempDir(PathBuf::from(std::ffi::OsStr::from_bytes(&tmpl))))
    }
}

impl Drop for TempDir {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}

// ---- compose ------------------------------------------------------------

/// What the native Compose made of the files.
#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct Resolved {
    pub services: Vec<ResolvedService>,
}

#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct ResolvedService {
    pub name: String,
    pub image: Option<String>,
    pub build: bool,
    /// (protocol, host port, host ip).
    pub ports: Vec<(String, u16, String)>,
    /// Absolute bind sources.
    pub binds: Vec<String>,
    /// High-risk settings, by Core's rules (044.1): what needs the owner.
    pub risks: Vec<String>,
}

/// Host paths whose mount gives a container control of the host; the same
/// list as Core's `sensitiveHostPaths` (core/internal/projects/compose.go).
pub const SENSITIVE_HOST_PATHS: &[&str] = &[
    "/",
    "/etc",
    "/root",
    "/proc",
    "/sys",
    "/dev",
    "/boot",
    "/var/lib/docker",
    "/var/lib/containers",
    "/run",
    "/var/run",
];
pub const ENGINE_SOCKETS: &[&str] = &["docker.sock", "podman.sock", "containerd.sock"];

/// Core's high-risk rules applied to one resolved service. `dir` is the
/// revision's directory: binds inside it are the unit's own files.
fn risks_of(s: &Value, binds: &[String], dir: &Path) -> Vec<String> {
    let mut out = Vec::new();
    if s.get("privileged").and_then(Value::as_bool) == Some(true) {
        out.push("privileged".into());
    }
    for k in ["network_mode", "pid", "ipc", "userns_mode", "cgroup"] {
        if s.get(k).and_then(Value::as_str) == Some("host") {
            out.push(format!("{k}: host"));
        }
    }
    for k in ["cap_add", "devices"] {
        if s.get(k)
            .and_then(Value::as_array)
            .is_some_and(|a| !a.is_empty())
        {
            out.push(k.to_string());
        }
    }
    for o in s
        .get("security_opt")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
    {
        let o = o.as_str().unwrap_or("");
        if o.contains("unconfined") || o.starts_with("no-new-privileges:false") {
            out.push(format!("security_opt {o}"));
        }
    }
    for b in binds {
        let p = Path::new(b);
        if p.starts_with(dir) {
            continue;
        }
        if ENGINE_SOCKETS.iter().any(|e| b.ends_with(&format!("/{e}"))) {
            out.push(format!("engine socket {b}"));
        } else if SENSITIVE_HOST_PATHS
            .iter()
            .any(|s| p == Path::new(s) || (*s != "/" && p.starts_with(s)))
        {
            out.push(format!("host system path {b}"));
        }
    }
    out
}

pub fn parse_config(v: &Value, dir: &Path) -> Result<Resolved, String> {
    let services = v
        .get("services")
        .and_then(Value::as_object)
        .ok_or("compose config has no services")?;
    let mut out = Resolved::default();
    for (name, s) in services {
        let mut r = ResolvedService {
            name: name.clone(),
            image: s.get("image").and_then(Value::as_str).map(str::to_string),
            build: s.get("build").is_some(),
            ..Default::default()
        };
        for p in s
            .get("ports")
            .and_then(Value::as_array)
            .into_iter()
            .flatten()
        {
            let published = match p.get("published") {
                Some(Value::String(x)) => x.parse::<u16>().ok(),
                Some(Value::Number(x)) => x.as_u64().and_then(|n| u16::try_from(n).ok()),
                _ => None,
            };
            if let Some(port) = published.filter(|p| *p != 0) {
                let proto = p.get("protocol").and_then(Value::as_str).unwrap_or("tcp");
                let ip = p.get("host_ip").and_then(Value::as_str).unwrap_or("");
                r.ports.push((proto.to_string(), port, ip.to_string()));
            }
        }
        for m in s
            .get("volumes")
            .and_then(Value::as_array)
            .into_iter()
            .flatten()
        {
            if m.get("type").and_then(Value::as_str) == Some("bind")
                && let Some(src) = m.get("source").and_then(Value::as_str)
            {
                r.binds.push(src.to_string());
            }
        }
        r.risks = risks_of(s, &r.binds, dir);
        out.services.push(r);
    }
    Ok(out)
}

/// `docker compose` with the fixed global arguments for one revision laid
/// out in `dir`, in a clean environment.
fn compose(docker: &Path, sock: &Path, dir: &Path, m: &ComposeManifest) -> CommandSpec {
    let pdir = m.project_dir();
    let mut spec = CommandSpec::new(docker)
        .env("DOCKER_HOST", format!("unix://{}", sock.display()))
        // No HOME and no .env: Compose interpolates exactly what Core
        // analysed (044.1 reads no .env and no environment). Registry
        // credentials come from root's Docker config.
        .env("DOCKER_CONFIG", "/root/.docker")
        .arg("compose")
        .arg("--env-file")
        .arg("/dev/null")
        .arg("--progress")
        .arg("plain")
        .arg("--project-name")
        .arg(m.project.as_str())
        .arg("--project-directory")
        .arg(if pdir.is_empty() {
            dir.to_path_buf()
        } else {
            dir.join(pdir)
        });
    for f in std::iter::once(&m.main_file).chain(&m.extra_files) {
        spec = spec.arg("--file").arg(dir.join(f.as_str()));
    }
    for p in &m.profiles {
        spec = spec.arg("--profile").arg(p.as_str());
    }
    spec
}

fn tail(b: &[u8]) -> String {
    let s = String::from_utf8_lossy(b);
    let t: String = s.chars().rev().take(MAX_ERROR_TEXT).collect();
    t.chars()
        .rev()
        .filter(|c| *c == '\n' || !c.is_control())
        .collect()
}

async fn resolve(
    runner: &Arc<dyn CommandRunner>,
    docker: &Path,
    sock: &Path,
    dir: &Path,
    m: &ComposeManifest,
) -> Result<Resolved, String> {
    let spec = compose(docker, sock, dir, m)
        .arg("config")
        .arg("--format")
        .arg("json");
    let out = tokio::time::timeout(CONFIG_TIMEOUT, runner.run(spec, OutputSink::discard()))
        .await
        .map_err(|_| "docker compose config did not finish in 60 s".to_string())?
        .map_err(|e| format!("run docker compose config: {e}"))?;
    if !out.success() {
        // stderr names the file and line; it does not print values.
        return Err(format!(
            "docker compose config failed: {}",
            tail(&out.stderr)
        ));
    }
    let v: Value = serde_json::from_slice(&out.stdout)
        .map_err(|e| format!("docker compose config output is not JSON: {e}"))?;
    parse_config(&v, dir)
}

// ---- checks -------------------------------------------------------------

fn free_bytes(p: &Path) -> Option<u64> {
    let c = CString::new(p.as_os_str().as_bytes()).ok()?;
    let mut st: libc::statvfs = unsafe { std::mem::zeroed() };
    // SAFETY: valid C string and out pointer.
    (unsafe { libc::statvfs(c.as_ptr(), &mut st) } == 0)
        .then(|| (st.f_bavail as u64).saturating_mul(st.f_frsize as u64))
}

/// The nearest existing ancestor of `p` (for free space of a path not yet
/// created).
fn existing(p: &Path) -> PathBuf {
    let mut p = p.to_path_buf();
    while !p.exists() {
        if !p.pop() {
            return PathBuf::from("/");
        }
    }
    p
}

/// Which relative bind sources are not unit files: their paths relative to
/// the file root (044.1's `data-dir`).
pub fn data_dirs(dir: &Path, m: &ComposeManifest, r: &Resolved) -> Vec<String> {
    let files: Vec<&str> = m.files.iter().map(|f| f.path.as_str()).collect();
    let mut out = BTreeSet::new();
    for s in &r.services {
        for b in &s.binds {
            let Ok(rel) = Path::new(b).strip_prefix(dir) else {
                continue;
            };
            let rel = rel.to_string_lossy().to_string();
            if rel.is_empty() {
                continue; // the whole file root: unit files
            }
            let is_source = files
                .iter()
                .any(|f| *f == rel || f.starts_with(&format!("{rel}/")));
            if !is_source && !rel.split('/').any(|s| s == "..") {
                out.insert(rel);
            }
        }
    }
    out.into_iter().collect()
}

#[allow(clippy::too_many_arguments)]
async fn host_checks(
    runner: &Arc<dyn CommandRunner>,
    sysroot: &Path,
    docker: &Path,
    sock: &Path,
    dir: &Path,
    m: &ComposeManifest,
    r: &Resolved,
    project_root: &Path,
    deploying: bool,
) -> Vec<Check> {
    let mut checks = Vec::new();

    // High-risk settings as native Compose resolved them. Core's plan asked
    // the owner when its own analysis found them; anything it did not see
    // (or that was not approved) is refused here.
    for sv in &r.services {
        for risk in &sv.risks {
            let (sev, detail) = match (m.host_risk_approved, deploying) {
                (true, _) => (
                    "warning",
                    format!("{risk}: approved by the owner in the change plan"),
                ),
                (false, true) => (
                    "error",
                    format!(
                        "{risk}: high-risk setting without the owner's approval; nothing was started"
                    ),
                ),
                (false, false) => (
                    "warning",
                    format!("{risk}: needs the owner's approval in the change plan"),
                ),
            };
            checks.push(check(sev, "HOST_RISK", Some(&sv.name), detail));
        }
    }

    // Ownership of the project name.
    let filter = format!(
        "/containers/json?all=1&filters=%7B%22label%22%3A%5B%22com.docker.compose.project%3D{}%22%5D%7D",
        m.project.as_str()
    );
    let running = match get_json(sock, &filter, 0).await {
        Ok(v) => v,
        Err(e) => {
            checks.push(check("error", "ENGINE_UNREACHABLE", None, probe_err(e)));
            return checks;
        }
    };
    let ours = project_root.join("revisions").is_dir();
    if running.as_array().is_some_and(|a| !a.is_empty()) && !ours {
        checks.push(check(
            "error",
            "PROJECT_NOT_PLATFORM_OWNED",
            None,
            format!(
                "containers of a Compose project named {} already run on this engine and were not deployed by HazelminX; taking them over is ticket 042",
                m.project.as_str()
            ),
        ));
    }

    // Disk space: the engine's data root and the platform's directory.
    let docker_root = get_json(sock, "/info", 0).await.ok().and_then(|v| {
        v.get("DockerRootDir")
            .and_then(Value::as_str)
            .map(PathBuf::from)
    });
    let mut disks = vec![("the platform's Compose directory", existing(project_root))];
    if let Some(d) = docker_root {
        disks.push(("the engine's data directory", existing(&under(sysroot, &d))));
    }
    for (what, p) in disks {
        match free_bytes(&p) {
            Some(f) if f < DISK_MIN_BYTES => checks.push(check(
                "error",
                "DISK_LOW",
                None,
                format!(
                    "{what} ({}) has {f} bytes free; at least {DISK_MIN_BYTES} are needed",
                    p.display()
                ),
            )),
            Some(f) if f < DISK_WARN_BYTES => checks.push(check(
                "warning",
                "DISK_LOW",
                None,
                format!("{what} ({}) has {f} bytes free", p.display()),
            )),
            Some(_) => {}
            None => checks.push(check(
                "warning",
                "DISK_UNKNOWN",
                None,
                format!("free space of {what} ({}) cannot be read", p.display()),
            )),
        }
    }

    // Host port conflicts with other listeners.
    let own = own_ports(&running);
    match listening(sysroot) {
        Ok(ls) => {
            for s in &r.services {
                for (proto, port, ip) in &s.ports {
                    if own.contains(&(proto.clone(), *port)) {
                        continue;
                    }
                    let clash = ls.iter().find(|l| {
                        l.port == *port
                            && l.proto.trim_end_matches('6') == proto
                            && (ip.is_empty()
                                || ip == "0.0.0.0"
                                || ip == "::"
                                || l.address == *ip
                                || l.address == "0.0.0.0"
                                || l.address == "::")
                    });
                    if let Some(l) = clash {
                        checks.push(check(
                            "error",
                            "PORT_IN_USE",
                            Some(&s.name),
                            format!(
                                "host port {proto}/{port} is already listened on at {}",
                                l.address
                            ),
                        ));
                    }
                }
            }
        }
        Err(e) => checks.push(check(
            "warning",
            "PORTS_UNKNOWN",
            None,
            format!("listening ports cannot be read: {e}"),
        )),
    }

    // Bind mount sources outside the revision must exist; Docker would
    // otherwise create an empty directory.
    for s in &r.services {
        for b in &s.binds {
            if Path::new(b).starts_with(dir) {
                continue;
            }
            if std::fs::symlink_metadata(under(sysroot, Path::new(b))).is_err() {
                checks.push(check(
                    "error",
                    "BIND_SOURCE_MISSING",
                    Some(&s.name),
                    format!("bind mount source {b} does not exist on the host"),
                ));
            }
        }
    }

    // Images: built ones need no lookup; others must be local or in a
    // registry.
    let mut remote = 0;
    for s in &r.services {
        let Some(img) = s.image.as_deref() else {
            continue;
        };
        if s.build {
            continue;
        }
        if img.starts_with('-') || img.chars().any(char::is_whitespace) {
            checks.push(check(
                "error",
                "IMAGE_INVALID",
                Some(&s.name),
                "image reference is not valid".into(),
            ));
            continue;
        }
        let local = get_json(sock, &format!("/images/{}/json", urlencode(img)), 0)
            .await
            .is_ok();
        if local {
            continue;
        }
        remote += 1;
        if remote > MAX_REMOTE_IMAGES {
            checks.push(check(
                "warning",
                "IMAGE_UNCHECKED",
                Some(&s.name),
                format!("{img} was not looked up (more than {MAX_REMOTE_IMAGES} images)"),
            ));
            continue;
        }
        let spec = CommandSpec::new(docker)
            .env("DOCKER_HOST", format!("unix://{}", sock.display()))
            .env("DOCKER_CONFIG", "/root/.docker")
            .arg("manifest")
            .arg("inspect")
            .arg(img);
        match tokio::time::timeout(IMAGE_TIMEOUT, runner.run(spec, OutputSink::discard())).await {
            Ok(Ok(o)) if o.success() => {}
            Ok(Ok(o)) => checks.push(check(
                "error",
                "IMAGE_UNAVAILABLE",
                Some(&s.name),
                format!(
                    "{img} is not on this host and cannot be found in its registry: {}",
                    tail(&o.stderr)
                ),
            )),
            Ok(Err(e)) => checks.push(check(
                "warning",
                "IMAGE_UNCHECKED",
                Some(&s.name),
                format!("{img}: {e}"),
            )),
            Err(_) => checks.push(check(
                "warning",
                "IMAGE_UNCHECKED",
                Some(&s.name),
                format!("{img}: registry did not answer in 30 s"),
            )),
        }
    }
    checks
}

fn urlencode(s: &str) -> String {
    s.bytes()
        .map(|b| match b {
            b'a'..=b'z' | b'A'..=b'Z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                (b as char).to_string()
            }
            _ => format!("%{b:02X}"),
        })
        .collect()
}

fn summary(r: &Resolved) -> Value {
    json!(r.services.iter().map(|s| json!({
        "name": s.name,
        "image": s.image,
        "build": s.build,
        "ports": s.ports.iter().map(|(p, n, ip)| json!({"protocol": p, "published": n, "hostIp": ip})).collect::<Vec<_>>(),
    })).collect::<Vec<_>>())
}

fn blocked(checks: &[Check]) -> bool {
    checks.iter().any(|c| c.severity == "error")
}

// ---- actions ------------------------------------------------------------

struct Prepared {
    docker: PathBuf,
    sock: PathBuf,
    contents: Vec<Vec<u8>>,
}

fn prepare(
    sysroot: &Path,
    cfg: &ComposeConfig,
    m: &ComposeManifest,
) -> Result<Prepared, ActionError> {
    let docker = docker_bin(sysroot).map_err(fail)?;
    let sock = engine_socket(sysroot, &m.environment).map_err(fail)?;
    let contents = read_spool(cfg, m).map_err(fail)?;
    Ok(Prepared {
        docker,
        sock,
        contents,
    })
}

/// `compose_check`: read-only.
pub async fn check_only(
    runner: Arc<dyn CommandRunner>,
    sysroot: &Path,
    cfg: &ComposeConfig,
    m: ComposeManifest,
) -> ActionResult {
    let p = prepare(sysroot, cfg, &m)?;
    let tmp = TempDir::new().map_err(fail)?;
    write_files(&tmp.0, &m, &p.contents).map_err(fail)?;
    let project_root = under(sysroot, Path::new(COMPOSE_ROOT)).join(m.project.as_str());
    let r = match resolve(&runner, &p.docker, &p.sock, &tmp.0, &m).await {
        Ok(r) => r,
        Err(e) => {
            return Ok(
                json!({"ready": false, "checks": [check("error", "COMPOSE_INVALID", None, e)], "services": []}),
            );
        }
    };
    let checks = host_checks(
        &runner,
        sysroot,
        &p.docker,
        &p.sock,
        &tmp.0,
        &m,
        &r,
        &project_root,
        false,
    )
    .await;
    Ok(json!({
        "ready": !blocked(&checks),
        "checks": checks,
        "services": summary(&r),
        "dataDirs": data_dirs(&tmp.0, &m, &r),
    }))
}

/// `compose_deploy`: the checks again (the host may have changed since
/// `compose_check`), then the revision directory, the data directories and
/// `docker compose up`.
pub async fn deploy(
    runner: Arc<dyn CommandRunner>,
    sysroot: &Path,
    cfg: &ComposeConfig,
    m: ComposeManifest,
    sink: OutputSink,
) -> ActionResult {
    let p = prepare(sysroot, cfg, &m)?;
    let root = under(sysroot, Path::new(COMPOSE_ROOT));
    let project_root = root.join(m.project.as_str());
    let revisions = project_root.join("revisions");
    let rev = revisions.join(m.revision.as_str());
    let data = project_root.join("data");

    // The platform's own directory comes with the install; the Compose
    // directories below it are created here (declared in the ledger).
    let platform = root.parent().expect("COMPOSE_ROOT has a parent");
    if !platform.is_dir() {
        return Err(fail(format!(
            "{} does not exist; the platform is not installed on this host",
            platform.display()
        )));
    }
    for (d, mode) in [(&root, 0o755), (&project_root, 0o700), (&revisions, 0o700)] {
        root_dir(d, mode).map_err(fail)?;
    }
    // Lay out (or, on a retried step, recognise) the revision before the
    // checks: Compose resolves paths against it.
    let existed = rev.exists();
    if existed {
        root_dir(&rev, 0o700).map_err(fail)?;
        if !same_files(&rev, &m) {
            return Err(fail(format!(
                "revision {} already exists with other files; a revision is never rewritten",
                m.revision.as_str()
            )));
        }
    } else {
        root_dir(&rev, 0o700).map_err(fail)?;
        if let Err(e) = write_files(&rev, &m, &p.contents) {
            let _ = std::fs::remove_dir_all(&rev);
            return Err(fail(e));
        }
    }
    let r = resolve(&runner, &p.docker, &p.sock, &rev, &m)
        .await
        .map_err(fail)?;
    let checks = host_checks(
        &runner,
        sysroot,
        &p.docker,
        &p.sock,
        &rev,
        &m,
        &r,
        &project_root,
        true,
    )
    .await;
    if blocked(&checks) {
        if !existed {
            let _ = std::fs::remove_dir_all(&rev);
        }
        return Err(fail(format!(
            "host pre-checks failed; nothing was started: {}",
            checks
                .iter()
                .filter(|c| c.severity == "error")
                .map(|c| c.detail.as_str())
                .collect::<Vec<_>>()
                .join("; ")
        )));
    }

    // Persistent data: one directory per data path directly under
    // <project>/data/ (the relative path with '/' and '%' escaped), linked
    // from the revision. Containers write inside those directories but not
    // in data/ itself, which only root can write, so nothing a container
    // plants can redirect a later mount. A data path inside another data
    // path is refused: its link would have to be created through a
    // directory a container controls.
    root_dir(&data, 0o755).map_err(fail)?;
    let dirs = data_dirs(&rev, &m, &r);
    if let Some((a, b)) = dirs
        .iter()
        .flat_map(|a| dirs.iter().map(move |b| (a, b)))
        .find(|(a, b)| b.starts_with(&format!("{a}/")))
    {
        return Err(fail(format!(
            "data path {b} is inside data path {a}; mount one of them, or use a named volume"
        )));
    }
    for rel in dirs {
        let target = data.join(rel.replace('%', "%25").replace('/', "%2F"));
        match std::fs::symlink_metadata(&target) {
            Ok(md) if md.is_dir() && !md.file_type().is_symlink() => {}
            Ok(_) => {
                return Err(fail(format!(
                    "data path {rel} is not a directory on the host (a link or a file); refusing to mount it"
                )));
            }
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
                std::fs::DirBuilder::new()
                    .mode(0o755)
                    .create(&target)
                    .map_err(|e| fail(format!("create data directory {rel}: {e}")))?;
            }
            Err(e) => return Err(fail(format!("data directory {rel}: {e}"))),
        }
        let link = rev.join(&rel);
        if let Some(parent) = link.parent() {
            let mut cur = rev.clone();
            for seg in parent.strip_prefix(&rev).unwrap_or(Path::new("")).iter() {
                cur.push(seg);
                root_dir(&cur, 0o700).map_err(fail)?;
            }
        }
        match std::fs::symlink_metadata(&link) {
            Ok(md)
                if md.file_type().is_symlink()
                    && std::fs::read_link(&link).ok().as_deref() == Some(target.as_path()) => {}
            Ok(_) => {
                return Err(fail(format!(
                    "{rel} in the revision is not the expected data link"
                )));
            }
            Err(_) => std::os::unix::fs::symlink(&target, &link)
                .map_err(|e| fail(format!("link data directory {rel}: {e}")))?,
        }
    }

    sink.send(
        StreamKind::Stdout,
        format!(
            "deploying revision {} of {}\n",
            m.revision.as_str(),
            m.project.as_str()
        )
        .into_bytes(),
    )
    .await;
    let spec = compose(&p.docker, &p.sock, &rev, &m)
        .arg("up")
        .arg("--detach")
        .arg("--build")
        .arg("--remove-orphans");
    let out = runner
        .run(spec, sink)
        .await
        .map_err(|e| fail(format!("run docker compose up: {e}")))?;
    let state = project_state(sysroot, &m.environment, &m.project).await;
    let containers = match &state {
        Ok(s) => json!(s.containers),
        Err(e) => json!({"error": e}),
    };
    if !out.success() {
        return Err(fail(format!(
            "docker compose up exited with {}; the containers may be partly updated, see the host ledger for their state before and after: {}",
            out.exit_code.map_or("a signal".into(), |c| c.to_string()),
            tail(&out.stderr)
        )));
    }
    Ok(json!({
        "revision": m.revision.as_str(),
        "directory": format!("{COMPOSE_ROOT}/{}/revisions/{}", m.project.as_str(), m.revision.as_str()),
        "checks": checks,
        "services": summary(&r),
        "containers": containers,
    }))
}

#[cfg(test)]
mod tests;
