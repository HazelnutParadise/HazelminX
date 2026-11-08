//! Pre-install snapshot (55.1, UNX-002): a summary of the host taken before
//! the platform writes anything, compared with the host after removal (037)
//! to report what is the same and what is not.
//!
//! A section that cannot be read is stored as `unavailable` with the reason,
//! and a comparison involving it says "not comparable" rather than "same".

use super::probe::{DPKG_QUERY, RPM, detect_package_db, read_accounts, under};
use super::entry::{PackageDb, PathKind};
use crate::command::{CommandRunner, CommandSpec, OutputSink};
use crate::handlers::system_info::{self, SystemInfo};
use crate::handlers::systemd::SYSTEMCTL;
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use time::OffsetDateTime;

pub const SNAPSHOT_FILE: &str = "preinstall-snapshot.json";
pub const SNAPSHOT_FORMAT: u32 = 1;
pub const NFT: &str = "/usr/sbin/nft";
pub const IPTABLES_SAVE: &str = "/usr/sbin/iptables-save";
pub const IP6TABLES_SAVE: &str = "/usr/sbin/ip6tables-save";

/// Paths the platform writes or edits, from the deploy units in
/// `agent/deploy/` and spec 55.1 / 55.6. The installer (ticket 013) passes
/// its own list when it has a longer one.
pub const DEFAULT_PLATFORM_PATHS: &[&str] = &[
    "/etc/hazelminx",
    "/var/lib/hazelminx",
    "/var/log/hazelminx",
    "/run/hazelminx",
    "/usr/local/bin/hazelminx",
    "/usr/local/bin/hazelminx-agent",
    "/usr/local/bin/hazelminx-privileged",
    "/usr/local/bin/hazelminx-core",
    "/etc/systemd/system/hazelminx-core.service",
    "/etc/systemd/system/hazelminx-agent.service",
    "/etc/systemd/system/hazelminx-privileged.service",
    "/etc/sysusers.d/hazelminx.conf",
    "/etc/ssh/sshd_config.d/50-hazelminx.conf",
    "/etc/docker/daemon.json",
    "/etc/systemd/journald.conf",
];

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "status", rename_all = "snake_case")]
pub enum Section<T> {
    Captured { data: T },
    Unavailable { reason: String },
}

