//! Tests for the step journal (written against the expected results in mod.rs).

use super::*;
use std::fs;
use std::io::Write;
use std::os::unix::fs::PermissionsExt;
use tempfile::tempdir;
use time::macros::datetime;

// ---------- helpers ----------

fn at() -> OffsetDateTime {
    datetime!(2026-11-08 01:02:03 UTC)
}

fn new_rec(kind: EventKind, job: &str, step: u32) -> NewRecord {
    NewRecord {
        kind,
        job_id: job.to_string(),
        step,
        request_id: format!("req-{job}-{step}"),
        at: at(),
        control_generation: 1,
        evidence: Vec::new(),
        result: None,
        error: None,
    }
}

/// A complete record as it would be stored, for hand-written journal files.
fn full_record(seq: u64, kind: EventKind, job: &str, step: u32, generation: u64) -> Record {
    Record {
        seq,
        kind,
        job_id: job.to_string(),
        step,
        request_id: format!("req-{job}-{step}"),
        at: at(),
        control_generation: generation,
        evidence: Vec::new(),
        result: None,
        error: None,
    }
}

fn failure(summary: &str) -> StepFailure {
    StepFailure {
        code: "disk_full".to_string(),
        summary: summary.to_string(),
        retryable: true,
    }
}

fn json_line(rec: &Record) -> String {
    format!(
        "{}\n",
        serde_json::to_string(rec).expect("serialize record")
    )
}

fn journal_file(dir: &Path) -> PathBuf {
    dir.join(JOURNAL_FILE)
}

fn acked_file(dir: &Path) -> PathBuf {
    dir.join(ACKED_FILE)
}

fn file_bytes(dir: &Path) -> Vec<u8> {
    fs::read(journal_file(dir)).expect("read journal file")
}

fn mode_of(path: &Path) -> u32 {
    fs::metadata(path).expect("stat").permissions().mode() & 0o777
}

fn write_raw(dir: &Path, bytes: &[u8]) {
    fs::write(journal_file(dir), bytes).expect("write journal file");
}

fn append_raw(dir: &Path, bytes: &[u8]) {
    let mut f = fs::OpenOptions::new()
        .append(true)
        .open(journal_file(dir))
        .expect("open journal file for append");
    f.write_all(bytes).expect("write raw tail");
}

fn write_records(dir: &Path, recs: &[Record]) {
    let text: String = recs.iter().map(json_line).collect();
    write_raw(dir, text.as_bytes());
}

fn open_err(dir: &Path) -> String {
    match Journal::open(dir) {
        Ok(_) => panic!("open unexpectedly succeeded"),
        Err(e) => format!("{e:#}"),
    }
}

/// True when `n` appears in `msg` as a standalone number. The directory path
/// is removed first so digits inside a random temp directory name do not count.
fn mentions_number(msg: &str, dir: &Path, n: u64) -> bool {
    let mut cleaned = msg.to_string();
    let canonical = fs::canonicalize(dir).unwrap_or_else(|_| dir.to_path_buf());
    for p in [dir.to_path_buf(), canonical] {
        if let Some(s) = p.to_str() {
            cleaned = cleaned.replace(s, "<dir>");
        }
    }
    let want = n.to_string();
    cleaned
        .split(|c: char| !c.is_ascii_digit())
        .any(|tok| tok == want)
}

fn seqs(recs: &[Record]) -> Vec<u64> {
    recs.iter().map(|r| r.seq).collect()
}

/// Opens a journal in `dir` and appends `Started` for steps 1..=n of job-1.
fn seed_started(dir: &Path, n: u32) -> Journal {
    let mut j = Journal::open(dir).expect("open fresh journal");
    for step in 1..=n {
        j.append(new_rec(EventKind::Started, "job-1", step))
            .expect("append started");
    }
    j
}

/// Appends a record that must be rejected, and checks nothing was written.
fn expect_rejected(j: &mut Journal, rec: NewRecord) {
    let dir = j.dir().to_path_buf();
    let seq_before = j.last_seq();
    let bytes_before = file_bytes(&dir);
    assert!(j.append(rec).is_err(), "append should be rejected");
    assert_eq!(j.last_seq(), seq_before, "last_seq must not change");
    assert_eq!(file_bytes(&dir), bytes_before, "file must not change");
}

// ---------- permissions and creation ----------

#[test]
fn open_creates_missing_dir_with_mode_0700() {
    let tmp = tempdir().unwrap();
    let dir = tmp.path().join("journal");
    assert!(!dir.exists());
    Journal::open(&dir).expect("open creates the directory");
    assert!(dir.is_dir());
    assert_eq!(mode_of(&dir), 0o700);
}

