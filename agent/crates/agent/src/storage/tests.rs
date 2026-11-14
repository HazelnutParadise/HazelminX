use std::fs;
use std::path::Path;

use hazelminx_protocol::storage::{Mount, category};

use super::inventory::{self, Inventory};
use super::usage::{self, Budget};
use super::*;

fn write(root: &Path, rel: &str, body: &str) {
    let p = root.join(rel.trim_start_matches('/'));
    fs::create_dir_all(p.parent().unwrap()).unwrap();
    fs::write(p, body).unwrap();
}

#[cfg(unix)]
fn link(root: &Path, rel: &str, target: &str) {
    let p = root.join(rel.trim_start_matches('/'));
    fs::create_dir_all(p.parent().unwrap()).unwrap();
    std::os::unix::fs::symlink(target, p).unwrap();
}

/// A host with a SATA disk (one ext4 partition by UUID), a USB stick
/// mounted by device name, a stale fstab line, a duplicate mount point
/// and a systemd unit.
#[cfg(unix)]
fn fake_host() -> tempfile::TempDir {
    let t = tempfile::tempdir().unwrap();
    let r = t.path();
    for (disk, dev, size, usb) in [
        ("sda", "8:0", "1953525168", false),
        ("sdb", "8:16", "500118192", true),
    ] {
        let sys = if usb {
            format!("devices/pci0000:00/usb2/{disk}")
        } else {
            format!("devices/pci0000:00/ata1/{disk}")
        };
        write(r, &format!("/sys/{sys}/dev"), dev);
        write(r, &format!("/sys/{sys}/size"), size);
        write(r, &format!("/sys/{sys}/ro"), "0");
        write(
            r,
            &format!("/sys/{sys}/removable"),
            if usb { "1" } else { "0" },
        );
        write(r, &format!("/sys/{sys}/queue/rotational"), "1");
        write(
            r,
            &format!("/sys/{sys}/device/model"),
            if usb { "SanDisk  " } else { "WDC WD10EFRX" },
        );
        fs::create_dir_all(r.join(format!("sys/class/block/{disk}/holders"))).unwrap();
        link(
            r,
            &format!("/sys/block/{disk}"),
            &r.join("sys").join(&sys).to_string_lossy(),
        );
        let part = format!("{disk}1");
        let pdev = if usb { "8:17" } else { "8:1" };
        write(r, &format!("/sys/{sys}/{part}/dev"), pdev);
        write(r, &format!("/sys/{sys}/{part}/size"), "1000");
        write(r, &format!("/sys/{sys}/{part}/partition"), "1");
        fs::create_dir_all(r.join(format!("sys/class/block/{part}/holders"))).unwrap();
    }
    write(
        r,
        "/run/udev/data/b8:1",
        "E:ID_FS_TYPE=ext4\nE:ID_FS_UUID=5a7c-e2\nE:ID_FS_LABEL=data\nE:ID_PART_ENTRY_UUID=8f1d-01\n",
    );
    write(r, "/run/udev/data/b8:16", "E:ID_SERIAL_SHORT=4C530001\n");
    // No udev entry for sdb1: its UUID comes from the by-uuid link.
    link(r, "/dev/disk/by-uuid/64F2-91AB", "../../sdb1");
    link(r, "/dev/disk/by-label/my\\x20stick", "../../sdb1");
    write(
        r,
        "/etc/fstab",
        "# comment\n\nUUID=5a7c-e2 /mnt/data ext4 defaults 0 2\n\
         UUID=9d2c-07 /mnt/old ext4 defaults 0 2\n\
         nas:/media /mnt/media nfs4 noauto 0 0\n\
         //nas/media /mnt/media cifs x-systemd.automount 0 0\n\
         LABEL=my\\040stick /mnt/stick exfat noauto 0 0\n\
         /swapfile none swap sw 0 0\n",
    );
    write(
        r,
        "/etc/systemd/system/mnt-backup.mount",
        "[Unit]\nDescription=b\n[Mount]\nWhat=nas:/backup\nWhere=/mnt/backup\nType=nfs4\nOptions=_netdev\n[Install]\nWantedBy=multi-user.target\n",
    );
    write(
        r,
        "/proc/self/mountinfo",
        "22 1 8:1 / /mnt/data rw,relatime shared:1 - ext4 /dev/sda1 rw\n\
         23 1 8:17 / /mnt/scratch rw,relatime shared:2 - exfat /dev/sdb1 rw\n\
         24 1 0:5 / /proc rw - proc proc rw\n\
         25 1 0:40 / /mnt/backup rw - nfs4 nas:/backup rw\n",
    );
    write(
        r,
        "/proc/swaps",
        "Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n/swapfile                               file\t\t4194300\t\t524288\t\t-2\n",
    );
    t
}

