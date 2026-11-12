//! Tests for journal compaction (ticket 122), written against the expected
//! results on `Journal::compact`.

use super::*;
use std::fs;
use std::os::unix::fs::PermissionsExt;
use tempfile::tempdir;
use time::macros::datetime;

// ---------- helpers ----------

/// Older than `RETAIN_AGE` before the test clock.
fn old() -> OffsetDateTime {
    datetime!(2026-10-01 00:00:00 UTC)
}

/// Within `RETAIN_AGE` of the test clock.
fn recent() -> OffsetDateTime {
    datetime!(2026-11-08 01:00:00 UTC)
}

fn rec(kind: EventKind, job: &str, step: u32, at: OffsetDateTime, generation: u64) -> NewRecord {
    NewRecord {
        kind,
        job_id: job.to_string(),
        step,
        request_id: format!("req-{job}-{step}"),
        at,
        control_generation: generation,
        evidence: Vec::new(),
        result: None,
        error: None,
    }
}

fn run_step(j: &mut Journal, job: &str, step: u32, at: OffsetDateTime, generation: u64) {
    let started = j
        .append(rec(EventKind::Started, job, step, at, generation))
        .expect("append started");
    let mut done = rec(EventKind::Completed, job, step, at, generation);
    done.result = Some(serde_json::json!({"ok": true}));
    done.evidence = vec![format!("agent://journal/{}", started.seq)];
    j.append(done).expect("append completed");
}

fn fail_step(j: &mut Journal, job: &str, step: u32, at: OffsetDateTime) {
    let started = j
        .append(rec(EventKind::Started, job, step, at, 1))
        .expect("append started");
    let mut failed = rec(EventKind::Failed, job, step, at, 1);
    failed.evidence = vec![format!("agent://journal/{}", started.seq)];
    failed.error = Some(StepFailure {
        code: "disk_full".to_string(),
        summary: "磁碟已滿".to_string(),
        retryable: false,
    });
    j.append(failed).expect("append failed");
}

/// A journal with:
/// - job-old: two old steps, both completed, acked, highest generation (7);
/// - job-failed: one old failed step, acked;
/// - job-recent: one recent completed step, acked;
/// - job-open: an old step that started and never finished, acked;
/// - job-unacked: an old completed step, *not* acked.
///
/// Returns the journal and the seq of the last acked record.
fn mixed(dir: &Path) -> (Journal, u64) {
    let mut j = Journal::open(dir).expect("open");
    run_step(&mut j, "job-old", 1, old(), 7);
    run_step(&mut j, "job-old", 2, old(), 7);
    fail_step(&mut j, "job-failed", 1, old());
    run_step(&mut j, "job-recent", 1, recent(), 2);
    j.append(rec(EventKind::Started, "job-open", 1, old(), 2))
        .expect("append open start");
    let acked = j.last_seq();
    j.ack(acked).expect("ack");
    run_step(&mut j, "job-unacked", 1, old(), 2);
    (j, acked)
}

fn jobs_of(recs: &[Record]) -> Vec<String> {
    let mut jobs: Vec<String> = recs.iter().map(|r| r.job_id.clone()).collect();
    jobs.dedup();
    jobs
}

fn read_lines(path: &Path) -> Vec<Record> {
    let text = match fs::read_to_string(path) {
        Ok(t) => t,
        Err(e) if e.kind() == ErrorKind::NotFound => return Vec::new(),
        Err(e) => panic!("read {}: {e}", path.display()),
    };
    text.lines()
        .filter(|l| !l.is_empty())
        .map(|l| serde_json::from_str::<Record>(l).expect("archive line is a record"))
        .collect()
}

fn archived(dir: &Path) -> Vec<Record> {
    let mut all = read_lines(&dir.join(ARCHIVE_PREV_FILE));
    all.extend(read_lines(&dir.join(ARCHIVE_FILE)));
    all
}

fn archived_seqs_sorted(dir: &Path) -> Vec<u64> {
    let mut s: Vec<u64> = archived(dir).iter().map(|r| r.seq).collect();
    s.sort_unstable();
    s
}

