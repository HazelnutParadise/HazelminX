use std::fs;
use std::path::Path;

use hazelminx_protocol::Message;
use time::OffsetDateTime;

use super::collect::{Collector, disk_class, pressure_error_state};
use super::*;

fn put(root: &Path, rel: &str, text: &str) {
    let p = root.join(rel);
    fs::create_dir_all(p.parent().unwrap()).unwrap();
    fs::write(p, text).unwrap();
}

/// A fake /proc and /sys. MemAvailable is missing on purpose.
fn fake_host(root: &Path) {
    put(
        root,
        "proc/stat",
        "cpu  100 0 50 800 50 0 0 0 0 0\ncpu0 50 0 25 400 25 0 0 0 0 0\ncpu1 50 0 25 400 25 0 0 0 0 0\nbtime 1761984760\n",
    );
    put(
        root,
        "proc/meminfo",
        "MemTotal: 4000 kB\nMemFree: 1000 kB\nCached: 2000 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n",
    );
    put(root, "proc/loadavg", "0.25 0.50 0.75 2/312 999\n");
    put(root, "proc/sys/kernel/pid_max", "4194304\n");
    put(root, "proc/sys/fs/file-nr", "1920 32 9223372036854775807\n");
    put(root, "proc/sys/kernel/hostname", "edge-01\n");
    put(root, "proc/sys/kernel/osrelease", "6.12.48\n");
    put(
        root,
        "proc/cpuinfo",
        "processor\t: 0\nmodel name\t: Test CPU\n",
    );
    put(
        root,
        "etc/os-release",
        "NAME=\"Debian GNU/Linux\"\nVERSION_ID=\"13\"\nPRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\n",
    );
    put(root, "etc/timezone", "Asia/Taipei\n");
    put(
        root,
        "proc/self/mountinfo",
        "22 1 8:1 / / rw,relatime - ext4 /dev/sda1 rw\n23 22 0:5 / /proc rw - proc proc rw\n24 22 0:6 / /data rw - btrfs /dev/sdb rw\n25 22 0:7 / /gone rw - ext4 /dev/sdc rw\n",
    );
    fs::create_dir_all(root.join("data")).unwrap();
    put(
        root,
        "proc/diskstats",
        "   8       0 sda 100 0 800 50 200 0 1600 400 0 300 450 0 0 0 0\n",
    );
    put(root, "sys/block/sda/size", "1000\n");
    put(root, "sys/block/sda/queue/rotational", "1\n");
    put(root, "sys/block/sda/device/model", "TestDisk  \n");
    put(root, "sys/block/loop0/size", "0\n");
    put(root, "sys/block/md0/size", "2000\n");
    put(
        root,
        "proc/net/dev",
        "h\nh\n    lo: 100 1 0 0 0 0 0 0 100 1 0 0 0 0 0 0\n  eth0: 1000 10 0 2 0 0 0 0 500 5 0 0 0 0 0 0\n",
    );
    put(root, "sys/class/net/eth0/device/x", "");
    put(root, "sys/class/net/eth0/operstate", "up\n");
    put(root, "sys/class/net/eth0/speed", "-1\n");
    put(root, "sys/class/hwmon/hwmon0/name", "k10temp\n");
    put(root, "sys/class/hwmon/hwmon0/temp1_input", "48500\n");
    put(root, "sys/class/hwmon/hwmon0/temp1_label", "Tctl\n");
    put(root, "sys/class/hwmon/hwmon0/temp1_crit", "95000\n");
    put(root, "sys/class/thermal/thermal_zone0/temp", "27800\n");
    put(root, "sys/class/thermal/thermal_zone0/type", "acpitz\n");
    put(
        root,
        "sys/class/thermal/thermal_zone0/trip_point_0_type",
        "critical\n",
    );
    put(
        root,
        "sys/class/thermal/thermal_zone0/trip_point_0_temp",
        "105000\n",
    );
}

fn now() -> OffsetDateTime {
    OffsetDateTime::from_unix_timestamp(1_763_089_200).unwrap()
}

