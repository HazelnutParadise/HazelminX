use std::fs;
use std::path::Path;

use hazelminx_protocol::storage::category;

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

fn root_mount(_: &Path) -> Mount {
    Mount {
        mount_point: "/".into(),
        dev: "8:1".into(),
        class: "local",
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
    let u = measure(r, &[m], &["/".into()], Budget::default(), |_| Some(used));
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
    let u = measure(r, &[root_mount(r)], &["/".into()], budget, |_| {
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
    let u = measure(
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
    net.class = "network";
    net.mount_point = "/mnt/nfs".into();
    net.dev = "0:99".into();
    fs::set_permissions(r.join("var/log/secret"), fs::Permissions::from_mode(0o000)).unwrap();
    let u = measure(
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
    root.class = "virtual";
    let u = measure(r, &[root, data], &points, Budget::default(), |_| {
        Some(10 << 20)
    });
    assert_eq!(u.filesystems.len(), 1, "{:?}", u.filesystems);
    let d = &u.filesystems[0];
    assert_eq!(d.mount_point, "/var/lib/docker");
    assert_eq!(d.categories.len(), 1);
    assert_eq!(d.categories[0].category, category::CONTAINER_IMAGES);
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
    let u = measure(
        r,
        &[root_mount(r)],
        &["/".into()],
        Budget::default(),
        |_| Some(1),
    );
    assert_eq!(u.filesystems[0].state, "partial");
    assert_eq!(u.filesystems[0].other_bytes, None);
}

#[test]
fn mounts_from_mountinfo_keep_points_and_skip_pseudo() {
    let (m, points) = mounts_from(
        "22 1 8:1 / / rw - ext4 /dev/sda1 rw\n\
         23 22 0:5 / /proc rw - proc proc rw\n\
         24 22 0:40 / /mnt/nfs rw - nfs4 nas:/x rw\n\
         25 22 8:2 / /mnt/my\\040disk rw - xfs /dev/sdb1 rw\n",
    );
    assert_eq!(points, ["/", "/proc", "/mnt/nfs", "/mnt/my disk"]);
    let got: Vec<_> = m
        .iter()
        .map(|m| (m.mount_point.as_str(), m.class))
        .collect();
    assert_eq!(
        got,
        [
            ("/", "local"),
            ("/mnt/my disk", "local"),
            ("/mnt/nfs", "network")
        ]
    );
}

/// A directory swapped for a symlink is not listed through the link.
#[cfg(unix)]
#[test]
fn open_dir_refuses_a_symlink() {
    let t = tempfile::tempdir().unwrap();
    fs::create_dir(t.path().join("real")).unwrap();
    std::os::unix::fs::symlink(t.path().join("real"), t.path().join("link")).unwrap();
    assert!(open_dir(&t.path().join("real")).is_ok());
    assert!(open_dir(&t.path().join("link")).is_err());
}

/// One walk at a time.
#[test]
fn only_one_measurement_runs() {
    let a = RunningGuard::take().expect("first");
    assert!(RunningGuard::take().is_none());
    drop(a);
    assert!(RunningGuard::take().is_some());
}

/// The host's view comes from PID 1 when it is readable.
#[test]
fn host_view_prefers_pid_1() {
    let t = tempfile::tempdir().unwrap();
    write(t.path(), "/proc/self/mountinfo", "self");
    let (root, text) = host_view(t.path());
    assert_eq!(
        (root, text.unwrap()),
        (t.path().to_path_buf(), "self".to_string())
    );
    write(t.path(), "/proc/1/mountinfo", "init");
    fs::create_dir_all(t.path().join("proc/1/root")).unwrap();
    let (root, text) = host_view(t.path());
    assert_eq!(
        (root, text.unwrap()),
        (t.path().join("proc/1/root"), "init".to_string())
    );
}
