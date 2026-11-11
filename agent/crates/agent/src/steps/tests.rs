//! Tests for the step decisions (written against the expected results in mod.rs).
//!
//! Every test drives `Steps` through its public methods and checks the journal
//! through `Journal`'s public accessors, so an empty slot fails every test.

use super::*;
use crate::actions;
use hazelminx_protocol::EventKind;
use serde_json::json;
use std::path::Path;
use time::Duration;
use time::macros::datetime;

const NODE: &str = "node-fp-test";

fn now() -> OffsetDateTime {
    datetime!(2026-11-08 03:00:00 UTC)
}

/// A finish time one second after `now()`.
fn later() -> OffsetDateTime {
    now() + Duration::seconds(1)
}

/// A `step_execute` request, with every field editable before `msg()`.
#[derive(Clone)]
struct Req {
    request_id: String,
    job_id: String,
    step: u32,
    action: String,
    action_version: u32,
    params: Map<String, Value>,
    target_node: String,
    control_generation: u64,
    expires_at: OffsetDateTime,
    retryable: bool,
}

impl Req {
    /// A request that passes every check: this node, not expired, generation 3,
    /// `agent.info` v1 with no params.
    fn new(job_id: &str, step: u32) -> Req {
        Req {
            request_id: format!("stp_{job_id}_{step}_1"),
            job_id: job_id.to_string(),
            step,
            action: "agent.info".to_string(),
            action_version: 1,
            params: Map::new(),
            target_node: NODE.to_string(),
            control_generation: 3,
            expires_at: now() + Duration::minutes(5),
            retryable: false,
        }
    }

    fn msg(&self) -> Message {
        Message::StepExecute {
            request_id: self.request_id.clone(),
            job_id: self.job_id.clone(),
            step: self.step,
            step_name: "collect agent info".to_string(),
            action: self.action.clone(),
            action_version: self.action_version,
            params: self.params.clone(),
            target_node: self.target_node.clone(),
            control_generation: self.control_generation,
            expires_at: self.expires_at,
            retryable: self.retryable,
            irreversible: false,
        }
    }

    fn expired(mut self) -> Req {
        self.expires_at = now() - Duration::minutes(1);
        self
    }

    fn stale(mut self) -> Req {
        self.control_generation = 1;
        self
    }
}

fn reconcile_msg(job_id: &str, step: u32, target_node: &str) -> Message {
    Message::StepReconcile {
        request_id: format!("rec_{job_id}_{step}_2"),
        job_id: job_id.to_string(),
        step,
        target_node: target_node.to_string(),
    }
}

fn steps_in(dir: &Path) -> Steps {
    let journal = Journal::open(dir).expect("journal opens");
    Steps::new(
        journal,
        NODE.to_string(),
        actions::specs(),
        actions::capabilities(),
    )
}

fn decide(steps: &mut Steps, msg: &Message, at: OffsetDateTime) -> Decision {
    steps.decide(msg, at).expect("decide returns Ok")
}

/// Decides `req` at `now()` and expects `Run`.
fn run(steps: &mut Steps, req: &Req) -> (RunTicket, Record) {
    match decide(steps, &req.msg(), now()) {
        Decision::Run { ticket, started } => (ticket, started),
        other => panic!("{}: expected Run, got {other:?}", req.request_id),
    }
}

fn finish_ok(steps: &mut Steps, ticket: &RunTicket) -> Record {
    steps
        .finish(ticket, Ok(json!({ "agentVersion": "0.1.0" })), later())
        .expect("finish journals the outcome")
}

fn finish_err(steps: &mut Steps, ticket: &RunTicket) -> Record {
    let failure = StepFailure {
        code: "ACTION_FAILED".to_string(),
        summary: "hostname lookup failed".to_string(),
        retryable: false,
    };
    steps
        .finish(ticket, Err(failure), later())
        .expect("finish journals the outcome")
}

/// Starts `req`, then drops the Steps without finishing it, like an agent
/// that restarted mid-step. Returns a new Steps on the same journal directory.
fn start_then_restart(dir: &Path, req: &Req) -> Steps {
    {
        let mut before = steps_in(dir);
        run(&mut before, req);
    }
    steps_in(dir)
}

