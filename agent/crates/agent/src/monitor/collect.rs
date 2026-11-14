//! Reads host facts and one metrics sample from /proc, /sys and statvfs.
//! Paths are joined to `root` so tests can point the collector at a fake
//! tree; production uses `/`.

use std::collections::{BTreeMap, HashSet};
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use hazelminx_protocol::metrics::{
    CoreUsage, Cpu, Disk, Fact, Filesystem, HostFacts, Limits, Load, Memory, MetricsSample,
    Network, Pressure, Swap, Temperature,
};
use time::OffsetDateTime;

use super::parse::{self, DiskCounters, NetCounters};

/// How long statvfs on a network filesystem may take before the mount is
/// reported `unresponsive`.
const STATVFS_TIMEOUT: Duration = Duration::from_secs(2);

pub use hazelminx_protocol::storage::{NETWORK_FS, PSEUDO_FS};

/// Filesystems without a fixed inode table: inode use is not applicable.
const NO_INODE_TABLE: &[&str] = &["btrfs", "zfs", "bcachefs"];

pub struct Collector {
    root: PathBuf,
    interval_seconds: u32,
    seq: u64,
    prev: Option<Previous>,
    /// Mount points whose statvfs has not returned yet; skipped until it
    /// does, so a dead NFS server cannot pile up threads.
    stuck: Arc<Mutex<HashSet<String>>>,
}

struct Previous {
    at: Instant,
    stat: parse::Stat,
    disks: BTreeMap<String, DiskCounters>,
    nets: BTreeMap<String, NetCounters>,
}

impl Collector {
    pub fn new(root: impl Into<PathBuf>, interval_seconds: u32) -> Self {
        Collector {
            root: root.into(),
            interval_seconds,
            seq: 0,
            prev: None,
            stuck: Arc::default(),
        }
    }

    fn path(&self, rel: &str) -> PathBuf {
        self.root.join(rel.trim_start_matches('/'))
    }

    fn read(&self, rel: &str) -> Option<String> {
        std::fs::read_to_string(self.path(rel)).ok()
    }

    fn read_trim(&self, rel: &str) -> Option<String> {
        self.read(rel)
            .map(|s| s.trim().to_string())
            .filter(|s| !s.is_empty())
    }

    fn read_num<T: std::str::FromStr>(&self, rel: &str) -> Option<T> {
        self.read_trim(rel)?.parse().ok()
    }

