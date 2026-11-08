//! Host change ledger at the socket boundary (spec 55.1, UNX-001, T-250):
//! host-changing actions are recorded before and after they run, and are
//! refused, with a security event, when they cannot be recorded.

mod common;

use common::{FakeExecutor, Harness, LedgerSetup, current_uid};
use hazelminx_privileged::action::{
    Action, PERM_HOST_INFO_READ, PERM_HOST_LEDGER_READ, PERM_HOST_SERVICES_MANAGE,
};
use hazelminx_privileged::audit::SecurityEvent;
use hazelminx_privileged::client::ClientError;
use hazelminx_privileged::command::{OutputSink, RecordingRunner};
use hazelminx_privileged::executor::{Executor, HostExecutor};
use hazelminx_privileged::ledger::entry::{ChangeKind, Outcome, Ownership, Phase, Target};
use hazelminx_privileged::ledger::sync::LedgerBatch;
use hazelminx_privileged::params::UnitName;
use hazelminx_privileged::protocol::ErrorCode;
use hazelminx_privileged::server::{
    REFUSE_BEFORE_STATE, REFUSE_NO_DESCRIPTION, REFUSE_UNAVAILABLE,
};
use std::sync::Arc;
use std::sync::atomic::Ordering;

fn restart(unit: &str) -> Action {
    Action::RestartService {
        unit: UnitName::try_from(unit.to_string()).unwrap(),
    }
}

fn code(e: ClientError) -> ErrorCode {
    e.code()
        .unwrap_or_else(|| panic!("expected rejection, got {e}"))
}

#[tokio::test]
async fn a_host_change_is_recorded_before_and_after_with_its_job() {
    let h = Harness::start(FakeExecutor::default(), current_uid(), 2).await;
    assert!(h.client.health().await.unwrap().ledger_ready);
    let g = h.grant("g-ops", &[PERM_HOST_SERVICES_MANAGE]);
    h.client
        .execute_for_job(
            "req-1",
            Some("job-42"),
            &g,
            &restart("nginx.service"),
            |_, _| {},
        )
        .await
        .unwrap();
    assert_eq!(h.executor.started.load(Ordering::SeqCst), 1);
    assert_eq!(h.probe.calls.load(Ordering::SeqCst), 2, "before and after");

    let ledger = h.ledger.as_ref().unwrap();
    let batch = ledger.export(0, 10, None).unwrap();
    batch.verify(None).unwrap();
    let e = batch.entries().unwrap();
    assert_eq!(e.len(), 2);
    let (intent, result) = (&e[0], &e[1]);
    assert_eq!(intent.phase, Phase::Intent);
    assert_eq!(result.phase, Phase::Result);
    assert_eq!(result.intent_seq, Some(intent.seq));
    assert_eq!(intent.change_id, result.change_id);
    for x in [intent, result] {
        assert_eq!(x.job_id.as_deref(), Some("job-42"));
        assert_eq!(x.request_id.as_deref(), Some("req-1"));
        assert_eq!(x.grant_id.as_deref(), Some("g-ops"));
        assert_eq!(x.subject.as_deref(), Some("user-1"));
        assert_eq!(x.action, "restart_service");
        assert_eq!(x.change, ChangeKind::Restart);
        assert_eq!(
            x.target,
            Target::SystemdUnit {
                unit: UnitName::try_from("nginx.service".to_string()).unwrap()
            }
        );
    }
    assert!(intent.after.is_none() && intent.outcome.is_none());
    assert_eq!(result.outcome, Some(Outcome::Succeeded));
    assert_eq!(result.after.as_ref(), Some(&result.before));
    // Restarting a pre-existing unit that keeps running does not make it ours.
    assert_eq!(result.ownership, Ownership::Existing);
}

#[tokio::test]
async fn a_failed_change_is_recorded_as_failed() {
    let h = Harness::start(
        FakeExecutor {
            fail_with: Some("Job for nginx.service failed".into()),
            ..Default::default()
        },
        current_uid(),
        2,
    )
    .await;
    let g = h.grant("g-ops", &[PERM_HOST_SERVICES_MANAGE]);
    let e = h
        .client
        .execute("r1", &g, &restart("nginx.service"), |_, _| {})
        .await
        .unwrap_err();
    assert_eq!(code(e), ErrorCode::ActionFailed);
    let entries = h
        .ledger
        .as_ref()
        .unwrap()
        .export(0, 10, None)
        .unwrap()
        .entries()
        .unwrap();
    assert_eq!(
        entries[1].outcome,
        Some(Outcome::Failed {
            error: "Job for nginx.service failed".into()
        })
    );
}

#[tokio::test]
async fn read_only_actions_write_nothing() {
    let h = Harness::start(FakeExecutor::default(), current_uid(), 2).await;
    let g = h.grant("g-ro", &[PERM_HOST_INFO_READ]);
    h.client
        .execute("r1", &g, &Action::SystemInfo {}, |_, _| {})
        .await
        .unwrap();
    assert_eq!(h.ledger.as_ref().unwrap().head().seq, 0);
    assert_eq!(h.probe.calls.load(Ordering::SeqCst), 0);
}

