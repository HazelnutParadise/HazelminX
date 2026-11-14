//! `host.metrics` v1: host facts and resource samples (ticket 020.1).
//!
//! Every value the agent could not read is `null` on the wire. A `null` is
//! "not provided"; it is never a zero.

use serde::{Deserialize, Serialize};
use time::OffsetDateTime;

/// A host fact and where it was read from (`source` is a path or an API
/// name, e.g. `/etc/os-release`). `value` is `null` when no source had it.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Fact<T> {
    pub value: Option<T>,
    pub source: String,
}

impl<T> Fact<T> {
    pub fn new(value: Option<T>, source: impl Into<String>) -> Self {
        Fact {
            value,
            source: source.into(),
        }
    }
}

/// What a host is. Sent after `ready` and whenever a value changes.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct HostFacts {
    pub hostname: Fact<String>,
    /// `NAME` of os-release, e.g. "Debian GNU/Linux".
    pub os_name: Fact<String>,
    /// `VERSION_ID`, e.g. "13".
    pub os_version: Fact<String>,
    /// `PRETTY_NAME`.
    pub os_pretty_name: Fact<String>,
    /// Kernel release, e.g. "6.12.48+deb13-amd64".
    pub kernel: Fact<String>,
    /// Machine architecture as the kernel names it, e.g. "x86_64".
    pub arch: Fact<String>,
    /// RFC 3339 UTC, whole seconds.
    pub boot_time: Fact<String>,
    /// IANA name, e.g. "Asia/Taipei".
    pub timezone: Fact<String>,
    /// Whether the kernel clock is synchronized to a time source.
    pub time_synchronized: Fact<bool>,
    /// The kernel's maximum clock error estimate, microseconds.
    pub time_max_error_micros: Fact<u64>,
    pub cpu_model: Fact<String>,
    pub logical_cpus: Fact<u32>,
    pub memory_total_bytes: Fact<u64>,
}

/// Share of one CPU (or of all of them) over the sample interval, percent
/// (0–100). `usage` is everything but idle and iowait.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct CpuUsage {
    pub usage: f64,
    pub user: f64,
    pub system: f64,
    pub iowait: f64,
    pub steal: f64,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct CoreUsage {
    /// The kernel's CPU number (`cpuN` in /proc/stat).
    pub id: u32,
    pub usage: CpuUsage,
}

/// `/proc/stat` deltas. `total` is `null` and `cores` empty on the first
/// sample after start (no interval yet).
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Cpu {
    pub total: Option<CpuUsage>,
    pub cores: Vec<CoreUsage>,
}

/// `/proc/meminfo`, bytes. `used` is `total - available`: page cache and
/// reclaimable slab count as available, not as used.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Memory {
    pub total_bytes: Option<u64>,
    pub available_bytes: Option<u64>,
    pub used_bytes: Option<u64>,
    pub free_bytes: Option<u64>,
    pub buffers_bytes: Option<u64>,
    pub cached_bytes: Option<u64>,
    pub shmem_bytes: Option<u64>,
    pub reclaimable_slab_bytes: Option<u64>,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Swap {
    pub total_bytes: Option<u64>,
    pub free_bytes: Option<u64>,
    pub used_bytes: Option<u64>,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Load {
    pub one: Option<f64>,
    pub five: Option<f64>,
    pub fifteen: Option<f64>,
}

/// One `/proc/pressure/*` file, percent. `full*` is `null` where the kernel
/// has no `full` line (cpu before 5.13).
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct PressureLine {
    pub some_avg10: f64,
    pub some_avg60: f64,
    pub some_avg300: f64,
    pub full_avg10: Option<f64>,
    pub full_avg60: Option<f64>,
    pub full_avg300: Option<f64>,
}

/// Linux PSI. `state` is `ok`, `unsupported` (no /proc/pressure: the
/// kernel has no PSI), `disabled` (PSI built in but turned off, e.g.
/// `psi=0`) or `unreadable`; the three lines are `null` unless `ok`.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Pressure {
    pub state: String,
    pub cpu: Option<PressureLine>,
    pub memory: Option<PressureLine>,
    pub io: Option<PressureLine>,
}