#[test]
fn open_creates_journal_file_with_mode_0600_in_existing_dir() {
    let tmp = tempdir().unwrap();
    assert!(!journal_file(tmp.path()).exists());
    Journal::open(tmp.path()).expect("open");
    assert!(journal_file(tmp.path()).is_file());
    assert_eq!(mode_of(&journal_file(tmp.path())), 0o600);
}

#[test]
fn journal_file_keeps_mode_0600_after_appends() {
    let tmp = tempdir().unwrap();
    drop(seed_started(tmp.path(), 2));
    assert_eq!(mode_of(&journal_file(tmp.path())), 0o600);
}

#[test]
fn new_journal_is_empty_with_zero_seq_ack_and_generation() {
    let tmp = tempdir().unwrap();
    let j = Journal::open(tmp.path()).expect("open");
    assert_eq!(j.last_seq(), 0);
    assert_eq!(j.acked(), 0);
    assert!(j.unacked().is_empty());
    assert_eq!(j.max_control_generation(), 0);
    assert_eq!(j.step_state("job-1", 1), StepState::NotStarted);
}

#[test]
fn open_on_clean_journal_does_not_rewrite_file_bytes() {
    let tmp = tempdir().unwrap();
    drop(seed_started(tmp.path(), 2));
    let before = file_bytes(tmp.path());
    let j = Journal::open(tmp.path()).expect("reopen");
    assert_eq!(j.last_seq(), 2);
    assert_eq!(file_bytes(tmp.path()), before);
}

// ---------- reopen keeps content ----------

#[test]
fn reopen_returns_same_records_as_before_close() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    j.append(new_rec(EventKind::Started, "job-1", 1)).unwrap();
    j.append(new_rec(EventKind::Completed, "job-1", 1)).unwrap();
    j.append(new_rec(EventKind::Started, "job-1", 2)).unwrap();
    let before_unacked = j.unacked();
    let before_s1 = j.step_state("job-1", 1);
    let before_s2 = j.step_state("job-1", 2);
    drop(j);

    let j = Journal::open(tmp.path()).unwrap();
    assert_eq!(j.last_seq(), 3);
    assert_eq!(j.unacked(), before_unacked);
    assert_eq!(j.step_state("job-1", 1), before_s1);
    assert_eq!(j.step_state("job-1", 2), before_s2);
    assert!(matches!(j.step_state("job-1", 1), StepState::Completed(r) if r.seq == 2));
    assert!(matches!(
        j.step_state("job-1", 2),
        StepState::Interrupted { started } if started.seq == 3
    ));
}

#[test]
fn reopen_preserves_evidence_result_error_generation_and_subsecond_time() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    let mut n = new_rec(EventKind::Started, "job-9", 4);
    n.at = datetime!(2026-11-08 01:02:03.123456789 UTC);
    n.control_generation = 7;
    j.append(n).unwrap();
    let mut f = new_rec(EventKind::Failed, "job-9", 4);
    f.control_generation = 7;
    f.evidence = vec!["exit=2".to_string(), "stderr: 找不到檔案".to_string()];
    f.result = Some(serde_json::json!({"changed": false, "n": 3}));
    f.error = Some(failure("磁碟已滿"));
    let stored = j.append(f).unwrap();
    drop(j);

    let j = Journal::open(tmp.path()).unwrap();
    assert_eq!(j.last_seq(), 2);
    match j.step_state("job-9", 4) {
        StepState::Failed(r) => assert_eq!(r, stored),
        other => panic!("expected Failed, got {other:?}"),
    }
    assert_eq!(j.max_control_generation(), 7);
}

#[test]
fn reopen_preserves_acked_watermark() {
    let tmp = tempdir().unwrap();
    let mut j = seed_started(tmp.path(), 3);
    j.ack(2).unwrap();
    drop(j);
    let j = Journal::open(tmp.path()).unwrap();
    assert_eq!(j.acked(), 2);
    assert_eq!(seqs(&j.unacked()), vec![3]);
}

// ---------- torn writes at the tail ----------