/// Everything a restarted agent can observe about the journal.
#[derive(Debug, PartialEq)]
struct Observed {
    last_seq: u64,
    acked: u64,
    unacked: Vec<Record>,
    max_generation: u64,
    recent: StepState,
    open: StepState,
    unacked_step: StepState,
}

fn observe(j: &Journal) -> Observed {
    Observed {
        last_seq: j.last_seq(),
        acked: j.acked(),
        unacked: j.unacked(),
        max_generation: j.max_control_generation(),
        recent: j.step_state("job-recent", 1),
        open: j.step_state("job-open", 1),
        unacked_step: j.step_state("job-unacked", 1),
    }
}

// ---------- what moves and what stays ----------

#[test]
fn compact_moves_only_acked_finished_jobs_past_retention_to_the_archive() {
    let d = tempdir().unwrap();
    let (mut j, acked) = mixed(d.path());
    let before_last = j.last_seq();

    let moved = j.compact(TEST_NOW).expect("compact");
    assert_eq!(moved, 6, "job-old (4 records) and job-failed (2) move");

    // Gone from memory; only the outcome is left.
    assert!(matches!(
        j.step_state("job-old", 1),
        StepState::Compacted(_)
    ));
    assert!(matches!(
        j.step_state("job-old", 2),
        StepState::Compacted(_)
    ));
    assert!(matches!(
        j.step_state("job-failed", 1),
        StepState::Failed(_)
    ));
    assert_eq!(
        jobs_of(&j.records),
        ["job-recent", "job-open", "job-unacked"]
    );

    // Kept: recent completed, interrupted, and unacked.
    assert!(matches!(
        j.step_state("job-recent", 1),
        StepState::Completed(_)
    ));
    assert!(matches!(
        j.step_state("job-open", 1),
        StepState::Interrupted { .. }
    ));
    assert!(matches!(
        j.step_state("job-unacked", 1),
        StepState::Completed(_)
    ));

    // Counters survive even though the highest generation left with job-old.
    assert_eq!(j.last_seq(), before_last);
    assert_eq!(j.acked(), acked);
    assert_eq!(j.max_control_generation(), 7);

    // In the archive, with every field intact.
    let arch = archived(d.path());
    assert_eq!(seqs_of(&arch), vec![1, 2, 3, 4, 5, 6]);
    assert_eq!(arch[1].result, Some(serde_json::json!({"ok": true})));
    assert_eq!(arch[5].error.as_ref().unwrap().summary, "磁碟已滿");

    // Gone from journal.jsonl.
    let text = fs::read_to_string(d.path().join(JOURNAL_FILE)).unwrap();
    assert!(!text.contains("job-old"), "journal: {text}");
    assert!(!text.contains("job-failed"), "journal: {text}");
}

fn full(seq: u64, kind: EventKind, job: &str) -> Record {
    Record {
        seq,
        kind,
        job_id: job.to_string(),
        step: 1,
        request_id: format!("req-{job}"),
        at: old(),
        control_generation: 1,
        evidence: Vec::new(),
        result: None,
        error: None,
    }
}

fn seqs_of(recs: &[Record]) -> Vec<u64> {
    recs.iter().map(|r| r.seq).collect()
}

#[test]
fn compact_with_nothing_eligible_writes_nothing() {
    let d = tempdir().unwrap();
    let mut j = Journal::open(d.path()).unwrap();
    run_step(&mut j, "job-recent", 1, recent(), 1);
    j.ack(j.last_seq()).unwrap();
    let before = fs::read(d.path().join(JOURNAL_FILE)).unwrap();

    assert_eq!(j.compact(TEST_NOW).unwrap(), 0);
    assert_eq!(fs::read(d.path().join(JOURNAL_FILE)).unwrap(), before);
    assert!(!d.path().join(ARCHIVE_FILE).exists());
}

#[test]
fn a_job_with_one_unacked_record_is_kept_whole() {
    let d = tempdir().unwrap();
    let mut j = Journal::open(d.path()).unwrap();
    run_step(&mut j, "job-a", 1, old(), 1);
    run_step(&mut j, "job-a", 2, old(), 1);
    j.ack(j.last_seq() - 1).unwrap(); // the last completed is not acked

    assert_eq!(j.compact(TEST_NOW).unwrap(), 0);
    assert_eq!(j.records.len(), 4);
}

