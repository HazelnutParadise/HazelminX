//! Storage inventory and usage (ticket 030.1, `host.storage` v1). A
//! thread reads the inventory every `INVENTORY_INTERVAL`; the session sends
//! it when it changes. Usage is measured by the privileged executor
//! (`storage_usage`, as root) when the Core asks with a grant: the agent's
//! own account cannot read engine storage or database directories.

pub mod inventory;

use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use hazelminx_privileged::action::Action;
use hazelminx_privileged::client::{Client, ClientError};
use hazelminx_privileged::grant::Grant;
use hazelminx_privileged::protocol::ErrorCode;
use hazelminx_protocol::storage::{StorageInventory, StorageUsage};
use hazelminx_protocol::{ExecutorGrant, Message, storage_usage_unavailable_code as code};
use time::OffsetDateTime;

pub const INVENTORY_INTERVAL: Duration = Duration::from_secs(60);

#[derive(Default)]
struct State {
    inventory: Option<(OffsetDateTime, StorageInventory)>,
    inventory_version: u64,
}

/// Shared between the inventory thread and the session.
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
}

/// Starts the inventory thread; `wake` runs after each change so the
/// session sends it.
pub fn spawn(
    storage: Arc<Storage>,
    root: impl Into<PathBuf>,
    wake: impl Fn() + Send + 'static,
) -> std::io::Result<()> {
    let root: PathBuf = root.into();
    std::thread::Builder::new()
        .name("storage".into())
        .spawn(move || {
            let inv = inventory::Inventory::new(root);
            loop {
                let now = OffsetDateTime::now_utc();
                let now = now.replace_nanosecond(0).unwrap_or(now);
                if storage.set_inventory(now, fit(inv.read_all())) {
                    wake();
                }
                std::thread::sleep(INVENTORY_INTERVAL);
            }
        })
        .map(|_| ())
}

/// The answer to one `storage_usage_request`: the executor's measurement,
/// or `storage_usage_unavailable` saying why there is none. Never a
/// measurement of what the agent itself can read.
pub async fn answer(client: &Client, request_id: String, grant: ExecutorGrant) -> Message {
    let unavailable = |c: &str, message: String| Message::StorageUsageUnavailable {
        request_id: request_id.clone(),
        code: c.to_string(),
        message,
    };
    let grant: Grant = match serde_json::to_value(&grant).and_then(serde_json::from_value) {
        Ok(g) => g,
        Err(e) => return unavailable(code::EXECUTOR_REFUSED, format!("grant is malformed: {e}")),
    };
    let value = match client
        .execute(&request_id, &grant, &Action::StorageUsage {}, |_, _| {})
        .await
    {
        Ok(v) => v,
        Err(ClientError::Io(e)) => {
            return unavailable(
                code::EXECUTOR_UNREACHABLE,
                format!("cannot reach the privileged executor: {e}"),
            );
        }
        Err(ClientError::Rejected(b)) if b.code == ErrorCode::ActionFailed => {
            return unavailable(code::MEASURE_FAILED, b.message);
        }
        Err(ClientError::Rejected(b)) => {
            return unavailable(
                code::EXECUTOR_REFUSED,
                format!("{:?}: {}", b.code, b.message),
            );
        }
        Err(ClientError::Protocol(m)) => {
            return unavailable(
                code::MEASURE_FAILED,
                format!("executor answered unexpectedly: {m}"),
            );
        }
    };
    let usage: StorageUsage = match serde_json::from_value(value) {
        Ok(u) => u,
        Err(e) => {
            return unavailable(
                code::MEASURE_FAILED,
                format!("executor returned an unreadable measurement: {e}"),
            );
        }
    };
    let msg = Message::StorageUsage {
        request_id: request_id.clone(),
        usage: Box::new(usage),
    };
    if hazelminx_protocol::encode(&msg).len() > INVENTORY_BYTES {
        return unavailable(
            code::MEASURE_FAILED,
            format!("usage measurement is larger than {INVENTORY_BYTES} bytes"),
        );
    }
    msg
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
    let dev_rank =
        |d: &hazelminx_protocol::storage::BlockDevice| if d.class == "loop" { 0 } else { 2 };
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
    if devices + mounts == 0 {
        return inv;
    }
    inv.unreadable.push(format!(
        "truncated: {devices} devices and {mounts} mounts left out (inventory over {INVENTORY_BYTES} bytes)"
    ));
    inv
}

#[cfg(test)]
mod tests;