/// Makes the journal's highest control generation `generation`, by running and
/// finishing a step of another job.
fn raise_generation(steps: &mut Steps, generation: u64) {
    let mut req = Req::new("job_gen", 1);
    req.control_generation = generation;
    let (ticket, _) = run(steps, &req);
    finish_ok(steps, &ticket);
}

fn last_seq(steps: &Steps) -> u64 {
    steps.journal().last_seq()
}

/// Asserts a `step_rejected` with `code`, the request's ids, and the right
/// `supported` field. Returns the message text.
fn expect_reject(d: Decision, req: &Req, code: &str) -> String {
    let msg = match d {
        Decision::Reject(msg) => msg,
        other => panic!("{}: expected reject {code}, got {other:?}", req.request_id),
    };
    match msg {
        Message::StepRejected {
            request_id,
            job_id,
            step,
            code: got,
            message,
            supported,
        } => {
            assert_eq!(got.as_str(), code, "reject code");
            assert_eq!(request_id, req.request_id, "reject carries the requestId");
            assert_eq!(job_id, req.job_id, "reject carries the jobId");
            assert_eq!(step, req.step, "reject carries the step");
            assert!(!message.is_empty(), "{code}: reject message is empty");
            if code == "UNSUPPORTED_ACTION" {
                assert_eq!(
                    supported,
                    Some(actions::capabilities()),
                    "UNSUPPORTED_ACTION lists the full capability list"
                );
            } else {
                assert_eq!(supported, None, "{code} must not carry supported");
            }
            message
        }
        other => panic!("reject must be a step_rejected message, got {other:?}"),
    }
}

/// Asserts `Now(reconcile_report)` with the reconcile's ids and returns its fields.
fn expect_report(
    answer: ReconcileAnswer,
    request_id: &str,
    job_id: &str,
    step: u32,
) -> (bool, bool, bool, Vec<String>, Option<StepFailure>) {
    let msg = match answer {
        ReconcileAnswer::Now(msg) => msg,
        other => panic!("expected an immediate report, got {other:?}"),
    };
    match msg {
        Message::ReconcileReport {
            request_id: rid,
            job_id: jid,
            step: s,
            started,
            completed,
            failed,
            evidence,
            error,
        } => {
            assert_eq!(rid, request_id, "report carries the reconcile requestId");
            assert_eq!(jid, job_id, "report carries the reconcile jobId");
            assert_eq!(s, step, "report carries the reconcile step");
            (started, completed, failed, evidence, error)
        }
        other => panic!("expected a reconcile_report, got {other:?}"),
    }
}

// ---------- decide: check order (module docs, first match wins) ----------

#[test]
fn decide_wrong_node_beats_expired() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let mut req = Req::new("job_a", 1).expired();
    req.target_node = "some-other-node".to_string();
    let d = decide(&mut steps, &req.msg(), now());
    expect_reject(d, &req, "WRONG_NODE");
}

#[test]
fn decide_wrong_node_beats_completed_resend() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let req = Req::new("job_a", 1);
    let (ticket, _) = run(&mut steps, &req);
    finish_ok(&mut steps, &ticket);

    let mut other_node = req.clone();
    other_node.target_node = "some-other-node".to_string();
    let d = decide(&mut steps, &other_node.msg(), now());
    expect_reject(d, &other_node, "WRONG_NODE");
}

#[test]
fn decide_completed_step_resends_even_when_expired() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let req = Req::new("job_a", 1);
    let (ticket, _) = run(&mut steps, &req);
    let completed = finish_ok(&mut steps, &ticket);

    let again = req.clone().expired();
    let d = decide(&mut steps, &again.msg(), now());
    assert_eq!(
        d,
        Decision::Resend(completed),
        "completed step re-sends its record"
    );
    assert_eq!(last_seq(&steps), 2, "nothing new is journaled");
}

