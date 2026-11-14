//! Measures how much of each local filesystem a fixed set of directories
//! holds, by category. Only those directories are walked; the rest of the
//! used space is `other`. A walk stays on the filesystem of the directory
//! it starts in, counts allocated blocks (not apparent sizes) and counts a
//! hard-linked file once.
//!
//! Each path belongs to one category: a directory that is itself a root
//! of another category (`/var/lib/docker/volumes` inside
//! `/var/lib/docker`) is left to that root.

use std::collections::{BTreeMap, HashSet};
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use hazelminx_protocol::storage::{CategoryUsage, FilesystemUsage, Mount, StorageUsage, category};
use time::OffsetDateTime;

/// What is measured. Databases and backup staging the platform manages
/// get their own roots when those features exist (tickets 069, 078);
/// databases installed outside the platform count as app data.
pub const ROOTS: &[(&str, &str)] = &[
    // Container engine storage: image layers, plus container writable
    // layers, which the engines keep in the same directories.
    ("/var/lib/docker", category::CONTAINER_IMAGES),
    ("/var/lib/containerd", category::CONTAINER_IMAGES),
    ("/var/lib/containers/storage", category::CONTAINER_IMAGES),
    (
        "/var/lib/rancher/k3s/agent/containerd",
        category::CONTAINER_IMAGES,
    ),
    // Named volumes hold application data.
    ("/var/lib/docker/volumes", category::APP_DATA),
    ("/var/lib/containers/storage/volumes", category::APP_DATA),
    // Databases installed outside the platform.
    ("/var/lib/mysql", category::APP_DATA),
    ("/var/lib/mariadb", category::APP_DATA),
    ("/var/lib/postgresql", category::APP_DATA),
    ("/var/lib/pgsql", category::APP_DATA),
    ("/var/lib/mongodb", category::APP_DATA),
    ("/var/lib/redis", category::APP_DATA),
    // Container logs.
    ("/var/lib/docker/containers", category::OS_AND_LOGS),
    // Operating system, packages and logs.
    ("/usr", category::OS_AND_LOGS),
    ("/etc", category::OS_AND_LOGS),
    ("/boot", category::OS_AND_LOGS),
    ("/bin", category::OS_AND_LOGS),
    ("/sbin", category::OS_AND_LOGS),
    ("/lib", category::OS_AND_LOGS),
    ("/lib64", category::OS_AND_LOGS),
    ("/var/log", category::OS_AND_LOGS),
    ("/var/cache", category::OS_AND_LOGS),
    ("/var/lib/dpkg", category::OS_AND_LOGS),
    ("/var/lib/apt", category::OS_AND_LOGS),
    ("/var/lib/rpm", category::OS_AND_LOGS),
    ("/var/lib/dnf", category::OS_AND_LOGS),
    ("/var/lib/pacman", category::OS_AND_LOGS),
    ("/var/lib/snapd", category::OS_AND_LOGS),
];

/// Limits of one round.
#[derive(Debug, Clone, Copy)]
pub struct Budget {
    /// Entries (files and directories) one round may look at.
    pub max_entries: u64,
    /// Wall time one round may take.
    pub max_time: Duration,
    /// After this many entries the walk sleeps `pause`.
    pub batch: u64,
    pub pause: Duration,
    /// Hard-linked inodes remembered to count them once; past this the
    /// round is `partial`.
    pub max_links: usize,
}

impl Default for Budget {
    fn default() -> Self {
        Budget {
            max_entries: 5_000_000,
            max_time: Duration::from_secs(20 * 60),
            batch: 2048,
            pause: Duration::from_millis(10),
            max_links: 1_000_000,
        }
    }
}

#[derive(Default)]
struct Tally {
    /// The mount point the measured roots were found under.
    mount_point: Option<String>,
    by_cat: BTreeMap<&'static str, (u64, Vec<String>)>,
    unreadable: u64,
    partial: bool,
}

struct Walk<'a> {
    budget: Budget,
    started: Instant,
    entries: u64,
    links: HashSet<(u64, u64)>,
    /// Roots (joined to the tree root) that other walks own.
    roots: &'a HashSet<PathBuf>,
    /// Every mount point of the host (joined to the tree root): the walk
    /// does not cross into another mount. Device numbers cannot tell,
    /// since btrfs gives each subvolume its own.
    points: &'a HashSet<PathBuf>,
    out_of_budget: bool,
}

impl Walk<'_> {
    fn tick(&mut self) -> bool {
        self.entries += 1;
        if self.entries >= self.budget.max_entries || self.started.elapsed() >= self.budget.max_time
        {
            self.out_of_budget = true;
            return false;
        }
        if self.entries.is_multiple_of(self.budget.batch) && !self.budget.pause.is_zero() {
            std::thread::sleep(self.budget.pause);
        }
        true
    }

    /// Adds the allocated bytes under `top` to `t`; false when the walk
    /// ran out of budget part way or could not read everything.
    fn walk(&mut self, top: &Path, t: &mut u64, unreadable: &mut u64) -> bool {
        let mut stack = vec![top.to_path_buf()];
        let mut complete = true;
        while let Some(dir) = stack.pop() {
            let it = match std::fs::read_dir(&dir) {
                Ok(it) => it,
                Err(_) => {
                    *unreadable += 1;
                    complete = false;
                    continue;
                }
            };
            for e in it {
                if !self.tick() {
                    return false;
                }
                let Ok(e) = e else {
                    complete = false;
                    continue;
                };
                let p = e.path();
                if self.roots.contains(&p) || self.points.contains(&p) {
                    continue;
                }
                let Ok(md) = std::fs::symlink_metadata(&p) else {
                    continue;
                };
                let (d, ino, nlink, blocks) = meta(&md);
                if md.is_dir() {
                    stack.push(p);
                } else if nlink > 1 {
                    if self.links.len() >= self.budget.max_links {
                        complete = false;
                    } else if !self.links.insert((d, ino)) {
                        continue;
                    }
                }
                *t += blocks;
            }
        }
        complete
    }
}

