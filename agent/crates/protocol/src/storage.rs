//! `host.storage` v1: block devices, mounts, mount configuration and the
//! per-category usage of each filesystem (ticket 030.1).
//!
//! Capacity and inodes of mounted filesystems travel in `metrics_report`
//! (`host.metrics`); this inventory says what the disks, partitions and
//! mounts are and how each mount is configured. A `null` is "not
//! provided", never a zero.

use serde::{Deserialize, Serialize};
use time::OffsetDateTime;

/// A block device: a whole disk (`/sys/block/X`) or one of its partitions.
/// `class` is the disk class of `metrics::Disk` (`physical`, `raid`,
/// `mapper`, `virtual`, `loop`, `network`); a partition has its disk's.
/// Filesystem and partition identity come from the udev database
/// (`/run/udev/data`) or the `/dev/disk/by-*` links; without either they
/// are `null`.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct BlockDevice {
    pub name: String,
    /// `disk` or `partition`.
    pub kind: String,
    /// The disk a partition belongs to.
    pub parent: Option<String>,
    /// `major:minor`.
    pub dev: String,
    pub class: String,
    pub size_bytes: Option<u64>,
    pub model: Option<String>,
    pub serial: Option<String>,
    pub transport: Option<String>,
    pub rotational: Option<bool>,
    pub removable: Option<bool>,
    pub read_only: Option<bool>,
    pub fs_type: Option<String>,
    pub fs_uuid: Option<String>,
    pub fs_label: Option<String>,
    pub part_uuid: Option<String>,
    pub part_label: Option<String>,
    /// Devices built on this one (`/sys/class/block/X/holders`: LVM, LUKS,
    /// RAID).
    pub holders: Vec<String>,
}

/// How a mount names what it mounts. `uuid`, `label`, `partuuid`,
/// `partlabel` and `id` (`/dev/disk/by-id`) survive a disk moving to
/// another port; `path` (`/dev/disk/by-path`) names the port itself;
/// `device` is a kernel name such as `/dev/sdb1`, which can point at
/// another disk after a reboot or a re-plug; `mapper` is a device-mapper
/// or RAID name; `network` a remote share; `virtual` a filesystem without
/// a device (tmpfs); `other` anything else.
pub mod reference_kind {
    pub const UUID: &str = "uuid";
    pub const LABEL: &str = "label";
    pub const PARTUUID: &str = "partuuid";
    pub const PARTLABEL: &str = "partlabel";
    pub const ID: &str = "id";
    pub const PATH: &str = "path";
    pub const DEVICE: &str = "device";
    pub const MAPPER: &str = "mapper";
    pub const NETWORK: &str = "network";
    pub const VIRTUAL: &str = "virtual";
    pub const OTHER: &str = "other";
}

/// One configured mount: a line of `/etc/fstab` or a systemd `.mount`
/// unit under `/etc/systemd/system`. `boot` is `auto` (mounted at boot),
/// `onDemand` (`x-systemd.automount`) or `noauto`.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct MountConfig {
    /// `fstab` or `systemdUnit`.
    pub origin: String,
    /// The file that holds it.
    pub file: String,
    /// Line number in `file`, from 1; `null` for a unit.
    pub line: Option<u32>,
    /// What it mounts, as written (`UUID=…`, `/dev/sdb1`, `host:/export`).
    pub spec: String,
    pub reference: String,
    pub mount_point: String,
    pub fs_type: String,
    pub options: String,
    pub boot: String,
    /// The block device `spec` names on this host now; `null` when it is
    /// not a block device or names none present.
    pub resolves_to: Option<String>,
}

/// A mounted filesystem (`/proc/self/mountinfo`; a later mount on the
/// same point hides the earlier one and is not listed). `configs` are the
/// indexes in `StorageInventory::configs` of the entries for this mount
/// point; none means it was mounted by hand and will not come back after
/// a reboot. `reference` is how it is named: from its configuration when
/// there is one, from the mounted source otherwise.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Mount {
    pub mount_point: String,
    pub source: String,
    /// The block device behind it; `null` for network and virtual ones.
    pub device: Option<String>,
    /// `major:minor` from mountinfo.
    pub dev: String,
    pub fs_type: String,
    /// `local`, `network` or `virtual` (as `metrics::Filesystem`).
    pub class: String,
    pub options: String,
    pub read_only: bool,
    pub reference: String,
    pub configs: Vec<u32>,
}

