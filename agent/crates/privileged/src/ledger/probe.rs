//! How the state of each [`Target`] is read, before and after a change.
//! Paths and accounts are read from files under `sysroot`; units and
//! packages ask systemd and the package database through the argv-only
//! [`CommandRunner`]. A probe that cannot tell the state is an error, never a
//! guess, and the change it was for does not run.

use super::entry::{
    Backup, GroupState, PackageDb, PackageState, PathKind, PathState, Target, TargetState,
    UnitState, UserState,
};
use super::store::{random_id, sync_dir};
use crate::BoxFuture;
use crate::command::{CommandRunner, CommandSpec, OutputSink};
use crate::handlers::systemd::SYSTEMCTL;
use sha2::{Digest, Sha256};
use std::fs::{File, OpenOptions};
use std::io::{self, Read, Write};
use std::os::unix::fs::{MetadataExt, OpenOptionsExt};
use std::path::{Path, PathBuf};
use std::sync::Arc;

/// Files up to this size have their original content kept.
pub const BACKUP_LIMIT_BYTES: u64 = 64 * 1024 * 1024;
pub const DPKG_QUERY: &str = "/usr/bin/dpkg-query";
pub const RPM: &str = "/usr/bin/rpm";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Capture {
    /// Before a change: files are also backed up.
    Before,
    After,
}

pub trait Probe: Send + Sync {
    fn probe(&self, target: &Target, capture: Capture)
    -> BoxFuture<'static, Result<TargetState, String>>;
}

/// The real probe. `sysroot` is `/` in production.
pub struct HostProbe {
    runner: Arc<dyn CommandRunner>,
    sysroot: PathBuf,
    /// Where file backups go (`<ledger>/backups`); `None` skips backups.
    backups: Option<PathBuf>,
}

impl HostProbe {
    pub fn new(
        runner: Arc<dyn CommandRunner>,
        sysroot: impl Into<PathBuf>,
        backups: Option<PathBuf>,
    ) -> Self {
        Self {
            runner,
            sysroot: sysroot.into(),
            backups,
        }
    }
}

impl Probe for HostProbe {
    fn probe(
        &self,
        target: &Target,
        capture: Capture,
    ) -> BoxFuture<'static, Result<TargetState, String>> {
        let target = target.clone();
        let runner = self.runner.clone();
        let sysroot = self.sysroot.clone();
        let backups = match capture {
            Capture::Before => self.backups.clone(),
            Capture::After => None,
        };
        Box::pin(async move {
            match target {
                Target::Path { path } => {
                    let full = under(&sysroot, path.as_path());
                    tokio::task::spawn_blocking(move || probe_path(&full, backups.as_deref()))
                        .await
                        .map_err(|e| format!("path probe task: {e}"))?
                        .map(TargetState::Path)
                }
                Target::SystemdUnit { unit } => probe_unit(runner, unit.as_str())
                    .await
                    .map(TargetState::SystemdUnit),
                Target::Package { name } => probe_package(runner, &sysroot, name.as_str())
                    .await
                    .map(TargetState::Package),
                Target::User { name } => {
                    read_accounts(&sysroot).map(|a| TargetState::User(a.user(name.as_str())))
                }
                Target::Group { name } => {
                    read_accounts(&sysroot).map(|a| TargetState::Group(a.group(name.as_str())))
                }
            }
        })
    }
}

/// `sysroot` + an absolute host path.
pub fn under(sysroot: &Path, abs: &Path) -> PathBuf {
    sysroot.join(abs.strip_prefix("/").unwrap_or(abs))
}