#[test]
fn a_job_with_an_unfinished_step_is_kept_whole() {
    let d = tempdir().unwrap();
    let mut j = Journal::open(d.path()).unwrap();
    run_step(&mut j, "job-a", 1, old(), 1);
    j.append(rec(EventKind::Started, "job-a", 2, old(), 1))
        .unwrap();
    j.ack(j.last_seq()).unwrap();

    assert_eq!(j.compact(TEST_NOW).unwrap(), 0);
    assert!(matches!(j.step_state("job-a", 1), StepState::Completed(_)));
}

#[test]
fn a_job_is_kept_while_its_newest_record_is_within_retention() {
    let d = tempdir().unwrap();
    let mut j = Journal::open(d.path()).unwrap();
    run_step(&mut j, "job-a", 1, old(), 1);
    run_step(&mut j, "job-a", 2, recent(), 1);
    j.ack(j.last_seq()).unwrap();

    assert_eq!(j.compact(TEST_NOW).unwrap(), 0);
    assert!(matches!(j.step_state("job-a", 1), StepState::Completed(_)));
}

#[test]
fn retention_boundary_is_exactly_retain_age() {
    let d = tempdir().unwrap();
    let mut j = Journal::open(d.path()).unwrap();
    run_step(&mut j, "job-edge", 1, TEST_NOW - RETAIN_AGE, 1);
    run_step(
        &mut j,
        "job-inside",
        1,
        TEST_NOW - RETAIN_AGE + time::Duration::seconds(1),
        1,
    );
    j.ack(j.last_seq()).unwrap();

    assert_eq!(j.compact(TEST_NOW).unwrap(), 2);
    assert!(matches!(
        j.step_state("job-edge", 1),
        StepState::Compacted(_)
    ));
    assert!(matches!(
        j.step_state("job-inside", 1),
        StepState::Completed(_)
    ));
}

#[test]
fn recent_finished_records_beyond_the_count_cap_move_oldest_job_first() {
    let d = tempdir().unwrap();
    // Written by hand: thousands of fsynced appends would be slow.
    let pairs = RETAIN_MAX_RECORDS / 2 + 1; // one job over the cap
    let mut text = String::new();
    let mut seq = 0;
    for n in 0..pairs {
        for kind in [EventKind::Started, EventKind::Completed] {
            seq += 1;
            let r = Record {
                seq,
                kind,
                job_id: format!("job-{n}"),
                step: 1,
                request_id: format!("req-{n}"),
                at: recent(),
                control_generation: 1,
                evidence: Vec::new(),
                result: None,
                error: None,
            };
            text.push_str(&serde_json::to_string(&r).unwrap());
            text.push('\n');
        }
    }
    fs::write(d.path().join(JOURNAL_FILE), text).unwrap();
    fs::write(d.path().join(ACKED_FILE), seq.to_string()).unwrap();

    // Opening compacts.
    let j = Journal::open(d.path()).unwrap();
    assert_eq!(j.records.len(), RETAIN_MAX_RECORDS);
    assert!(matches!(j.step_state("job-0", 1), StepState::Compacted(_)));
    assert!(matches!(j.step_state("job-1", 1), StepState::Completed(_)));
    assert_eq!(archived_seqs_sorted(d.path()), vec![1, 2]);
    assert_eq!(j.last_seq(), seq);
}

// ---------- restart after compaction ----------

#[test]
fn after_compaction_and_restart_unacked_records_are_resent_and_recent_ones_answer() {
    let d = tempdir().unwrap();
    let (mut j, _) = mixed(d.path());
    j.compact(TEST_NOW).unwrap();
    let expected = observe(&j);
    drop(j);

    let j = Journal::open(d.path()).expect("reopen compacted journal");
    let got = observe(&j);
    assert_eq!(got, expected);
    assert_eq!(
        seqs_of(&got.unacked),
        vec![10, 11],
        "job-unacked is re-sent"
    );
    assert!(matches!(got.recent, StepState::Completed(_)));
    assert!(matches!(got.open, StepState::Interrupted { .. }));
    assert_eq!(got.max_generation, 7);
}