    /// One collection round. Rates and CPU shares need a previous round, so
    /// the first sample after start has them as `null`.
    pub fn sample(&mut self, collected_at: OffsetDateTime) -> MetricsSample {
        let now = Instant::now();
        let stat = self
            .read("/proc/stat")
            .map(|t| parse::stat(&t))
            .unwrap_or_default();
        let disks_now = self
            .read("/proc/diskstats")
            .map(|t| parse::diskstats(&t))
            .unwrap_or_default();
        let nets_now = self
            .read("/proc/net/dev")
            .map(|t| parse::net_dev(&t))
            .unwrap_or_default();
        let prev = self.prev.take();
        let dt = prev
            .as_ref()
            .map(|p| now.duration_since(p.at).as_secs_f64())
            .filter(|d| *d > 0.0);

        let cpu = match &prev {
            Some(p) => Cpu {
                total: match (&p.stat.total, &stat.total) {
                    (Some(a), Some(b)) => parse::cpu_usage(a, b),
                    _ => None,
                },
                cores: stat
                    .cores
                    .iter()
                    .filter_map(|(id, cur)| {
                        let usage = parse::cpu_usage(p.stat.cores.get(id)?, cur)?;
                        Some(CoreUsage { id: *id, usage })
                    })
                    .collect(),
            },
            None => Cpu {
                total: None,
                cores: Vec::new(),
            },
        };

        let mem = self
            .read("/proc/meminfo")
            .map(|t| parse::meminfo(&t))
            .unwrap_or_default();
        let g = |k: &str| mem.get(k).copied();
        let total = g("MemTotal");
        let available = g("MemAvailable");
        let memory = Memory {
            total_bytes: total,
            available_bytes: available,
            used_bytes: total.zip(available).map(|(t, a)| t.saturating_sub(a)),
            free_bytes: g("MemFree"),
            buffers_bytes: g("Buffers"),
            cached_bytes: g("Cached"),
            shmem_bytes: g("Shmem"),
            reclaimable_slab_bytes: g("SReclaimable"),
        };
        let swap = Swap {
            total_bytes: g("SwapTotal"),
            free_bytes: g("SwapFree"),
            used_bytes: g("SwapTotal")
                .zip(g("SwapFree"))
                .map(|(t, f)| t.saturating_sub(f)),
        };

        let (one, five, fifteen, tasks) = self
            .read("/proc/loadavg")
            .map(|t| parse::loadavg(&t))
            .unwrap_or((None, None, None, None));

        let limits = Limits {
            tasks,
            pid_max: self.read_num("/proc/sys/kernel/pid_max"),
            file_handles: self
                .read("/proc/sys/fs/file-nr")
                .and_then(|t| parse::file_nr(&t)),
            file_max: self.read_num("/proc/sys/fs/file-max"),
        };

        let disks = self.disks(&disks_now, prev.as_ref().map(|p| &p.disks), dt);
        let networks = self.networks(&nets_now, prev.as_ref().map(|p| &p.nets), dt);

        self.prev = Some(Previous {
            at: now,
            stat,
            disks: disks_now,
            nets: nets_now,
        });
        self.seq += 1;
        MetricsSample {
            seq: self.seq,
            collected_at,
            interval_seconds: self.interval_seconds,
            cpu,
            memory,
            swap,
            load: Load { one, five, fifteen },
            pressure: self.pressure(),
            limits,
            filesystems: self.filesystems(),
            disks,
            networks,
            temperatures: self.temperatures(),
        }
    }

    fn pressure(&self) -> Pressure {
        let empty = |state: &str| Pressure {
            state: state.into(),
            cpu: None,
            memory: None,
            io: None,
        };
        if !self.path("/proc/pressure").is_dir() {
            return empty("unsupported");
        }
        let mut lines = Vec::new();
        for kind in ["cpu", "memory", "io"] {
            match std::fs::read_to_string(self.path(&format!("/proc/pressure/{kind}"))) {
                Ok(t) => lines.push(parse::pressure(&t)),
                Err(e) => return empty(pressure_error_state(&e)),
            }
        }
        if lines.iter().all(Option::is_none) {
            return empty("unreadable");
        }
        let mut it = lines.into_iter();
        Pressure {
            state: "ok".into(),
            cpu: it.next().flatten(),
            memory: it.next().flatten(),
            io: it.next().flatten(),
        }
    }

    fn filesystems(&self) -> Vec<Filesystem> {
        let Some(text) = self.read("/proc/self/mountinfo") else {
            return Vec::new();
        };
        // A later mount on the same point hides the earlier one.
        let mut by_point: BTreeMap<String, parse::Mount> = BTreeMap::new();
        for m in parse::mountinfo(&text) {
            if !PSEUDO_FS.contains(&m.fs_type.as_str()) {
                by_point.insert(m.mount_point.clone(), m);
            }
        }
        by_point
            .into_values()
            .map(|m| {
                let class = if NETWORK_FS.contains(&m.fs_type.as_str()) {
                    "network"
                } else if m.fs_type.starts_with("fuse") {
                    "virtual"
                } else {
                    "local"
                };
                let target = self.path(&m.mount_point);
                let st = if class == "network" {
                    self.statvfs_bounded(&m.mount_point, target)
                } else {
                    Ok(statvfs(&target))
                };
                let no_table = NO_INODE_TABLE.contains(&m.fs_type.as_str());
                let mut fs = Filesystem {
                    mount_point: m.mount_point,
                    device: m.source,
                    fs_type: m.fs_type,
                    class: class.into(),
                    read_only: m.read_only,
                    state: "ok".into(),
                    total_bytes: None,
                    free_bytes: None,
                    available_bytes: None,
                    used_bytes: None,
                    inodes: if no_table { "notApplicable" } else { "ok" }.into(),
                    inodes_total: None,
                    inodes_free: None,
                };
                match st {
                    Err(()) => fs.state = "unresponsive".into(),
                    Ok(None) => fs.state = "unreadable".into(),
                    Ok(Some(v)) => {
                        fs.total_bytes = Some(v.total);
                        fs.free_bytes = Some(v.free);
                        fs.available_bytes = Some(v.available);
                        fs.used_bytes = Some(v.total.saturating_sub(v.free));
                        // A zero inode count means the filesystem does not
                        // keep one (vfat, many FUSE filesystems).
                        if v.files == 0 {
                            fs.inodes = "notApplicable".into();
                        } else if !no_table {
                            fs.inodes_total = Some(v.files);
                            fs.inodes_free = Some(v.files_free);
                        }
                    }
                }
                fs
            })
            .collect()
    }