/// Reads a path without following it. Regular files are hashed; with
/// `backups`, their content is also copied to `backups/<sha256>`.
pub fn probe_path(full: &Path, backups: Option<&Path>) -> Result<PathState, String> {
    let md = match std::fs::symlink_metadata(full) {
        Ok(md) => md,
        Err(e) if e.kind() == io::ErrorKind::NotFound => return Ok(PathState::default()),
        Err(e) => return Err(format!("stat {}: {e}", full.display())),
    };
    let ft = md.file_type();
    let mut state = PathState {
        exists: true,
        kind: Some(if ft.is_file() {
            PathKind::File
        } else if ft.is_dir() {
            PathKind::Directory
        } else if ft.is_symlink() {
            PathKind::Symlink
        } else {
            PathKind::Other
        }),
        mode: Some(md.mode() & 0o7777),
        uid: Some(md.uid()),
        gid: Some(md.gid()),
        ..Default::default()
    };
    if ft.is_symlink() {
        state.link_target = Some(
            std::fs::read_link(full).map_err(|e| format!("readlink {}: {e}", full.display()))?,
        );
    }
    if ft.is_file() {
        let (sha, size, backup) = hash_file(full, &md, backups)?;
        state.sha256 = Some(sha);
        state.size = Some(size);
        state.backup = backup;
    }
    Ok(state)
}

/// Hashes (and optionally backs up) a regular file. The open handle is
/// checked against the `lstat` result so a path swapped for a symlink in
/// between is refused rather than followed.
fn hash_file(
    full: &Path,
    lstat: &std::fs::Metadata,
    backups: Option<&Path>,
) -> Result<(String, u64, Option<Backup>), String> {
    let mut f = File::open(full).map_err(|e| format!("open {}: {e}", full.display()))?;
    let md = f
        .metadata()
        .map_err(|e| format!("fstat {}: {e}", full.display()))?;
    if md.dev() != lstat.dev() || md.ino() != lstat.ino() {
        return Err(format!("{} changed while it was read", full.display()));
    }
    let keep = backups.filter(|_| md.len() <= BACKUP_LIMIT_BYTES);
    let mut tmp = match keep {
        Some(dir) => {
            let path = dir.join(format!(".{}.tmp", random_id().map_err(|e| e.to_string())?));
            let file = OpenOptions::new()
                .write(true)
                .create_new(true)
                .mode(0o600)
                .open(&path)
                .map_err(|e| format!("create backup {}: {e}", path.display()))?;
            Some((path, file))
        }
        None => None,
    };
    let mut hasher = Sha256::new();
    let mut size = 0u64;
    let mut buf = vec![0u8; 64 * 1024];
    let result = (|| -> io::Result<()> {
        loop {
            let n = f.read(&mut buf)?;
            if n == 0 {
                break;
            }
            hasher.update(&buf[..n]);
            size += n as u64;
            if let Some((_, file)) = tmp.as_mut() {
                file.write_all(&buf[..n])?;
            }
        }
        if let Some((_, file)) = tmp.as_mut() {
            file.sync_all()?;
        }
        Ok(())
    })();
    if let Err(e) = result {
        if let Some((path, _)) = &tmp {
            let _ = std::fs::remove_file(path);
        }
        return Err(format!("read {}: {e}", full.display()));
    }
    let sha = hex::encode(hasher.finalize());
    let backup = match (backups, tmp) {
        (Some(dir), Some((path, file))) => {
            drop(file);
            let dest = dir.join(&sha);
            if dest.exists() {
                let _ = std::fs::remove_file(&path);
            } else {
                std::fs::rename(&path, &dest)
                    .map_err(|e| format!("store backup {}: {e}", dest.display()))?;
                sync_dir(dir).map_err(|e| e.to_string())?;
            }
            Some(Backup::Stored)
        }
        (Some(_), None) => Some(Backup::TooLarge {
            limit_bytes: BACKUP_LIMIT_BYTES,
        }),
        _ => None,
    };
    Ok((sha, size, backup))
}

pub fn unit_show_spec(unit: &str) -> CommandSpec {
    CommandSpec::new(SYSTEMCTL)
        .arg("show")
        .arg("--property=LoadState,UnitFileState,ActiveState,SubState")
        .arg("--")
        .arg(unit)
}