#[test]
fn decide_completed_step_resends_even_when_generation_is_stale() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let req = Req::new("job_a", 1);
    let (ticket, _) = run(&mut steps, &req);
    let completed = finish_ok(&mut steps, &ticket);

    let again = req.clone().stale();
    let d = decide(&mut steps, &again.msg(), now());
    assert_eq!(
        d,
        Decision::Resend(completed),
        "stale generation does not block a resend"
    );
}

#[test]
fn decide_failed_step_resends_even_when_expired() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let req = Req::new("job_a", 1);
    let (ticket, _) = run(&mut steps, &req);
    let failed = finish_err(&mut steps, &ticket);

    let again = req.clone().expired();
    let d = decide(&mut steps, &again.msg(), now());
    assert_eq!(
        d,
        Decision::Resend(failed),
        "failed step re-sends its record"
    );
    assert_eq!(last_seq(&steps), 2, "nothing new is journaled");
}

#[test]
fn decide_running_step_is_already_running_even_when_expired() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let req = Req::new("job_a", 1);
    run(&mut steps, &req);
    let before = last_seq(&steps);

    let again = req.clone().expired();
    let d = decide(&mut steps, &again.msg(), now());
    assert_eq!(d, Decision::AlreadyRunning);
    assert_eq!(last_seq(&steps), before, "AlreadyRunning journals nothing");
}

#[test]
fn decide_running_step_is_already_running_even_when_action_unsupported() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let req = Req::new("job_a", 1);
    run(&mut steps, &req);
    let before = last_seq(&steps);

    let mut again = req.clone();
    again.action = "disk.format".to_string();
    let d = decide(&mut steps, &again.msg(), now());
    assert_eq!(d, Decision::AlreadyRunning);
    assert_eq!(last_seq(&steps), before, "AlreadyRunning journals nothing");
}

#[test]
fn decide_interrupted_non_retryable_expired_is_outcome_unknown() {
    let dir = tempfile::tempdir().expect("tempdir");
    let req = Req::new("job_a", 1);
    let mut steps = start_then_restart(dir.path(), &req);
    let before = last_seq(&steps);

    let expired = req.clone().expired();
    let d = decide(&mut steps, &expired.msg(), now());
    expect_reject(d, &expired, "OUTCOME_UNKNOWN");
    assert_eq!(last_seq(&steps), before, "OUTCOME_UNKNOWN journals nothing");
}

#[test]
fn decide_interrupted_retryable_expired_is_expired() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut req = Req::new("job_a", 1);
    req.retryable = true;
    let mut steps = start_then_restart(dir.path(), &req);
    let before = last_seq(&steps);

    let expired = req.clone().expired();
    let d = decide(&mut steps, &expired.msg(), now());
    expect_reject(d, &expired, "EXPIRED");
    assert_eq!(last_seq(&steps), before, "EXPIRED journals nothing");
}

#[test]
fn decide_interrupted_non_retryable_stale_is_outcome_unknown() {
    let dir = tempfile::tempdir().expect("tempdir");
    let req = Req::new("job_a", 1);
    let mut steps = start_then_restart(dir.path(), &req);
    let before = last_seq(&steps);

    let stale = req.clone().stale();
    let d = decide(&mut steps, &stale.msg(), now());
    expect_reject(d, &stale, "OUTCOME_UNKNOWN");
    assert_eq!(last_seq(&steps), before, "OUTCOME_UNKNOWN journals nothing");
}

#[test]
fn decide_interrupted_retryable_stale_is_stale_generation() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut req = Req::new("job_a", 1);
    req.retryable = true;
    let mut steps = start_then_restart(dir.path(), &req);
    raise_generation(&mut steps, 5);
    let before = last_seq(&steps);

    let stale = req.clone().stale();
    let d = decide(&mut steps, &stale.msg(), now());
    expect_reject(d, &stale, "STALE_CONTROL_GENERATION");
    assert_eq!(
        last_seq(&steps),
        before,
        "STALE_CONTROL_GENERATION journals nothing"
    );
}