#[test]
fn appends_after_compaction_continue_the_seq_across_restarts() {
    let d = tempdir().unwrap();
    let mut j = Journal::open(d.path()).unwrap();
    run_step(&mut j, "job-old", 1, old(), 1);
    j.ack(2).unwrap();
    j.compact(TEST_NOW).unwrap();
    assert!(j.records.is_empty(), "everything moved");
    assert_eq!(j.last_seq(), 2);
    drop(j);

    let mut j = Journal::open(d.path()).unwrap();
    assert_eq!(j.last_seq(), 2);
    assert_eq!(j.acked(), 2);
    let r = j
        .append(rec(EventKind::Started, "job-new", 1, recent(), 1))
        .unwrap();
    assert_eq!(r.seq, 3);
    drop(j);

    let j = Journal::open(d.path()).unwrap();
    assert_eq!(j.last_seq(), 3);
    assert_eq!(seqs_of(&j.unacked()), vec![3]);
}

#[test]
fn a_compacted_step_refuses_a_new_start_even_after_restart() {
    let d = tempdir().unwrap();
    let mut j = Journal::open(d.path()).unwrap();
    run_step(&mut j, "job-old", 1, old(), 1);
    j.ack(2).unwrap();
    j.compact(TEST_NOW).unwrap();
    assert!(
        j.append(rec(EventKind::Started, "job-old", 1, recent(), 1))
            .is_err(),
        "a finished step never starts again"
    );
    drop(j);
    let mut j = Journal::open(d.path()).unwrap();
    assert!(
        j.append(rec(EventKind::Started, "job-old", 1, recent(), 1))
            .is_err()
    );
    assert_eq!(j.last_seq(), 2, "nothing was written");
}

// ---------- outcomes of compacted steps ----------

/// job-old's steps and job-failed's step answer with their outcome, whether
/// their records are still in the journal or were compacted.
fn assert_outcomes_known(j: &Journal, context: &str) {
    for (job, step, kind, evidence) in [
        ("job-old", 1, EventKind::Completed, "agent://journal/1"),
        ("job-old", 2, EventKind::Completed, "agent://journal/3"),
        ("job-failed", 1, EventKind::Failed, "agent://journal/5"),
    ] {
        let (got_kind, got_evidence) = match j.step_state(job, step) {
            StepState::Completed(r) | StepState::Failed(r) => (r.kind, r.evidence),
            StepState::Compacted(t) => (t.kind, t.evidence),
            other => panic!("{context}: {job} step {step} lost its outcome: {other:?}"),
        };
        assert_eq!(got_kind, kind, "{context}: {job} step {step}");
        assert_eq!(got_evidence, vec![evidence.to_string()], "{context}: {job}");
    }
}

#[test]
fn compacted_steps_keep_their_outcome_in_memory_and_across_restart() {
    let d = tempdir().unwrap();
    let (mut j, _) = mixed(d.path());
    let failed_record = match j.step_state("job-failed", 1) {
        StepState::Failed(r) => r,
        other => panic!("expected Failed, got {other:?}"),
    };
    j.compact(TEST_NOW).unwrap();
    // A failed step has no result, so its record is kept whole and still
    // answers as Failed (a re-send of it stays possible).
    assert_eq!(
        j.step_state("job-failed", 1),
        StepState::Failed(failed_record.clone())
    );
    assert!(matches!(
        j.step_state("job-old", 2),
        StepState::Compacted(_)
    ));
    assert_outcomes_known(&j, "after compaction");
    drop(j);

    let j = Journal::open(d.path()).unwrap();
    assert!(matches!(
        j.step_state("job-old", 1),
        StepState::Compacted(_)
    ));
    assert_outcomes_known(&j, "after restart");
    assert_eq!(
        j.step_state("job-failed", 1),
        StepState::Failed(failed_record)
    );
    assert_eq!(j.step_state("job-never", 1), StepState::NotStarted);
    let mode = fs::metadata(d.path().join(OUTCOMES_FILE))
        .unwrap()
        .permissions()
        .mode()
        & 0o777;
    assert_eq!(mode, 0o600);
}