/// A swap area of `/proc/swaps`. `kind` is `partition` or `file`. How
/// much swap is in use travels in `metrics_report`.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct SwapArea {
    pub name: String,
    pub kind: String,
    pub size_bytes: u64,
    pub priority: i32,
    pub configs: Vec<u32>,
}

/// What is wrong with the mount configuration.
/// - `duplicateMountPoint`: two entries mount the same point.
/// - `missingDevice`: an entry names a device this host does not have
///   (with `nofail` absent, boot waits for it).
/// - `deviceName`: a local mount is named by kernel device name.
/// - `notMounted`: a boot-time entry is not mounted now.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct MountProblem {
    pub kind: String,
    pub mount_point: String,
    /// Indexes in `StorageInventory::configs`.
    pub configs: Vec<u32>,
}

/// `host_storage`: sent after `ready` and whenever it changes.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct StorageInventory {
    pub devices: Vec<BlockDevice>,
    pub mounts: Vec<Mount>,
    pub configs: Vec<MountConfig>,
    pub swaps: Vec<SwapArea>,
    pub problems: Vec<MountProblem>,
    /// Sources the agent could not read (`/etc/fstab`, `/run/udev/data`),
    /// so a reader knows a missing entry may only be unknown.
    pub unreadable: Vec<String>,
}

/// Usage categories. Every disk uses the same set; a path counts in one
/// category only and `other` is the rest of the used space.
pub mod category {
    pub const APP_DATA: &str = "appData";
    pub const DATABASE: &str = "database";
    pub const BACKUP_STAGING: &str = "backupStaging";
    pub const CONTAINER_IMAGES: &str = "containerImages";
    pub const OS_AND_LOGS: &str = "osAndLogs";
    pub const OTHER: &str = "other";
}

/// Bytes one category holds on one filesystem, and the measured
/// directories that make it up.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct CategoryUsage {
    pub category: String,
    pub bytes: u64,
    pub paths: Vec<String>,
}

/// The usage of one mounted local filesystem. `state` is `complete`,
/// `partial` (the walk hit its entry or time limit, or some directories
/// could not be read; the measured bytes are a lower bound) or
/// `unmeasured` (no statvfs). `categories` lists only categories with
/// bytes; `otherBytes` is used space no category claims, `null` unless
/// `complete`. Hard-linked files count once.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct FilesystemUsage {
    pub mount_point: String,
    pub dev: String,
    pub state: String,
    /// statvfs `total - free` when the walk ended.
    pub used_bytes: Option<u64>,
    pub categories: Vec<CategoryUsage>,
    pub other_bytes: Option<u64>,
    /// Directories that could not be read.
    pub unreadable_dirs: u64,
}

/// `storage_usage`: one measurement round, sent when it finishes.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct StorageUsage {
    #[serde(with = "time::serde::rfc3339")]
    pub started_at: OffsetDateTime,
    pub duration_ms: u64,
    pub entries: u64,
    pub filesystems: Vec<FilesystemUsage>,
}

/// Filesystem types that hold no user data (kernel interfaces, container
/// layers, RAM). They are not reported.
pub const PSEUDO_FS: &[&str] = &[
    "proc",
    "sysfs",
    "cgroup",
    "cgroup2",
    "devpts",
    "devtmpfs",
    "mqueue",
    "debugfs",
    "tracefs",
    "securityfs",
    "pstore",
    "bpf",
    "configfs",
    "fusectl",
    "hugetlbfs",
    "autofs",
    "binfmt_misc",
    "efivarfs",
    "nsfs",
    "rpc_pipefs",
    "selinuxfs",
    "overlay",
    "squashfs",
    "tmpfs",
    "ramfs",
    "fuse.gvfsd-fuse",
    "fuse.portal",
    "nfsd",
];

/// Network filesystems: not walked for usage, statvfs bounded in time.
pub const NETWORK_FS: &[&str] = &[
    "nfs",
    "nfs4",
    "cifs",
    "smb3",
    "smbfs",
    "ceph",
    "glusterfs",
    "fuse.glusterfs",
    "fuse.sshfs",
    "fuse.rclone",
    "fuse.s3fs",
    "davfs",
    "9p",
    "afs",
    "lustre",
];

/// Mount class from the filesystem type: `network`, `virtual` (FUSE) or
/// `local`. Pseudo filesystems are left out before this.
pub fn mount_class(fs_type: &str) -> &'static str {
    if NETWORK_FS.contains(&fs_type) {
        "network"
    } else if fs_type.starts_with("fuse") {
        "virtual"
    } else {
        "local"
    }
}