async fn probe_unit(runner: Arc<dyn CommandRunner>, unit: &str) -> Result<UnitState, String> {
    let out = runner
        .run(unit_show_spec(unit), OutputSink::discard())
        .await
        .map_err(|e| format!("spawn systemctl: {e}"))?;
    if !out.success() {
        return Err(format!(
            "systemctl show exited with {:?}: {}",
            out.exit_code,
            out.stderr_lossy().trim()
        ));
    }
    Ok(parse_unit_show(&out.stdout_lossy()))
}

/// `Key=value` lines from `systemctl show`.
pub fn parse_unit_show(text: &str) -> UnitState {
    let mut s = UnitState::default();
    for line in text.lines() {
        let Some((k, v)) = line.split_once('=') else {
            continue;
        };
        let v = (!v.is_empty()).then(|| v.to_string());
        match k {
            "LoadState" => s.load_state = v,
            "UnitFileState" => s.unit_file_state = v,
            "ActiveState" => s.active_state = v,
            "SubState" => s.sub_state = v,
            _ => {}
        }
    }
    s.exists = s.load_state.as_deref().is_some_and(|l| l != "not-found");
    s
}

pub fn detect_package_db(sysroot: &Path) -> Option<PackageDb> {
    if under(sysroot, Path::new(DPKG_QUERY)).exists() {
        Some(PackageDb::Dpkg)
    } else if under(sysroot, Path::new(RPM)).exists() {
        Some(PackageDb::Rpm)
    } else {
        None
    }
}

async fn probe_package(
    runner: Arc<dyn CommandRunner>,
    sysroot: &Path,
    name: &str,
) -> Result<PackageState, String> {
    let db = detect_package_db(sysroot).ok_or("no package database (dpkg or rpm) found")?;
    let spec = match db {
        PackageDb::Dpkg => CommandSpec::new(DPKG_QUERY)
            .arg("-W")
            .arg("-f=${db:Status-Abbrev}\t${Version}\n")
            .arg("--")
            .arg(name),
        PackageDb::Rpm => CommandSpec::new(RPM)
            .arg("-q")
            .arg("--qf")
            .arg("%{EPOCH}:%{VERSION}-%{RELEASE}\n")
            .arg("--")
            .arg(name),
    };
    let out = runner
        .run(spec, OutputSink::discard())
        .await
        .map_err(|e| format!("spawn package query: {e}"))?;
    let stdout = out.stdout_lossy();
    let stderr = out.stderr_lossy();
    match (db, out.exit_code) {
        (PackageDb::Dpkg, Some(0)) => Ok(parse_dpkg_status(&stdout)),
        (PackageDb::Dpkg, Some(1)) if stderr.contains("no packages found") => Ok(PackageState {
            db,
            installed: false,
            version: None,
        }),
        (PackageDb::Rpm, Some(0)) => Ok(PackageState {
            db,
            installed: true,
            version: stdout.lines().next().map(rpm_version),
        }),
        (PackageDb::Rpm, Some(1)) if stdout.contains("is not installed") => Ok(PackageState {
            db,
            installed: false,
            version: None,
        }),
        (_, code) => Err(format!(
            "package query exited with {code:?}: {}",
            stderr.trim()
        )),
    }
}

/// `ii \t1.2-3` → installed 1.2-3. The second status letter is the current
/// state: `i` installed; anything else (`c` config-files only, `n` not
/// installed, …) is not installed.
pub fn parse_dpkg_status(stdout: &str) -> PackageState {
    let line = stdout.lines().next().unwrap_or("");
    let (status, version) = line.split_once('\t').unwrap_or((line, ""));
    let installed = status.as_bytes().get(1) == Some(&b'i');
    PackageState {
        db: PackageDb::Dpkg,
        installed,
        version: (installed && !version.is_empty()).then(|| version.to_string()),
    }
}