#[cfg(unix)]
#[test]
fn inventory_reads_devices_with_identity() {
    let t = fake_host();
    let inv = Inventory::new(t.path()).read_all();
    let names: Vec<_> = inv.devices.iter().map(|d| d.name.as_str()).collect();
    assert_eq!(names, ["sda", "sda1", "sdb", "sdb1"]);
    let sda1 = &inv.devices[1];
    assert_eq!(sda1.kind, "partition");
    assert_eq!(sda1.parent.as_deref(), Some("sda"));
    assert_eq!(sda1.fs_type.as_deref(), Some("ext4"));
    assert_eq!(sda1.fs_uuid.as_deref(), Some("5a7c-e2"));
    assert_eq!(sda1.size_bytes, Some(512_000));
    let sdb = &inv.devices[2];
    assert_eq!(sdb.transport.as_deref(), Some("usb"));
    assert_eq!(sdb.removable, Some(true));
    assert_eq!(sdb.model.as_deref(), Some("SanDisk"));
    assert_eq!(sdb.serial.as_deref(), Some("4C530001"));
    // Values without a source stay null, never a guess.
    let sdb1 = &inv.devices[3];
    assert_eq!(sdb1.fs_type, None);
    assert_eq!(sdb1.fs_uuid, None);
    assert!(inv.unreadable.is_empty());
}

#[cfg(unix)]
#[test]
fn mounts_carry_reference_and_configuration() {
    let t = fake_host();
    let inv = Inventory::new(t.path()).read_all();
    let points: Vec<_> = inv.mounts.iter().map(|m| m.mount_point.as_str()).collect();
    assert_eq!(
        points,
        ["/mnt/backup", "/mnt/data", "/mnt/scratch"],
        "proc is not storage"
    );
    let data = &inv.mounts[1];
    assert_eq!(data.reference, "uuid");
    assert_eq!(data.device.as_deref(), Some("sda1"));
    assert_eq!(data.configs, vec![0]);
    assert_eq!(inv.configs[0].resolves_to.as_deref(), Some("sda1"));
    assert_eq!(inv.configs[0].line, Some(3));
    let scratch = &inv.mounts[2];
    assert_eq!(scratch.reference, "device");
    assert!(scratch.configs.is_empty(), "mounted by hand");
    let backup = &inv.mounts[0];
    assert_eq!(backup.class, "network");
    assert_eq!(backup.reference, "network");
    let unit = &inv.configs[backup.configs[0] as usize];
    assert_eq!(unit.origin, "systemdUnit");
    assert_eq!(unit.boot, "auto");
    // An escaped label resolves through the udev-escaped link.
    let stick = inv
        .configs
        .iter()
        .find(|c| c.mount_point == "/mnt/stick")
        .unwrap();
    assert_eq!(stick.reference, "label");
    assert_eq!(stick.resolves_to.as_deref(), Some("sdb1"));
    assert_eq!(stick.boot, "noauto");
    let media: Vec<_> = inv
        .configs
        .iter()
        .filter(|c| c.mount_point == "/mnt/media")
        .collect();
    assert_eq!(media[1].boot, "onDemand");
    assert_eq!(inv.swaps.len(), 1);
    assert_eq!(inv.swaps[0].size_bytes, 4_194_300 * 1024);
    assert_eq!(inv.swaps[0].configs.len(), 1);
}

#[cfg(unix)]
#[test]
fn problems_are_reported() {
    let t = fake_host();
    let inv = Inventory::new(t.path()).read_all();
    let has = |kind: &str, point: &str| {
        inv.problems
            .iter()
            .any(|p| p.kind == kind && p.mount_point == point)
    };
    assert!(has("duplicateMountPoint", "/mnt/media"));
    assert!(has("missingDevice", "/mnt/old"));
    assert!(has("notMounted", "/mnt/old"));
    assert!(has("deviceName", "/mnt/scratch"));
    assert!(!has("deviceName", "/mnt/data"));
    assert!(
        !has("notMounted", "/mnt/media"),
        "noauto and automount entries are not expected mounted"
    );
    assert!(
        !has("missingDevice", "/mnt/media"),
        "network shares are not block devices"
    );
}

