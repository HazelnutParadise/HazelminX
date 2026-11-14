//! Parsers for the Linux files the monitor reads. Each takes the file's
//! text and returns `None` (or skips an entry) for anything it cannot read
//! exactly; nothing is guessed.

use std::collections::BTreeMap;

/// One `cpu` / `cpuN` line of /proc/stat, in USER_HZ ticks.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct CpuTicks {
    pub user: u64,
    pub nice: u64,
    pub system: u64,
    pub idle: u64,
    pub iowait: u64,
    pub irq: u64,
    pub softirq: u64,
    pub steal: u64,
}

impl CpuTicks {
    pub fn total(&self) -> u64 {
        self.user
            + self.nice
            + self.system
            + self.idle
            + self.iowait
            + self.irq
            + self.softirq
            + self.steal
    }
}

/// /proc/stat: the aggregate line, each `cpuN` line by N, and `btime`.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct Stat {
    pub total: Option<CpuTicks>,
    pub cores: BTreeMap<u32, CpuTicks>,
    pub btime: Option<i64>,
}

pub fn stat(text: &str) -> Stat {
    let mut out = Stat::default();
    for line in text.lines() {
        let mut it = line.split_whitespace();
        let Some(key) = it.next() else { continue };
        if key == "btime" {
            out.btime = it.next().and_then(|v| v.parse().ok());
            continue;
        }
        let Some(rest) = key.strip_prefix("cpu") else {
            continue;
        };
        // guest and guest_nice are already counted in user and nice.
        let v: Vec<u64> = it.take(8).map_while(|x| x.parse().ok()).collect();
        if v.len() < 8 {
            continue;
        }
        let t = CpuTicks {
            user: v[0],
            nice: v[1],
            system: v[2],
            idle: v[3],
            iowait: v[4],
            irq: v[5],
            softirq: v[6],
            steal: v[7],
        };
        if rest.is_empty() {
            out.total = Some(t);
        } else if let Ok(n) = rest.parse() {
            out.cores.insert(n, t);
        }
    }
    out
}

/// Usage between two readings, percent. `None` when the counters did not
/// move forward (same reading, counter reset after a CPU went offline).
pub fn cpu_usage(prev: &CpuTicks, cur: &CpuTicks) -> Option<hazelminx_protocol::metrics::CpuUsage> {
    let d = |a: u64, b: u64| b.checked_sub(a);
    let total = d(prev.total(), cur.total()).filter(|t| *t > 0)? as f64;
    let user = (d(prev.user, cur.user)? + d(prev.nice, cur.nice)?) as f64;
    let system = (d(prev.system, cur.system)?
        + d(prev.irq, cur.irq)?
        + d(prev.softirq, cur.softirq)?) as f64;
    let idle = d(prev.idle, cur.idle)? as f64;
    let iowait = d(prev.iowait, cur.iowait)? as f64;
    let steal = d(prev.steal, cur.steal)? as f64;
    let pct = |x: f64| round2(x * 100.0 / total);
    Some(hazelminx_protocol::metrics::CpuUsage {
        usage: pct(total - idle - iowait),
        user: pct(user),
        system: pct(system),
        iowait: pct(iowait),
        steal: pct(steal),
    })
}

pub fn round2(x: f64) -> f64 {
    (x * 100.0).round() / 100.0
}

/// /proc/meminfo values in bytes, by key (`MemTotal`, …).
pub fn meminfo(text: &str) -> BTreeMap<String, u64> {
    let mut out = BTreeMap::new();
    for line in text.lines() {
        let Some((k, v)) = line.split_once(':') else {
            continue;
        };
        let mut it = v.split_whitespace();
        let Some(n) = it.next().and_then(|n| n.parse::<u64>().ok()) else {
            continue;
        };
        let bytes = match it.next() {
            Some("kB") => n.checked_mul(1024),
            None => Some(n),
            Some(_) => None,
        };
        if let Some(b) = bytes {
            out.insert(k.trim().to_string(), b);
        }
    }
    out
}

/// /proc/loadavg: the three averages and the number of tasks (`4/312`).
pub fn loadavg(text: &str) -> (Option<f64>, Option<f64>, Option<f64>, Option<u64>) {
    let f: Vec<&str> = text.split_whitespace().collect();
    let num = |i: usize| f.get(i).and_then(|x| x.parse::<f64>().ok());
    let tasks = f
        .get(3)
        .and_then(|x| x.split_once('/'))
        .and_then(|(_, t)| t.parse().ok());
    (num(0), num(1), num(2), tasks)
}

