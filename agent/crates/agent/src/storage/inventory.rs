//! Reads block devices, mounts, mount configuration and swap areas from
//! `/sys`, `/proc`, `/run/udev/data`, `/dev/disk/by-*`, `/etc/fstab` and
//! `/etc/systemd/system/*.mount`. Paths are joined to `root` so tests can
//! use a fake tree.

use std::collections::{BTreeMap, BTreeSet};
use std::path::{Path, PathBuf};

use hazelminx_protocol::storage::{
    BlockDevice, Mount, MountConfig, MountProblem, StorageInventory, SwapArea, reference_kind,
};

use crate::monitor::collect::{NETWORK_FS, PSEUDO_FS, disk_class};
use crate::monitor::parse;

pub struct Inventory {
    root: PathBuf,
}

impl Inventory {
    pub fn new(root: impl Into<PathBuf>) -> Self {
        Inventory { root: root.into() }
    }

    fn path(&self, abs: &str) -> PathBuf {
        self.root.join(abs.trim_start_matches('/'))
    }

    fn read(&self, abs: &str) -> Option<String> {
        std::fs::read_to_string(self.path(abs)).ok()
    }

    fn read_trim(&self, abs: &str) -> Option<String> {
        self.read(abs)
            .map(|s| s.trim().to_string())
            .filter(|s| !s.is_empty())
    }

    pub fn read_all(&self) -> StorageInventory {
        let mut unreadable = Vec::new();
        let devices = self.devices(&mut unreadable);
        let ids = Identities::new(&devices, self);
        let mut configs = Vec::new();
        match self.read("/etc/fstab") {
            Some(t) => configs.extend(fstab(&t, "/etc/fstab")),
            None if self.path("/etc/fstab").exists() => unreadable.push("/etc/fstab".into()),
            None => {}
        }
        configs.extend(self.units());
        for c in configs.iter_mut() {
            c.resolves_to = ids.resolve(&c.spec);
        }
        let mounts = match self.read("/proc/self/mountinfo") {
            Some(t) => mounts(&t, &devices, &configs),
            None => {
                unreadable.push("/proc/self/mountinfo".into());
                Vec::new()
            }
        };
        let swaps = match self.read("/proc/swaps") {
            Some(t) => swaps(&t, &configs),
            None => Vec::new(),
        };
        let problems = problems(&configs, &mounts, &swaps, &devices);
        StorageInventory {
            devices,
            mounts,
            configs,
            swaps,
            problems,
            unreadable,
        }
    }

    fn devices(&self, unreadable: &mut Vec<String>) -> Vec<BlockDevice> {
        let mut names = sorted_dir(&self.path("/sys/block"));
        names.retain(|n| {
            // Unused loop and ram devices have no size.
            self.read_trim(&format!("/sys/block/{n}/size")).as_deref() != Some("0")
        });
        let udev_dir = self.path("/run/udev/data");
        let have_udev = udev_dir.is_dir();
        if !have_udev && self.path("/run/udev").exists() {
            unreadable.push("/run/udev/data".into());
        }
        let mut out = Vec::new();
        for name in names {
            let base = format!("/sys/block/{name}");
            let has_device = self.path(&format!("{base}/device")).exists();
            let class = disk_class(&name, has_device);
            let real = std::fs::canonicalize(self.path(&base))
                .map(|p| p.to_string_lossy().into_owned())
                .unwrap_or_default();
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
            let mut disk = self.device(&name, &base, "disk", None, class);
            disk.model = self.read_trim(&format!("{base}/device/model"));
            disk.serial = self
                .read_trim(&format!("{base}/device/serial"))
                .or_else(|| disk.serial.take());
            disk.transport = transport.map(str::to_string);
            disk.rotational = self
                .read_trim(&format!("{base}/queue/rotational"))
                .map(|v| v == "1");
            disk.removable = self
                .read_trim(&format!("{base}/removable"))
                .map(|v| v == "1");
            out.push(disk);
            for part in sorted_dir(&self.path(&base)) {
                if part.starts_with(&name)
                    && self.path(&format!("{base}/{part}/partition")).exists()
                {
                    let p = self.device(
                        &part,
                        &format!("{base}/{part}"),
                        "partition",
                        Some(name.clone()),
                        class,
                    );
                    out.push(p);
                }
            }
        }
        out
    }