#[test]
fn outcomes_beyond_the_cap_drop_the_oldest_first() {
    let d = tempdir().unwrap();
    // A full outcomes file, written by hand, for seqs below anything real.
    let mut text = String::new();
    for n in 0..OUTCOMES_MAX_STEPS as u64 {
        let t = Outcome {
            seq: 0,
            kind: EventKind::Completed,
            job_id: format!("job-pre-{n}"),
            step: 1,
            request_id: format!("req-pre-{n}"),
            at: old(),
            control_generation: 1,
            evidence: Vec::new(),
            error: None,
        };
        text.push_str(&serde_json::to_string(&t).unwrap());
        text.push('\n');
    }
    fs::write(d.path().join(OUTCOMES_FILE), text).unwrap();
    let (mut j, _) = mixed(d.path());
    j.compact(TEST_NOW).unwrap();
    assert_eq!(j.outcomes.len(), OUTCOMES_MAX_STEPS);
    assert_outcomes_known(&j, "newest kept");
    drop(j);
    let j = Journal::open(d.path()).unwrap();
    assert_eq!(j.outcomes.len(), OUTCOMES_MAX_STEPS);
    assert_outcomes_known(&j, "newest kept after restart");
}

#[test]
fn an_invalid_outcomes_file_fails_open() {
    let d = tempdir().unwrap();
    fs::write(d.path().join(OUTCOMES_FILE), "{\"seq\":\n").unwrap();
    assert!(Journal::open(d.path()).is_err());
}

fn steps_for(dir: &Path) -> crate::steps::Steps {
    crate::steps::Steps::new(
        Journal::open(dir).unwrap(),
        "node-fp".to_string(),
        crate::actions::specs(),
        crate::actions::capabilities(),
    )
}

#[test]
fn a_reconcile_of_a_compacted_step_reports_its_recorded_outcome() {
    let d = tempdir().unwrap();
    let (mut j, _) = mixed(d.path());
    j.compact(TEST_NOW).unwrap();
    drop(j);
    let steps = steps_for(d.path());
    let ask = |job: &str| Message::StepReconcile {
        request_id: format!("rec-{job}"),
        job_id: job.to_string(),
        step: 1,
        target_node: "node-fp".to_string(),
    };

    match steps.reconcile(&ask("job-old")).unwrap() {
        crate::steps::ReconcileAnswer::Now(Message::ReconcileReport {
            started,
            completed,
            failed,
            evidence,
            error,
            ..
        }) => {
            assert!(started && completed && !failed);
            assert_eq!(evidence, vec!["agent://journal/1".to_string()]);
            assert_eq!(error, None);
        }
        other => panic!("expected a report, got {other:?}"),
    }
    match steps.reconcile(&ask("job-failed")).unwrap() {
        crate::steps::ReconcileAnswer::Now(Message::ReconcileReport {
            started,
            completed,
            failed,
            error,
            ..
        }) => {
            assert!(started && !completed && failed);
            assert_eq!(error.unwrap().summary, "磁碟已滿");
        }
        other => panic!("expected a report, got {other:?}"),
    }
}

#[test]
fn a_repeated_execute_of_a_compacted_step_is_refused_as_outcome_unknown() {
    let d = tempdir().unwrap();
    let (mut j, _) = mixed(d.path());
    j.compact(TEST_NOW).unwrap();
    let last = j.last_seq();
    drop(j);
    let mut steps = steps_for(d.path());
    let msg = Message::StepExecute {
        request_id: "req-again".to_string(),
        job_id: "job-old".to_string(),
        step: 1,
        step_name: "again".to_string(),
        action: "agent.info".to_string(),
        action_version: 1,
        params: serde_json::Map::new(),
        target_node: "node-fp".to_string(),
        resources: Vec::new(),
        control_generation: 9,
        expires_at: TEST_NOW + time::Duration::minutes(5),
        retryable: true,
        irreversible: false,
    };
    match steps.decide(&msg, TEST_NOW).unwrap() {
        crate::steps::Decision::Reject(Message::StepRejected { code, .. }) => {
            assert_eq!(code, hazelminx_protocol::step_reject_code::OUTCOME_UNKNOWN);
        }
        other => panic!("a compacted step must not run again: {other:?}"),
    }
    assert_eq!(steps.journal().last_seq(), last, "nothing journaled");
}