/// One /proc/pressure file. The `some` line is required; `full` is
/// optional (cpu before Linux 5.13 has none).
pub fn pressure(text: &str) -> Option<hazelminx_protocol::metrics::PressureLine> {
    let line = |kind: &str| -> Option<(f64, f64, f64)> {
        let l = text.lines().find(|l| l.starts_with(kind))?;
        let get = |key: &str| {
            l.split_whitespace()
                .find_map(|kv| kv.strip_prefix(key))
                .and_then(|v| v.parse::<f64>().ok())
        };
        Some((get("avg10=")?, get("avg60=")?, get("avg300=")?))
    };
    let (s10, s60, s300) = line("some ")?;
    let full = line("full ");
    Some(hazelminx_protocol::metrics::PressureLine {
        some_avg10: s10,
        some_avg60: s60,
        some_avg300: s300,
        full_avg10: full.map(|f| f.0),
        full_avg60: full.map(|f| f.1),
        full_avg300: full.map(|f| f.2),
    })
}

/// /proc/sys/fs/file-nr: allocated minus free handles.
pub fn file_nr(text: &str) -> Option<u64> {
    let v: Vec<u64> = text
        .split_whitespace()
        .take(2)
        .map_while(|x| x.parse().ok())
        .collect();
    (v.len() == 2).then(|| v[0].saturating_sub(v[1]))
}

/// A line of /proc/self/mountinfo.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Mount {
    pub mount_point: String,
    pub fs_type: String,
    pub source: String,
    pub read_only: bool,
}

/// Undoes the octal escapes mountinfo uses for space, tab, newline and `\`.
pub fn unescape(s: &str) -> String {
    let b = s.as_bytes();
    let mut out = Vec::with_capacity(b.len());
    let mut i = 0;
    while i < b.len() {
        if b[i] == b'\\'
            && i + 3 < b.len()
            && b[i + 1..i + 4].iter().all(|c| (b'0'..=b'7').contains(c))
        {
            let v = (b[i + 1] - b'0') * 64 + (b[i + 2] - b'0') * 8 + (b[i + 3] - b'0');
            out.push(v);
            i += 4;
        } else {
            out.push(b[i]);
            i += 1;
        }
    }
    String::from_utf8_lossy(&out).into_owned()
}

pub fn mountinfo(text: &str) -> Vec<Mount> {
    let mut out = Vec::new();
    for line in text.lines() {
        let Some((left, right)) = line.split_once(" - ") else {
            continue;
        };
        let l: Vec<&str> = left.split(' ').collect();
        let r: Vec<&str> = right.split(' ').collect();
        if l.len() < 6 || r.len() < 2 {
            continue;
        }
        let per_mount_ro = l[5].split(',').any(|o| o == "ro");
        let super_ro = r.get(2).is_some_and(|o| o.split(',').any(|o| o == "ro"));
        out.push(Mount {
            mount_point: unescape(l[4]),
            fs_type: r[0].to_string(),
            source: unescape(r[1]),
            read_only: per_mount_ro || super_ro,
        });
    }
    out
}

/// A line of /proc/self/mountinfo with its device number and options.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MountFull {
    pub mount_point: String,
    pub fs_type: String,
    pub source: String,
    pub read_only: bool,
    /// `major:minor`.
    pub dev: String,
    /// Per-mount options.
    pub options: String,
}

pub fn mountinfo_full(text: &str) -> Vec<MountFull> {
    let mut out = Vec::new();
    for line in text.lines() {
        let Some((left, right)) = line.split_once(" - ") else {
            continue;
        };
        let l: Vec<&str> = left.split(' ').collect();
        let r: Vec<&str> = right.split(' ').collect();
        if l.len() < 6 || r.len() < 2 {
            continue;
        }
        let sup = r.get(2).copied().unwrap_or("");
        let ro = l[5].split(',').any(|o| o == "ro") || sup.split(',').any(|o| o == "ro");
        out.push(MountFull {
            mount_point: unescape(l[4]),
            fs_type: r[0].to_string(),
            source: unescape(r[1]),
            read_only: ro,
            dev: l[2].to_string(),
            options: l[5].to_string(),
        });
    }
    out
}

