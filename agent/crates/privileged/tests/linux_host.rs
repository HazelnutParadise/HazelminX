//! Real-host checks for the host change ledger (ticket 113). They change the
//! host (units, users, files under /etc and /var/lib), so they are ignored by
//! default and refuse to run unless the host is marked disposable:
//!
//! ```text
//! # as root, on a throwaway Linux host or container booted with systemd
//! HAZELMINX_DISPOSABLE_HOST=1 cargo test -p hazelminx-privileged \
//!     --test linux_host -- --ignored --test-threads=1
//! ```
#![cfg(target_os = "linux")]

use ed25519_dalek::SigningKey;
use ed25519_dalek::pkcs8::EncodePublicKey;
use hazelminx_privileged::action::{Action, PERM_HOST_LEDGER_READ, PERM_HOST_SERVICES_MANAGE};
use hazelminx_privileged::client::Client;
use hazelminx_privileged::command::HostRunner;
use hazelminx_privileged::grant::Grant;
use hazelminx_privileged::ledger::entry::{
    ChangeKind, Ownership, Phase, Target, TargetState,
};
use hazelminx_privileged::ledger::probe::{Capture, HostProbe, Probe};
use hazelminx_privileged::ledger::recorder::{ChangeContext, Declared, Recorder};
use hazelminx_privileged::ledger::store::{Ledger, Security};
use hazelminx_privileged::ledger::sync::LedgerBatch;
use hazelminx_privileged::params::{HostPath, PackageName, UnitName, UserName};
use hazelminx_privileged::protocol::ErrorCode;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::sync::Arc;
use std::time::Duration;
use time::OffsetDateTime;

const BIN: &str = env!("CARGO_BIN_EXE_hazelminx-privileged");
const UNIT: &str = "hzx-e2e.service";

fn require_disposable_root_host() {
    assert_eq!(
        std::env::var("HAZELMINX_DISPOSABLE_HOST").as_deref(),
        Ok("1"),
        "these tests change the host; set HAZELMINX_DISPOSABLE_HOST=1 only on a throwaway host"
    );
    let uid = String::from_utf8(Command::new("id").arg("-u").output().unwrap().stdout).unwrap();
    assert_eq!(uid.trim(), "0", "run as root");
    assert!(
        Path::new("/run/systemd/system").exists(),
        "host must be booted with systemd"
    );
}

fn sh(cmd: &str) -> std::process::Output {
    let out = Command::new("/bin/sh").arg("-c").arg(cmd).output().unwrap();
    assert!(
        out.status.success(),
        "{cmd}: {}",
        String::from_utf8_lossy(&out.stderr)
    );
    out
}

fn fresh_root_dir(name: &str) -> PathBuf {
    let base = PathBuf::from("/var/lib").join(name);
    let _ = std::fs::remove_dir_all(&base);
    std::fs::create_dir(&base).unwrap();
    base
}

fn grant(key: &SigningKey, id: &str, perms: &[&str]) -> Grant {
    let mut g = Grant::new(
        id,
        "e2e",
        perms.iter().map(|s| s.to_string()).collect(),
        OffsetDateTime::now_utc(),
        time::Duration::minutes(10),
    );
    g.sign(key);
    g
}

struct Executor {
    child: std::process::Child,
    client: Client,
    log: PathBuf,
}

impl Executor {
    fn start(base: &Path, ledger: &Path, key: &SigningKey, tag: &str) -> Executor {
        let sock = base.join(format!("{tag}.sock"));
        let key_path = base.join("issuer.pub");
        let pem = key
            .verifying_key()
            .to_public_key_pem(ed25519_dalek::pkcs8::spki::der::pem::LineEnding::LF)
            .unwrap();
        std::fs::write(&key_path, pem).unwrap();
        let log = base.join(format!("{tag}.log"));
        let child = Command::new(BIN)
            .args(["--config", "/nonexistent.toml", "--allowed-uid", "0"])
            .arg("--socket")
            .arg(&sock)
            .arg("--issuer-key")
            .arg(&key_path)
            .arg("--ledger-dir")
            .arg(ledger)
            .arg("run")
            .stderr(Stdio::null())
            .stdout(std::fs::File::create(&log).unwrap())
            .spawn()
            .unwrap();
        for _ in 0..200 {
            if sock.exists() {
                break;
            }
            std::thread::sleep(Duration::from_millis(25));
        }
        assert!(sock.exists(), "socket did not appear");
        Executor {
            child,
            client: Client::new(&sock),
            log,
        }
    }