/// Exhaustion counters (ROB-009).
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Limits {
    /// Tasks (processes and threads) that hold a PID.
    pub tasks: Option<u64>,
    /// `/proc/sys/kernel/pid_max`.
    pub pid_max: Option<u64>,
    /// Allocated file handles (`/proc/sys/fs/file-nr`, first minus second).
    pub file_handles: Option<u64>,
    /// `/proc/sys/fs/file-max`.
    pub file_max: Option<u64>,
}

/// A mounted filesystem. `class` is `local`, `network` or `virtual`.
/// `state` is `ok`, `unresponsive` (statvfs did not return in time; values
/// are `null`) or `unreadable`. `inodes` is `ok` or `notApplicable` (no
/// fixed inode count, e.g. btrfs, ZFS); inode values are `null` unless `ok`.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Filesystem {
    pub mount_point: String,
    pub device: String,
    pub fs_type: String,
    pub class: String,
    pub read_only: bool,
    pub state: String,
    pub total_bytes: Option<u64>,
    pub free_bytes: Option<u64>,
    /// Free for unprivileged users (excludes the root reserve).
    pub available_bytes: Option<u64>,
    pub used_bytes: Option<u64>,
    pub inodes: String,
    pub inodes_total: Option<u64>,
    pub inodes_free: Option<u64>,
}

/// A block device of `/sys/block`. `class` is `physical`, `raid` (md),
/// `mapper` (device-mapper: LVM, LUKS), `virtual` (virtio, Xen, zram,
/// ram), `loop` or `network` (nbd, rbd). Rates are over the sample
/// interval and `null` on the first sample.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Disk {
    pub name: String,
    pub class: String,
    pub model: Option<String>,
    pub transport: Option<String>,
    pub rotational: Option<bool>,
    pub removable: Option<bool>,
    pub size_bytes: Option<u64>,
    pub read_bytes_per_second: Option<f64>,
    pub write_bytes_per_second: Option<f64>,
    pub reads_per_second: Option<f64>,
    pub writes_per_second: Option<f64>,
    /// Average time per completed read, milliseconds; `null` when no read
    /// completed in the interval.
    pub read_latency_ms: Option<f64>,
    pub write_latency_ms: Option<f64>,
    /// Share of the interval the device was busy, percent.
    pub utilization: Option<f64>,
}

/// A network interface. `class` is `physical`, `virtual` or `loopback`.
/// Rates are over the sample interval; error and drop counts are totals
/// since boot.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Network {
    pub name: String,
    pub class: String,
    pub oper_state: Option<String>,
    pub speed_mbps: Option<u64>,
    pub rx_bytes_per_second: Option<f64>,
    pub tx_bytes_per_second: Option<f64>,
    pub rx_packets_per_second: Option<f64>,
    pub tx_packets_per_second: Option<f64>,
    pub rx_errors: Option<u64>,
    pub tx_errors: Option<u64>,
    pub rx_drops: Option<u64>,
    pub tx_drops: Option<u64>,
}

/// A temperature sensor. `source` is `hwmon` or `thermal_zone`; `chip` is
/// the hwmon `name` or the zone `type`.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Temperature {
    pub source: String,
    pub chip: String,
    pub label: String,
    pub celsius: f64,
    pub high_celsius: Option<f64>,
    pub critical_celsius: Option<f64>,
}

/// One collection round.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct MetricsSample {
    /// Increases by one per sample while the agent runs; starts at 1 again
    /// after a restart.
    pub seq: u64,
    #[serde(with = "time::serde::rfc3339")]
    pub collected_at: OffsetDateTime,
    /// The configured collection interval.
    pub interval_seconds: u32,
    pub cpu: Cpu,
    pub memory: Memory,
    pub swap: Swap,
    pub load: Load,
    pub pressure: Pressure,
    pub limits: Limits,
    pub filesystems: Vec<Filesystem>,
    pub disks: Vec<Disk>,
    pub networks: Vec<Network>,
    pub temperatures: Vec<Temperature>,
}