#[test]
fn decide_expired_beats_stale_generation() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    raise_generation(&mut steps, 5);

    let req = Req::new("job_a", 1).expired().stale();
    let d = decide(&mut steps, &req.msg(), now());
    // Stale only if the generation is below 5: the request uses 1, so it is stale.
    expect_reject(d, &req, "EXPIRED");
}

#[test]
fn decide_expired_beats_unsupported_action() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let mut req = Req::new("job_a", 1).expired();
    req.action = "disk.format".to_string();
    let d = decide(&mut steps, &req.msg(), now());
    expect_reject(d, &req, "EXPIRED");
}

#[test]
fn decide_expired_beats_invalid_params() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let mut req = Req::new("job_a", 1).expired();
    req.params.insert("x".to_string(), json!(1));
    let d = decide(&mut steps, &req.msg(), now());
    expect_reject(d, &req, "EXPIRED");
}

#[test]
fn decide_stale_generation_beats_unsupported_action() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    raise_generation(&mut steps, 5);

    let mut req = Req::new("job_a", 1).stale();
    req.action = "disk.format".to_string();
    let d = decide(&mut steps, &req.msg(), now());
    expect_reject(d, &req, "STALE_CONTROL_GENERATION");
}

#[test]
fn decide_stale_generation_beats_invalid_params() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    raise_generation(&mut steps, 5);

    let mut req = Req::new("job_a", 1).stale();
    req.params.insert("x".to_string(), json!(1));
    let d = decide(&mut steps, &req.msg(), now());
    expect_reject(d, &req, "STALE_CONTROL_GENERATION");
}

#[test]
fn decide_unsupported_action_beats_invalid_params() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let mut req = Req::new("job_a", 1);
    req.action = "disk.format".to_string();
    req.params.insert("x".to_string(), json!(1));
    let d = decide(&mut steps, &req.msg(), now());
    expect_reject(d, &req, "UNSUPPORTED_ACTION");
}

#[test]
fn decide_generation_equal_to_highest_is_not_stale() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    raise_generation(&mut steps, 5);

    let mut req = Req::new("job_a", 1);
    req.control_generation = 5;
    let (ticket, started) = run(&mut steps, &req);
    assert_eq!(ticket.control_generation, 5);
    assert_eq!(started.control_generation, 5);
}

#[test]
fn decide_expiry_boundary_is_expired_when_equal_to_now() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let mut req = Req::new("job_a", 1);
    req.expires_at = now();
    let d = decide(&mut steps, &req.msg(), now());
    expect_reject(d, &req, "EXPIRED");
}

#[test]
fn decide_one_nanosecond_before_expiry_still_runs() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let mut req = Req::new("job_a", 1);
    req.expires_at = now() + Duration::nanoseconds(1);
    let (ticket, _) = run(&mut steps, &req);
    assert_eq!(ticket.step, 1);
}

// ---------- decide: per-check rejections ----------

#[test]
fn decide_unknown_action_is_unsupported_with_capabilities() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let mut req = Req::new("job_a", 1);
    req.action = "disk.format".to_string();
    let d = decide(&mut steps, &req.msg(), now());
    expect_reject(d, &req, "UNSUPPORTED_ACTION");
    assert_eq!(last_seq(&steps), 0, "rejected step journals nothing");
}

#[test]
fn decide_newer_action_version_than_spec_is_unsupported() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let mut req = Req::new("job_a", 1);
    req.action_version = 2;
    let d = decide(&mut steps, &req.msg(), now());
    expect_reject(d, &req, "UNSUPPORTED_ACTION");
    assert_eq!(last_seq(&steps), 0, "rejected step journals nothing");
}

#[test]
fn decide_extra_param_is_invalid_params_with_validator_message() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let mut req = Req::new("job_a", 1);
    req.params.insert("x".to_string(), json!(1));

    let spec = actions::specs()
        .iter()
        .find(|s| s.name == "agent.info")
        .expect("agent.info spec exists");
    let validator_message = (spec.validate)(&req.params).expect_err("validator refuses extra key");

    let d = decide(&mut steps, &req.msg(), now());
    let message = expect_reject(d, &req, "INVALID_PARAMS");
    assert!(
        message.contains(&validator_message),
        "reject message {message:?} should contain the validator message {validator_message:?}"
    );
    assert_eq!(last_seq(&steps), 0, "rejected step journals nothing");
}

