//! End to end through the real executable: config from flags, issuer key
//! from a PEM file, a signed `system_info` action against the real host
//! executor, revocation, and SIGTERM shutdown that removes the socket.

use ed25519_dalek::SigningKey;
use ed25519_dalek::pkcs8::EncodePublicKey;
use hazelminx_privileged::action::{Action, PERM_HOST_INFO_READ};
use hazelminx_privileged::client::Client;
use hazelminx_privileged::grant::Grant;
use hazelminx_privileged::protocol::ErrorCode;
use std::os::unix::fs::MetadataExt;
use std::process::Stdio;
use std::time::Duration;
use time::OffsetDateTime;

#[tokio::test]
async fn binary_serves_signed_actions_and_shuts_down_on_sigterm() {
    let dir = tempfile::tempdir().unwrap();
    let sock = dir.path().join("p.sock");
    let issuer = SigningKey::generate(&mut rand::rngs::OsRng);
    let pem = issuer
        .verifying_key()
        .to_public_key_pem(ed25519_dalek::pkcs8::spki::der::pem::LineEnding::LF)
        .unwrap();
    let key_path = dir.path().join("issuer.pub");
    std::fs::write(&key_path, pem).unwrap();
    let uid = std::fs::metadata(&key_path).unwrap().uid();

    let mut child = tokio::process::Command::new(env!("CARGO_BIN_EXE_hazelminx-privileged"))
        .arg("--config")
        .arg(dir.path().join("missing.toml"))
        .arg("--socket")
        .arg(&sock)
        .arg("--issuer-key")
        .arg(&key_path)
        .arg("--allowed-uid")
        .arg(uid.to_string())
        .arg("--log-format")
        .arg("text")
        .arg("run")
        .stdout(Stdio::piped())
        .stderr(Stdio::inherit())
        .kill_on_drop(true)
        .spawn()
        .unwrap();

    tokio::time::timeout(Duration::from_secs(10), async {
        while !sock.exists() {
            tokio::time::sleep(Duration::from_millis(20)).await;
        }
    })
    .await
    .expect("socket did not appear");
    assert_eq!(std::fs::metadata(&sock).unwrap().mode() & 0o777, 0o660);

    let client = Client::new(&sock);
    let hr = client.health().await.unwrap();
    assert_eq!(hr.protocol_version, 1);

    let mut g = Grant::new(
        "bin-1",
        "user-1",
        vec![PERM_HOST_INFO_READ.into()],
        OffsetDateTime::now_utc(),
        time::Duration::minutes(1),
    );
    g.sign(&issuer);
    let v = client
        .execute("r1", &g, &Action::SystemInfo {}, |_, _| {})
        .await
        .unwrap();
    // Real host files: present on Linux, null on other hosts. Either way the
    // action ran and the result has the documented shape.
    assert!(
        v.get("hostname").is_some() && v.get("kernel").is_some(),
        "{v}"
    );

    client.revoke("bin-1").await.unwrap();
    let e = client
        .execute("r2", &g, &Action::SystemInfo {}, |_, _| {})
        .await
        .unwrap_err();
    assert_eq!(e.code(), Some(ErrorCode::GrantInvalid));

    let pid = child.id().unwrap();
    let status = std::process::Command::new("kill")
        .arg("-TERM")
        .arg(pid.to_string())
        .status()
        .unwrap();
    assert!(status.success());
    let exit = tokio::time::timeout(Duration::from_secs(10), child.wait())
        .await
        .expect("did not exit on SIGTERM")
        .unwrap();
    assert!(exit.success(), "{exit:?}");
    assert!(!sock.exists(), "socket file left behind");

    // tracing-subscriber writes to stdout (journald captures both streams).
    let mut logs = String::new();
    use tokio::io::AsyncReadExt;
    child
        .stdout
        .take()
        .unwrap()
        .read_to_string(&mut logs)
        .await
        .unwrap();
    assert!(logs.contains("privileged executor listening"), "{logs}");
    assert!(logs.contains("grant_revoked"), "{logs}");
    assert!(logs.contains("shutting down"), "{logs}");
    assert!(!logs.contains(&g.signature), "signature in logs");
}