    /// statvfs in its own thread; `Err` when it did not answer in time (or
    /// an earlier call on this mount still has not).
    fn statvfs_bounded(&self, mount_point: &str, target: PathBuf) -> Result<Option<Vfs>, ()> {
        if self.stuck.lock().unwrap().contains(mount_point) {
            return Err(());
        }
        let (tx, rx) = std::sync::mpsc::channel();
        let stuck = self.stuck.clone();
        let key = mount_point.to_string();
        stuck.lock().unwrap().insert(key.clone());
        let spawned = std::thread::Builder::new()
            .name("statvfs".into())
            .spawn(move || {
                let v = statvfs(&target);
                stuck.lock().unwrap().remove(&key);
                let _ = tx.send(v);
            });
        if spawned.is_err() {
            self.stuck.lock().unwrap().remove(mount_point);
            return Ok(None);
        }
        rx.recv_timeout(STATVFS_TIMEOUT).map_err(|_| ())
    }

    fn disks(
        &self,
        now: &BTreeMap<String, DiskCounters>,
        prev: Option<&BTreeMap<String, DiskCounters>>,
        dt: Option<f64>,
    ) -> Vec<Disk> {
        let Ok(entries) = std::fs::read_dir(self.path("/sys/block")) else {
            return Vec::new();
        };
        let mut names: Vec<String> = entries
            .filter_map(|e| e.ok()?.file_name().into_string().ok())
            .collect();
        names.sort();
        names
            .into_iter()
            .filter_map(|name| {
                let base = format!("/sys/block/{name}");
                let size_bytes = self
                    .read_num::<u64>(&format!("{base}/size"))
                    .and_then(|s| s.checked_mul(512));
                // Unused loop and ram devices have no size.
                if size_bytes == Some(0) {
                    return None;
                }
                let has_device = self.path(&format!("{base}/device")).exists();
                let real = std::fs::canonicalize(self.path(&base))
                    .map(|p| p.to_string_lossy().into_owned())
                    .unwrap_or_default();
                let class = disk_class(&name, has_device);
                let transport = if real.contains("/usb") {
                    Some("usb")
                } else if name.starts_with("nvme") {
                    Some("nvme")
                } else if real.contains("/virtio") {
                    Some("virtio")
                } else if real.contains("/ata") {
                    Some("ata")
                } else {
                    None
                };
                let rates = match (now.get(&name), prev.and_then(|p| p.get(&name)), dt) {
                    (Some(c), Some(p), Some(dt)) => disk_rates(p, c, dt),
                    _ => DiskRates::default(),
                };
                Some(Disk {
                    model: self.read_trim(&format!("{base}/device/model")),
                    transport: transport.map(str::to_string),
                    rotational: self
                        .read_num::<u8>(&format!("{base}/queue/rotational"))
                        .map(|v| v == 1),
                    removable: self
                        .read_num::<u8>(&format!("{base}/removable"))
                        .map(|v| v == 1),
                    size_bytes,
                    read_bytes_per_second: rates.read_bps,
                    write_bytes_per_second: rates.write_bps,
                    reads_per_second: rates.reads,
                    writes_per_second: rates.writes,
                    read_latency_ms: rates.read_latency,
                    write_latency_ms: rates.write_latency,
                    utilization: rates.util,
                    class: class.into(),
                    name,
                })
            })
            .collect()
    }