    fn stop(mut self) -> String {
        let _ = Command::new("kill")
            .arg(self.child.id().to_string())
            .status();
        let _ = self.child.wait();
        std::fs::read_to_string(&self.log).unwrap()
    }
}

fn unit_state(s: &TargetState) -> (&str, &str) {
    let TargetState::SystemdUnit(u) = s else {
        panic!("{s:?}")
    };
    (
        u.active_state.as_deref().unwrap_or(""),
        u.unit_file_state.as_deref().unwrap_or(""),
    )
}

/// UNX-001 / T-250 / UNX-002 through the real executable on a real host.
#[tokio::test]
#[ignore = "changes the host; see the module docs"]
async fn executor_records_refuses_and_snapshots_on_a_real_host() {
    require_disposable_root_host();
    let base = fresh_root_dir("hazelminx-e2e");
    let ledger_dir = base.join("ledger");
    // A pre-existing, stopped unit for the platform to restart.
    std::fs::write(
        format!("/etc/systemd/system/{UNIT}"),
        "[Unit]\nDescription=hazelminx e2e\n[Service]\nExecStart=/bin/sleep infinity\n",
    )
    .unwrap();
    sh("systemctl daemon-reload && systemctl stop hzx-e2e.service || true");

    // UNX-002: snapshot before anything else.
    let out = Command::new(BIN)
        .arg("--ledger-dir")
        .arg(&ledger_dir)
        .args(["snapshot", "capture", "--path", "/etc/hzx-e2e"])
        .output()
        .unwrap();
    assert!(out.status.success(), "{}", String::from_utf8_lossy(&out.stderr));
    let snap = ledger_dir.join("preinstall-snapshot.json");
    let text = std::fs::read_to_string(&snap).unwrap();
    for section in ["packages", "enabled_units", "listening", "users", "groups", "firewall"] {
        let v: serde_json::Value = serde_json::from_str(&text).unwrap();
        assert_eq!(v[section]["status"], "captured", "{section}: {}", v[section]);
    }
    let again = Command::new(BIN)
        .arg("--ledger-dir")
        .arg(&ledger_dir)
        .args(["snapshot", "capture"])
        .output()
        .unwrap();
    assert!(!again.status.success(), "snapshot is taken once");

    // UNX-001: a real restart is recorded before and after.
    let key = SigningKey::generate(&mut rand::rngs::OsRng);
    let ex = Executor::start(&base, &ledger_dir, &key, "a");
    assert!(ex.client.health().await.unwrap().ledger_ready);
    let g = grant(&key, "g-e2e", &[PERM_HOST_SERVICES_MANAGE, PERM_HOST_LEDGER_READ]);
    let unit = UnitName::try_from(UNIT.to_string()).unwrap();
    let r = ex
        .client
        .execute_for_job(
            "req-e2e-1",
            Some("job-e2e-1"),
            &g,
            &Action::RestartService { unit: unit.clone() },
            |_, _| {},
        )
        .await
        .unwrap();
    assert_eq!(r["active_state"], "active");
    let batch: LedgerBatch = serde_json::from_value(
        ex.client
            .execute(
                "req-e2e-2",
                &g,
                &Action::LedgerExport {
                    after_seq: 0,
                    limit: None,
                },
                |_, _| {},
            )
            .await
            .unwrap(),
    )
    .unwrap();
    batch.verify(None).unwrap();
    let e = batch.entries().unwrap();
    assert_eq!(e.len(), 2, "{e:#?}");
    assert_eq!((e[0].phase, e[1].phase), (Phase::Intent, Phase::Result));
    assert_eq!(e[0].target, Target::SystemdUnit { unit });
    assert_eq!(e[1].job_id.as_deref(), Some("job-e2e-1"));
    assert_eq!(unit_state(&e[0].before).0, "inactive");
    assert_eq!(unit_state(e[1].after.as_ref().unwrap()).0, "active");
    assert_eq!(e[1].ownership, Ownership::PlatformModified);
    let log = ex.stop();
    assert!(log.contains("action_executed"), "{log}");

    // Root-only: another account can neither read nor append.
    for (p, m) in [(&ledger_dir, 0o700), (&ledger_dir.join("ledger.jsonl"), 0o600)] {
        use std::os::unix::fs::MetadataExt;
        let md = std::fs::metadata(p).unwrap();
        assert_eq!((md.uid(), md.mode() & 0o777), (0, m), "{}", p.display());
    }
    let ledger_file = ledger_dir.join("ledger.jsonl");
    for cmd in [
        format!("cat {}", ledger_file.display()),
        format!("echo x >> {}", ledger_file.display()),
    ] {
        let out = Command::new("setpriv")
            .args(["--reuid=65534", "--regid=65534", "--clear-groups", "/bin/sh", "-c"])
            .arg(&cmd)
            .output()
            .unwrap();
        assert!(!out.status.success(), "nobody could run: {cmd}");
    }

    // T-250: a ledger that fails its check makes every host change refused,
    // and the refusal is in the security log.
    let original = std::fs::read_to_string(&ledger_file).unwrap();
    std::fs::write(&ledger_file, original.replacen("\"inactive\"", "\"active\"", 1)).unwrap();
    let verify = Command::new(BIN)
        .arg("--ledger-dir")
        .arg(&ledger_dir)
        .args(["ledger", "verify"])
        .output()
        .unwrap();
    assert!(!verify.status.success());
    assert!(String::from_utf8_lossy(&verify.stderr).contains("integrity"));
    sh("systemctl stop hzx-e2e.service");
    let ex = Executor::start(&base, &ledger_dir, &key, "b");
    assert!(!ex.client.health().await.unwrap().ledger_ready);
    let err = ex
        .client
        .execute(
            "req-e2e-3",
            &g,
            &Action::RestartService {
                unit: UnitName::try_from(UNIT.to_string()).unwrap(),
            },
            |_, _| {},
        )
        .await
        .unwrap_err();
    assert_eq!(err.code(), Some(ErrorCode::LedgerRefused));
    let log = ex.stop();
    assert!(log.contains("ledger_refused"), "{log}");
    let state = sh("systemctl is-active hzx-e2e.service || true");
    assert_eq!(String::from_utf8_lossy(&state.stdout).trim(), "inactive", "nothing ran");
    std::fs::write(&ledger_file, &original).unwrap();

    // UNX-002: compare after changes, then after undoing them.
    sh("useradd --system --no-create-home hzx-e2e-user && mkdir -p /etc/hzx-e2e");
    let cmp = Command::new(BIN)
        .arg("--ledger-dir")
        .arg(&ledger_dir)
        .args(["snapshot", "compare", "--path", "/etc/hzx-e2e"])
        .output()
        .unwrap();
    assert_eq!(cmp.status.code(), Some(3));
    let v: serde_json::Value = serde_json::from_slice(&cmp.stdout).unwrap();
    assert_eq!(v["identical"], false);
    assert_eq!(v["sections"]["users"]["added"], serde_json::json!(["hzx-e2e-user"]));
    assert_eq!(
        v["sections"]["platform_paths"]["added"],
        serde_json::json!(["/etc/hzx-e2e"])
    );
    sh("userdel hzx-e2e-user && rmdir /etc/hzx-e2e");
    let cmp = Command::new(BIN)
        .arg("--ledger-dir")
        .arg(&ledger_dir)
        .args(["snapshot", "compare", "--path", "/etc/hzx-e2e"])
        .output()
        .unwrap();
    assert_eq!(
        cmp.status.code(),
        Some(0),
        "{}",
        String::from_utf8_lossy(&cmp.stdout)
    );

    sh(&format!(
        "systemctl stop {UNIT}; rm -f /etc/systemd/system/{UNIT}; systemctl daemon-reload"
    ));
    std::fs::remove_dir_all(&base).unwrap();
}