    fn device(
        &self,
        name: &str,
        sys: &str,
        kind: &str,
        parent: Option<String>,
        class: &str,
    ) -> BlockDevice {
        let dev = self.read_trim(&format!("{sys}/dev")).unwrap_or_default();
        let udev = self
            .read(&format!("/run/udev/data/b{dev}"))
            .map(|t| udev_props(&t))
            .unwrap_or_default();
        let get = |k: &str| udev.get(k).cloned().filter(|v| !v.is_empty());
        BlockDevice {
            name: name.to_string(),
            kind: kind.to_string(),
            parent,
            dev,
            class: class.to_string(),
            size_bytes: self
                .read_trim(&format!("{sys}/size"))
                .and_then(|s| s.parse::<u64>().ok())
                .and_then(|s| s.checked_mul(512)),
            model: None,
            serial: get("ID_SERIAL_SHORT"),
            transport: None,
            rotational: None,
            removable: None,
            read_only: self.read_trim(&format!("{sys}/ro")).map(|v| v == "1"),
            fs_type: get("ID_FS_TYPE"),
            fs_uuid: get("ID_FS_UUID"),
            fs_label: get("ID_FS_LABEL"),
            part_uuid: get("ID_PART_ENTRY_UUID"),
            part_label: get("ID_PART_ENTRY_NAME"),
            holders: sorted_dir(&self.path(&format!("/sys/class/block/{name}/holders"))),
        }
    }

    /// systemd `.mount` units an administrator (or the platform) wrote.
    /// Units generated from fstab live under /run and are not read: they
    /// would list every fstab line twice.
    fn units(&self) -> Vec<MountConfig> {
        let dir = "/etc/systemd/system";
        let mut out = Vec::new();
        for f in sorted_dir(&self.path(dir)) {
            if !f.ends_with(".mount") {
                continue;
            }
            let file = format!("{dir}/{f}");
            let Some(text) = self.read(&file) else {
                continue;
            };
            let has_automount = self
                .path(&format!("{dir}/{}", f.replace(".mount", ".automount")))
                .exists();
            if let Some(c) = unit(&text, &file, has_automount) {
                out.push(c);
            }
        }
        out
    }
}

/// The identities of the block devices, for resolving `UUID=` and friends.
struct Identities {
    by: BTreeMap<(&'static str, String), String>,
    names: BTreeSet<String>,
}

impl Identities {
    fn new(devices: &[BlockDevice], inv: &Inventory) -> Self {
        let mut by = BTreeMap::new();
        for d in devices {
            for (k, v) in [
                ("uuid", &d.fs_uuid),
                ("label", &d.fs_label),
                ("partuuid", &d.part_uuid),
                ("partlabel", &d.part_label),
            ] {
                if let Some(v) = v {
                    by.insert((k, v.clone()), d.name.clone());
                }
            }
        }
        // The by-* links fill in what the udev database did not have.
        for (k, dir) in [
            ("uuid", "by-uuid"),
            ("label", "by-label"),
            ("partuuid", "by-partuuid"),
            ("partlabel", "by-partlabel"),
            ("id", "by-id"),
            ("path", "by-path"),
        ] {
            let d = inv.path(&format!("/dev/disk/{dir}"));
            for link in sorted_dir(&d) {
                if let Ok(t) = std::fs::read_link(d.join(&link))
                    && let Some(n) = t.file_name().and_then(|n| n.to_str())
                {
                    by.entry((k, unescape_udev(&link))).or_insert(n.to_string());
                }
            }
        }
        Identities {
            by,
            names: devices.iter().map(|d| d.name.clone()).collect(),
        }
    }