#[test]
fn references_by_spec() {
    for (spec, ty, want) in [
        ("UUID=abc", "ext4", "uuid"),
        ("/dev/disk/by-uuid/abc", "ext4", "uuid"),
        ("PARTUUID=x", "ext4", "partuuid"),
        ("/dev/disk/by-id/ata-WDC", "ext4", "id"),
        ("/dev/disk/by-path/pci-0000", "ext4", "path"),
        ("/dev/sdb1", "ext4", "device"),
        ("/dev/nvme0n1p2", "ext4", "device"),
        ("/dev/mapper/vg-root", "ext4", "mapper"),
        ("/dev/md0", "ext4", "mapper"),
        ("nas:/export", "nfs4", "network"),
        ("//nas/share", "cifs", "network"),
        ("tmpfs", "tmpfs", "virtual"),
        ("/swapfile", "swap", "other"),
    ] {
        assert_eq!(inventory::reference(spec, ty), want, "{spec}");
    }
}

#[test]
fn unreadable_sources_are_listed() {
    let t = tempfile::tempdir().unwrap();
    let inv = Inventory::new(t.path()).read_all();
    assert_eq!(inv.unreadable, ["/proc/self/mountinfo"]);
    assert!(inv.devices.is_empty() && inv.mounts.is_empty());
}

fn root_mount(_: &Path) -> Mount {
    Mount {
        mount_point: "/".into(),
        source: "/dev/test".into(),
        device: None,
        dev: "8:1".into(),
        fs_type: "ext4".into(),
        class: "local".into(),
        options: "rw".into(),
        read_only: false,
        reference: "uuid".into(),
        configs: vec![],
    }
}

#[cfg(unix)]
fn sizes(
    u: &hazelminx_protocol::storage::FilesystemUsage,
) -> std::collections::BTreeMap<String, u64> {
    u.categories
        .iter()
        .map(|c| (c.category.clone(), c.bytes))
        .collect()
}

/// Each path counts in one category, hard links once, and the categories
/// plus other add up to used space.
#[cfg(unix)]
#[test]
fn usage_counts_each_path_once_and_adds_up() {
    let t = tempfile::tempdir().unwrap();
    let r = t.path();
    let big = vec![7u8; 64 * 1024];
    write(r, "/var/lib/docker/overlay2/l1/file", "");
    fs::write(r.join("var/lib/docker/overlay2/l1/file"), &big).unwrap();
    write(r, "/var/lib/docker/volumes/db/data", "");
    fs::write(r.join("var/lib/docker/volumes/db/data"), &big).unwrap();
    write(r, "/usr/bin/tool", "");
    fs::write(r.join("usr/bin/tool"), &big).unwrap();
    // A hard link of the same layer file in another layer.
    fs::create_dir_all(r.join("var/lib/docker/overlay2/l2")).unwrap();
    fs::hard_link(
        r.join("var/lib/docker/overlay2/l1/file"),
        r.join("var/lib/docker/overlay2/l2/file"),
    )
    .unwrap();
    // A symlinked root (merged /usr) is not walked twice.
    link(r, "/lib", "usr/lib");
    let m = root_mount(r);
    let used = 10 * 1024 * 1024;
    let u = usage::measure(r, &[m], &["/".into()], Budget::default(), |_| Some(used));
    assert_eq!(u.filesystems.len(), 1);
    let fs0 = &u.filesystems[0];
    assert_eq!(fs0.state, "complete");
    let s = sizes(fs0);
    let block = |n: &str| {
        use std::os::unix::fs::MetadataExt;
        fs::symlink_metadata(r.join(n)).unwrap().blocks() * 512
    };
    let file = block("usr/bin/tool");
    assert!(
        s[category::CONTAINER_IMAGES] >= file && s[category::CONTAINER_IMAGES] < 2 * file,
        "hard link counted once: {s:?}"
    );
    assert!(
        s[category::APP_DATA] >= file && s[category::APP_DATA] < 2 * file,
        "volumes are app data only: {s:?}"
    );
    assert!(s[category::OS_AND_LOGS] >= file);
    let sum: u64 = s.values().sum();
    assert_eq!(sum + fs0.other_bytes.unwrap(), used);
    assert!(!s.contains_key(category::DATABASE) && !s.contains_key(category::BACKUP_STAGING));
    let docker = fs0
        .categories
        .iter()
        .find(|c| c.category == category::CONTAINER_IMAGES)
        .unwrap();
    assert_eq!(docker.paths, ["/var/lib/docker"]);
}