// ---------- decide: Run and AlreadyRunning ----------

#[test]
fn decide_run_journals_started_with_request_fields_and_marks_running() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let req = Req::new("job_a", 1);

    let (ticket, started) = run(&mut steps, &req);

    assert_eq!(ticket.request_id, req.request_id);
    assert_eq!(ticket.job_id, req.job_id);
    assert_eq!(ticket.step, req.step);
    assert_eq!(ticket.action, "agent.info");
    assert_eq!(ticket.params, req.params);
    assert_eq!(ticket.control_generation, req.control_generation);

    assert_eq!(started.kind, EventKind::Started);
    assert_eq!(started.request_id, req.request_id);
    assert_eq!(started.control_generation, req.control_generation);
    assert_eq!(started.at, now(), "started is stamped with the decide time");
    assert_eq!(started.seq, 1);
    assert_eq!(last_seq(&steps), 1, "exactly one record was journaled");

    assert_eq!(
        steps.journal().step_state("job_a", 1),
        StepState::Interrupted {
            started: started.clone()
        },
        "the journal holds the returned started record"
    );
    assert!(
        steps.is_running("job_a", 1),
        "step is running in this process"
    );
}

#[test]
fn decide_identical_request_while_running_is_already_running() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let req = Req::new("job_a", 1);
    run(&mut steps, &req);
    let before = last_seq(&steps);

    let d = decide(&mut steps, &req.msg(), now());
    assert_eq!(d, Decision::AlreadyRunning);
    assert_eq!(last_seq(&steps), before, "a second decide journals nothing");
}

#[test]
fn decide_non_execute_message_is_error_and_journals_nothing() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let ack = Message::Ack { seq: 1 };
    assert!(
        steps.decide(&ack, now()).is_err(),
        "Ack is not a step_execute"
    );
    let reconcile = reconcile_msg("job_a", 1, NODE);
    assert!(
        steps.decide(&reconcile, now()).is_err(),
        "step_reconcile is not a step_execute"
    );
    assert_eq!(last_seq(&steps), 0, "non-execute input journals nothing");
    assert!(!steps.is_running("job_a", 1));
}

// ---------- finish ----------

#[test]
fn finish_ok_journals_completed_with_result_evidence_and_resend_after() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let req = Req::new("job_a", 1);
    let (ticket, started) = run(&mut steps, &req);

    let value = json!({ "agentVersion": "0.1.0", "os": "linux" });
    let completed = steps
        .finish(&ticket, Ok(value.clone()), later())
        .expect("finish journals");

    assert_eq!(completed.kind, EventKind::Completed);
    assert_eq!(completed.request_id, req.request_id);
    assert_eq!(completed.control_generation, req.control_generation);
    assert_eq!(
        completed.at,
        later(),
        "completed is stamped with the finish time"
    );
    assert_eq!(completed.result, Some(value));
    assert_eq!(completed.error, None);
    assert_eq!(
        completed.evidence,
        vec![format!("agent://journal/{}", started.seq)],
        "evidence points at the started record"
    );
    assert_eq!(
        steps.journal().step_state("job_a", 1),
        StepState::Completed(completed.clone())
    );
    assert!(
        !steps.is_running("job_a", 1),
        "no longer running after finish"
    );

    let d = decide(&mut steps, &req.msg(), now());
    assert_eq!(
        d,
        Decision::Resend(completed),
        "same step now re-sends the record"
    );
}