#[test]
fn first_sample_has_no_rates_and_missing_values_are_null() {
    let dir = tempfile::tempdir().unwrap();
    fake_host(dir.path());
    let mut c = Collector::new(dir.path(), 10);
    let s = c.sample(now());
    assert_eq!(s.seq, 1);
    assert_eq!(s.interval_seconds, 10);
    assert!(s.cpu.total.is_none() && s.cpu.cores.is_empty());
    // No MemAvailable: available and used are not provided, not computed
    // from MemFree (which would count the page cache as used).
    assert_eq!(s.memory.total_bytes, Some(4000 * 1024));
    assert_eq!(s.memory.available_bytes, None);
    assert_eq!(s.memory.used_bytes, None);
    assert_eq!(s.memory.cached_bytes, Some(2000 * 1024));
    assert_eq!(s.load.fifteen, Some(0.75));
    assert_eq!(s.limits.tasks, Some(312));
    assert_eq!(s.limits.pid_max, Some(4194304));
    assert_eq!(s.limits.file_handles, Some(1888));
    assert_eq!(s.limits.file_max, None, "file-max is not in the fake tree");
    assert_eq!(s.pressure.state, "unsupported");
    assert!(s.pressure.memory.is_none());
    let sda = s.disks.iter().find(|d| d.name == "sda").unwrap();
    assert_eq!(sda.read_bytes_per_second, None);
    assert_eq!(sda.read_latency_ms, None);
    assert_eq!(sda.size_bytes, Some(512_000));
    assert_eq!(sda.rotational, Some(true));
    assert_eq!(sda.model.as_deref(), Some("TestDisk"));
    assert_eq!(sda.class, "physical", "sys/block/sda/device exists");
    assert!(
        s.disks.iter().all(|d| d.name != "loop0"),
        "empty loop devices are left out"
    );
    assert_eq!(
        s.disks.iter().find(|d| d.name == "md0").unwrap().class,
        "raid"
    );
    let eth = s.networks.iter().find(|n| n.name == "eth0").unwrap();
    assert_eq!(eth.class, "physical");
    assert_eq!(eth.speed_mbps, None, "speed -1 is not provided");
    assert_eq!(eth.rx_bytes_per_second, None);
    assert_eq!(eth.rx_drops, Some(2));
    assert_eq!(
        s.networks.iter().find(|n| n.name == "lo").unwrap().class,
        "loopback"
    );
    assert_eq!(s.temperatures.len(), 2);
    assert_eq!(s.temperatures[0].label, "Tctl");
    assert_eq!(s.temperatures[0].critical_celsius, Some(95.0));
    assert_eq!(s.temperatures[1].critical_celsius, Some(105.0));
    assert_eq!(s.temperatures[1].high_celsius, None);
}

#[test]
fn filesystems_report_inodes_and_unreadable_mounts() {
    let dir = tempfile::tempdir().unwrap();
    fake_host(dir.path());
    let mut c = Collector::new(dir.path(), 10);
    let s = c.sample(now());
    let points: Vec<&str> = s
        .filesystems
        .iter()
        .map(|f| f.mount_point.as_str())
        .collect();
    assert_eq!(points, ["/", "/data", "/gone"], "proc is left out");
    let root = &s.filesystems[0];
    assert_eq!(root.state, "ok");
    assert!(root.total_bytes.unwrap() > 0);
    assert_eq!(
        root.used_bytes,
        Some(root.total_bytes.unwrap() - root.free_bytes.unwrap())
    );
    let btrfs = &s.filesystems[1];
    assert_eq!(btrfs.inodes, "notApplicable");
    assert_eq!(btrfs.inodes_total, None);
    let gone = &s.filesystems[2];
    assert_eq!(gone.state, "unreadable");
    assert_eq!(gone.total_bytes, None, "no value is made up");
}