/// One device line of /proc/diskstats.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct DiskCounters {
    pub reads: u64,
    pub read_sectors: u64,
    pub read_ms: u64,
    pub writes: u64,
    pub write_sectors: u64,
    pub write_ms: u64,
    pub io_ms: u64,
}

pub fn diskstats(text: &str) -> BTreeMap<String, DiskCounters> {
    let mut out = BTreeMap::new();
    for line in text.lines() {
        let f: Vec<&str> = line.split_whitespace().collect();
        if f.len() < 14 {
            continue;
        }
        let n = |i: usize| f[i].parse::<u64>().ok();
        let (Some(r), Some(rs), Some(rm), Some(w), Some(ws), Some(wm), Some(io)) =
            (n(3), n(5), n(6), n(7), n(9), n(10), n(12))
        else {
            continue;
        };
        out.insert(
            f[2].to_string(),
            DiskCounters {
                reads: r,
                read_sectors: rs,
                read_ms: rm,
                writes: w,
                write_sectors: ws,
                write_ms: wm,
                io_ms: io,
            },
        );
    }
    out
}

/// One interface of /proc/net/dev.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct NetCounters {
    pub rx_bytes: u64,
    pub rx_packets: u64,
    pub rx_errors: u64,
    pub rx_drops: u64,
    pub tx_bytes: u64,
    pub tx_packets: u64,
    pub tx_errors: u64,
    pub tx_drops: u64,
}

pub fn net_dev(text: &str) -> BTreeMap<String, NetCounters> {
    let mut out = BTreeMap::new();
    for line in text.lines() {
        let Some((name, rest)) = line.split_once(':') else {
            continue;
        };
        let v: Vec<u64> = rest
            .split_whitespace()
            .map_while(|x| x.parse().ok())
            .collect();
        if v.len() < 16 {
            continue;
        }
        out.insert(
            name.trim().to_string(),
            NetCounters {
                rx_bytes: v[0],
                rx_packets: v[1],
                rx_errors: v[2],
                rx_drops: v[3],
                tx_bytes: v[8],
                tx_packets: v[9],
                tx_errors: v[10],
                tx_drops: v[11],
            },
        );
    }
    out
}

/// KEY=value lines of os-release; values may be quoted.
pub fn os_release(text: &str) -> BTreeMap<String, String> {
    let mut out = BTreeMap::new();
    for line in text.lines() {
        let line = line.trim();
        if line.starts_with('#') {
            continue;
        }
        let Some((k, v)) = line.split_once('=') else {
            continue;
        };
        let v = v.trim();
        let v = v
            .strip_prefix('"')
            .and_then(|x| x.strip_suffix('"'))
            .or_else(|| v.strip_prefix('\'').and_then(|x| x.strip_suffix('\'')))
            .unwrap_or(v);
        out.insert(
            k.trim().to_string(),
            v.replace("\\\"", "\"").replace("\\\\", "\\"),
        );
    }
    out
}

/// The zone name of a /etc/localtime symlink target
/// (`/usr/share/zoneinfo/Asia/Taipei` → `Asia/Taipei`).
pub fn zone_from_link(target: &str) -> Option<String> {
    let (_, zone) = target.split_once("zoneinfo/")?;
    let zone = zone.strip_prefix("posix/").unwrap_or(zone);
    (!zone.is_empty()).then(|| zone.to_string())
}