#[test]
fn finish_err_journals_failed_with_error_evidence_and_resend_after() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let req = Req::new("job_a", 1);
    let (ticket, started) = run(&mut steps, &req);

    let failure = StepFailure {
        code: "ACTION_FAILED".to_string(),
        summary: "hostname lookup failed".to_string(),
        retryable: false,
    };
    let failed = steps
        .finish(&ticket, Err(failure.clone()), later())
        .expect("finish journals");

    assert_eq!(failed.kind, EventKind::Failed);
    assert_eq!(failed.request_id, req.request_id);
    assert_eq!(failed.control_generation, req.control_generation);
    assert_eq!(failed.at, later());
    assert_eq!(failed.result, None);
    assert_eq!(failed.error, Some(failure));
    assert_eq!(
        failed.evidence,
        vec![format!("agent://journal/{}", started.seq)]
    );
    assert!(!steps.is_running("job_a", 1));

    let d = decide(&mut steps, &req.msg(), now());
    assert_eq!(
        d,
        Decision::Resend(failed),
        "failed step re-sends its record"
    );
}

#[test]
fn finish_evidence_points_at_latest_started_record_not_the_first() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());

    let first = Req::new("job_a", 0);
    let (ticket0, _) = run(&mut steps, &first);
    finish_ok(&mut steps, &ticket0);

    let second = Req::new("job_a", 1);
    let (ticket1, started) = run(&mut steps, &second);
    assert_eq!(started.seq, 3, "started follows the completed pair");

    let completed = finish_ok(&mut steps, &ticket1);
    assert_eq!(completed.evidence, vec!["agent://journal/3".to_string()]);
}

// ---------- restart (agent restarted mid-step) ----------

#[test]
fn restart_non_retryable_interrupted_step_is_outcome_unknown_and_not_rerun() {
    let dir = tempfile::tempdir().expect("tempdir");
    let req = Req::new("job_a", 1);
    let mut steps = start_then_restart(dir.path(), &req);
    let before = last_seq(&steps);

    let d = decide(&mut steps, &req.msg(), now());
    expect_reject(d, &req, "OUTCOME_UNKNOWN");
    assert_eq!(
        last_seq(&steps),
        before,
        "the interrupted step is not journaled again"
    );
    assert!(
        !steps.is_running("job_a", 1),
        "nothing runs after the restart"
    );
}

#[test]
fn restart_retryable_interrupted_step_runs_again_as_new_attempt() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut req = Req::new("job_a", 1);
    req.retryable = true;
    let mut steps = start_then_restart(dir.path(), &req);
    let before = last_seq(&steps);

    let (ticket, started) = run(&mut steps, &req);
    assert_eq!(ticket.request_id, req.request_id);
    assert_eq!(started.kind, EventKind::Started);
    assert_eq!(
        started.seq,
        before + 1,
        "one new started record is journaled"
    );
    assert_eq!(last_seq(&steps), before + 1);
    assert_eq!(
        steps.journal().step_state("job_a", 1),
        StepState::Interrupted {
            started: started.clone()
        },
        "the new attempt is the most recent started record"
    );
    assert!(steps.is_running("job_a", 1));
}

// ---------- reconcile ----------

#[test]
fn reconcile_other_variant_is_error_and_journals_nothing() {
    let dir = tempfile::tempdir().expect("tempdir");
    let steps = steps_in(dir.path());
    assert!(steps.reconcile(&Message::Ack { seq: 1 }).is_err());
    assert_eq!(last_seq(&steps), 0);
}

#[test]
fn reconcile_wrong_node_is_wrong_node_rejection() {
    let dir = tempfile::tempdir().expect("tempdir");
    let steps = steps_in(dir.path());
    let msg = reconcile_msg("job_a", 1, "some-other-node");

    let answer = steps.reconcile(&msg).expect("reconcile returns Ok");
    match answer {
        ReconcileAnswer::Now(Message::StepRejected {
            request_id,
            job_id,
            step,
            code,
            message,
            supported,
        }) => {
            assert_eq!(
                request_id, "rec_job_a_1_2",
                "reject carries the reconcile requestId"
            );
            assert_eq!(job_id, "job_a");
            assert_eq!(step, 1);
            assert_eq!(code.as_str(), "WRONG_NODE");
            assert!(!message.is_empty());
            assert_eq!(supported, None);
        }
        other => panic!("expected Now(step_rejected WRONG_NODE), got {other:?}"),
    }
}