    fn networks(
        &self,
        now: &BTreeMap<String, NetCounters>,
        prev: Option<&BTreeMap<String, NetCounters>>,
        dt: Option<f64>,
    ) -> Vec<Network> {
        now.iter()
            .map(|(name, c)| {
                let base = format!("/sys/class/net/{name}");
                let class = if name == "lo" {
                    "loopback"
                } else if self.path(&format!("{base}/device")).exists() {
                    "physical"
                } else {
                    "virtual"
                };
                let p = prev.and_then(|p| p.get(name));
                let rate = |f: fn(&NetCounters) -> u64| -> Option<f64> {
                    let (p, dt) = (p?, dt?);
                    Some(parse::round2(f(c).checked_sub(f(p))? as f64 / dt))
                };
                Network {
                    name: name.clone(),
                    class: class.into(),
                    oper_state: self.read_trim(&format!("{base}/operstate")),
                    // -1 (or a read error) when the link is down or the
                    // driver does not say.
                    speed_mbps: self
                        .read_num::<i64>(&format!("{base}/speed"))
                        .filter(|s| *s > 0)
                        .map(|s| s as u64),
                    rx_bytes_per_second: rate(|c| c.rx_bytes),
                    tx_bytes_per_second: rate(|c| c.tx_bytes),
                    rx_packets_per_second: rate(|c| c.rx_packets),
                    tx_packets_per_second: rate(|c| c.tx_packets),
                    rx_errors: Some(c.rx_errors),
                    tx_errors: Some(c.tx_errors),
                    rx_drops: Some(c.rx_drops),
                    tx_drops: Some(c.tx_drops),
                }
            })
            .collect()
    }

    fn temperatures(&self) -> Vec<Temperature> {
        let mut out = Vec::new();
        let milli = |rel: &str| {
            self.read_num::<i64>(rel)
                .map(|m| parse::round2(m as f64 / 1000.0))
        };
        for dir in sorted_dir(&self.path("/sys/class/hwmon")) {
            let base = format!("/sys/class/hwmon/{dir}");
            let chip = self
                .read_trim(&format!("{base}/name"))
                .unwrap_or_else(|| dir.clone());
            let mut inputs: Vec<String> = sorted_dir(&self.path(&base))
                .into_iter()
                .filter(|f| f.starts_with("temp") && f.ends_with("_input"))
                .collect();
            inputs.sort_by_key(|f| {
                f.trim_start_matches("temp")
                    .trim_end_matches("_input")
                    .parse::<u32>()
                    .unwrap_or(u32::MAX)
            });
            for input in inputs {
                let stem = input.trim_end_matches("_input");
                let Some(c) = milli(&format!("{base}/{input}")) else {
                    continue;
                };
                out.push(Temperature {
                    source: "hwmon".into(),
                    label: self
                        .read_trim(&format!("{base}/{stem}_label"))
                        .unwrap_or_else(|| stem.to_string()),
                    chip: chip.clone(),
                    celsius: c,
                    high_celsius: milli(&format!("{base}/{stem}_max")),
                    critical_celsius: milli(&format!("{base}/{stem}_crit")),
                });
            }
        }
        for zone in sorted_dir(&self.path("/sys/class/thermal")) {
            if !zone.starts_with("thermal_zone") {
                continue;
            }
            let base = format!("/sys/class/thermal/{zone}");
            let Some(c) = milli(&format!("{base}/temp")) else {
                continue;
            };
            let trip = |kind: &str| {
                (0..16).find_map(|i| {
                    (self.read_trim(&format!("{base}/trip_point_{i}_type"))? == kind)
                        .then(|| milli(&format!("{base}/trip_point_{i}_temp")))
                        .flatten()
                })
            };
            out.push(Temperature {
                source: "thermal_zone".into(),
                chip: self.read_trim(&format!("{base}/type")).unwrap_or_default(),
                label: zone.clone(),
                celsius: c,
                high_celsius: trip("hot"),
                critical_celsius: trip("critical"),
            });
        }
        out
    }