#[test]
fn open_drops_partial_last_line_without_newline_and_truncates_file() {
    let tmp = tempdir().unwrap();
    drop(seed_started(tmp.path(), 2));
    let complete = file_bytes(tmp.path());
    append_raw(tmp.path(), br#"{"seq":3,"ki"#);
    assert_ne!(file_bytes(tmp.path()), complete);

    let j = Journal::open(tmp.path()).expect("a torn tail must not fail open");
    assert_eq!(j.last_seq(), 2);
    assert_eq!(seqs(&j.unacked()), vec![1, 2]);
    assert_eq!(
        file_bytes(tmp.path()),
        complete,
        "file truncated to last full line"
    );
}

#[test]
fn open_drops_newline_terminated_invalid_json_as_last_line() {
    let tmp = tempdir().unwrap();
    drop(seed_started(tmp.path(), 2));
    let complete = file_bytes(tmp.path());
    append_raw(tmp.path(), b"{\"seq\":3,\"ki\n");

    let j = Journal::open(tmp.path()).expect("invalid last line is a torn write");
    assert_eq!(j.last_seq(), 2);
    assert_eq!(file_bytes(tmp.path()), complete);
}

#[test]
fn open_drops_complete_json_missing_only_its_newline_as_torn() {
    // The docs call a last line without a trailing newline incomplete, even
    // when its JSON is valid. Such a line is dropped.
    let tmp = tempdir().unwrap();
    drop(seed_started(tmp.path(), 1));
    let complete = file_bytes(tmp.path());
    let line = json_line(&full_record(2, EventKind::Started, "job-1", 2, 1));
    append_raw(tmp.path(), line.trim_end_matches('\n').as_bytes());

    let j = Journal::open(tmp.path()).expect("open");
    assert_eq!(j.last_seq(), 1);
    assert_eq!(file_bytes(tmp.path()), complete);
}

#[test]
fn open_drops_tail_cut_inside_a_multibyte_utf8_character() {
    let tmp = tempdir().unwrap();
    drop(seed_started(tmp.path(), 2));
    let complete = file_bytes(tmp.path());
    let line =
        "{\"seq\":3,\"error\":{\"code\":\"x\",\"summary\":\"磁碟已滿\",\"retryable\":true}}\n";
    let bytes = line.as_bytes();
    let cut = line.find('磁').expect("character present") + 1;
    let tail = &bytes[..cut];
    assert!(
        std::str::from_utf8(tail).is_err(),
        "tail must split a character"
    );
    append_raw(tmp.path(), tail);

    let j = Journal::open(tmp.path()).expect("a torn UTF-8 tail must not fail open");
    assert_eq!(j.last_seq(), 2);
    assert_eq!(file_bytes(tmp.path()), complete);
}

#[test]
fn open_with_only_a_torn_line_truncates_to_empty_and_next_seq_is_1() {
    let tmp = tempdir().unwrap();
    write_raw(tmp.path(), br#"{"seq":1,"ki"#);
    let mut j = Journal::open(tmp.path()).expect("open");
    assert_eq!(j.last_seq(), 0);
    assert!(file_bytes(tmp.path()).is_empty());
    let r = j.append(new_rec(EventKind::Started, "job-1", 1)).unwrap();
    assert_eq!(r.seq, 1);
}

#[test]
fn append_after_torn_tail_starts_on_clean_line_and_reopen_is_complete() {
    let tmp = tempdir().unwrap();
    drop(seed_started(tmp.path(), 2));
    append_raw(tmp.path(), br#"{"seq":3,"ki"#);

    let mut j = Journal::open(tmp.path()).unwrap();
    let r = j.append(new_rec(EventKind::Completed, "job-1", 1)).unwrap();
    assert_eq!(r.seq, 3);
    drop(j);

    let j = Journal::open(tmp.path()).expect("reopen after repair");
    assert_eq!(j.last_seq(), 3);
    assert!(matches!(j.step_state("job-1", 1), StepState::Completed(r) if r.seq == 3));
    let text = String::from_utf8(file_bytes(tmp.path())).unwrap();
    assert!(text.ends_with('\n'));
    assert_eq!(text.lines().count(), 3);
    for line in text.lines() {
        serde_json::from_str::<Record>(line).expect("every line is a complete record");
    }
}

#[test]
fn open_fails_when_acked_is_beyond_last_complete_seq_after_torn_tail() {
    let tmp = tempdir().unwrap();
    drop(seed_started(tmp.path(), 2));
    append_raw(tmp.path(), br#"{"seq":3,"ki"#);
    fs::write(acked_file(tmp.path()), "3").unwrap();
    open_err(tmp.path());
}

// ---------- corruption that is not a torn tail ----------

#[test]
fn open_fails_on_invalid_middle_line_naming_its_line_number() {
    let tmp = tempdir().unwrap();
    let mut text = String::new();
    text += &json_line(&full_record(1, EventKind::Started, "job-1", 1, 1));
    text += "{not json}\n";
    text += &json_line(&full_record(2, EventKind::Started, "job-1", 2, 1));
    write_raw(tmp.path(), text.as_bytes());
    let before = file_bytes(tmp.path());

    let msg = open_err(tmp.path());
    assert!(
        mentions_number(&msg, tmp.path(), 2),
        "error must name line 2: {msg}"
    );
    assert_eq!(
        file_bytes(tmp.path()),
        before,
        "corrupt file must be left untouched"
    );
    // Corruption is never repaired silently, so a second open fails too.
    assert!(Journal::open(tmp.path()).is_err());
}

#[test]
fn open_fails_on_invalid_first_line_naming_line_1() {
    let tmp = tempdir().unwrap();
    let mut text = String::from("garbage\n");
    text += &json_line(&full_record(1, EventKind::Started, "job-1", 1, 1));
    write_raw(tmp.path(), text.as_bytes());
    let before = file_bytes(tmp.path());

    let msg = open_err(tmp.path());
    assert!(
        mentions_number(&msg, tmp.path(), 1),
        "error must name line 1: {msg}"
    );
    assert_eq!(file_bytes(tmp.path()), before);
}

#[test]
fn open_fails_on_seq_gap_1_then_3() {
    let tmp = tempdir().unwrap();
    write_records(
        tmp.path(),
        &[
            full_record(1, EventKind::Started, "job-1", 1, 1),
            full_record(3, EventKind::Started, "job-1", 2, 1),
        ],
    );
    let before = file_bytes(tmp.path());
    open_err(tmp.path());
    assert_eq!(file_bytes(tmp.path()), before);
}

#[test]
fn open_fails_on_duplicate_seq() {
    let tmp = tempdir().unwrap();
    write_records(
        tmp.path(),
        &[
            full_record(1, EventKind::Started, "job-1", 1, 1),
            full_record(1, EventKind::Started, "job-1", 2, 1),
        ],
    );
    open_err(tmp.path());
}

#[test]
fn open_fails_when_first_seq_is_not_1() {
    let tmp = tempdir().unwrap();
    write_records(
        tmp.path(),
        &[full_record(2, EventKind::Started, "job-1", 1, 1)],
    );
    open_err(tmp.path());
}

// ---------- acked file ----------

#[test]
fn missing_acked_file_means_acked_zero() {
    let tmp = tempdir().unwrap();
    drop(seed_started(tmp.path(), 2));
    assert!(!acked_file(tmp.path()).exists());
    let j = Journal::open(tmp.path()).expect("open");
    assert_eq!(j.acked(), 0);
    assert_eq!(seqs(&j.unacked()), vec![1, 2]);
}

#[test]
fn acked_file_that_is_not_a_number_fails_open() {
    let tmp = tempdir().unwrap();
    drop(seed_started(tmp.path(), 2));
    fs::write(acked_file(tmp.path()), "abc").unwrap();
    open_err(tmp.path());
}

#[test]
fn acked_file_with_negative_number_fails_open() {
    let tmp = tempdir().unwrap();
    drop(seed_started(tmp.path(), 2));
    fs::write(acked_file(tmp.path()), "-1").unwrap();
    open_err(tmp.path());
}

#[test]
fn acked_file_beyond_u64_range_fails_open() {
    let tmp = tempdir().unwrap();
    drop(seed_started(tmp.path(), 2));
    fs::write(acked_file(tmp.path()), "18446744073709551616").unwrap();
    open_err(tmp.path());
}

#[test]
fn acked_file_larger_than_last_seq_fails_open() {
    let tmp = tempdir().unwrap();
    drop(seed_started(tmp.path(), 2));
    fs::write(acked_file(tmp.path()), "3").unwrap();
    open_err(tmp.path());
}

#[test]
fn acked_file_equal_to_last_seq_is_accepted() {
    let tmp = tempdir().unwrap();
    drop(seed_started(tmp.path(), 2));
    fs::write(acked_file(tmp.path()), "2").unwrap();
    let j = Journal::open(tmp.path()).expect("acked == last_seq is valid");
    assert_eq!(j.acked(), 2);
    assert!(j.unacked().is_empty());
}

// ---------- append: state rules ----------

#[test]
fn append_returns_record_with_next_seq_and_given_fields() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    let mut n = new_rec(EventKind::Started, "job-1", 1);
    n.control_generation = 4;
    n.evidence = vec!["probe".to_string()];
    let r = j.append(n).unwrap();
    assert_eq!(r.seq, 1);
    assert_eq!(r.kind, EventKind::Started);
    assert_eq!(r.job_id, "job-1");
    assert_eq!(r.step, 1);
    assert_eq!(r.request_id, "req-job-1-1");
    assert_eq!(r.at, at());
    assert_eq!(r.control_generation, 4);
    assert_eq!(r.evidence, vec!["probe".to_string()]);
    assert!(r.result.is_none());
    assert!(r.error.is_none());
    assert_eq!(j.last_seq(), 1);

    let r2 = j.append(new_rec(EventKind::Started, "job-1", 2)).unwrap();
    assert_eq!(r2.seq, 2);
}

#[test]
fn started_then_completed_is_accepted_and_observed() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    j.append(new_rec(EventKind::Started, "job-1", 1)).unwrap();
    let done = j.append(new_rec(EventKind::Completed, "job-1", 1)).unwrap();
    assert_eq!(done.seq, 2);
    assert_eq!(j.step_state("job-1", 1), StepState::Completed(done));
}

#[test]
fn started_then_failed_is_accepted_and_keeps_error() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    j.append(new_rec(EventKind::Started, "job-1", 1)).unwrap();
    let mut f = new_rec(EventKind::Failed, "job-1", 1);
    f.error = Some(failure("磁碟已滿"));
    let failed = j.append(f).unwrap();
    match j.step_state("job-1", 1) {
        StepState::Failed(r) => {
            assert_eq!(r, failed);
            assert_eq!(r.error, Some(failure("磁碟已滿")));
        }
        other => panic!("expected Failed, got {other:?}"),
    }
}

#[test]
fn append_started_on_interrupted_step_is_a_new_attempt_and_reports_latest_started() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    j.append(new_rec(EventKind::Started, "job-1", 1)).unwrap();
    let second = j.append(new_rec(EventKind::Started, "job-1", 1)).unwrap();
    assert_eq!(second.seq, 2);
    assert_eq!(j.last_seq(), 2);
    match j.step_state("job-1", 1) {
        StepState::Interrupted { started } => assert_eq!(started, second),
        other => panic!("expected Interrupted with the latest started, got {other:?}"),
    }
    drop(j);

    // Both started records stay in the file, and the reopened state is the same.
    let j = Journal::open(tmp.path()).unwrap();
    assert_eq!(j.last_seq(), 2);
    assert_eq!(seqs(&j.unacked()), vec![1, 2]);
    assert!(
        matches!(j.step_state("job-1", 1), StepState::Interrupted { started } if started.seq == 2)
    );
}