#[test]
fn second_sample_has_rates_from_deltas() {
    let dir = tempfile::tempdir().unwrap();
    fake_host(dir.path());
    let mut c = Collector::new(dir.path(), 10);
    c.sample(now());
    put(
        dir.path(),
        "proc/stat",
        "cpu  150 0 60 830 60 0 0 0 0 0\ncpu0 100 0 30 400 30 0 0 0 0 0\ncpu1 50 0 30 430 30 0 0 0 0 0\nbtime 1761984760\n",
    );
    put(
        dir.path(),
        "proc/diskstats",
        "   8       0 sda 110 0 900 70 200 0 1600 400 0 400 450 0 0 0 0\n",
    );
    put(
        dir.path(),
        "proc/net/dev",
        "h\nh\n  eth0: 2000 20 0 2 0 0 0 0 500 5 0 0 0 0 0 0\n",
    );
    std::thread::sleep(std::time::Duration::from_millis(50));
    let s = c.sample(now());
    assert_eq!(s.seq, 2);
    let t = s.cpu.total.unwrap();
    assert_eq!(t.usage, 60.0, "busy 60 of 100 ticks (idle 30, iowait 10)");
    assert_eq!(t.iowait, 10.0);
    assert_eq!(s.cpu.cores.len(), 2);
    let sda = s.disks.iter().find(|d| d.name == "sda").unwrap();
    assert_eq!(sda.read_latency_ms, Some(2.0), "20 ms over 10 reads");
    assert_eq!(sda.write_latency_ms, None, "no write completed: no latency");
    assert!(sda.read_bytes_per_second.unwrap() > 0.0);
    assert_eq!(sda.writes_per_second, Some(0.0));
    let eth = s.networks.iter().find(|n| n.name == "eth0").unwrap();
    assert!(eth.rx_bytes_per_second.unwrap() > 0.0);
    assert_eq!(eth.tx_bytes_per_second, Some(0.0));
}

#[test]
fn facts_come_with_sources_and_unknowns_are_null() {
    let dir = tempfile::tempdir().unwrap();
    fake_host(dir.path());
    let f = Collector::new(dir.path(), 10).facts();
    assert_eq!(f.hostname.value.as_deref(), Some("edge-01"));
    assert_eq!(f.os_name.value.as_deref(), Some("Debian GNU/Linux"));
    assert_eq!(f.os_name.source, "/etc/os-release");
    assert_eq!(f.kernel.value.as_deref(), Some("6.12.48"));
    assert_eq!(f.boot_time.value.as_deref(), Some("2025-11-01T08:12:40Z"));
    assert_eq!(f.timezone.value.as_deref(), Some("Asia/Taipei"));
    assert_eq!(f.timezone.source, "/etc/timezone");
    assert_eq!(f.cpu_model.value.as_deref(), Some("Test CPU"));
    assert_eq!(f.logical_cpus.value, Some(2));
    assert_eq!(f.memory_total_bytes.value, Some(4000 * 1024));
    let empty = tempfile::tempdir().unwrap();
    let f = Collector::new(empty.path(), 10).facts();
    assert_eq!(f.hostname.value, None);
    assert_eq!(f.boot_time.value, None);
    assert_eq!(f.timezone.value, None);
    assert_eq!(f.logical_cpus.value, None);
}

#[cfg(unix)]
#[test]
fn timezone_from_localtime_link() {
    let dir = tempfile::tempdir().unwrap();
    fs::create_dir_all(dir.path().join("etc")).unwrap();
    std::os::unix::fs::symlink(
        "/usr/share/zoneinfo/Europe/Berlin",
        dir.path().join("etc/localtime"),
    )
    .unwrap();
    let f = Collector::new(dir.path(), 10).facts();
    assert_eq!(f.timezone.value.as_deref(), Some("Europe/Berlin"));
    assert_eq!(f.timezone.source, "/etc/localtime");
}

#[test]
fn psi_states() {
    // T-172: PSI built in but turned off reads as EOPNOTSUPP.
    assert_eq!(
        pressure_error_state(&std::io::Error::from_raw_os_error(95)),
        "disabled"
    );
    assert_eq!(
        pressure_error_state(&std::io::Error::from(std::io::ErrorKind::NotFound)),
        "unsupported"
    );
    assert_eq!(
        pressure_error_state(&std::io::Error::from_raw_os_error(13)),
        "unreadable"
    );
    let dir = tempfile::tempdir().unwrap();
    fake_host(dir.path());
    put(
        dir.path(),
        "proc/pressure/cpu",
        "some avg10=0.50 avg60=1.25 avg300=0.75 total=1\n",
    );
    put(
        dir.path(),
        "proc/pressure/memory",
        "some avg10=1.00 avg60=11.50 avg300=3.00 total=1\nfull avg10=0.10 avg60=0.20 avg300=0.30 total=1\n",
    );
    put(
        dir.path(),
        "proc/pressure/io",
        "some avg10=0.00 avg60=0.00 avg300=0.00 total=0\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n",
    );
    let s = Collector::new(dir.path(), 10).sample(now());
    assert_eq!(s.pressure.state, "ok");
    assert_eq!(s.pressure.memory.unwrap().some_avg60, 11.5);
    assert_eq!(s.pressure.cpu.unwrap().full_avg60, None);
}