    /// What the host is.
    pub fn facts(&self) -> HostFacts {
        let (os_src, os) = ["/etc/os-release", "/usr/lib/os-release"]
            .iter()
            .find_map(|p| Some((*p, parse::os_release(&self.read(p)?))))
            .unwrap_or(("/etc/os-release", BTreeMap::new()));
        let osf = |k: &str| Fact::new(os.get(k).cloned(), os_src);
        let stat = self.read("/proc/stat").map(|t| parse::stat(&t));
        let boot = stat
            .as_ref()
            .and_then(|s| s.btime)
            .and_then(|b| OffsetDateTime::from_unix_timestamp(b).ok())
            .and_then(|t| {
                t.format(&time::format_description::well_known::Rfc3339)
                    .ok()
            });
        let mem = self.read("/proc/meminfo").map(|t| parse::meminfo(&t));
        let (tz_src, tz) = self.timezone();
        let (synced, max_error) = time_sync();
        HostFacts {
            hostname: Fact::new(
                self.read_trim("/proc/sys/kernel/hostname"),
                "/proc/sys/kernel/hostname",
            ),
            os_name: osf("NAME"),
            os_version: osf("VERSION_ID"),
            os_pretty_name: osf("PRETTY_NAME"),
            kernel: Fact::new(
                self.read_trim("/proc/sys/kernel/osrelease"),
                "/proc/sys/kernel/osrelease",
            ),
            arch: Fact::new(machine(), "uname"),
            boot_time: Fact::new(boot, "/proc/stat btime"),
            timezone: Fact::new(tz, tz_src),
            time_synchronized: Fact::new(synced, "adjtimex"),
            time_max_error_micros: Fact::new(max_error, "adjtimex"),
            cpu_model: Fact::new(
                self.read("/proc/cpuinfo")
                    .and_then(|t| parse::cpu_model(&t)),
                "/proc/cpuinfo",
            ),
            logical_cpus: Fact::new(
                stat.filter(|s| !s.cores.is_empty())
                    .map(|s| s.cores.len() as u32),
                "/proc/stat",
            ),
            memory_total_bytes: Fact::new(
                mem.and_then(|m| m.get("MemTotal").copied()),
                "/proc/meminfo",
            ),
        }
    }

    fn timezone(&self) -> (&'static str, Option<String>) {
        if let Some(z) = self.read_trim("/etc/timezone") {
            return ("/etc/timezone", Some(z));
        }
        let zone = std::fs::read_link(self.path("/etc/localtime"))
            .ok()
            .and_then(|t| parse::zone_from_link(&t.to_string_lossy()));
        ("/etc/localtime", zone)
    }
}

/// The PSI state a read error means. Linux answers EOPNOTSUPP when PSI is
/// built in but turned off (`psi=0`).
pub fn pressure_error_state(e: &std::io::Error) -> &'static str {
    const EOPNOTSUPP: i32 = 95;
    match e.raw_os_error() {
        Some(EOPNOTSUPP) => "disabled",
        _ if e.kind() == std::io::ErrorKind::NotFound => "unsupported",
        _ => "unreadable",
    }
}

pub fn disk_class(name: &str, has_device: bool) -> &'static str {
    let starts = |p: &[&str]| p.iter().any(|x| name.starts_with(x));
    if starts(&["loop"]) {
        "loop"
    } else if starts(&["md"]) {
        "raid"
    } else if starts(&["dm-"]) {
        "mapper"
    } else if starts(&["nbd", "rbd"]) {
        "network"
    } else if starts(&["zram", "ram", "vd", "xvd"]) || !has_device {
        "virtual"
    } else {
        "physical"
    }
}

#[derive(Default)]
struct DiskRates {
    read_bps: Option<f64>,
    write_bps: Option<f64>,
    reads: Option<f64>,
    writes: Option<f64>,
    read_latency: Option<f64>,
    write_latency: Option<f64>,
    util: Option<f64>,
}