#[test]
fn append_started_on_interrupted_step_then_completed_is_accepted() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    j.append(new_rec(EventKind::Started, "job-1", 1)).unwrap();
    j.append(new_rec(EventKind::Started, "job-1", 1)).unwrap();
    let done = j.append(new_rec(EventKind::Completed, "job-1", 1)).unwrap();
    assert_eq!(done.seq, 3);
    assert_eq!(j.step_state("job-1", 1), StepState::Completed(done));
}

#[test]
fn append_started_twice_in_a_row_then_failed_is_accepted() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    j.append(new_rec(EventKind::Started, "job-1", 1)).unwrap();
    j.append(new_rec(EventKind::Started, "job-1", 1)).unwrap();
    let failed = j.append(new_rec(EventKind::Failed, "job-1", 1)).unwrap();
    assert_eq!(failed.seq, 3);
    assert_eq!(j.step_state("job-1", 1), StepState::Failed(failed));
}

#[test]
fn append_started_after_completed_fails_and_writes_nothing() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    j.append(new_rec(EventKind::Started, "job-1", 1)).unwrap();
    j.append(new_rec(EventKind::Completed, "job-1", 1)).unwrap();
    expect_rejected(&mut j, new_rec(EventKind::Started, "job-1", 1));
    drop(j);

    let j = Journal::open(tmp.path()).unwrap();
    assert_eq!(j.last_seq(), 2);
    assert!(matches!(j.step_state("job-1", 1), StepState::Completed(r) if r.seq == 2));
}