    /// The device a mount spec names, if this host has it.
    fn resolve(&self, spec: &str) -> Option<String> {
        let (kind, value) = split_spec(spec)?;
        match kind {
            "device" => {
                let n = value.strip_prefix("/dev/")?;
                self.names.contains(n).then(|| n.to_string())
            }
            "mapper" => {
                // /dev/mapper/X and /dev/md/X are links to dm-N / mdN.
                let link = value.strip_prefix("/dev/")?;
                self.by
                    .get(&("mapper", link.to_string()))
                    .cloned()
                    .or_else(|| {
                        let n = Path::new(value).file_name()?.to_str()?;
                        self.names.contains(n).then(|| n.to_string())
                    })
            }
            k => self.by.get(&(k, value.to_string())).cloned(),
        }
    }
}

/// (kind, value) of a mount spec that names a block device.
fn split_spec(spec: &str) -> Option<(&'static str, &str)> {
    for (p, k) in [
        ("UUID=", "uuid"),
        ("LABEL=", "label"),
        ("PARTUUID=", "partuuid"),
        ("PARTLABEL=", "partlabel"),
        ("/dev/disk/by-uuid/", "uuid"),
        ("/dev/disk/by-label/", "label"),
        ("/dev/disk/by-partuuid/", "partuuid"),
        ("/dev/disk/by-partlabel/", "partlabel"),
        ("/dev/disk/by-id/", "id"),
        ("/dev/disk/by-path/", "path"),
    ] {
        if let Some(v) = spec.strip_prefix(p) {
            return Some((k, v.trim_matches('"')));
        }
    }
    if spec.starts_with("/dev/mapper/") || spec.starts_with("/dev/md/") {
        return Some(("mapper", spec));
    }
    if spec.starts_with("/dev/") {
        return Some(("device", spec));
    }
    None
}

/// How a spec names its source (`reference_kind`).
pub fn reference(spec: &str, fs_type: &str) -> &'static str {
    match split_spec(spec) {
        Some(("uuid", _)) => reference_kind::UUID,
        Some(("label", _)) => reference_kind::LABEL,
        Some(("partuuid", _)) => reference_kind::PARTUUID,
        Some(("partlabel", _)) => reference_kind::PARTLABEL,
        Some(("id", _)) => reference_kind::ID,
        Some(("path", _)) => reference_kind::PATH,
        Some(("mapper", _)) => reference_kind::MAPPER,
        Some(_) => {
            let n = spec.trim_start_matches("/dev/");
            if n.starts_with("dm-") || n.starts_with("md") {
                reference_kind::MAPPER
            } else {
                reference_kind::DEVICE
            }
        }
        None if NETWORK_FS.contains(&fs_type)
            || spec.starts_with("//")
            || (spec.contains(':') && !spec.starts_with('/')) =>
        {
            reference_kind::NETWORK
        }
        None if !spec.starts_with('/') => reference_kind::VIRTUAL,
        None => reference_kind::OTHER,
    }
}

fn boot_of(options: &str, automount: bool) -> &'static str {
    let opts: Vec<&str> = options.split(',').collect();
    if automount || opts.contains(&"x-systemd.automount") {
        "onDemand"
    } else if opts.contains(&"noauto") {
        "noauto"
    } else {
        "auto"
    }
}

/// The `\x2f`-style escapes udev uses in by-* link names.
fn unescape_udev(s: &str) -> String {
    let mut out = String::new();
    let mut rest = s;
    while let Some(i) = rest.find("\\x") {
        out.push_str(&rest[..i]);
        let hex = rest.get(i + 2..i + 4);
        match hex.and_then(|h| u8::from_str_radix(h, 16).ok()) {
            Some(v) => {
                out.push(v as char);
                rest = &rest[i + 4..];
            }
            None => {
                out.push_str("\\x");
                rest = &rest[i + 2..];
            }
        }
    }
    out.push_str(rest);
    out
}

/// Option keys whose values are secrets (CIFS, davfs, sshfs and others
/// accept a password in the options). `credentials=` names a file and is
/// kept.
const SECRET_OPTIONS: &[&str] = &[
    "password",
    "pass",
    "passwd",
    "pw",
    "secret",
    "token",
    "key",
    "passphrase",
];

/// The options with every secret value replaced by `***`, so mount
/// configuration never carries a password off the host.
pub fn redact_options(options: &str) -> String {
    options
        .split(',')
        .map(|o| match o.split_once('=') {
            Some((k, _)) if SECRET_OPTIONS.contains(&k.to_ascii_lowercase().as_str()) => {
                format!("{k}=***")
            }
            _ => o.to_string(),
        })
        .collect::<Vec<_>>()
        .join(",")
}

