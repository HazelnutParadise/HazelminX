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

    pub fn mounts(&self) -> Vec<hazelminx_protocol::storage::Mount> {
        let st = self.state.lock().unwrap();
        st.inventory
            .as_ref()
            .map(|(_, i)| i.mounts.clone())
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
                    if storage.set_inventory(now, inv.read_all()) {
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
            std::thread::sleep(USAGE_FIRST);
            loop {
                let mounts = storage.mounts();
                let u = usage::measure(&root, &mounts, usage::Budget::default(), |p| {
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