#[test]
fn append_started_after_failed_fails_and_writes_nothing() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    j.append(new_rec(EventKind::Started, "job-1", 1)).unwrap();
    j.append(new_rec(EventKind::Failed, "job-1", 1)).unwrap();
    expect_rejected(&mut j, new_rec(EventKind::Started, "job-1", 1));
    drop(j);

    let j = Journal::open(tmp.path()).unwrap();
    assert_eq!(j.last_seq(), 2);
    assert!(matches!(j.step_state("job-1", 1), StepState::Failed(r) if r.seq == 2));
}

#[test]
fn append_completed_for_never_started_step_fails_and_writes_nothing() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    expect_rejected(&mut j, new_rec(EventKind::Completed, "job-1", 1));
    drop(j);
    let j = Journal::open(tmp.path()).unwrap();
    assert_eq!(j.last_seq(), 0);
    assert_eq!(j.step_state("job-1", 1), StepState::NotStarted);
}

#[test]
fn append_failed_for_never_started_step_fails_and_writes_nothing() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    expect_rejected(&mut j, new_rec(EventKind::Failed, "job-1", 1));
    drop(j);
    let j = Journal::open(tmp.path()).unwrap();
    assert_eq!(j.last_seq(), 0);
    assert_eq!(j.step_state("job-1", 1), StepState::NotStarted);
}