#[test]
fn a_repeated_execute_of_a_compacted_failed_step_resends_its_failure() {
    let d = tempdir().unwrap();
    let (mut j, _) = mixed(d.path());
    let original = match j.step_state("job-failed", 1) {
        StepState::Failed(r) => r,
        other => panic!("expected Failed, got {other:?}"),
    };
    j.compact(TEST_NOW).unwrap();
    let last = j.last_seq();
    drop(j);
    let mut steps = steps_for(d.path());
    // A retryable step the Core reconciled as failed comes back as a new
    // request; answering OUTCOME_UNKNOWN would send it round the reconcile
    // loop forever. It gets the recorded failure, as before compaction.
    let msg = Message::StepExecute {
        request_id: "req-retry".to_string(),
        job_id: "job-failed".to_string(),
        step: 1,
        step_name: "retry".to_string(),
        action: "agent.info".to_string(),
        action_version: 1,
        params: serde_json::Map::new(),
        target_node: "node-fp".to_string(),
        resources: Vec::new(),
        control_generation: 9,
        expires_at: TEST_NOW + time::Duration::minutes(5),
        retryable: true,
        irreversible: false,
    };
    assert_eq!(
        steps.decide(&msg, TEST_NOW).unwrap(),
        crate::steps::Decision::Resend(original)
    );
    assert_eq!(steps.journal().last_seq(), last, "nothing journaled");
}

#[test]
fn compacted_journal_and_archive_are_mode_0600() {
    let d = tempdir().unwrap();
    let (mut j, _) = mixed(d.path());
    j.compact(TEST_NOW).unwrap();
    for f in [JOURNAL_FILE, ARCHIVE_FILE] {
        let mode = fs::metadata(d.path().join(f)).unwrap().permissions().mode() & 0o777;
        assert_eq!(mode, 0o600, "{f}");
    }
}

#[test]
fn compacting_twice_appends_to_the_archive_without_duplicates() {
    let d = tempdir().unwrap();
    let (mut j, _) = mixed(d.path());
    j.compact(TEST_NOW).unwrap();
    j.ack(j.last_seq()).unwrap(); // job-unacked is now acked and old
    assert_eq!(j.compact(TEST_NOW).unwrap(), 2);
    assert_eq!(
        archived_seqs_sorted(d.path()),
        vec![1, 2, 3, 4, 5, 6, 10, 11]
    );
    assert_eq!(jobs_of(&j.records), ["job-recent", "job-open"]);

    // Kept records now lie below the compacted floor (11); they still load.
    let expected = observe(&j);
    drop(j);
    let j = Journal::open(d.path()).expect("reopen");
    assert_eq!(observe(&j), expected);
    assert_eq!(j.last_seq(), 11);
}

// ---------- header validation ----------

#[test]
fn open_fails_on_a_gap_after_the_compacted_floor() {
    let d = tempdir().unwrap();
    let (mut j, _) = mixed(d.path());
    j.compact(TEST_NOW).unwrap();
    drop(j);
    // Remove job-unacked's started record (seq 10): 11 no longer follows.
    let text = fs::read_to_string(d.path().join(JOURNAL_FILE)).unwrap();
    let kept: String = text
        .lines()
        .filter(|l| !l.contains("\"seq\":10,"))
        .map(|l| format!("{l}\n"))
        .collect();
    fs::write(d.path().join(JOURNAL_FILE), kept).unwrap();
    let err = match Journal::open(d.path()) {
        Ok(_) => panic!("open accepted a gap"),
        Err(e) => format!("{e:#}"),
    };
    assert!(err.contains("seq"), "{err}");
}

#[test]
fn open_fails_when_the_header_is_not_the_first_line() {
    let d = tempdir().unwrap();
    let (mut j, _) = mixed(d.path());
    j.compact(TEST_NOW).unwrap();
    drop(j);
    let text = fs::read_to_string(d.path().join(JOURNAL_FILE)).unwrap();
    let mut lines: Vec<&str> = text.lines().collect();
    lines.swap(0, 1);
    let swapped: String = lines.iter().map(|l| format!("{l}\n")).collect();
    fs::write(d.path().join(JOURNAL_FILE), swapped).unwrap();
    assert!(Journal::open(d.path()).is_err());
}