#[test]
fn disk_classes() {
    assert_eq!(disk_class("sda", true), "physical");
    assert_eq!(disk_class("nvme0n1", true), "physical");
    assert_eq!(disk_class("vda", true), "virtual");
    assert_eq!(disk_class("md127", false), "raid");
    assert_eq!(disk_class("dm-0", false), "mapper");
    assert_eq!(disk_class("nbd0", false), "network");
    assert_eq!(disk_class("zram0", false), "virtual");
    assert_eq!(disk_class("loop3", false), "loop");
}

fn sample(seq: u64) -> hazelminx_protocol::metrics::MetricsSample {
    let dir = tempfile::tempdir().unwrap();
    let mut s = Collector::new(dir.path(), 10).sample(now());
    s.seq = seq;
    s
}

#[test]
fn buffer_drops_oldest_and_counts_once() {
    let m = Monitor::with_capacity(3);
    for i in 1..=5 {
        m.push(sample(i));
    }
    assert_eq!(m.len(), 3);
    let (last, msg) = m.report_after(0).unwrap();
    assert_eq!(last, 5);
    match msg {
        Message::MetricsReport {
            samples,
            dropped_samples,
        } => {
            assert_eq!(samples.iter().map(|s| s.seq).collect::<Vec<_>>(), [3, 4, 5]);
            assert_eq!(dropped_samples, 2);
        }
        other => panic!("{other:?}"),
    }
    // A second report while the first is unacknowledged does not carry the
    // count again (both may be stored).
    let dropped = |m: &Monitor, after: u64| match m.report_after(after).unwrap().1 {
        Message::MetricsReport {
            dropped_samples, ..
        } => dropped_samples,
        other => panic!("{other:?}"),
    };
    assert_eq!(dropped(&m, 0), 0);
    assert!(m.report_after(5).is_none());
    // No acknowledgement: everything waiting, and the count, go again.
    assert!(m.unacked_through(5));
    assert_eq!(m.rewind(), 0);
    assert_eq!(dropped(&m, 0), 2);
    // An acknowledgement short of that report keeps the count.
    m.ack(4);
    assert_eq!(m.len(), 1);
    assert!(m.unacked_through(5));
    assert!(!m.unacked_through(4));
    m.ack(5);
    assert_eq!(m.len(), 0);
    m.push(sample(6));
    assert_eq!(dropped(&m, 5), 0, "an acknowledged count is not sent again");
}

#[test]
fn report_is_split_by_size() {
    let m = Monitor::with_capacity(BUFFER_SAMPLES);
    let mut big = sample(1);
    big.disks = (0..400)
        .map(|i| hazelminx_protocol::metrics::Disk {
            name: format!("sd{i}"),
            class: "physical".into(),
            model: Some("x".repeat(200)),
            transport: None,
            rotational: None,
            removable: None,
            size_bytes: None,
            read_bytes_per_second: None,
            write_bytes_per_second: None,
            reads_per_second: None,
            writes_per_second: None,
            read_latency_ms: None,
            write_latency_ms: None,
            utilization: None,
        })
        .collect();
    for seq in 1..=3 {
        let mut s = big.clone();
        s.seq = seq;
        m.push(s);
    }
    let (last, _) = m.report_after(0).unwrap();
    assert!(last < 3, "three ~140 KiB samples do not fit one report");
    let (next, _) = m.report_after(last).unwrap();
    assert!(next > last);
}

#[test]
fn facts_resend_only_when_changed() {
    let m = Monitor::default();
    assert!(m.facts_after(0).is_none());
    let dir = tempfile::tempdir().unwrap();
    let f = Collector::new(dir.path(), 10).facts();
    assert!(m.set_facts(now(), f.clone()));
    assert!(
        !m.set_facts(now(), f.clone()),
        "same values are not a change"
    );
    let (v, msg) = m.facts_after(0).unwrap();
    assert!(matches!(msg, Message::HostFacts { .. }));
    assert!(m.facts_after(v).is_none());
    let mut g = f;
    g.hostname.value = Some("renamed".into());
    assert!(m.set_facts(now(), g));
    assert!(m.facts_after(v).is_some());
}
