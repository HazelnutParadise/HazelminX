//! Storage inventory and usage (ticket 030.1, `host.storage` v1). One
//! thread reads the inventory every `INVENTORY_INTERVAL`; another measures
//! usage every `USAGE_INTERVAL` at the lowest CPU and I/O priority. The
//! session sends whichever it has not sent yet.

pub mod inventory;
pub mod usage;

use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use hazelminx_protocol::Message;
use hazelminx_protocol::storage::{StorageInventory, StorageUsage};
use time::OffsetDateTime;

pub const INVENTORY_INTERVAL: Duration = Duration::from_secs(60);
/// First usage round after start, so it does not add to startup.
pub const USAGE_FIRST: Duration = Duration::from_secs(5 * 60);
pub const USAGE_INTERVAL: Duration = Duration::from_secs(6 * 60 * 60);

#[derive(Default)]
struct State {
    inventory: Option<(OffsetDateTime, StorageInventory)>,
    inventory_version: u64,
    usage_mounts: Option<Vec<hazelminx_protocol::storage::Mount>>,
    usage: Option<StorageUsage>,
    usage_version: u64,
}

/// Shared between the storage threads and the session.
#[derive(Default)]
pub struct Storage {
    state: Mutex<State>,
}

impl Storage {
    /// Stores a new inventory; true when it differs from the current one.
    pub fn set_inventory(&self, at: OffsetDateTime, inv: StorageInventory) -> bool {
        let mut st = self.state.lock().unwrap();
        if st.inventory.as_ref().is_some_and(|(_, cur)| *cur == inv) {
            return false;
        }
        st.inventory = Some((at, inv));
        st.inventory_version += 1;
        true
    }

    pub fn has_inventory(&self) -> bool {
        self.state.lock().unwrap().usage_mounts.is_some()
    }

    pub fn set_usage_mounts(&self, m: Vec<hazelminx_protocol::storage::Mount>) {
        self.state.lock().unwrap().usage_mounts = Some(m);
    }

    /// Every mount of the latest inventory read, before `fit` cut it.
    pub fn mounts(&self) -> Vec<hazelminx_protocol::storage::Mount> {
        self.state
            .lock()
            .unwrap()
            .usage_mounts
            .clone()
            .unwrap_or_default()
    }

    pub fn set_usage(&self, u: StorageUsage) {
        let mut st = self.state.lock().unwrap();
        st.usage = Some(u);
        st.usage_version += 1;
    }

    /// The inventory message when the session has not sent version `sent`.
    pub fn inventory_after(&self, sent: u64) -> Option<(u64, Message)> {
        let st = self.state.lock().unwrap();
        let (at, inv) = st.inventory.as_ref()?;
        (st.inventory_version > sent).then(|| {
            (
                st.inventory_version,
                Message::HostStorage {
                    collected_at: *at,
                    storage: Box::new(inv.clone()),
                },
            )
        })
    }

    /// The usage message when the session has not sent version `sent`.
    pub fn usage_after(&self, sent: u64) -> Option<(u64, Message)> {
        let st = self.state.lock().unwrap();
        let u = st.usage.as_ref()?;
        (st.usage_version > sent).then(|| {
            (
                st.usage_version,
                Message::StorageUsage {
                    usage: Box::new(u.clone()),
                },
            )
        })
    }
}

/// Starts both threads; `wake` runs after each change so the session
/// sends it.
pub fn spawn(
    storage: Arc<Storage>,
    root: impl Into<PathBuf>,
    wake: impl Fn() + Send + Sync + 'static,
) -> std::io::Result<()> {
    let root: PathBuf = root.into();
    let wake = Arc::new(wake);
    {
        let (storage, root, wake) = (storage.clone(), root.clone(), wake.clone());
        std::thread::Builder::new()
            .name("storage".into())
            .spawn(move || {
                let inv = inventory::Inventory::new(root);
                loop {
                    let now = OffsetDateTime::now_utc();
                    let now = now.replace_nanosecond(0).unwrap_or(now);
                    let full = inv.read_all();
                    // Usage measures every local mount, also the ones a
                    // too-large inventory leaves out of the message.
                    storage.set_usage_mounts(full.mounts.clone());
                    if storage.set_inventory(now, fit(full)) {
                        wake();
                    }
                    std::thread::sleep(INVENTORY_INTERVAL);
                }
            })?;
    }
    std::thread::Builder::new()
        .name("storage-usage".into())
        .spawn(move || {
            lowest_priority();
            std::thread::sleep(usage_first());
            loop {
                // The walk measures the mounts of the inventory; without
                // one yet it would measure nothing.
                while !storage.has_inventory() {
                    std::thread::sleep(Duration::from_secs(1));
                }
                let mounts = storage.mounts();
                let points = all_mount_points(&root);
                let u = usage::measure(&root, &mounts, &points, usage::Budget::default(), |p| {
                    crate::monitor::collect::statvfs_used(p)
                });
                tracing::info!(
                    entries = u.entries,
                    duration_ms = u.duration_ms,
                    "measured storage usage"
                );
                storage.set_usage(u);
                wake();
                std::thread::sleep(USAGE_INTERVAL);
            }
        })
        .map(|_| ())
}