#[test]
fn append_failed_after_completed_fails_and_writes_nothing() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    j.append(new_rec(EventKind::Started, "job-1", 1)).unwrap();
    j.append(new_rec(EventKind::Completed, "job-1", 1)).unwrap();
    expect_rejected(&mut j, new_rec(EventKind::Failed, "job-1", 1));
    drop(j);

    let j = Journal::open(tmp.path()).unwrap();
    assert_eq!(j.last_seq(), 2);
    assert!(matches!(j.step_state("job-1", 1), StepState::Completed(r) if r.seq == 2));
}

#[test]
fn append_completed_after_failed_fails_and_writes_nothing() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    j.append(new_rec(EventKind::Started, "job-1", 1)).unwrap();
    j.append(new_rec(EventKind::Failed, "job-1", 1)).unwrap();
    expect_rejected(&mut j, new_rec(EventKind::Completed, "job-1", 1));
    drop(j);

    let j = Journal::open(tmp.path()).unwrap();
    assert_eq!(j.last_seq(), 2);
    assert!(matches!(j.step_state("job-1", 1), StepState::Failed(r) if r.seq == 2));
}

#[test]
fn append_completed_after_completed_fails_and_writes_nothing() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    j.append(new_rec(EventKind::Started, "job-1", 1)).unwrap();
    j.append(new_rec(EventKind::Completed, "job-1", 1)).unwrap();
    expect_rejected(&mut j, new_rec(EventKind::Completed, "job-1", 1));
}

#[test]
fn rejected_append_does_not_consume_a_seq_number() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    j.append(new_rec(EventKind::Started, "job-1", 1)).unwrap();
    expect_rejected(&mut j, new_rec(EventKind::Completed, "job-1", 2));
    let r = j.append(new_rec(EventKind::Started, "job-1", 2)).unwrap();
    assert_eq!(r.seq, 2, "no gap left by the rejected append");
    drop(j);

    let j = Journal::open(tmp.path()).expect("file must still be contiguous");
    assert_eq!(seqs(&j.unacked()), vec![1, 2]);
}

#[test]
fn interrupted_step_stays_interrupted_after_restart_and_can_be_completed() {
    let tmp = tempdir().unwrap();
    drop(seed_started(tmp.path(), 1));

    let mut j = Journal::open(tmp.path()).unwrap();
    assert!(matches!(
        j.step_state("job-1", 1),
        StepState::Interrupted { started } if started.kind == EventKind::Started && started.seq == 1
    ));
    let done = j.append(new_rec(EventKind::Completed, "job-1", 1)).unwrap();
    assert_eq!(done.seq, 2);
    assert_eq!(j.step_state("job-1", 1), StepState::Completed(done));
}

#[test]
fn step_states_of_other_jobs_and_steps_do_not_interfere() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    j.append(new_rec(EventKind::Started, "job-a", 1)).unwrap();
    j.append(new_rec(EventKind::Started, "job-a", 2)).unwrap();
    j.append(new_rec(EventKind::Started, "job-b", 1)).unwrap();
    j.append(new_rec(EventKind::Completed, "job-a", 1)).unwrap();
    j.append(new_rec(EventKind::Failed, "job-b", 1)).unwrap();

    // job-b step 2 never started, even though job-a step 2 did.
    expect_rejected(&mut j, new_rec(EventKind::Completed, "job-b", 2));
    // job-a step 2 is interrupted, so a new start is a retry and is allowed.
    j.append(new_rec(EventKind::Started, "job-a", 2)).unwrap();
    // job-a step 1 is completed, so a start for it is refused.
    expect_rejected(&mut j, new_rec(EventKind::Started, "job-a", 1));

    assert!(matches!(j.step_state("job-a", 1), StepState::Completed(_)));
    assert!(matches!(
        j.step_state("job-a", 2),
        StepState::Interrupted { .. }
    ));
    assert!(matches!(j.step_state("job-b", 1), StepState::Failed(_)));
    assert_eq!(j.step_state("job-b", 2), StepState::NotStarted);
    assert_eq!(j.step_state("job-a", 3), StepState::NotStarted);
    assert_eq!(j.step_state("job-c", 1), StepState::NotStarted);
}