#[cfg(unix)]
#[test]
fn usage_budget_makes_the_round_partial() {
    let t = tempfile::tempdir().unwrap();
    let r = t.path();
    for i in 0..50 {
        write(r, &format!("/var/log/f{i}"), "x");
    }
    let budget = Budget {
        max_entries: 10,
        ..Budget::default()
    };
    let u = usage::measure(r, &[root_mount(r)], &["/".into()], budget, |_| {
        Some(1 << 20)
    });
    let fs0 = &u.filesystems[0];
    assert_eq!(fs0.state, "partial");
    assert_eq!(fs0.other_bytes, None, "a lower bound gives no honest rest");
    assert!(u.entries <= 10);
}

#[cfg(unix)]
#[test]
fn usage_without_statvfs_is_unmeasured() {
    let t = tempfile::tempdir().unwrap();
    let u = usage::measure(
        t.path(),
        &[root_mount(t.path())],
        &["/".into()],
        Budget::default(),
        |_| None,
    );
    assert_eq!(u.filesystems[0].state, "unmeasured");
    assert_eq!(u.filesystems[0].used_bytes, None);
    assert_eq!(u.filesystems[0].other_bytes, None);
}

#[cfg(unix)]
#[test]
fn usage_skips_network_and_unreadable_dirs_mark_partial() {
    use std::os::unix::fs::PermissionsExt;
    let t = tempfile::tempdir().unwrap();
    let r = t.path();
    write(r, "/var/log/secret/x", "x");
    let mut net = root_mount(r);
    net.class = "network".into();
    net.mount_point = "/mnt/nfs".into();
    net.dev = "0:99".into();
    fs::set_permissions(r.join("var/log/secret"), fs::Permissions::from_mode(0o000)).unwrap();
    let u = usage::measure(
        r,
        &[root_mount(r), net],
        &["/".into(), "/mnt/nfs".into()],
        Budget::default(),
        |_| Some(1 << 20),
    );
    fs::set_permissions(r.join("var/log/secret"), fs::Permissions::from_mode(0o700)).unwrap();
    assert_eq!(u.filesystems.len(), 1, "network mounts are not walked");
    // Root can read a 0o000 directory.
    if unsafe { libc::geteuid() } != 0 {
        assert_eq!(u.filesystems[0].state, "partial");
        assert_eq!(u.filesystems[0].unreadable_dirs, 1);
    }
}

#[test]
fn versions_send_each_change_once() {
    let s = Storage::default();
    assert!(s.inventory_after(0).is_none());
    let inv = Inventory::new("/nonexistent-root").read_all();
    let at = OffsetDateTime::UNIX_EPOCH;
    assert!(s.set_inventory(at, inv.clone()));
    assert!(
        !s.set_inventory(at, inv),
        "unchanged inventory is not resent"
    );
    let (v, m) = s.inventory_after(0).unwrap();
    assert!(matches!(m, Message::HostStorage { .. }));
    assert!(s.inventory_after(v).is_none());
    assert!(s.usage_after(0).is_none());
}

/// The walk stops at every mount point, and a root under a mount that is
/// not local (a container's overlay root) is not measured at all.
#[cfg(unix)]
#[test]
fn usage_stops_at_mount_points_and_skips_virtual_roots() {
    let t = tempfile::tempdir().unwrap();
    let r = t.path();
    let big = vec![1u8; 64 * 1024];
    write(r, "/var/log/a", "");
    fs::write(r.join("var/log/a"), &big).unwrap();
    // Another filesystem mounted inside /var/log.
    write(r, "/var/log/mnt/b", "");
    fs::write(r.join("var/log/mnt/b"), &big).unwrap();
    write(r, "/var/lib/docker/x", "");
    fs::write(r.join("var/lib/docker/x"), &big).unwrap();
    let mut data = root_mount(r);
    data.mount_point = "/var/lib/docker".into();
    data.dev = "8:2".into();
    // "/" is an overlay (not in the local list); /var/log/mnt is tmpfs.
    let points = [
        "/".to_string(),
        "/var/log/mnt".into(),
        "/var/lib/docker".into(),
    ];
    let mut root = root_mount(r);
    root.class = "virtual".into();
    let u = usage::measure(r, &[root, data], &points, Budget::default(), |_| {
        Some(10 << 20)
    });
    assert_eq!(u.filesystems.len(), 1, "{:?}", u.filesystems);
    let d = &u.filesystems[0];
    assert_eq!(d.mount_point, "/var/lib/docker");
    assert_eq!(d.categories.len(), 1);
    assert_eq!(d.categories[0].category, category::CONTAINER_IMAGES);
}