/// A source with a password in it (`https://user:secret@host/dav`) keeps
/// the user and loses the password.
pub fn redact_spec(spec: &str) -> String {
    let Some((scheme, rest)) = spec.split_once("://") else {
        return spec.to_string();
    };
    let Some(at) = rest.find('@') else {
        return spec.to_string();
    };
    let (userinfo, host) = rest.split_at(at);
    match userinfo.split_once(':') {
        Some((user, _)) => format!("{scheme}://{user}:***{host}"),
        None => spec.to_string(),
    }
}

pub fn fstab(text: &str, file: &str) -> Vec<MountConfig> {
    let mut out = Vec::new();
    for (i, line) in text.lines().enumerate() {
        let l = line.trim();
        if l.is_empty() || l.starts_with('#') {
            continue;
        }
        let f: Vec<&str> = l.split_whitespace().collect();
        if f.len() < 3 {
            continue;
        }
        let spec = redact_spec(&parse::unescape(f[0]));
        let target = parse::unescape(f[1]);
        let fs_type = f[2].to_string();
        let options = redact_options(f.get(3).copied().unwrap_or("defaults"));
        out.push(MountConfig {
            origin: "fstab".into(),
            file: file.into(),
            line: Some(i as u32 + 1),
            reference: reference(&spec, &fs_type).into(),
            boot: boot_of(&options, false).into(),
            spec,
            mount_point: target,
            fs_type,
            options,
            resolves_to: None,
        });
    }
    out
}

/// A `.mount` unit: `What=`, `Where=`, `Type=`, `Options=` of `[Mount]`,
/// and whether `WantedBy=` (or a matching `.automount`) brings it up.
pub fn unit(text: &str, file: &str, automount: bool) -> Option<MountConfig> {
    let mut section = "";
    let (mut what, mut wh, mut ty, mut opts, mut wanted) =
        (None, None, String::new(), String::new(), false);
    for line in text.lines() {
        let l = line.trim();
        if l.starts_with('[') {
            section = l;
            continue;
        }
        let Some((k, v)) = l.split_once('=') else {
            continue;
        };
        let (k, v) = (k.trim(), v.trim().to_string());
        match (section, k) {
            ("[Mount]", "What") => what = Some(redact_spec(&v)),
            ("[Mount]", "Where") => wh = Some(v),
            ("[Mount]", "Type") => ty = v,
            ("[Mount]", "Options") => opts = redact_options(&v),
            ("[Install]", "WantedBy" | "RequiredBy") => wanted = !v.is_empty(),
            _ => {}
        }
    }
    let (what, wh) = (what?, wh?);
    let boot = if automount {
        "onDemand"
    } else if wanted {
        boot_of(&opts, false)
    } else {
        "noauto"
    };
    Some(MountConfig {
        origin: "systemdUnit".into(),
        file: file.into(),
        line: None,
        reference: reference(&what, &ty).into(),
        spec: what,
        mount_point: wh,
        fs_type: ty,
        options: opts,
        boot: boot.into(),
        resolves_to: None,
    })
}

fn configs_for(configs: &[MountConfig], point: &str) -> Vec<u32> {
    configs
        .iter()
        .enumerate()
        .filter(|(_, c)| c.mount_point == point)
        .map(|(i, _)| i as u32)
        .collect()
}

pub fn mounts(text: &str, devices: &[BlockDevice], configs: &[MountConfig]) -> Vec<Mount> {
    let by_dev: BTreeMap<&str, &str> = devices
        .iter()
        .map(|d| (d.dev.as_str(), d.name.as_str()))
        .collect();
    let mut by_point: BTreeMap<String, Mount> = BTreeMap::new();
    for m in parse::mountinfo_full(text) {
        if PSEUDO_FS.contains(&m.fs_type.as_str()) {
            continue;
        }
        let class = if NETWORK_FS.contains(&m.fs_type.as_str()) {
            "network"
        } else if m.fs_type.starts_with("fuse") {
            "virtual"
        } else {
            "local"
        };
        let device = by_dev.get(m.dev.as_str()).map(|s| s.to_string());
        let cfg = configs_for(configs, &m.mount_point);
        let reference = cfg
            .first()
            .map(|&i| configs[i as usize].reference.clone())
            .unwrap_or_else(|| reference(&m.source, &m.fs_type).into());
        by_point.insert(
            m.mount_point.clone(),
            Mount {
                source: m.source,
                device,
                dev: m.dev,
                fs_type: m.fs_type,
                class: class.into(),
                options: m.options,
                read_only: m.read_only,
                reference,
                configs: cfg,
                mount_point: m.mount_point,
            },
        );
    }
    by_point.into_values().collect()
}