/// `(none):1.2-3` → `1.2-3`; `2:1.2-3` stays.
pub fn rpm_version(line: &str) -> String {
    line.trim()
        .strip_prefix("(none):")
        .unwrap_or(line.trim())
        .to_string()
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Accounts {
    /// name, uid, gid
    pub users: Vec<(String, u32, u32)>,
    /// name, gid, members
    pub groups: Vec<(String, u32, Vec<String>)>,
}

impl Accounts {
    pub fn user(&self, name: &str) -> UserState {
        let Some(&(_, uid, gid)) = self.users.iter().find(|u| u.0 == name) else {
            return UserState::default();
        };
        let mut groups: Vec<String> = self
            .groups
            .iter()
            .filter(|(_, g, members)| *g == gid || members.iter().any(|m| m == name))
            .map(|(n, _, _)| n.clone())
            .collect();
        groups.sort();
        groups.dedup();
        UserState {
            exists: true,
            uid: Some(uid),
            gid: Some(gid),
            groups,
        }
    }

    pub fn group(&self, name: &str) -> GroupState {
        let Some((_, gid, members)) = self.groups.iter().find(|g| g.0 == name) else {
            return GroupState::default();
        };
        let mut members = members.clone();
        members.sort();
        GroupState {
            exists: true,
            gid: Some(*gid),
            members,
        }
    }
}

/// Local accounts from `/etc/passwd` and `/etc/group` (the platform only
/// creates local accounts; directory-service users are out of scope).
pub fn read_accounts(sysroot: &Path) -> Result<Accounts, String> {
    let read = |rel: &str| {
        let p = sysroot.join(rel);
        std::fs::read_to_string(&p).map_err(|e| format!("read {}: {e}", p.display()))
    };
    Ok(parse_accounts(&read("etc/passwd")?, &read("etc/group")?))
}

pub fn parse_accounts(passwd: &str, group: &str) -> Accounts {
    let fields = |line: &str| line.split(':').map(str::to_string).collect::<Vec<_>>();
    let users = passwd
        .lines()
        .filter(|l| !l.trim().is_empty() && !l.starts_with('#'))
        .filter_map(|l| {
            let f = fields(l);
            Some((f.first()?.clone(), f.get(2)?.parse().ok()?, f.get(3)?.parse().ok()?))
        })
        .collect();
    let groups = group
        .lines()
        .filter(|l| !l.trim().is_empty() && !l.starts_with('#'))
        .filter_map(|l| {
            let f = fields(l);
            let members = f
                .get(3)
                .map(|m| {
                    m.split(',')
                        .filter(|s| !s.is_empty())
                        .map(str::to_string)
                        .collect()
                })
                .unwrap_or_default();
            Some((f.first()?.clone(), f.get(2)?.parse().ok()?, members))
        })
        .collect();
    Accounts { users, groups }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::command::{RecordingRunner, output};
    use crate::params::{HostPath, PackageName, UnitName, UserName};
    use std::ffi::OsString;
    use std::os::unix::fs::PermissionsExt;

    fn probe_with(root: &Path, runner: Arc<RecordingRunner>, backups: Option<PathBuf>) -> HostProbe {
        HostProbe::new(runner, root, backups)
    }

    fn path(p: &str) -> Target {
        Target::Path {
            path: HostPath::try_from(p.to_string()).unwrap(),
        }
    }

    #[tokio::test]
    async fn path_state_with_backup_and_no_follow() {
        let root = tempfile::tempdir().unwrap();
        let backups = root.path().join("backups");
        std::fs::create_dir(&backups).unwrap();
        let etc = root.path().join("etc");
        std::fs::create_dir(&etc).unwrap();
        std::fs::write(etc.join("app.conf"), b"a=1\n").unwrap();
        std::fs::set_permissions(etc.join("app.conf"), std::fs::Permissions::from_mode(0o640))
            .unwrap();
        std::os::unix::fs::symlink("/etc/shadow", etc.join("link")).unwrap();
        let p = probe_with(root.path(), Arc::default(), Some(backups.clone()));

        let TargetState::Path(s) = p.probe(&path("/etc/app.conf"), Capture::Before).await.unwrap()
        else {
            panic!()
        };
        let sha = hex::encode(Sha256::digest(b"a=1\n"));
        assert!(s.exists);
        assert_eq!(s.kind, Some(PathKind::File));
        assert_eq!(s.mode, Some(0o640));
        assert_eq!(s.size, Some(4));
        assert_eq!(s.sha256.as_deref(), Some(sha.as_str()));
        assert_eq!(s.backup, Some(Backup::Stored));
        let kept = backups.join(&sha);
        assert_eq!(std::fs::read(&kept).unwrap(), b"a=1\n");
        assert_eq!(std::fs::metadata(&kept).unwrap().mode() & 0o777, 0o600);
        // No temp files left behind.
        assert_eq!(std::fs::read_dir(&backups).unwrap().count(), 1);

        // After-capture does not back up.
        let TargetState::Path(a) = p.probe(&path("/etc/app.conf"), Capture::After).await.unwrap()
        else {
            panic!()
        };
        assert_eq!(a.backup, None);

        // A symlink is recorded as a link, not followed.
        let TargetState::Path(l) = p.probe(&path("/etc/link"), Capture::Before).await.unwrap()
        else {
            panic!()
        };
        assert_eq!(l.kind, Some(PathKind::Symlink));
        assert_eq!(l.link_target.as_deref(), Some(Path::new("/etc/shadow")));
        assert_eq!(l.sha256, None);

        let TargetState::Path(d) = p.probe(&path("/etc"), Capture::Before).await.unwrap() else {
            panic!()
        };
        assert_eq!(d.kind, Some(PathKind::Directory));
        let TargetState::Path(m) = p.probe(&path("/etc/missing"), Capture::Before).await.unwrap()
        else {
            panic!()
        };
        assert_eq!(m, PathState::default());
    }

    #[tokio::test]
    async fn unit_state_from_systemctl_show() {
        let root = tempfile::tempdir().unwrap();
        let runner = Arc::new(RecordingRunner::with_replies([
            output(
                0,
                "LoadState=loaded\nUnitFileState=enabled\nActiveState=active\nSubState=running\n",
            ),
            output(0, "LoadState=not-found\nUnitFileState=\nActiveState=inactive\nSubState=dead\n"),
            output(1, ""),
        ]));
        let p = probe_with(root.path(), runner.clone(), None);
        let unit = |u: &str| Target::SystemdUnit {
            unit: UnitName::try_from(u.to_string()).unwrap(),
        };
        let TargetState::SystemdUnit(s) =
            p.probe(&unit("nginx.service"), Capture::Before).await.unwrap()
        else {
            panic!()
        };
        assert!(s.exists);
        assert_eq!(s.unit_file_state.as_deref(), Some("enabled"));
        assert_eq!(s.active_state.as_deref(), Some("active"));
        let TargetState::SystemdUnit(s) = p.probe(&unit("x.service"), Capture::Before).await.unwrap()
        else {
            panic!()
        };
        assert!(!s.exists);
        assert_eq!(s.unit_file_state, None);
        assert!(p.probe(&unit("y.service"), Capture::Before).await.is_err());
        assert_eq!(
            runner.calls()[0].args,
            [
                "show",
                "--property=LoadState,UnitFileState,ActiveState,SubState",
                "--",
                "nginx.service"
            ]
            .map(OsString::from)
        );
    }

    #[tokio::test]
    async fn package_state_dpkg_and_rpm() {
        let root = tempfile::tempdir().unwrap();
        std::fs::create_dir_all(root.path().join("usr/bin")).unwrap();
        std::fs::write(root.path().join("usr/bin/dpkg-query"), "").unwrap();
        let mut missing = output(1, "");
        missing.stderr = b"dpkg-query: no packages found matching docker-ce\n".to_vec();
        let runner = Arc::new(RecordingRunner::with_replies([
            output(0, "ii \t5:27.3.1-1~debian.12~bookworm\n"),
            missing,
            output(0, "rc \t1.0\n"),
            output(2, ""),
        ]));
        let p = probe_with(root.path(), runner.clone(), None);
        let pkg = |n: &str| Target::Package {
            name: PackageName::try_from(n.to_string()).unwrap(),
        };
        let st = |s: TargetState| match s {
            TargetState::Package(p) => p,
            _ => panic!(),
        };
        let s = st(p.probe(&pkg("docker-ce"), Capture::Before).await.unwrap());
        assert!(s.installed);
        assert_eq!(s.version.as_deref(), Some("5:27.3.1-1~debian.12~bookworm"));
        assert!(!st(p.probe(&pkg("docker-ce"), Capture::Before).await.unwrap()).installed);
        assert!(!st(p.probe(&pkg("old"), Capture::Before).await.unwrap()).installed);
        assert!(p.probe(&pkg("x"), Capture::Before).await.is_err());
        assert_eq!(
            runner.calls()[0].args,
            ["-W", "-f=${db:Status-Abbrev}\t${Version}\n", "--", "docker-ce"].map(OsString::from)
        );

        let rpm_root = tempfile::tempdir().unwrap();
        std::fs::create_dir_all(rpm_root.path().join("usr/bin")).unwrap();
        std::fs::write(rpm_root.path().join("usr/bin/rpm"), "").unwrap();
        let runner = Arc::new(RecordingRunner::with_replies([
            output(0, "(none):27.3.1-1.fc40\n"),
            output(1, "package moby is not installed\n"),
        ]));
        let p = probe_with(rpm_root.path(), runner, None);
        let s = st(p.probe(&pkg("docker-ce"), Capture::Before).await.unwrap());
        assert_eq!(s.version.as_deref(), Some("27.3.1-1.fc40"));
        assert_eq!(s.db, PackageDb::Rpm);
        assert!(!st(p.probe(&pkg("moby"), Capture::Before).await.unwrap()).installed);

        let none = tempfile::tempdir().unwrap();
        let p = probe_with(none.path(), Arc::default(), None);
        assert!(p.probe(&pkg("x"), Capture::Before).await.is_err());
    }

    #[tokio::test]
    async fn users_and_groups_from_etc() {
        let root = tempfile::tempdir().unwrap();
        std::fs::create_dir(root.path().join("etc")).unwrap();
        std::fs::write(
            root.path().join("etc/passwd"),
            "root:x:0:0:root:/root:/bin/bash\nhazelminx-agent:x:998:998::/var/lib/hazelminx/agent:/usr/sbin/nologin\n",
        )
        .unwrap();
        std::fs::write(
            root.path().join("etc/group"),
            "root:x:0:\ndocker:x:990:hazelminx-agent,alice\nhazelminx-agent:x:998:\n",
        )
        .unwrap();
        let p = probe_with(root.path(), Arc::default(), None);
        let user = |n: &str| Target::User {
            name: UserName::try_from(n.to_string()).unwrap(),
        };
        let group = |n: &str| Target::Group {
            name: UserName::try_from(n.to_string()).unwrap(),
        };
        let TargetState::User(u) = p.probe(&user("hazelminx-agent"), Capture::Before).await.unwrap()
        else {
            panic!()
        };
        assert_eq!(u.uid, Some(998));
        assert_eq!(u.groups, vec!["docker", "hazelminx-agent"]);
        let TargetState::User(u) = p.probe(&user("nobody2"), Capture::Before).await.unwrap() else {
            panic!()
        };
        assert!(!u.exists);
        let TargetState::Group(g) = p.probe(&group("docker"), Capture::Before).await.unwrap() else {
            panic!()
        };
        assert_eq!(g.members, vec!["alice", "hazelminx-agent"]);
    }
}