fn disk_rates(p: &DiskCounters, c: &DiskCounters, dt: f64) -> DiskRates {
    let d = |f: fn(&DiskCounters) -> u64| f(c).checked_sub(f(p));
    let per_s = |v: Option<u64>| v.map(|v| parse::round2(v as f64 / dt));
    let latency = |ms: Option<u64>, ops: Option<u64>| match (ms, ops) {
        (Some(ms), Some(ops)) if ops > 0 => Some(parse::round2(ms as f64 / ops as f64)),
        _ => None,
    };
    DiskRates {
        read_bps: per_s(d(|c| c.read_sectors).map(|s| s * 512)),
        write_bps: per_s(d(|c| c.write_sectors).map(|s| s * 512)),
        reads: per_s(d(|c| c.reads)),
        writes: per_s(d(|c| c.writes)),
        read_latency: latency(d(|c| c.read_ms), d(|c| c.reads)),
        write_latency: latency(d(|c| c.write_ms), d(|c| c.writes)),
        util: d(|c| c.io_ms).map(|ms| parse::round2((ms as f64 / (dt * 10.0)).min(100.0))),
    }
}

fn sorted_dir(p: &Path) -> Vec<String> {
    let mut v: Vec<String> = std::fs::read_dir(p)
        .map(|it| {
            it.filter_map(|e| e.ok()?.file_name().into_string().ok())
                .collect()
        })
        .unwrap_or_default();
    v.sort();
    v
}

struct Vfs {
    total: u64,
    free: u64,
    available: u64,
    files: u64,
    files_free: u64,
}

#[cfg(unix)]
fn statvfs(p: &Path) -> Option<Vfs> {
    use std::os::unix::ffi::OsStrExt;
    let c = std::ffi::CString::new(p.as_os_str().as_bytes()).ok()?;
    let mut st: libc::statvfs = unsafe { std::mem::zeroed() };
    // SAFETY: c is a valid C string and st a valid out pointer.
    if unsafe { libc::statvfs(c.as_ptr(), &mut st) } != 0 {
        return None;
    }
    let frsize = st.f_frsize as u64;
    Some(Vfs {
        total: (st.f_blocks as u64).saturating_mul(frsize),
        free: (st.f_bfree as u64).saturating_mul(frsize),
        available: (st.f_bavail as u64).saturating_mul(frsize),
        files: st.f_files as u64,
        files_free: st.f_ffree as u64,
    })
}

#[cfg(not(unix))]
fn statvfs(_: &Path) -> Option<Vfs> {
    None
}

#[cfg(unix)]
fn machine() -> Option<String> {
    let mut u: libc::utsname = unsafe { std::mem::zeroed() };
    // SAFETY: u is a valid out pointer.
    if unsafe { libc::uname(&mut u) } != 0 {
        return None;
    }
    // SAFETY: uname NUL-terminates machine.
    let m = unsafe { std::ffi::CStr::from_ptr(u.machine.as_ptr()) };
    Some(m.to_string_lossy().into_owned()).filter(|s| !s.is_empty())
}

#[cfg(not(unix))]
fn machine() -> Option<String> {
    None
}

/// Whether the kernel clock is synchronized and its maximum error, from
/// adjtimex (read-only with modes = 0, no privilege needed).
#[cfg(target_os = "linux")]
fn time_sync() -> (Option<bool>, Option<u64>) {
    const STA_UNSYNC: i32 = 0x0040;
    const TIME_ERROR: i32 = 5;
    let mut tx: libc::timex = unsafe { std::mem::zeroed() };
    // SAFETY: tx is a valid timex with modes = 0 (query only).
    let state = unsafe { libc::adjtimex(&mut tx) };
    if state < 0 {
        return (None, None);
    }
    let synced = state != TIME_ERROR && tx.status & STA_UNSYNC == 0;
    (Some(synced), u64::try_from(tx.maxerror).ok())
}

#[cfg(not(target_os = "linux"))]
fn time_sync() -> (Option<bool>, Option<u64>) {
    (None, None)
}