pub fn swaps(text: &str, configs: &[MountConfig]) -> Vec<SwapArea> {
    let mut out = Vec::new();
    for line in text.lines().skip(1) {
        let f: Vec<&str> = line.split_whitespace().collect();
        if f.len() < 5 {
            continue;
        }
        let name = parse::unescape(f[0]);
        let dev = name.strip_prefix("/dev/").unwrap_or("");
        let cfg = configs
            .iter()
            .enumerate()
            .filter(|(_, c)| {
                c.fs_type == "swap"
                    && (c.spec == name || c.resolves_to.as_deref().is_some_and(|r| r == dev))
            })
            .map(|(i, _)| i as u32)
            .collect();
        out.push(SwapArea {
            kind: if f[1] == "partition" {
                "partition"
            } else {
                "file"
            }
            .into(),
            size_bytes: f[2].parse::<u64>().unwrap_or(0).saturating_mul(1024),
            priority: f[4].parse().unwrap_or(0),
            configs: cfg,
            name,
        });
    }
    out
}

pub fn problems(
    configs: &[MountConfig],
    mounts: &[Mount],
    swaps: &[SwapArea],
    devices: &[BlockDevice],
) -> Vec<MountProblem> {
    let mut out = Vec::new();
    let mut points: BTreeMap<&str, Vec<u32>> = BTreeMap::new();
    for (i, c) in configs.iter().enumerate() {
        if c.fs_type != "swap" && c.mount_point != "none" {
            points.entry(&c.mount_point).or_default().push(i as u32);
        }
    }
    for (p, ix) in &points {
        if ix.len() > 1 {
            out.push(MountProblem {
                kind: "duplicateMountPoint".into(),
                mount_point: p.to_string(),
                configs: ix.clone(),
            });
        }
    }
    let mounted: BTreeSet<&str> = mounts.iter().map(|m| m.mount_point.as_str()).collect();
    for (i, c) in configs.iter().enumerate() {
        let names_device = split_spec(&c.spec).is_some();
        if names_device && c.resolves_to.is_none() {
            out.push(MountProblem {
                kind: "missingDevice".into(),
                mount_point: c.mount_point.clone(),
                configs: vec![i as u32],
            });
        }
        if c.boot == "auto" && c.fs_type != "swap" && !mounted.contains(c.mount_point.as_str()) {
            out.push(MountProblem {
                kind: "notMounted".into(),
                mount_point: c.mount_point.clone(),
                configs: vec![i as u32],
            });
        }
    }
    // A kernel name can point at another disk after a re-plug or reboot.
    // Device-mapper and RAID names are stable and loop devices are files.
    let kernel_named = |name: &Option<String>| {
        name.as_ref()
            .and_then(|n| devices.iter().find(|d| &d.name == n))
            .is_some_and(|d| d.class == "physical" || d.class == "virtual")
    };
    for m in mounts {
        if m.class == "local" && m.reference == reference_kind::DEVICE && kernel_named(&m.device) {
            out.push(MountProblem {
                kind: "deviceName".into(),
                mount_point: m.mount_point.clone(),
                configs: m.configs.clone(),
            });
        }
    }
    for s in swaps {
        if s.kind == "partition" {
            let dev = s.name.strip_prefix("/dev/").map(str::to_string);
            let by_name = s.configs.is_empty()
                || s.configs
                    .iter()
                    .any(|&i| configs[i as usize].reference == reference_kind::DEVICE);
            if by_name && kernel_named(&dev) {
                out.push(MountProblem {
                    kind: "deviceName".into(),
                    mount_point: s.name.clone(),
                    configs: s.configs.clone(),
                });
            }
        }
    }
    out
}

fn udev_props(text: &str) -> BTreeMap<String, String> {
    text.lines()
        .filter_map(|l| l.strip_prefix("E:")?.split_once('='))
        .map(|(k, v)| (k.to_string(), v.to_string()))
        .collect()
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