/// Real probes (dpkg, systemd, /etc) and drift detection on real files.
#[tokio::test]
#[ignore = "changes the host; see the module docs"]
async fn real_probes_and_user_edit_detection() {
    require_disposable_root_host();
    let base = fresh_root_dir("hazelminx-e2e-drift");
    let (ledger, _) = Ledger::open(&base.join("ledger"), &Security::root()).unwrap();
    let ledger = Arc::new(ledger);
    let probe = Arc::new(HostProbe::new(
        Arc::new(HostRunner),
        "/",
        Some(ledger.backup_dir()),
    ));

    let pkg = |n: &str| Target::Package {
        name: PackageName::try_from(n.to_string()).unwrap(),
    };
    let TargetState::Package(p) = probe.probe(&pkg("systemd"), Capture::Before).await.unwrap()
    else {
        panic!()
    };
    assert!(p.installed && p.version.is_some(), "{p:?}");
    let TargetState::Package(p) = probe
        .probe(&pkg("hzx-no-such-package"), Capture::Before)
        .await
        .unwrap()
    else {
        panic!()
    };
    assert!(!p.installed);
    let TargetState::User(u) = probe
        .probe(
            &Target::User {
                name: UserName::try_from("root".to_string()).unwrap(),
            },
            Capture::Before,
        )
        .await
        .unwrap()
    else {
        panic!()
    };
    assert_eq!(u.uid, Some(0));
    let TargetState::SystemdUnit(s) = probe
        .probe(
            &Target::SystemdUnit {
                unit: UnitName::try_from("hzx-no-such.service".to_string()).unwrap(),
            },
            Capture::Before,
        )
        .await
        .unwrap()
    else {
        panic!()
    };
    assert!(!s.exists);

    // The platform writes a drop-in; the user edits it afterwards.
    let dir = Path::new("/etc/hzx-e2e-drift");
    let _ = std::fs::remove_dir_all(dir);
    std::fs::create_dir(dir).unwrap();
    let file = dir.join("50-hazelminx.conf");
    std::fs::write(&file, "PasswordAuthentication yes\n").unwrap();
    let rec = Recorder::new(Ok(ledger.clone()), probe.clone());
    let target = Target::Path {
        path: HostPath::try_from(file.display().to_string()).unwrap(),
    };
    let pending = rec
        .begin(
            &ChangeContext {
                job_id: Some("job-e2e-drift".into()),
                action: "write_sshd_dropin".into(),
                ..Default::default()
            },
            &[Declared {
                target: target.clone(),
                change: ChangeKind::Modify,
            }],
        )
        .await
        .unwrap();
    // The original content is backed up before the change.
    let TargetState::Path(before) = &pending.intents()[0].before else {
        panic!()
    };
    let kept = ledger.backup_dir().join(before.sha256.as_ref().unwrap());
    assert_eq!(std::fs::read(&kept).unwrap(), b"PasswordAuthentication yes\n");
    std::fs::write(&file, "PasswordAuthentication no\n").unwrap();
    let results = rec.finish(pending, &Ok(())).await.unwrap();
    assert_eq!(results[0].ownership, Ownership::PlatformModified);
    drop(rec);
    drop(ledger);

    let drift = || {
        let out = Command::new(BIN)
            .arg("--ledger-dir")
            .arg(base.join("ledger"))
            .args(["ledger", "drift"])
            .output()
            .unwrap();
        assert!(out.status.success(), "{}", String::from_utf8_lossy(&out.stderr));
        serde_json::from_slice::<serde_json::Value>(&out.stdout).unwrap()
    };
    assert_eq!(drift()["modified"], serde_json::json!([]));
    sh(&format!("echo 'PermitRootLogin yes' >> {}", file.display()));
    let v = drift();
    assert_eq!(v["modified"].as_array().unwrap().len(), 1, "{v}");
    assert_eq!(v["modified"][0]["differences"], serde_json::json!(["content"]));
    assert_eq!(v["modified"][0]["job_id"], "job-e2e-drift");

    std::fs::remove_dir_all(dir).unwrap();
    std::fs::remove_dir_all(&base).unwrap();
}