#[cfg(unix)]
fn meta(md: &std::fs::Metadata) -> (u64, u64, u64, u64) {
    use std::os::unix::fs::MetadataExt;
    (
        md.dev(),
        md.ino(),
        md.nlink(),
        md.blocks().saturating_mul(512),
    )
}

#[cfg(not(unix))]
fn meta(md: &std::fs::Metadata) -> (u64, u64, u64, u64) {
    (0, 0, 1, md.len())
}

/// One measurement round over the local mounts of the inventory. `root`
/// is joined to every path (tests use a fake tree); `points` are all mount
/// points of the host, the virtual ones too (`/proc`, container overlays);
/// `used` reads a mount's `total - free` (statvfs).
pub fn measure(
    root: &Path,
    mounts: &[Mount],
    points: &[String],
    budget: Budget,
    used: impl Fn(&Path) -> Option<u64>,
) -> StorageUsage {
    let started_at = OffsetDateTime::now_utc();
    let started_at = started_at.replace_nanosecond(0).unwrap_or(started_at);
    let join = |p: &str| root.join(p.trim_start_matches('/'));
    let roots: HashSet<PathBuf> = ROOTS.iter().map(|(p, _)| join(p)).collect();
    let point_set: HashSet<PathBuf> = points.iter().map(|p| join(p)).collect();
    let mut w = Walk {
        budget,
        started: Instant::now(),
        entries: 0,
        links: HashSet::new(),
        roots: &roots,
        points: &point_set,
        out_of_budget: false,
    };
    let local: Vec<&Mount> = mounts.iter().filter(|m| m.class == "local").collect();
    let mut tallies: BTreeMap<String, Tally> = BTreeMap::new();
    for (path, cat) in ROOTS {
        let p = join(path);
        let Ok(md) = std::fs::symlink_metadata(&p) else {
            continue;
        };
        // /lib -> usr/lib on merged-/usr systems: /usr counts it.
        if !md.is_dir() {
            continue;
        }
        // The filesystem a root is on is the mount with the longest mount
        // point above it; a root under a network or virtual mount (a
        // container's overlay root) is not measured.
        let owner = points
            .iter()
            .filter(|mp| under(path, mp))
            .max_by_key(|mp| mp.len());
        let Some(m) = owner.and_then(|mp| local.iter().find(|m| &m.mount_point == mp)) else {
            continue;
        };
        let t = tallies.entry(m.dev.clone()).or_default();
        t.mount_point.get_or_insert_with(|| m.mount_point.clone());
        let mut bytes = meta(&md).3;
        if w.out_of_budget {
            t.partial = true;
            continue;
        }
        if !w.walk(&p, &mut bytes, &mut t.unreadable) {
            t.partial = true;
        }
        let e = t.by_cat.entry(cat).or_default();
        e.0 += bytes;
        e.1.push(path.to_string());
    }
    let mut filesystems = Vec::new();
    // A filesystem mounted at several points (bind mounts) is listed once:
    // under the mount point its roots were found under, else the shortest.
    let mut seen = HashSet::new();
    let mut ordered = local.clone();
    ordered.sort_by_key(|m| (m.mount_point.len(), m.mount_point.clone()));
    for m in ordered {
        if !seen.insert(m.dev.clone()) {
            continue;
        }
        let t = tallies.remove(&m.dev).unwrap_or_default();
        let mount_point = t
            .mount_point
            .clone()
            .unwrap_or_else(|| m.mount_point.clone());
        let used_bytes = used(&join(&mount_point));
        let sum: u64 = t.by_cat.values().map(|(b, _)| *b).sum();
        let state = if used_bytes.is_none() {
            "unmeasured"
        } else if t.partial || t.unreadable > 0 {
            "partial"
        } else {
            "complete"
        };
        let other_bytes = (state == "complete")
            .then(|| used_bytes.map(|u| u.saturating_sub(sum)))
            .flatten();
        filesystems.push(FilesystemUsage {
            mount_point,
            dev: m.dev.clone(),
            state: state.into(),
            used_bytes,
            categories: t
                .by_cat
                .into_iter()
                .filter(|(_, (b, _))| *b > 0)
                .map(|(c, (bytes, paths))| CategoryUsage {
                    category: c.into(),
                    bytes,
                    paths,
                })
                .collect(),
            other_bytes,
            unreadable_dirs: t.unreadable,
        });
    }
    filesystems.sort_by(|a, b| a.mount_point.cmp(&b.mount_point));
    StorageUsage {
        started_at,
        duration_ms: w.started.elapsed().as_millis() as u64,
        entries: w.entries,
        filesystems,
    }
}

/// Whether `path` is `mount_point` or below it.
fn under(path: &str, mount_point: &str) -> bool {
    mount_point == "/"
        || path == mount_point
        || path
            .strip_prefix(mount_point)
            .is_some_and(|rest| rest.starts_with('/'))
}
