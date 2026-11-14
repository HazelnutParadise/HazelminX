//! `storage_usage` (ticket 030.1): measures how much of each local
//! filesystem a fixed set of directories holds, by category. It runs here,
//! as root, because the agent's account cannot read engine storage
//! (`/var/lib/docker` is 0710 root) or database directories (0700 of their
//! service accounts). It takes no parameters: the roots, the budget and the
//! mounts (read from mountinfo) are fixed here, not chosen by a caller. Only those directories are walked; the rest of the
//! used space is `other`. A walk stays on the filesystem of the directory
//! it starts in, counts allocated blocks (not apparent sizes) and counts a
//! hard-linked file once.
//!
//! Each path belongs to one category: a directory that is itself a root
//! of another category (`/var/lib/docker/volumes` inside
//! `/var/lib/docker`) is left to that root.

use std::collections::{BTreeMap, HashSet};
use std::path::{Path, PathBuf};

use crate::executor::{ActionError, ActionResult};
use std::time::{Duration, Instant};

use hazelminx_protocol::storage::{
    CategoryUsage, FilesystemUsage, PSEUDO_FS, StorageUsage, category, mount_class,
};
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
        // Each directory carries the (device, inode) seen when it was
        // queued; opening it must reach the same one, so swapping it, or
        // any directory above it, for a symlink does not redirect the walk.
        let Ok(top_md) = std::fs::symlink_metadata(top) else {
            *unreadable += 1;
            return false;
        };
        let (td, ti, ..) = meta(&top_md);
        let mut stack = vec![(top.to_path_buf(), (td, ti))];
        let mut complete = true;
        while let Some((dir, seen)) = stack.pop() {
            let it = match open_dir(&dir, seen) {
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
                // The entry's name under the real path: listed through
                // /proc/self/fd, e.path() would name the descriptor.
                let p = dir.join(e.file_name());
                if self.roots.contains(&p) || self.points.contains(&p) {
                    continue;
                }
                let Ok(md) = std::fs::symlink_metadata(&p) else {
                    continue;
                };
                let (d, ino, nlink, blocks) = meta(&md);
                if md.is_dir() {
                    stack.push((p, (d, ino)));
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
        // More measured than used (shared extents from reflinks or btrfs
        // snapshots, compression, files written during the walk): the
        // categories cannot add up, so the round is not complete.
        let over = used_bytes.is_some_and(|u| sum > u);
        let state = if used_bytes.is_none() {
            "unmeasured"
        } else if t.partial || t.unreadable > 0 || over {
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

/// Lists a directory without following a symlink at its own path, so a
/// directory swapped for a link to `/` after it was seen (a container
/// writing into a volume) is not walked as root. On Linux the directory is
/// opened with `O_NOFOLLOW | O_DIRECTORY` and read through that descriptor.
#[cfg(target_os = "linux")]
fn open_dir(dir: &Path, seen: (u64, u64)) -> std::io::Result<std::fs::ReadDir> {
    use std::os::unix::fs::OpenOptionsExt;
    use std::os::unix::io::AsRawFd;
    let f = std::fs::OpenOptions::new()
        .read(true)
        .custom_flags(libc::O_NOFOLLOW | libc::O_DIRECTORY)
        .open(dir)?;
    // O_NOFOLLOW only covers the last component; the descriptor must be the
    // directory that was seen, whatever happened to the path above it.
    let (d, i, ..) = meta(&f.metadata()?);
    if (d, i) != seen {
        return Err(std::io::Error::other("directory changed since it was seen"));
    }
    // read_dir opens its own descriptor of the directory f refers to.
    std::fs::read_dir(format!("/proc/self/fd/{}", f.as_raw_fd()))
}

/// Without /proc/self/fd: the same check, by path (a short window remains
/// between the check and the listing).
#[cfg(not(target_os = "linux"))]
fn open_dir(dir: &Path, seen: (u64, u64)) -> std::io::Result<std::fs::ReadDir> {
    let md = std::fs::symlink_metadata(dir)?;
    let (d, i, ..) = meta(&md);
    if md.file_type().is_symlink() || (d, i) != seen {
        return Err(std::io::Error::other("directory changed since it was seen"));
    }
    std::fs::read_dir(dir)
}

/// One walk at a time: a request whose caller went away keeps running (the
/// blocking thread cannot be cancelled), so another must not start beside
/// it and hold a second action slot for many minutes.
static RUNNING: std::sync::atomic::AtomicBool = std::sync::atomic::AtomicBool::new(false);

struct RunningGuard;

impl RunningGuard {
    fn take() -> Option<RunningGuard> {
        use std::sync::atomic::Ordering;
        RUNNING
            .compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
            .ok()
            .map(|_| RunningGuard)
    }
}

impl Drop for RunningGuard {
    fn drop(&mut self) {
        RUNNING.store(false, std::sync::atomic::Ordering::Release);
    }
}

/// The host's own view: the executor's unit hides `/home` (`ProtectHome`)
/// and `/tmp` in its mount namespace, so it reads PID 1's mountinfo and
/// walks through `/proc/1/root` when it can, and its own otherwise.
fn host_view(sysroot: &Path) -> (PathBuf, std::io::Result<String>) {
    let init = sysroot.join("proc/1/root");
    if let Ok(t) = std::fs::read_to_string(sysroot.join("proc/1/mountinfo"))
        && std::fs::read_dir(&init).is_ok()
    {
        return (init, Ok(t));
    }
    (
        sysroot.to_path_buf(),
        std::fs::read_to_string(sysroot.join("proc/self/mountinfo")),
    )
}

/// A local mount as the walk needs it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Mount {
    pub mount_point: String,
    /// `major:minor` from mountinfo.
    pub dev: String,
    pub class: &'static str,
}

/// Every mount point of mountinfo (pseudo filesystems too, so the walk
/// stops at them) and the mounts worth measuring; a later mount on the
/// same point hides the earlier one.
pub fn mounts_from(mountinfo: &str) -> (Vec<Mount>, Vec<String>) {
    let mut points = Vec::new();
    let mut by_point = BTreeMap::new();
    for line in mountinfo.lines() {
        let Some((left, right)) = line.split_once(" - ") else {
            continue;
        };
        let l: Vec<&str> = left.split(' ').collect();
        let fs_type = right.split(' ').next().unwrap_or("");
        if l.len() < 6 {
            continue;
        }
        let point = unescape(l[4]);
        points.push(point.clone());
        if PSEUDO_FS.contains(&fs_type) {
            by_point.remove(&point);
            continue;
        }
        by_point.insert(
            point.clone(),
            Mount {
                mount_point: point,
                dev: l[2].to_string(),
                class: mount_class(fs_type),
            },
        );
    }
    (by_point.into_values().collect(), points)
}

/// Undoes the octal escapes of mountinfo (`\040` for a space).
fn unescape(s: &str) -> String {
    let b = s.as_bytes();
    let mut out = Vec::with_capacity(b.len());
    let mut i = 0;
    while i < b.len() {
        if b[i] == b'\\'
            && i + 3 < b.len()
            && b[i + 1..i + 4].iter().all(|c| (b'0'..=b'7').contains(c))
        {
            out.push((b[i + 1] - b'0') * 64 + (b[i + 2] - b'0') * 8 + (b[i + 3] - b'0'));
            i += 4;
        } else {
            out.push(b[i]);
            i += 1;
        }
    }
    String::from_utf8_lossy(&out).into_owned()
}

/// `total - free` of the filesystem holding `p`.
#[cfg(unix)]
pub fn statvfs_used(p: &Path) -> Option<u64> {
    use std::os::unix::ffi::OsStrExt;
    let c = std::ffi::CString::new(p.as_os_str().as_bytes()).ok()?;
    let mut st: libc::statvfs = unsafe { std::mem::zeroed() };
    // SAFETY: c is a valid C string and st a valid out pointer.
    if unsafe { libc::statvfs(c.as_ptr(), &mut st) } != 0 {
        return None;
    }
    let f = st.f_frsize as u64;
    Some(
        (st.f_blocks as u64)
            .saturating_sub(st.f_bfree as u64)
            .saturating_mul(f),
    )
}

#[cfg(not(unix))]
pub fn statvfs_used(_: &Path) -> Option<u64> {
    None
}

/// Nice 19 and the idle I/O class for the calling thread only, so the
/// walk yields to everything else on the host.
#[cfg(target_os = "linux")]
fn lowest_priority() {
    // SAFETY: plain syscalls on the calling thread's id.
    unsafe {
        let tid = libc::syscall(libc::SYS_gettid) as libc::id_t;
        libc::setpriority(libc::PRIO_PROCESS, tid, 19);
        // IOPRIO_WHO_PROCESS = 1, IOPRIO_CLASS_IDLE = 3 << 13.
        libc::syscall(libc::SYS_ioprio_set, 1, tid, 3 << 13);
    }
}

#[cfg(not(target_os = "linux"))]
fn lowest_priority() {}

/// The action: one measurement round of the host under `sysroot`, on a
/// blocking thread at the lowest priority.
pub async fn run(sysroot: &Path) -> ActionResult {
    let sysroot = sysroot.to_path_buf();
    let Some(guard) = RunningGuard::take() else {
        return Err(ActionError::new(
            "a storage usage measurement is already running",
        ));
    };
    let usage = tokio::task::spawn_blocking(move || {
        let _guard = guard;
        lowest_priority();
        let (root, text) = host_view(&sysroot);
        let text = text.map_err(|e| ActionError::new(format!("cannot read mountinfo: {e}")))?;
        let (mounts, points) = mounts_from(&text);
        Ok::<_, ActionError>(measure(
            &root,
            &mounts,
            &points,
            Budget::default(),
            statvfs_used,
        ))
    })
    .await
    .map_err(|e| ActionError::new(format!("storage usage task: {e}")))??;
    Ok(serde_json::to_value(usage).expect("StorageUsage serializes"))
}

#[cfg(test)]
mod tests;