#[test]
fn reconcile_never_started_reports_all_false_without_evidence() {
    let dir = tempfile::tempdir().expect("tempdir");
    let steps = steps_in(dir.path());
    let answer = steps
        .reconcile(&reconcile_msg("job_a", 1, NODE))
        .expect("reconcile returns Ok");
    let (started, completed, failed, evidence, error) =
        expect_report(answer, "rec_job_a_1_2", "job_a", 1);
    assert!(!started);
    assert!(!completed);
    assert!(!failed);
    assert!(
        evidence.is_empty(),
        "no evidence for a step that never started"
    );
    assert_eq!(error, None);
}

#[test]
fn reconcile_running_here_is_when_finished() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let req = Req::new("job_a", 1);
    run(&mut steps, &req);
    let before = last_seq(&steps);

    let answer = steps
        .reconcile(&reconcile_msg("job_a", 1, NODE))
        .expect("reconcile returns Ok");
    assert_eq!(answer, ReconcileAnswer::WhenFinished);
    assert_eq!(last_seq(&steps), before, "reconcile journals nothing");
}

#[test]
fn reconcile_interrupted_reports_started_with_evidence_of_started_record() {
    let dir = tempfile::tempdir().expect("tempdir");
    let req = Req::new("job_a", 1);
    let steps = start_then_restart(dir.path(), &req);
    let before = last_seq(&steps);

    let answer = steps
        .reconcile(&reconcile_msg("job_a", 1, NODE))
        .expect("reconcile returns Ok");
    let (started, completed, failed, evidence, error) =
        expect_report(answer, "rec_job_a_1_2", "job_a", 1);
    assert!(started, "the step did start");
    assert!(!completed);
    assert!(!failed);
    assert_eq!(evidence, vec!["agent://journal/1".to_string()]);
    assert_eq!(error, None);
    assert_eq!(last_seq(&steps), before, "reconcile journals nothing");
}

#[test]
fn reconcile_completed_reports_completed_with_record_evidence() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let req = Req::new("job_a", 1);
    let (ticket, _) = run(&mut steps, &req);
    let completed_record = finish_ok(&mut steps, &ticket);

    let answer = steps
        .reconcile(&reconcile_msg("job_a", 1, NODE))
        .expect("reconcile returns Ok");
    let (started, completed, failed, evidence, error) =
        expect_report(answer, "rec_job_a_1_2", "job_a", 1);
    assert!(started);
    assert!(completed);
    assert!(!failed);
    assert_eq!(evidence, completed_record.evidence);
    assert_eq!(error, None);
}

#[test]
fn reconcile_failed_reports_failed_with_record_evidence_and_error() {
    let dir = tempfile::tempdir().expect("tempdir");
    let mut steps = steps_in(dir.path());
    let req = Req::new("job_a", 1);
    let (ticket, _) = run(&mut steps, &req);
    let failed_record = finish_err(&mut steps, &ticket);

    let answer = steps
        .reconcile(&reconcile_msg("job_a", 1, NODE))
        .expect("reconcile returns Ok");
    let (started, completed, failed, evidence, error) =
        expect_report(answer, "rec_job_a_1_2", "job_a", 1);
    assert!(started);
    assert!(!completed);
    assert!(failed);
    assert_eq!(evidence, failed_record.evidence);
    assert_eq!(error, failed_record.error);
}

#[test]
fn reconcile_never_journals_even_when_called_repeatedly() {
    let dir = tempfile::tempdir().expect("tempdir");
    let req = Req::new("job_a", 1);
    let steps = start_then_restart(dir.path(), &req);
    let before = last_seq(&steps);

    for _ in 0..3 {
        steps
            .reconcile(&reconcile_msg("job_a", 1, NODE))
            .expect("reconcile returns Ok");
        steps
            .reconcile(&reconcile_msg("job_b", 9, NODE))
            .expect("reconcile returns Ok");
    }
    assert_eq!(
        last_seq(&steps),
        before,
        "reconcile never writes to the journal"
    );
}