impl<T> Section<T> {
    fn from_result(r: Result<T, String>) -> Self {
        match r {
            Ok(data) => Section::Captured { data },
            Err(reason) => Section::Unavailable { reason },
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
pub struct Package {
    pub name: String,
    pub version: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Packages {
    pub db: PackageDb,
    pub installed: Vec<Package>,
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
pub struct Listener {
    /// `tcp`, `tcp6`, `udp`, `udp6`.
    pub proto: String,
    pub address: String,
    pub port: u16,
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
pub struct Account {
    pub name: String,
    pub id: u32,
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
pub struct GroupEntry {
    pub name: String,
    pub gid: u32,
    pub members: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Firewall {
    /// `nftables`, `iptables` or `none` (no tool installed).
    pub backend: String,
    pub rule_count: u64,
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
pub struct PathPresence {
    pub path: PathBuf,
    pub exists: bool,
    pub kind: Option<PathKind>,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Snapshot {
    pub format: u32,
    #[serde(with = "time::serde::rfc3339")]
    pub captured_at: OffsetDateTime,
    pub host: SystemInfo,
    pub packages: Section<Packages>,
    pub enabled_units: Section<Vec<String>>,
    pub listening: Section<Vec<Listener>>,
    pub users: Section<Vec<Account>>,
    pub groups: Section<Vec<GroupEntry>>,
    pub firewall: Section<Firewall>,
    pub platform_paths: Vec<PathPresence>,
}

/// Reads the host. `sysroot` is `/` in production.
pub async fn capture(
    runner: Arc<dyn CommandRunner>,
    sysroot: &Path,
    platform_paths: &[PathBuf],
) -> Snapshot {
    let accounts = read_accounts(sysroot);
    let mut paths: Vec<PathPresence> = platform_paths
        .iter()
        .map(|p| {
            let md = std::fs::symlink_metadata(under(sysroot, p));
            PathPresence {
                path: p.clone(),
                exists: md.is_ok(),
                kind: md.ok().map(|m| {
                    let ft = m.file_type();
                    if ft.is_file() {
                        PathKind::File
                    } else if ft.is_dir() {
                        PathKind::Directory
                    } else if ft.is_symlink() {
                        PathKind::Symlink
                    } else {
                        PathKind::Other
                    }
                }),
            }
        })
        .collect();
    paths.sort();
    paths.dedup();
    Snapshot {
        format: SNAPSHOT_FORMAT,
        captured_at: OffsetDateTime::now_utc(),
        host: system_info::read(sysroot),
        packages: Section::from_result(packages(runner.clone(), sysroot).await),
        enabled_units: Section::from_result(enabled_units(runner.clone()).await),
        listening: Section::from_result(listening(sysroot)),
        users: Section::from_result(accounts.clone().map(|a| {
            let mut v: Vec<Account> = a
                .users
                .into_iter()
                .map(|(name, id, _)| Account { name, id })
                .collect();
            v.sort();
            v
        })),
        groups: Section::from_result(accounts.map(|a| {
            let mut v: Vec<GroupEntry> = a
                .groups
                .into_iter()
                .map(|(name, gid, mut members)| {
                    members.sort();
                    GroupEntry { name, gid, members }
                })
                .collect();
            v.sort();
            v
        })),
        firewall: Section::from_result(firewall(runner, sysroot).await),
        platform_paths: paths,
    }
}

async fn run_ok(runner: &Arc<dyn CommandRunner>, spec: CommandSpec) -> Result<String, String> {
    let program = spec.program.display().to_string();
    let out = runner
        .run(spec, OutputSink::discard())
        .await
        .map_err(|e| format!("spawn {program}: {e}"))?;
    if !out.success() {
        return Err(format!(
            "{program} exited with {:?}: {}",
            out.exit_code,
            out.stderr_lossy().trim()
        ));
    }
    if out.truncated {
        return Err(format!("{program} output exceeded the capture limit"));
    }
    Ok(out.stdout_lossy())
}

async fn packages(runner: Arc<dyn CommandRunner>, sysroot: &Path) -> Result<Packages, String> {
    let db = detect_package_db(sysroot).ok_or("no package database (dpkg or rpm) found")?;
    let installed = match db {
        PackageDb::Dpkg => parse_dpkg_list(
            &run_ok(
                &runner,
                CommandSpec::new(DPKG_QUERY)
                    .arg("-W")
                    .arg("-f=${binary:Package}\t${Version}\t${db:Status-Abbrev}\n"),
            )
            .await?,
        ),
        PackageDb::Rpm => parse_rpm_list(
            &run_ok(
                &runner,
                CommandSpec::new(RPM)
                    .arg("-qa")
                    .arg("--qf")
                    .arg("%{NAME}.%{ARCH}\t%{EPOCH}:%{VERSION}-%{RELEASE}\n"),
            )
            .await?,
        ),
    };
    Ok(Packages { db, installed })
}

/// `name\tversion\tii ` lines; only currently installed packages.
pub fn parse_dpkg_list(text: &str) -> Vec<Package> {
    let mut v: Vec<Package> = text
        .lines()
        .filter_map(|l| {
            let mut f = l.split('\t');
            let (name, version, status) = (f.next()?, f.next()?, f.next()?);
            (status.as_bytes().get(1) == Some(&b'i')).then(|| Package {
                name: name.to_string(),
                version: version.to_string(),
            })
        })
        .collect();
    v.sort();
    v
}

pub fn parse_rpm_list(text: &str) -> Vec<Package> {
    let mut v: Vec<Package> = text
        .lines()
        .filter_map(|l| {
            let (name, version) = l.split_once('\t')?;
            Some(Package {
                name: name.to_string(),
                version: super::probe::rpm_version(version),
            })
        })
        .collect();
    v.sort();
    v
}

async fn enabled_units(runner: Arc<dyn CommandRunner>) -> Result<Vec<String>, String> {
    let text = run_ok(
        &runner,
        CommandSpec::new(SYSTEMCTL)
            .arg("list-unit-files")
            .arg("--state=enabled")
            .arg("--no-legend")
            .arg("--no-pager"),
    )
    .await?;
    Ok(parse_unit_files(&text))
}

/// First column of `systemctl list-unit-files` rows.
pub fn parse_unit_files(text: &str) -> Vec<String> {
    let mut v: Vec<String> = text
        .lines()
        .filter_map(|l| l.split_whitespace().next())
        .map(str::to_string)
        .collect();
    v.sort();
    v.dedup();
    v
}

/// Listening TCP sockets and bound, unconnected UDP sockets from
/// `/proc/net/{tcp,tcp6,udp,udp6}`.
pub fn listening(sysroot: &Path) -> Result<Vec<Listener>, String> {
    let mut out = Vec::new();
    for proto in ["tcp", "tcp6", "udp", "udp6"] {
        let p = sysroot.join("proc/net").join(proto);
        let text = match std::fs::read_to_string(&p) {
            Ok(t) => t,
            // IPv6 disabled hosts have no tcp6/udp6.
            Err(e) if e.kind() == std::io::ErrorKind::NotFound && proto.ends_with('6') => continue,
            Err(e) => return Err(format!("read {}: {e}", p.display())),
        };
        out.extend(parse_proc_net(proto, &text));
    }
    out.sort();
    out.dedup();
    Ok(out)
}

pub fn parse_proc_net(proto: &str, text: &str) -> Vec<Listener> {
    let tcp = proto.starts_with("tcp");
    text.lines()
        .skip(1)
        .filter_map(|line| {
            let cols: Vec<&str> = line.split_whitespace().collect();
            let (local, remote, state) = (cols.get(1)?, cols.get(2)?, cols.get(3)?);
            let listening = if tcp {
                *state == "0A"
            } else {
                *state == "07" && remote.ends_with(":0000")
            };
            if !listening {
                return None;
            }
            let (addr, port) = local.split_once(':')?;
            Some(Listener {
                proto: proto.to_string(),
                address: decode_proc_addr(addr)?,
                port: u16::from_str_radix(port, 16).ok()?,
            })
        })
        .collect()
}

/// `/proc/net` addresses are 32-bit words in host (little-endian) order.
fn decode_proc_addr(hex_addr: &str) -> Option<String> {
    let words: Vec<u32> = (0..hex_addr.len() / 8)
        .map(|i| u32::from_str_radix(&hex_addr[i * 8..i * 8 + 8], 16).ok())
        .collect::<Option<_>>()?;
    match words.as_slice() {
        [w] => Some(std::net::Ipv4Addr::from(w.to_le_bytes()).to_string()),
        [a, b, c, d] => {
            let mut bytes = [0u8; 16];
            for (i, w) in [a, b, c, d].iter().enumerate() {
                bytes[i * 4..i * 4 + 4].copy_from_slice(&w.to_le_bytes());
            }
            Some(std::net::Ipv6Addr::from(bytes).to_string())
        }
        _ => None,
    }
}

async fn firewall(runner: Arc<dyn CommandRunner>, sysroot: &Path) -> Result<Firewall, String> {
    let has = |p: &str| under(sysroot, Path::new(p)).exists();
    if has(NFT) {
        let text = run_ok(
            &runner,
            CommandSpec::new(NFT).arg("-j").arg("list").arg("ruleset"),
        )
        .await?;
        return Ok(Firewall {
            backend: "nftables".into(),
            rule_count: count_nft_rules(&text)?,
        });
    }
    if has(IPTABLES_SAVE) {
        let mut rule_count = count_iptables_rules(&run_ok(&runner, CommandSpec::new(IPTABLES_SAVE)).await?);
        if has(IP6TABLES_SAVE) {
            rule_count +=
                count_iptables_rules(&run_ok(&runner, CommandSpec::new(IP6TABLES_SAVE)).await?);
        }
        return Ok(Firewall {
            backend: "iptables".into(),
            rule_count,
        });
    }
    Ok(Firewall {
        backend: "none".into(),
        rule_count: 0,
    })
}

/// Objects with a `rule` key in `nft -j list ruleset`.
pub fn count_nft_rules(json: &str) -> Result<u64, String> {
    let v: serde_json::Value =
        serde_json::from_str(json).map_err(|e| format!("nft -j output does not parse: {e}"))?;
    let items = v
        .get("nftables")
        .and_then(|n| n.as_array())
        .ok_or("nft -j output has no nftables array")?;
    Ok(items.iter().filter(|i| i.get("rule").is_some()).count() as u64)
}

pub fn count_iptables_rules(text: &str) -> u64 {
    text.lines().filter(|l| l.starts_with("-A ")).count() as u64
}

/// Stores the snapshot as `dir/preinstall-snapshot.json` (mode 0600).
/// Refuses to replace one that exists: the pre-install state is taken once.
pub fn store(dir: &Path, snapshot: &Snapshot) -> Result<PathBuf, String> {
    let bytes = serde_json::to_vec_pretty(snapshot).expect("Snapshot serializes");
    super::store::write_new_file(dir, SNAPSHOT_FILE, &bytes).map_err(|e| e.to_string())?;
    Ok(dir.join(SNAPSHOT_FILE))
}

pub fn load(path: &Path) -> Result<Snapshot, String> {
    let bytes = std::fs::read(path).map_err(|e| format!("read {}: {e}", path.display()))?;
    let s: Snapshot = serde_json::from_slice(&bytes)
        .map_err(|e| format!("{} does not parse: {e}", path.display()))?;
    if s.format != SNAPSHOT_FORMAT {
        return Err(format!("unsupported snapshot format {}", s.format));
    }
    Ok(s)
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Changed {
    pub item: String,
    pub before: String,
    pub after: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "status", rename_all = "snake_case")]
pub enum SectionDiff {
    Same,
    Different {
        /// Present now, absent before.
        added: Vec<String>,
        /// Present before, absent now.
        removed: Vec<String>,
        changed: Vec<Changed>,
    },
    NotComparable {
        reason: String,
    },
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Comparison {
    /// True only when every section was readable both times and is the same.
    pub identical: bool,
    pub sections: BTreeMap<String, SectionDiff>,
}

fn diff_maps(before: &BTreeMap<String, String>, after: &BTreeMap<String, String>) -> SectionDiff {
    let added: Vec<String> = after
        .keys()
        .filter(|k| !before.contains_key(*k))
        .cloned()
        .collect();
    let removed: Vec<String> = before
        .keys()
        .filter(|k| !after.contains_key(*k))
        .cloned()
        .collect();
    let changed: Vec<Changed> = before
        .iter()
        .filter_map(|(k, b)| {
            let a = after.get(k)?;
            (a != b).then(|| Changed {
                item: k.clone(),
                before: b.clone(),
                after: a.clone(),
            })
        })
        .collect();
    if added.is_empty() && removed.is_empty() && changed.is_empty() {
        SectionDiff::Same
    } else {
        SectionDiff::Different {
            added,
            removed,
            changed,
        }
    }
}

fn diff_section<T>(
    before: &Section<T>,
    after: &Section<T>,
    key: impl Fn(&T) -> BTreeMap<String, String>,
) -> SectionDiff {
    match (before, after) {
        (Section::Captured { data: b }, Section::Captured { data: a }) => diff_maps(&key(b), &key(a)),
        (Section::Unavailable { reason }, _) => SectionDiff::NotComparable {
            reason: format!("not captured before install: {reason}"),
        },
        (_, Section::Unavailable { reason }) => SectionDiff::NotComparable {
            reason: format!("not readable now: {reason}"),
        },
    }
}

/// What differs between the pre-install snapshot and the host now.
pub fn compare(before: &Snapshot, after: &Snapshot) -> Comparison {
    let mut sections = BTreeMap::new();
    sections.insert(
        "packages".to_string(),
        diff_section(&before.packages, &after.packages, |p| {
            p.installed
                .iter()
                .map(|x| (x.name.clone(), x.version.clone()))
                .collect()
        }),
    );
    sections.insert(
        "enabled_units".to_string(),
        diff_section(&before.enabled_units, &after.enabled_units, |u| {
            u.iter().map(|n| (n.clone(), String::new())).collect()
        }),
    );
    sections.insert(
        "listening".to_string(),
        diff_section(&before.listening, &after.listening, |l| {
            l.iter()
                .map(|x| (format!("{} {} {}", x.proto, x.address, x.port), String::new()))
                .collect()
        }),
    );
    sections.insert(
        "users".to_string(),
        diff_section(&before.users, &after.users, |u| {
            u.iter()
                .map(|x| (x.name.clone(), format!("uid {}", x.id)))
                .collect()
        }),
    );
    sections.insert(
        "groups".to_string(),
        diff_section(&before.groups, &after.groups, |g| {
            g.iter()
                .map(|x| {
                    (
                        x.name.clone(),
                        format!("gid {} members {}", x.gid, x.members.join(",")),
                    )
                })
                .collect()
        }),
    );
    sections.insert(
        "firewall".to_string(),
        diff_section(&before.firewall, &after.firewall, |f| {
            BTreeMap::from([(
                "rule_count".to_string(),
                format!("{} ({})", f.rule_count, f.backend),
            )])
        }),
    );
    let present = |paths: &[PathPresence]| -> BTreeMap<String, String> {
        paths
            .iter()
            .filter(|p| p.exists)
            .map(|p| {
                (
                    p.path.display().to_string(),
                    format!("{:?}", p.kind).to_lowercase(),
                )
            })
            .collect()
    };
    sections.insert(
        "platform_paths".to_string(),
        diff_maps(&present(&before.platform_paths), &present(&after.platform_paths)),
    );
    let identical = sections.values().all(|d| *d == SectionDiff::Same);
    Comparison {
        identical,
        sections,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::command::{RecordingRunner, output};
    use std::ffi::OsString;

    const PROC_TCP: &str = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n   0: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1 1\n   1: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 2 1\n   2: 0100007F:1F90 0100007F:D431 01 00000000:00000000 00:00000000 00000000     0        0 3 1\n";
    const PROC_TCP6: &str = "  sl  local_address                         remote_address                        st\n   0: 00000000000000000000000000000000:0016 00000000000000000000000000000000:0000 0A\n   1: 00000000000000000000000001000000:0277 00000000000000000000000000000000:0000 0A\n";
    const PROC_UDP: &str = "  sl  local_address rem_address   st\n  1: 3500007F:0035 00000000:0000 07\n  2: 0100007F:0044 0200007F:0035 01\n";

    fn host(root: &Path) {
        let w = |rel: &str, body: &str| {
            let p = root.join(rel);
            std::fs::create_dir_all(p.parent().unwrap()).unwrap();
            std::fs::write(p, body).unwrap();
        };
        w("etc/hostname", "db-01\n");
        w("etc/passwd", "root:x:0:0::/root:/bin/sh\nalice:x:1000:1000::/home/alice:/bin/sh\n");
        w("etc/group", "root:x:0:\nalice:x:1000:\nsudo:x:27:alice\n");
        w("proc/net/tcp", PROC_TCP);
        w("proc/net/tcp6", PROC_TCP6);
        w("proc/net/udp", PROC_UDP);
        w("usr/bin/dpkg-query", "");
        w("usr/sbin/nft", "");
        w("etc/docker/daemon.json", "{}\n");
    }

    fn replies() -> Vec<crate::command::CommandOutput> {
        vec![
            output(0, "curl\t8.5.0-1\tii \nold\t1.0\trc \nopenssh-server\t1:9.2p1-2\tii \n"),
            output(0, "ssh.service enabled enabled\ncron.service enabled enabled\n"),
            output(
                0,
                r#"{"nftables":[{"metainfo":{"json_schema_version":1}},{"table":{"family":"inet","name":"filter"}},{"chain":{"name":"input"}},{"rule":{"chain":"input"}},{"rule":{"chain":"input"}}]}"#,
            ),
        ]
    }

    fn paths() -> Vec<PathBuf> {
        DEFAULT_PLATFORM_PATHS.iter().map(PathBuf::from).collect()
    }

    #[test]
    fn proc_net_parsing() {
        let v = parse_proc_net("tcp", PROC_TCP);
        assert_eq!(
            v,
            vec![
                Listener {
                    proto: "tcp".into(),
                    address: "0.0.0.0".into(),
                    port: 22
                },
                Listener {
                    proto: "tcp".into(),
                    address: "127.0.0.1".into(),
                    port: 8080
                }
            ]
        );
        let v6 = parse_proc_net("tcp6", PROC_TCP6);
        assert_eq!(v6[0].address, "::");
        assert_eq!(v6[1].address, "::1");
        assert_eq!(v6[1].port, 631);
        let u = parse_proc_net("udp", PROC_UDP);
        assert_eq!(u.len(), 1);
        assert_eq!(u[0].address, "127.0.0.53");
        assert_eq!(u[0].port, 53);
    }

    #[tokio::test]
    async fn capture_reads_every_section() {
        let root = tempfile::tempdir().unwrap();
        host(root.path());
        let runner = Arc::new(RecordingRunner::with_replies(replies()));
        let s = capture(runner.clone(), root.path(), &paths()).await;
        assert_eq!(s.host.hostname.as_deref(), Some("db-01"));
        let Section::Captured { data: p } = &s.packages else {
            panic!("{:?}", s.packages)
        };
        assert_eq!(p.db, PackageDb::Dpkg);
        assert_eq!(
            p.installed.iter().map(|x| x.name.as_str()).collect::<Vec<_>>(),
            ["curl", "openssh-server"]
        );
        assert_eq!(
            s.enabled_units,
            Section::Captured {
                data: vec!["cron.service".into(), "ssh.service".into()]
            }
        );
        let Section::Captured { data: l } = &s.listening else {
            panic!()
        };
        assert_eq!(l.len(), 5);
        let Section::Captured { data: u } = &s.users else {
            panic!()
        };
        assert_eq!(u[0].name, "alice");
        let Section::Captured { data: g } = &s.groups else {
            panic!()
        };
        assert!(g.iter().any(|x| x.name == "sudo" && x.members == ["alice"]));
        assert_eq!(
            s.firewall,
            Section::Captured {
                data: Firewall {
                    backend: "nftables".into(),
                    rule_count: 2
                }
            }
        );
        let daemon = s
            .platform_paths
            .iter()
            .find(|p| p.path == Path::new("/etc/docker/daemon.json"))
            .unwrap();
        assert!(daemon.exists);
        assert!(
            s.platform_paths
                .iter()
                .filter(|p| p.path != Path::new("/etc/docker/daemon.json"))
                .all(|p| !p.exists)
        );
        let calls = runner.calls();
        assert_eq!(calls[0].program, Path::new(DPKG_QUERY));
        assert_eq!(
            calls[1].args,
            ["list-unit-files", "--state=enabled", "--no-legend", "--no-pager"]
                .map(OsString::from)
        );
        assert_eq!(calls[2].args, ["-j", "list", "ruleset"].map(OsString::from));

        // Round-trips through the stored file.
        let dir = tempfile::tempdir().unwrap();
        let path = store(dir.path(), &s).unwrap();
        assert_eq!(load(&path).unwrap(), s);
        assert!(store(dir.path(), &s).is_err(), "taken once");
    }

    #[tokio::test]
    async fn compare_reports_same_different_and_not_comparable() {
        let root = tempfile::tempdir().unwrap();
        host(root.path());
        let before = capture(
            Arc::new(RecordingRunner::with_replies(replies())),
            root.path(),
            &paths(),
        )
        .await;
        let again = capture(
            Arc::new(RecordingRunner::with_replies(replies())),
            root.path(),
            &paths(),
        )
        .await;
        let same = compare(&before, &again);
        assert!(same.identical, "{same:?}");

        // Platform left things behind: a user, a listener, a path, a package
        // upgrade, an enabled unit; and the firewall cannot be read now.
        std::fs::write(
            root.path().join("etc/passwd"),
            "root:x:0:0::/root:/bin/sh\nalice:x:1000:1000::/home/alice:/bin/sh\nhazelminx-agent:x:998:998::/:/sbin/nologin\n",
        )
        .unwrap();
        std::fs::write(
            root.path().join("proc/net/tcp"),
            format!("{PROC_TCP}   3: 00000000:2661 00000000:0000 0A 0\n"),
        )
        .unwrap();
        std::fs::create_dir_all(root.path().join("etc/hazelminx")).unwrap();
        let mut r = replies();
        r[0] = output(0, "curl\t8.6.0-1\tii \nopenssh-server\t1:9.2p1-2\tii \n");
        r[1] = output(0, "ssh.service enabled enabled\ncron.service enabled enabled\nhazelminx-agent.service enabled enabled\n");
        r[2] = output(1, "");
        let after = capture(Arc::new(RecordingRunner::with_replies(r)), root.path(), &paths()).await;
        let c = compare(&before, &after);
        assert!(!c.identical);
        let added = |name: &str| match &c.sections[name] {
            SectionDiff::Different { added, .. } => added.clone(),
            other => panic!("{name}: {other:?}"),
        };
        assert_eq!(added("users"), ["hazelminx-agent"]);
        assert_eq!(added("listening"), ["tcp 0.0.0.0 9825"]);
        assert_eq!(added("platform_paths"), ["/etc/hazelminx"]);
        assert_eq!(added("enabled_units"), ["hazelminx-agent.service"]);
        let SectionDiff::Different { changed, .. } = &c.sections["packages"] else {
            panic!()
        };
        assert_eq!(changed[0].item, "curl");
        assert_eq!(changed[0].before, "8.5.0-1");
        assert!(matches!(
            c.sections["firewall"],
            SectionDiff::NotComparable { .. }
        ));
        assert_eq!(c.sections["groups"], SectionDiff::Same);
    }

    #[tokio::test]
    async fn unreadable_sections_are_marked_not_guessed() {
        let root = tempfile::tempdir().unwrap();
        let s = capture(Arc::new(RecordingRunner::default()), root.path(), &[]).await;
        assert!(matches!(s.packages, Section::Unavailable { .. }));
        assert!(matches!(s.users, Section::Unavailable { .. }));
        assert!(matches!(s.listening, Section::Unavailable { .. }));
        // No firewall tool is a fact, not a failure.
        assert_eq!(
            s.firewall,
            Section::Captured {
                data: Firewall {
                    backend: "none".into(),
                    rule_count: 0
                }
            }
        );
        assert!(!compare(&s, &s).identical);
    }
}