/// A password in fstab or a unit never leaves the host.
#[test]
fn mount_secrets_are_redacted() {
    let c = inventory::fstab(
        "//nas/share /mnt/s cifs username=u,password=hunter2,credentials=/etc/smb.cred,vers=3.0 0 0\n\
         https://u:hunter2@dav.example/x /mnt/dav davfs pass=hunter2 0 0\n",
        "/etc/fstab",
    );
    assert_eq!(
        c[0].options,
        "username=u,password=***,credentials=/etc/smb.cred,vers=3.0"
    );
    assert_eq!(c[1].spec, "https://u:***@dav.example/x");
    assert_eq!(c[1].options, "pass=***");
    let u = inventory::unit(
        "[Mount]\nWhat=//nas/b\nWhere=/mnt/b\nType=cifs\nOptions=user=u,Password=hunter2\n",
        "/etc/systemd/system/mnt-b.mount",
        false,
    )
    .unwrap();
    assert_eq!(u.options, "user=u,Password=***");
    let c2 = inventory::fstab(
        "//nas/s /mnt/x cifs username=u%hunter2,password=ab,,hunter2,,x,vers=3.0 0 0\n",
        "/etc/fstab",
    );
    assert_eq!(c2[0].options, "username=u%***,password=***,vers=3.0");
    let mi = inventory::mounts(
        "30 1 0:60 / /mnt/dav rw - fuse.davfs https://u:hunter2@dav.example/x rw\n",
        &[],
        &[],
    );
    assert_eq!(mi[0].source, "https://u:***@dav.example/x");
    let all = format!("{c:?}{u:?}{c2:?}{mi:?}");
    assert!(!all.contains("hunter2"), "{all}");
}

/// Measured more than used: the categories cannot add up, so the round is
/// partial and has no "other".
#[cfg(unix)]
#[test]
fn usage_over_used_space_is_partial() {
    let t = tempfile::tempdir().unwrap();
    let r = t.path();
    write(r, "/var/log/a", "");
    fs::write(r.join("var/log/a"), vec![1u8; 64 * 1024]).unwrap();
    let u = usage::measure(
        r,
        &[root_mount(r)],
        &["/".into()],
        Budget::default(),
        |_| Some(1),
    );
    assert_eq!(u.filesystems[0].state, "partial");
    assert_eq!(u.filesystems[0].other_bytes, None);
}

/// A huge inventory is cut to fit one message, and says so.
#[test]
fn oversized_inventory_is_truncated_and_named() {
    let mut inv = Inventory::new("/nonexistent-root").read_all();
    let m = root_mount(Path::new("/"));
    inv.mounts = (0..20_000)
        .map(|i| Mount {
            mount_point: format!("/snap/pkg{i}"),
            ..m.clone()
        })
        .collect();
    let out = fit(inv);
    assert!(serde_json::to_vec(&out).unwrap().len() <= INVENTORY_BYTES);
    assert!(out.mounts.len() < 20_000 && !out.mounts.is_empty());
    assert!(
        out.unreadable.iter().any(|u| u.starts_with("truncated:")),
        "{:?}",
        out.unreadable
    );
    // The data mount sorts last but survives; snap mounts go first.
    let mut inv = Inventory::new("/nonexistent-root").read_all();
    inv.mounts = (0..20_000)
        .map(|i| Mount {
            mount_point: format!("/snap/pkg{i}"),
            fs_type: "squashfs".into(),
            ..m.clone()
        })
        .collect();
    inv.mounts.push(Mount {
        mount_point: "/srv/data".into(),
        ..m.clone()
    });
    let out = fit(inv);
    assert!(out.mounts.iter().any(|x| x.mount_point == "/srv/data"));
    let small = fit(Inventory::new("/nonexistent-root").read_all());
    assert!(!small.unreadable.iter().any(|u| u.starts_with("truncated:")));
}