#[test]
fn open_fails_when_acked_is_below_the_compacted_floor() {
    let d = tempdir().unwrap();
    let (mut j, acked) = mixed(d.path());
    j.compact(TEST_NOW).unwrap();
    drop(j);
    // Only acked records are ever compacted, so a lower watermark means the
    // acked file and the journal disagree.
    fs::write(d.path().join(ACKED_FILE), "1").unwrap();
    assert!(acked > 1);
    assert!(Journal::open(d.path()).is_err());
}

// ---------- crashes during compaction ----------

/// Every point at which a compaction can be cut off by a power loss.
const CRASH_POINTS: [CrashPoint; 7] = [
    CrashPoint::ArchiveTorn,
    CrashPoint::AfterArchive,
    CrashPoint::AfterOutcomes,
    CrashPoint::TmpTorn,
    CrashPoint::AfterTmp,
    CrashPoint::AfterRename,
    CrashPoint::AfterRotate,
];

#[test]
fn a_crash_at_any_point_of_compaction_loses_no_unacked_or_kept_record() {
    for point in CRASH_POINTS {
        let d = tempdir().unwrap();
        if point == CrashPoint::AfterRotate {
            // Rotation only happens when the archive is full.
            fs::write(
                d.path().join(ARCHIVE_FILE),
                vec![b'\n'; ARCHIVE_MAX_BYTES as usize],
            )
            .unwrap();
        }
        let (mut j, _) = mixed(d.path());
        let before = observe(&j);
        let hot_before = j.records.clone();

        j.crash_at = Some(point);
        assert!(j.compact(TEST_NOW).is_err(), "{point:?}: simulated crash");
        drop(j); // the process is gone

        let mut j = Journal::load(d.path())
            .unwrap_or_else(|e| panic!("{point:?}: reopen after crash: {e:#}"));
        assert_eq!(observe(&j), before, "{point:?}");
        assert_outcomes_known(&j, &format!("{point:?}: after crash"));
        assert!(
            !d.path().join(JOURNAL_TMP_FILE).exists(),
            "{point:?}: leftover temp file is removed on open"
        );
        let compacted = point == CrashPoint::AfterRename;
        if compacted {
            assert_eq!(
                jobs_of(&j.records),
                ["job-recent", "job-open", "job-unacked"]
            );
        } else {
            assert_eq!(j.records, hot_before, "{point:?}: journal unchanged");
        }

        // The next compaction finishes the job, with each record archived once.
        j.compact(TEST_NOW)
            .unwrap_or_else(|e| panic!("{point:?}: compaction after crash: {e:#}"));
        assert_eq!(
            archived_seqs_sorted(d.path()),
            vec![1, 2, 3, 4, 5, 6],
            "{point:?}: archive"
        );
        assert_eq!(observe(&j), before, "{point:?}: after recovery");
        assert_outcomes_known(&j, &format!("{point:?}: after recovery"));
        drop(j);
        let j = Journal::open(d.path()).unwrap();
        assert_eq!(observe(&j), before, "{point:?}: after second restart");
        assert_outcomes_known(&j, &format!("{point:?}: after second restart"));
    }
}

#[test]
fn a_torn_archive_tail_is_cut_before_the_next_append() {
    let d = tempdir().unwrap();
    let (mut j, _) = mixed(d.path());
    j.crash_at = Some(CrashPoint::ArchiveTorn);
    assert!(j.compact(TEST_NOW).is_err());
    drop(j);
    let raw = fs::read(d.path().join(ARCHIVE_FILE)).unwrap();
    assert!(!raw.ends_with(b"\n"), "the crash left half a line");

    let mut j = Journal::load(d.path()).unwrap();
    j.compact(TEST_NOW).unwrap();
    let text = fs::read_to_string(d.path().join(ARCHIVE_FILE)).unwrap();
    assert!(text.ends_with('\n'));
    for l in text.lines() {
        serde_json::from_str::<Record>(l).expect("every archive line is whole");
    }
}

