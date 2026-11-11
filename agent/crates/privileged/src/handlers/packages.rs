//! `list_package_updates`: read-only query of the host package manager.
//! apt: `apt-get -q -s upgrade` (`Inst` lines); dnf: `dnf -q check-update`
//! (exit 100 means updates exist). Detection is by binary presence under
//! `sysroot`; an unknown manager is a failure, not a guess.

use crate::command::{CommandRunner, CommandSpec, OutputSink};
use crate::executor::{ActionError, ActionResult};
use serde::Serialize;
use std::path::Path;
use std::sync::Arc;

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct PackageUpdate {
    pub name: String,
    pub current: Option<String>,
    pub candidate: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum Manager {
    Apt,
    Dnf,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct Updates {
    pub manager: Manager,
    pub updates: Vec<PackageUpdate>,
}

pub const APT_GET: &str = "/usr/bin/apt-get";
pub const DNF: &str = "/usr/bin/dnf";

pub fn detect(sysroot: &Path) -> Option<Manager> {
    let present = |abs: &str| sysroot.join(abs.trim_start_matches('/')).exists();
    if present(APT_GET) {
        Some(Manager::Apt)
    } else if present(DNF) {
        Some(Manager::Dnf)
    } else {
        None
    }
}

pub fn spec_for(manager: Manager) -> CommandSpec {
    match manager {
        Manager::Apt => CommandSpec::new(APT_GET).arg("-q").arg("-s").arg("upgrade"),
        Manager::Dnf => CommandSpec::new(DNF).arg("-q").arg("check-update"),
    }
}

pub async fn run(runner: Arc<dyn CommandRunner>, sysroot: &Path, sink: OutputSink) -> ActionResult {
    let Some(manager) = detect(sysroot) else {
        return Err(ActionError::new(
            "no supported package manager (apt-get or dnf) found",
        ));
    };
    let out = runner
        .run(spec_for(manager), sink)
        .await
        .map_err(|e| ActionError::new(format!("spawn package manager: {e}")))?;
    let updates = match manager {
        Manager::Apt => {
            if !out.success() {
                return Err(ActionError::new(format!(
                    "apt-get exited with {:?}: {}",
                    out.exit_code,
                    out.stderr_lossy().trim()
                )));
            }
            parse_apt(&out.stdout_lossy())
        }
        Manager::Dnf => match out.exit_code {
            Some(0) => Vec::new(),
            Some(100) => parse_dnf(&out.stdout_lossy()),
            code => {
                return Err(ActionError::new(format!(
                    "dnf exited with {code:?}: {}",
                    out.stderr_lossy().trim()
                )));
            }
        },
    };
    Ok(serde_json::to_value(Updates { manager, updates }).expect("Updates serializes"))
}

/// `Inst name [current] (candidate suite [arch])` → one update per line.
pub fn parse_apt(stdout: &str) -> Vec<PackageUpdate> {
    stdout
        .lines()
        .filter_map(|line| {
            let rest = line.strip_prefix("Inst ")?;
            let mut parts = rest.splitn(2, ' ');
            let name = parts.next()?.to_string();
            let tail = parts.next().unwrap_or("").trim();
            let (current, tail) = match tail.strip_prefix('[') {
                Some(t) => {
                    let (cur, after) = t.split_once(']')?;
                    (Some(cur.to_string()), after.trim())
                }
                None => (None, tail),
            };
            let inner = tail.strip_prefix('(')?;
            let candidate = inner.split_whitespace().next()?.to_string();
            Some(PackageUpdate {
                name,
                current,
                candidate,
            })
        })
        .collect()
}

/// `name.arch   version   repo` lines after the header block.
pub fn parse_dnf(stdout: &str) -> Vec<PackageUpdate> {
    let mut out = Vec::new();
    for line in stdout.lines() {
        let line = line.trim();
        if line.is_empty() {
            continue;
        }
        if line.starts_with("Obsoleting") {
            break;
        }
        let cols: Vec<&str> = line.split_whitespace().collect();
        if cols.len() < 3 || cols[0].starts_with(|c: char| !c.is_ascii_alphanumeric()) {
            continue;
        }
        let name = cols[0]
            .rsplit_once('.')
            .map(|(n, _)| n)
            .unwrap_or(cols[0])
            .to_string();
        out.push(PackageUpdate {
            name,
            current: None,
            candidate: cols[1].to_string(),
        });
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::command::{RecordingRunner, output};

    #[test]
    fn apt_inst_lines() {
        let text = "Reading package lists...\nInst libssl3 [3.0.11-1~deb12u2] (3.0.13-1~deb12u1 Debian-Security:12/stable-security [amd64])\nInst curl (8.5.0-1 Debian:12.5/stable [amd64])\nConf libssl3 (3.0.13-1~deb12u1 Debian-Security:12/stable-security [amd64])\n";
        assert_eq!(
            parse_apt(text),
            vec![
                PackageUpdate {
                    name: "libssl3".into(),
                    current: Some("3.0.11-1~deb12u2".into()),
                    candidate: "3.0.13-1~deb12u1".into()
                },
                PackageUpdate {
                    name: "curl".into(),
                    current: None,
                    candidate: "8.5.0-1".into()
                },
            ]
        );
    }

    #[test]
    fn dnf_table() {
        let text = "\nkernel.x86_64    6.8.9-300.fc40    updates\nvim-minimal.x86_64   2:9.1.393-1.fc40   updates\nObsoleting Packages\nfoo.noarch 1-1 updates\n";
        let u = parse_dnf(text);
        assert_eq!(u.len(), 2);
        assert_eq!(u[0].name, "kernel");
        assert_eq!(u[0].candidate, "6.8.9-300.fc40");
        assert_eq!(u[1].name, "vim-minimal");
        assert_eq!(u[1].candidate, "2:9.1.393-1.fc40");
    }

    #[test]
    fn detect_by_binary_presence() {
        let dir = tempfile::tempdir().unwrap();
        assert_eq!(detect(dir.path()), None);
        std::fs::create_dir_all(dir.path().join("usr/bin")).unwrap();
        std::fs::write(dir.path().join("usr/bin/dnf"), "").unwrap();
        assert_eq!(detect(dir.path()), Some(Manager::Dnf));
        std::fs::write(dir.path().join("usr/bin/apt-get"), "").unwrap();
        assert_eq!(detect(dir.path()), Some(Manager::Apt));
    }

    #[tokio::test]
    async fn runs_the_fixed_argv_and_parses() {
        let dir = tempfile::tempdir().unwrap();
        std::fs::create_dir_all(dir.path().join("usr/bin")).unwrap();
        std::fs::write(dir.path().join("usr/bin/apt-get"), "").unwrap();
        let runner = Arc::new(RecordingRunner::with_replies([output(
            0,
            "Inst curl (8.5.0-1 Debian:12.5/stable [amd64])\n",
        )]));
        let v = run(runner.clone(), dir.path(), OutputSink::discard())
            .await
            .unwrap();
        assert_eq!(v["manager"], "apt");
        assert_eq!(v["updates"][0]["name"], "curl");
        let calls = runner.calls();
        assert_eq!(calls.len(), 1);
        assert_eq!(calls[0].program.to_str().unwrap(), APT_GET);
        assert_eq!(
            calls[0].args,
            ["-q", "-s", "upgrade"].map(std::ffi::OsString::from)
        );
    }

    #[tokio::test]
    async fn dnf_exit_100_is_updates_and_other_codes_fail() {
        let dir = tempfile::tempdir().unwrap();
        std::fs::create_dir_all(dir.path().join("usr/bin")).unwrap();
        std::fs::write(dir.path().join("usr/bin/dnf"), "").unwrap();
        let runner = Arc::new(RecordingRunner::with_replies([
            output(100, "\nkernel.x86_64 1-1 updates\n"),
            output(1, ""),
        ]));
        let v = run(runner.clone(), dir.path(), OutputSink::discard())
            .await
            .unwrap();
        assert_eq!(v["updates"][0]["name"], "kernel");
        let err = run(runner, dir.path(), OutputSink::discard())
            .await
            .unwrap_err();
        assert!(err.message.contains("dnf exited"));
    }

    #[tokio::test]
    async fn unsupported_host_is_an_error_without_spawning() {
        let dir = tempfile::tempdir().unwrap();
        let runner = Arc::new(RecordingRunner::default());
        let err = run(runner.clone(), dir.path(), OutputSink::discard())
            .await
            .unwrap_err();
        assert!(err.message.contains("no supported package manager"));
        assert!(runner.calls().is_empty());
    }
}