#[test]
fn step_state_interrupted_points_to_the_started_record() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    j.append(new_rec(EventKind::Started, "job-1", 1)).unwrap();
    let started = j.append(new_rec(EventKind::Started, "job-1", 2)).unwrap();
    match j.step_state("job-1", 2) {
        StepState::Interrupted { started: s } => assert_eq!(s, started),
        other => panic!("expected Interrupted, got {other:?}"),
    }
}

// ---------- ack and unacked ----------

#[test]
fn unacked_returns_all_records_in_order_before_any_ack() {
    let tmp = tempdir().unwrap();
    let j = seed_started(tmp.path(), 3);
    assert_eq!(seqs(&j.unacked()), vec![1, 2, 3]);
}

#[test]
fn ack_advances_watermark_and_unacked_returns_only_newer_records() {
    let tmp = tempdir().unwrap();
    let mut j = seed_started(tmp.path(), 3);
    j.ack(1).unwrap();
    assert_eq!(j.acked(), 1);
    assert_eq!(seqs(&j.unacked()), vec![2, 3]);
    j.ack(2).unwrap();
    assert_eq!(j.acked(), 2);
    assert_eq!(seqs(&j.unacked()), vec![3]);
}

#[test]
fn ack_backwards_or_to_same_value_is_a_successful_noop() {
    let tmp = tempdir().unwrap();
    let mut j = seed_started(tmp.path(), 3);
    j.ack(2).unwrap();
    j.ack(1).expect("backwards ack is a no-op, not an error");
    assert_eq!(j.acked(), 2);
    j.ack(2).expect("same ack is a no-op");
    j.ack(0).expect("zero is a no-op once acked");
    assert_eq!(j.acked(), 2);
    assert_eq!(seqs(&j.unacked()), vec![3]);
    drop(j);

    let j = Journal::open(tmp.path()).unwrap();
    assert_eq!(
        j.acked(),
        2,
        "a backwards ack must not lower the watermark on disk"
    );
}

#[test]
fn ack_zero_on_fresh_journal_is_a_noop() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    j.ack(0).expect("ack(0) with acked 0 is a no-op");
    assert_eq!(j.acked(), 0);
}

#[test]
fn ack_beyond_last_seq_fails_and_changes_nothing() {
    let tmp = tempdir().unwrap();
    let mut j = seed_started(tmp.path(), 2);
    j.ack(1).unwrap();
    let acked_before = fs::read_to_string(acked_file(tmp.path())).unwrap();
    assert!(j.ack(3).is_err());
    assert_eq!(j.acked(), 1);
    assert_eq!(seqs(&j.unacked()), vec![2]);
    assert_eq!(
        fs::read_to_string(acked_file(tmp.path())).unwrap(),
        acked_before
    );
    drop(j);

    let j = Journal::open(tmp.path()).unwrap();
    assert_eq!(j.acked(), 1);
}

#[test]
fn ack_on_empty_journal_beyond_zero_fails() {
    let tmp = tempdir().unwrap();
    let mut j = Journal::open(tmp.path()).unwrap();
    assert!(j.ack(1).is_err());
    assert_eq!(j.acked(), 0);
    assert!(
        !acked_file(tmp.path()).exists(),
        "a rejected ack must not write the acked file"
    );
}

#[test]
fn ack_writes_decimal_watermark_to_acked_file() {
    let tmp = tempdir().unwrap();
    let mut j = seed_started(tmp.path(), 3);
    j.ack(2).unwrap();
    let content = fs::read_to_string(acked_file(tmp.path())).unwrap();
    assert_eq!(content.trim(), "2");
}

#[test]
fn ack_leaves_only_journal_and_acked_files_behind() {
    let tmp = tempdir().unwrap();
    let mut j = seed_started(tmp.path(), 2);
    j.ack(1).unwrap();
    j.ack(2).unwrap();
    let mut names: Vec<String> = fs::read_dir(tmp.path())
        .unwrap()
        .map(|e| e.unwrap().file_name().to_string_lossy().into_owned())
        .collect();
    names.sort();
    assert_eq!(
        names,
        vec![ACKED_FILE.to_string(), JOURNAL_FILE.to_string()]
    );
}

#[test]
fn unacked_is_empty_once_everything_is_acked_and_after_reopen() {
    let tmp = tempdir().unwrap();
    let mut j = seed_started(tmp.path(), 2);
    j.ack(2).unwrap();
    assert!(j.unacked().is_empty());
    drop(j);
    let j = Journal::open(tmp.path()).unwrap();
    assert!(j.unacked().is_empty());
    assert_eq!(j.acked(), 2);
}