#[test]
fn a_corrupt_archive_line_stops_compaction_and_leaves_the_journal_alone() {
    let d = tempdir().unwrap();
    fs::write(d.path().join(ARCHIVE_FILE), "not json\n{}\n").unwrap();
    let (mut j, _) = mixed(d.path());
    let hot = j.records.clone();
    let journal_bytes = fs::read(d.path().join(JOURNAL_FILE)).unwrap();
    j.archive_clean = false; // as after a restart
    assert!(j.compact(TEST_NOW).is_err());
    assert_eq!(j.records, hot);
    assert_eq!(
        fs::read(d.path().join(JOURNAL_FILE)).unwrap(),
        journal_bytes
    );
}

// ---------- archive size cap ----------

#[test]
fn a_full_archive_is_rotated_and_the_older_generation_is_dropped() {
    let d = tempdir().unwrap();
    let mut oldest = full(999, EventKind::Completed, "job-oldest");
    oldest.request_id = "oldest generation".to_string();
    fs::write(
        d.path().join(ARCHIVE_PREV_FILE),
        format!("{}\n", serde_json::to_string(&oldest).unwrap()),
    )
    .unwrap();
    // A full archive of whole (blank) lines.
    fs::write(
        d.path().join(ARCHIVE_FILE),
        vec![b'\n'; ARCHIVE_MAX_BYTES as usize],
    )
    .unwrap();
    let (mut j, _) = mixed(d.path());
    j.compact(TEST_NOW).unwrap();

    let prev = fs::metadata(d.path().join(ARCHIVE_PREV_FILE)).unwrap();
    assert_eq!(prev.len(), ARCHIVE_MAX_BYTES, "the full archive became .1");
    assert_eq!(
        seqs_of(&read_lines(&d.path().join(ARCHIVE_FILE))),
        vec![1, 2, 3, 4, 5, 6],
        "the new batch starts a fresh archive"
    );
    let prev_text = fs::read_to_string(d.path().join(ARCHIVE_PREV_FILE)).unwrap();
    assert!(
        !prev_text.contains("oldest generation"),
        "the older .1 is gone"
    );
}

#[test]
fn an_archive_under_the_cap_is_appended_to() {
    let d = tempdir().unwrap();
    let (mut j, _) = mixed(d.path());
    j.compact(TEST_NOW).unwrap();
    j.ack(j.last_seq()).unwrap();
    j.compact(TEST_NOW).unwrap();
    assert!(!d.path().join(ARCHIVE_PREV_FILE).exists());
    assert_eq!(
        seqs_of(&read_lines(&d.path().join(ARCHIVE_FILE))),
        vec![1, 2, 3, 4, 5, 6, 10, 11]
    );
}

// ---------- when compaction runs ----------

#[test]
fn open_compacts() {
    let d = tempdir().unwrap();
    let (j, _) = mixed(d.path());
    drop(j);
    let j = Journal::open(d.path()).unwrap();
    assert!(matches!(
        j.step_state("job-old", 1),
        StepState::Compacted(_)
    ));
    assert_eq!(archived_seqs_sorted(d.path()), vec![1, 2, 3, 4, 5, 6]);
}

#[test]
fn ack_compacts_when_the_interval_has_passed() {
    let d = tempdir().unwrap();
    let (mut j, _) = mixed(d.path());
    // Not due yet: ack only moves the watermark.
    j.ack(j.last_seq() - 1).unwrap();
    assert!(matches!(
        j.step_state("job-old", 1),
        StepState::Completed(_)
    ));

    j.compact_due = std::time::Instant::now();
    let last = j.last_seq();
    j.ack(last).unwrap();
    assert!(matches!(
        j.step_state("job-old", 1),
        StepState::Compacted(_)
    ));
    assert!(matches!(
        j.step_state("job-unacked", 1),
        StepState::Compacted(_)
    ));
    assert!(
        j.compact_due > std::time::Instant::now(),
        "next run is scheduled"
    );
}

#[test]
fn a_failed_compaction_does_not_fail_the_ack() {
    let d = tempdir().unwrap();
    let (mut j, _) = mixed(d.path());
    j.compact_due = std::time::Instant::now();
    j.crash_at = Some(CrashPoint::AfterArchive);
    let last = j.last_seq();
    j.ack(last)
        .expect("the watermark is stored even if compaction fails");
    assert_eq!(j.acked(), last);
    drop(j);
    let j = Journal::load(d.path()).unwrap();
    assert_eq!(j.acked(), last);
}