/// T-250: an executor that would change the host but has no ledger
/// description for the change.
#[tokio::test]
async fn t250_change_without_a_ledger_description_is_refused_and_audited() {
    let h = Harness::start(
        FakeExecutor {
            no_ledger_description: true,
            ..Default::default()
        },
        current_uid(),
        2,
    )
    .await;
    let g = h.grant("g-ops", &[PERM_HOST_SERVICES_MANAGE]);
    let e = h
        .client
        .execute("r1", &g, &restart("nginx.service"), |_, _| {})
        .await
        .unwrap_err();
    assert_eq!(code(e), ErrorCode::LedgerRefused);
    assert_eq!(h.executor.started.load(Ordering::SeqCst), 0, "nothing ran");
    assert_eq!(h.ledger.as_ref().unwrap().head().seq, 0);
    let events = h.log.events();
    assert!(
        events.iter().any(|e| matches!(e,
            SecurityEvent::LedgerRefused { grant_id, action, reason }
                if grant_id == "g-ops" && action == "restart_service" && reason == REFUSE_NO_DESCRIPTION)),
        "{events:?}"
    );
}

/// T-250: the ledger cannot be written (failed integrity check at start).
#[tokio::test]
async fn t250_change_while_the_ledger_is_unavailable_is_refused_and_audited() {
    let h = Harness::start_with(
        FakeExecutor::default(),
        current_uid(),
        2,
        LedgerSetup {
            unavailable: true,
            ..Default::default()
        },
    )
    .await;
    assert!(!h.client.health().await.unwrap().ledger_ready);
    let g = h.grant("g-ops", &[PERM_HOST_SERVICES_MANAGE, PERM_HOST_INFO_READ]);
    let e = h
        .client
        .execute("r1", &g, &restart("nginx.service"), |_, _| {})
        .await
        .unwrap_err();
    assert_eq!(code(e), ErrorCode::LedgerRefused);
    assert_eq!(h.executor.started.load(Ordering::SeqCst), 0);
    assert!(h.log.events().iter().any(|e| matches!(e,
        SecurityEvent::LedgerRefused { reason, .. } if reason == REFUSE_UNAVAILABLE)));
    // Reading the host still works, so the operator can diagnose.
    h.client
        .execute("r2", &g, &Action::SystemInfo {}, |_, _| {})
        .await
        .unwrap();
}

/// T-250: the state before the change cannot be read, so it could not be
/// undone; the change does not run and nothing is half-recorded.
#[tokio::test]
async fn t250_change_with_an_unreadable_before_state_is_refused_and_audited() {
    let h = Harness::start_with(
        FakeExecutor::default(),
        current_uid(),
        2,
        LedgerSetup {
            probe_fails: true,
            ..Default::default()
        },
    )
    .await;
    let g = h.grant("g-ops", &[PERM_HOST_SERVICES_MANAGE]);
    let e = h
        .client
        .execute("r1", &g, &restart("nginx.service"), |_, _| {})
        .await
        .unwrap_err();
    assert_eq!(code(e), ErrorCode::LedgerRefused);
    assert_eq!(h.executor.started.load(Ordering::SeqCst), 0);
    assert_eq!(h.ledger.as_ref().unwrap().head().seq, 0);
    // The probe error names the unit; the security event must not.
    assert!(
        !format!("{:?}", h.log.events()).contains("nginx.service"),
        "{:?}",
        h.log.events()
    );
    assert!(h.log.events().iter().any(|e| matches!(e,
        SecurityEvent::LedgerRefused { reason, .. } if reason == REFUSE_BEFORE_STATE)));
    // The slot was released.
    assert_eq!(h.client.health().await.unwrap().active_actions, 0);
}

#[tokio::test]
async fn job_id_must_be_a_token() {
    let h = Harness::start(FakeExecutor::default(), current_uid(), 2).await;
    let g = h.grant("g-ops", &[PERM_HOST_SERVICES_MANAGE]);
    let e = h
        .client
        .execute_for_job(
            "r1",
            Some("job 1; id"),
            &g,
            &restart("a.service"),
            |_, _| {},
        )
        .await
        .unwrap_err();
    assert_eq!(code(e), ErrorCode::InvalidParam);
    assert_eq!(h.executor.started.load(Ordering::SeqCst), 0);
}

#[tokio::test]
async fn ledger_export_pages_raw_lines_for_core() {
    let h = Harness::start(FakeExecutor::default(), current_uid(), 2).await;
    let g = h.grant("g-ops", &[PERM_HOST_SERVICES_MANAGE]);
    for u in ["a.service", "b.service"] {
        h.client
            .execute("r", &g, &restart(u), |_, _| {})
            .await
            .unwrap();
    }
    let ledger = h.ledger.clone().unwrap();
    let exec = HostExecutor::new(
        Arc::new(RecordingRunner::default()),
        "/",
        Some(ledger.clone()),
    );
    let page = |after_seq, limit| {
        exec.execute(
            Action::LedgerExport {
                after_seq,
                limit: Some(limit),
                max_bytes: None,
            },
            OutputSink::discard(),
        )
    };
    let first: LedgerBatch = serde_json::from_value(page(0, 3).await.unwrap()).unwrap();
    assert!(first.more);
    let head = first.verify(None).unwrap();
    let rest: LedgerBatch = serde_json::from_value(page(head.seq, 3).await.unwrap()).unwrap();
    assert!(!rest.more);
    assert_eq!(rest.verify(Some(&head)).unwrap(), ledger.head());
    assert!(page(99, 1).await.is_err());
    assert_eq!(
        Action::LedgerExport {
            after_seq: 0,
            limit: None,
            max_bytes: None,
        }
        .required_permission(),
        PERM_HOST_LEDGER_READ
    );
    let none = HostExecutor::new(Arc::new(RecordingRunner::default()), "/", None);
    assert!(
        none.execute(
            Action::LedgerExport {
                after_seq: 0,
                limit: None,
                max_bytes: None,
            },
            OutputSink::discard()
        )
        .await
        .is_err()
    );
}