/// `model name` of the first processor in /proc/cpuinfo (x86), or `Model`
/// / `Hardware` lines that ARM kernels print.
pub fn cpu_model(text: &str) -> Option<String> {
    ["model name", "Model", "Hardware", "cpu model"]
        .iter()
        .find_map(|key| {
            text.lines().find_map(|l| {
                let (k, v) = l.split_once(':')?;
                (k.trim() == *key && !v.trim().is_empty()).then(|| v.trim().to_string())
            })
        })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn stat_reads_aggregate_cores_and_btime() {
        let s = stat(
            "cpu  10 1 5 100 4 1 1 2 0 0\ncpu0 6 1 3 50 2 1 1 1 0 0\ncpu1 4 0 2 50 2 0 0 1 0 0\nintr 1 2\nbtime 1761984760\n",
        );
        assert_eq!(s.total.unwrap().total(), 124);
        assert_eq!(s.cores.len(), 2);
        assert_eq!(s.btime, Some(1761984760));
    }

    #[test]
    fn cpu_usage_counts_idle_and_iowait_as_not_busy() {
        let a = CpuTicks {
            user: 100,
            idle: 100,
            ..Default::default()
        };
        let b = CpuTicks {
            user: 150,
            system: 10,
            idle: 130,
            iowait: 10,
            steal: 0,
            ..Default::default()
        };
        let u = cpu_usage(&a, &b).unwrap();
        assert_eq!(u.usage, 60.0);
        assert_eq!(u.user, 50.0);
        assert_eq!(u.system, 10.0);
        assert_eq!(u.iowait, 10.0);
        // No progress, or counters that went backwards: no value, not zero.
        assert!(cpu_usage(&b, &b).is_none());
        assert!(cpu_usage(&b, &a).is_none());
    }

    #[test]
    fn meminfo_is_bytes() {
        let m =
            meminfo("MemTotal:  2048 kB\nMemAvailable: 1024 kB\nHugePages_Total: 0\nbad line\n");
        assert_eq!(m["MemTotal"], 2048 * 1024);
        assert_eq!(m["HugePages_Total"], 0);
        assert_eq!(m.len(), 3);
    }

    #[test]
    fn loadavg_with_tasks() {
        assert_eq!(
            loadavg("0.25 0.50 0.75 2/312 999\n"),
            (Some(0.25), Some(0.5), Some(0.75), Some(312))
        );
        assert_eq!(loadavg(""), (None, None, None, None));
    }

    #[test]
    fn pressure_with_and_without_full() {
        let p = pressure("some avg10=0.50 avg60=1.25 avg300=0.75 total=1\n").unwrap();
        assert_eq!(p.some_avg60, 1.25);
        assert_eq!(p.full_avg60, None);
        let p = pressure("some avg10=1.00 avg60=2.00 avg300=3.00 total=1\nfull avg10=0.10 avg60=0.20 avg300=0.30 total=1\n").unwrap();
        assert_eq!(p.full_avg300, Some(0.3));
        assert!(pressure("garbage").is_none());
    }

    #[test]
    fn mountinfo_decodes_escapes_and_read_only() {
        let m = mountinfo(
            "22 1 8:1 / / rw,relatime shared:1 - ext4 /dev/sda1 rw\n36 22 0:30 / /mnt/my\\040disk ro,nosuid - vfat /dev/sdb1 rw\n37 22 0:31 / /srv rw - btrfs /dev/sdc ro,space_cache\n",
        );
        assert_eq!(m.len(), 3);
        assert_eq!(m[1].mount_point, "/mnt/my disk");
        assert!(!m[0].read_only);
        assert!(m[1].read_only);
        assert!(m[2].read_only, "superblock ro counts");
    }

    #[test]
    fn diskstats_and_net_dev() {
        let d = diskstats("   8       0 sda 100 0 800 50 200 0 1600 400 0 300 450 0 0 0 0\n");
        assert_eq!(d["sda"].read_sectors, 800);
        assert_eq!(d["sda"].io_ms, 300);
        let n = net_dev(
            "Inter-|   Receive\n face |bytes\n  eth0: 1000 10 1 2 0 0 0 0 500 5 0 3 0 0 0 0\n",
        );
        assert_eq!(n["eth0"].tx_bytes, 500);
        assert_eq!(n["eth0"].tx_drops, 3);
    }

    #[test]
    fn os_release_and_zone_and_model() {
        let o = os_release("NAME=\"Debian GNU/Linux\"\nVERSION_ID=\"13\"\nID=debian\n# c\n");
        assert_eq!(o["NAME"], "Debian GNU/Linux");
        assert_eq!(o["ID"], "debian");
        assert_eq!(
            zone_from_link("../usr/share/zoneinfo/Asia/Taipei").as_deref(),
            Some("Asia/Taipei")
        );
        assert_eq!(zone_from_link("/etc/foo"), None);
        assert_eq!(
            cpu_model("processor\t: 0\nmodel name\t: AMD EPYC\n").as_deref(),
            Some("AMD EPYC")
        );
        assert_eq!(cpu_model("processor : 0\n"), None);
        assert_eq!(file_nr("1920 32 9223372036854775807\n"), Some(1888));
    }
}