#[test]
fn missing_acked_file_with_records_leaves_all_unacked() {
    let tmp = tempdir().unwrap();
    drop(seed_started(tmp.path(), 2));
    let j = Journal::open(tmp.path()).unwrap();
    assert_eq!(j.acked(), 0);
    assert_eq!(seqs(&j.unacked()), vec![1, 2]);
}

// ---------- max_control_generation ----------

#[test]
fn max_control_generation_is_zero_for_empty_journal() {
    let tmp = tempdir().unwrap();
    let j = Journal::open(tmp.path()).unwrap();
    assert_eq!(j.max_control_generation(), 0);
}

#[test]
fn max_control_generation_is_the_maximum_not_the_last_record() {
    let tmp = tempdir().unwrap();
    write_records(
        tmp.path(),
        &[
            full_record(1, EventKind::Started, "job-1", 1, 2),
            full_record(2, EventKind::Completed, "job-1", 1, 5),
            full_record(3, EventKind::Started, "job-1", 2, 3),
        ],
    );
    let j = Journal::open(tmp.path()).unwrap();
    assert_eq!(j.max_control_generation(), 5);
}

#[test]
fn max_control_generation_keeps_large_values_across_reopen() {
    let tmp = tempdir().unwrap();
    let big: u64 = 1 << 40;
    let mut j = Journal::open(tmp.path()).unwrap();
    let mut n = new_rec(EventKind::Started, "job-1", 1);
    n.control_generation = big;
    j.append(n).unwrap();
    j.append(new_rec(EventKind::Started, "job-1", 2)).unwrap();
    drop(j);
    let j = Journal::open(tmp.path()).unwrap();
    assert_eq!(j.max_control_generation(), big);
}

// ---------- Record::to_message ----------

#[test]
fn to_message_is_an_event_with_the_same_seq_kind_and_ids() {
    let rec = full_record(7, EventKind::Completed, "job-x", 3, 2);
    match rec.to_message() {
        Message::Event {
            seq,
            kind,
            job_id,
            step,
            request_id,
            at: when,
            evidence,
            result,
            error,
        } => {
            assert_eq!(seq, 7);
            assert_eq!(kind, EventKind::Completed);
            assert_eq!(job_id, "job-x");
            assert_eq!(step, 3);
            assert_eq!(request_id, "req-job-x-3");
            assert_eq!(when, at());
            assert!(evidence.is_empty());
            assert!(result.is_none());
            assert!(error.is_none());
        }
        other => panic!("expected Message::Event, got {other:?}"),
    }
}

#[test]
fn to_message_maps_each_event_kind() {
    for kind in [EventKind::Started, EventKind::Completed, EventKind::Failed] {
        let rec = full_record(1, kind, "job-1", 1, 1);
        match rec.to_message() {
            Message::Event { kind: got, .. } => assert_eq!(got, kind),
            other => panic!("expected Message::Event, got {other:?}"),
        }
    }
}

#[test]
fn to_message_copies_evidence_result_and_error() {
    let mut rec = full_record(4, EventKind::Failed, "job-2", 5, 9);
    rec.evidence = vec!["exit=2".to_string(), "stderr".to_string()];
    rec.result = Some(serde_json::json!({"changed": false}));
    rec.error = Some(failure("磁碟已滿"));
    match rec.to_message() {
        Message::Event {
            evidence,
            result,
            error,
            ..
        } => {
            assert_eq!(evidence, rec.evidence);
            assert_eq!(result, rec.result);
            assert_eq!(error, rec.error);
        }
        other => panic!("expected Message::Event, got {other:?}"),
    }
}

#[test]
fn to_message_round_trips_through_encode_and_decode_with_all_fields() {
    let mut rec = full_record(12, EventKind::Failed, "job-rt", 2, 3);
    rec.at = datetime!(2026-11-08 01:02:03.5 UTC);
    rec.evidence = vec!["a".to_string(), "中文證據".to_string()];
    rec.result = Some(serde_json::json!({"n": 1, "ok": true}));
    rec.error = Some(failure("磁碟已滿"));
    let msg = rec.to_message();
    let wire = hazelminx_protocol::encode(&msg);
    assert!(wire.contains("\"type\":\"event\""), "wire: {wire}");
    let back = hazelminx_protocol::decode(&wire).expect("decode what encode produced");
    assert_eq!(back, msg);
}

#[test]
fn to_message_round_trips_when_optional_fields_are_empty() {
    let rec = full_record(1, EventKind::Started, "job-min", 1, 1);
    let msg = rec.to_message();
    let wire = hazelminx_protocol::encode(&msg);
    let back = hazelminx_protocol::decode(&wire).expect("decode what encode produced");
    assert_eq!(back, msg);
}