/// Upper bound of one encoded `host_storage`; the Core reads at most 1 MiB
/// per message, and the body is escaped again inside the frame.
pub const INVENTORY_BYTES: usize = 384 * 1024;

/// Drops entries until the inventory fits, the least useful first (loop
/// devices, snap and squashfs mounts, then virtual and network mounts),
/// then the last of the rest, and names what was left out in
/// `unreadable`, so a host with thousands of loop devices still reports
/// its data disks instead of losing its session.
pub fn fit(mut inv: StorageInventory) -> StorageInventory {
    fn len<T: serde::Serialize>(v: &T) -> usize {
        serde_json::to_vec(v).map(|b| b.len()).unwrap_or(usize::MAX)
    }
    // The whole is encoded once; each dropped entry takes its own encoded
    // length and a comma off it, so this stays linear.
    let mut size = len(&inv);
    // Room for the note added below.
    let limit = INVENTORY_BYTES - 200;
    if size <= limit {
        return inv;
    }
    let dev_rank = |d: &hazelminx_protocol::storage::BlockDevice| u8::from(d.class != "loop");
    let mount_rank = |m: &hazelminx_protocol::storage::Mount| {
        if m.fs_type == "squashfs" || m.mount_point.starts_with("/snap/") {
            0
        } else if m.class != "local" {
            1
        } else {
            2
        }
    };
    let (mut devices, mut mounts) = (0, 0);
    let mut keep_dev = vec![true; inv.devices.len()];
    let mut keep_mnt = vec![true; inv.mounts.len()];
    // Candidates by rank, last first within a rank.
    let mut order: Vec<(u8, bool, usize)> = inv
        .devices
        .iter()
        .enumerate()
        .map(|(i, d)| (dev_rank(d), true, i))
        .chain(
            inv.mounts
                .iter()
                .enumerate()
                .map(|(i, m)| (mount_rank(m), false, i)),
        )
        .collect();
    order.sort_by(|a, b| a.0.cmp(&b.0).then(b.2.cmp(&a.2)));
    for (_, is_dev, i) in order {
        if size <= limit {
            break;
        }
        if is_dev {
            size = size.saturating_sub(len(&inv.devices[i]) + 1);
            keep_dev[i] = false;
            devices += 1;
        } else {
            size = size.saturating_sub(len(&inv.mounts[i]) + 1);
            keep_mnt[i] = false;
            mounts += 1;
        }
    }
    let mut k = keep_dev.into_iter();
    inv.devices.retain(|_| k.next().unwrap_or(true));
    let mut k = keep_mnt.into_iter();
    inv.mounts.retain(|_| k.next().unwrap_or(true));
    inv.unreadable.push(format!(
        "truncated: {devices} devices and {mounts} mounts left out (inventory over {INVENTORY_BYTES} bytes)"
    ));
    inv
}

/// Every mount point of mountinfo, virtual ones included.
fn all_mount_points(root: &std::path::Path) -> Vec<String> {
    std::fs::read_to_string(root.join("proc/self/mountinfo"))
        .map(|t| {
            crate::monitor::parse::mountinfo_full(&t)
                .into_iter()
                .map(|m| m.mount_point)
                .collect()
        })
        .unwrap_or_default()
}

/// The delay before the first usage round. Builds with `test-actions`
/// (interop tests, never a release) read it from
/// `HAZELMINX_TEST_USAGE_FIRST_SECONDS`.
fn usage_first() -> Duration {
    #[cfg(feature = "test-actions")]
    if let Some(s) = std::env::var("HAZELMINX_TEST_USAGE_FIRST_SECONDS")
        .ok()
        .and_then(|v| v.parse().ok())
    {
        return Duration::from_secs(s);
    }
    USAGE_FIRST
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

#[cfg(test)]
mod tests;
