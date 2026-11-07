//! `system_info`: hostname (`/etc/hostname`, else `/proc/sys/kernel/hostname`),
//! OS release, kernel and uptime read from the host's own files under `sysroot`. No process is spawned. Missing files
//! yield `null` fields rather than an error, because a minimal container
//! host may lack some of them.

use crate::executor::ActionResult;
use serde::Serialize;
use std::path::Path;

#[derive(Debug, Default, Clone, PartialEq, Serialize)]
pub struct SystemInfo {
    pub hostname: Option<String>,
    /// `PRETTY_NAME` from os-release.
    pub os: Option<String>,
    /// `ID` from os-release (e.g. `debian`, `ubuntu`, `fedora`).
    pub os_id: Option<String>,
    pub os_version_id: Option<String>,
    pub kernel: Option<String>,
    pub uptime_seconds: Option<u64>,
}

pub async fn run(sysroot: &Path) -> ActionResult {
    let info = read(sysroot);
    Ok(serde_json::to_value(info).expect("SystemInfo serializes"))
}

pub fn read(sysroot: &Path) -> SystemInfo {
    let text = |rel: &str| std::fs::read_to_string(sysroot.join(rel)).ok();
    let first_line = |s: String| s.lines().next().unwrap_or("").trim().to_string();
    let os_release = text("etc/os-release")
        .map(|t| parse_os_release(&t))
        .unwrap_or_default();
    let field = |key: &str| {
        os_release
            .iter()
            .find(|(k, _)| k == key)
            .map(|(_, v)| v.clone())
    };
    SystemInfo {
        hostname: text("etc/hostname")
            .or_else(|| text("proc/sys/kernel/hostname"))
            .map(first_line)
            .filter(|s| !s.is_empty()),
        os: field("PRETTY_NAME"),
        os_id: field("ID"),
        os_version_id: field("VERSION_ID"),
        kernel: text("proc/sys/kernel/osrelease")
            .map(first_line)
            .filter(|s| !s.is_empty()),
        uptime_seconds: text("proc/uptime")
            .and_then(|t| t.split_whitespace().next()?.parse::<f64>().ok())
            .map(|f| f as u64),
    }
}

/// Parses the `KEY=value` / `KEY="value"` lines of os-release.
pub fn parse_os_release(text: &str) -> Vec<(String, String)> {
    text.lines()
        .filter_map(|line| {
            let line = line.trim();
            if line.is_empty() || line.starts_with('#') {
                return None;
            }
            let (k, v) = line.split_once('=')?;
            let v = v.trim();
            let v = v
                .strip_prefix('"')
                .and_then(|s| s.strip_suffix('"'))
                .or_else(|| v.strip_prefix('\'').and_then(|s| s.strip_suffix('\'')))
                .unwrap_or(v);
            Some((k.trim().to_string(), v.to_string()))
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn write(root: &Path, rel: &str, body: &str) {
        let p = root.join(rel);
        std::fs::create_dir_all(p.parent().unwrap()).unwrap();
        std::fs::write(p, body).unwrap();
    }

    #[test]
    fn reads_the_usual_files() {
        let dir = tempfile::tempdir().unwrap();
        let r = dir.path();
        write(r, "etc/hostname", "db-01\n");
        write(
            r,
            "etc/os-release",
            "PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\nNAME=\"Debian GNU/Linux\"\nID=debian\nVERSION_ID=\"12\"\n",
        );
        write(r, "proc/sys/kernel/osrelease", "6.1.0-25-amd64\n");
        write(r, "proc/uptime", "12345.67 54321.00\n");
        let info = read(r);
        assert_eq!(
            info,
            SystemInfo {
                hostname: Some("db-01".into()),
                os: Some("Debian GNU/Linux 12 (bookworm)".into()),
                os_id: Some("debian".into()),
                os_version_id: Some("12".into()),
                kernel: Some("6.1.0-25-amd64".into()),
                uptime_seconds: Some(12345),
            }
        );
    }

    #[test]
    fn hostname_falls_back_to_proc() {
        let dir = tempfile::tempdir().unwrap();
        write(dir.path(), "proc/sys/kernel/hostname", "web-02\n");
        assert_eq!(read(dir.path()).hostname.as_deref(), Some("web-02"));
    }

    #[test]
    fn missing_files_are_null_not_errors() {
        let dir = tempfile::tempdir().unwrap();
        assert_eq!(read(dir.path()), SystemInfo::default());
    }

    #[test]
    fn os_release_quotes_and_comments() {
        let kv = parse_os_release("# c\nA=1\nB=\"two words\"\nC='x'\nbroken\n");
        assert_eq!(
            kv,
            vec![
                ("A".into(), "1".into()),
                ("B".into(), "two words".into()),
                ("C".into(), "x".into())
            ]
        );
    }
}
