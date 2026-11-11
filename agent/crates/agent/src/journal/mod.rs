//! Persistent step journal (ENG-003, agent side).
//!
//! Every durable step event (`started`, `completed`, `failed`) is appended
//! to `dir/journal.jsonl` and fsynced *before* it is sent to the Core, so
//! after a crash or reboot the agent can say exactly which steps began and
//! which finished. A step with `started` but no terminal record is
//! *interrupted*: it is never reported as done and never re-run on its own.
//!
//! The journal's `seq` is the event sequence number of the protocol: it
//! starts at 1 and grows by one per record, across restarts. The Core
//! acknowledges events cumulatively; the acknowledged watermark lives in
//! `dir/acked` so unacknowledged events are re-sent after a reconnect.

use anyhow::{Context, Result, bail};
use hazelminx_protocol::{EventKind, Message, StepFailure};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::collections::HashMap;
use std::fs::{self, File, OpenOptions};
use std::io::{ErrorKind, Read, Write};
use std::os::unix::fs::{DirBuilderExt, OpenOptionsExt};
use std::path::{Path, PathBuf};
use time::OffsetDateTime;

pub const JOURNAL_FILE: &str = "journal.jsonl";
pub const ACKED_FILE: &str = "acked";
/// Staging name for the acked watermark; renamed over `ACKED_FILE`.
const ACKED_TMP_FILE: &str = "acked.tmp";

/// One line of the journal.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Record {
    pub seq: u64,
    pub kind: EventKind,
    pub job_id: String,
    pub step: u32,
    pub request_id: String,
    #[serde(with = "time::serde::rfc3339")]
    pub at: OffsetDateTime,
    pub control_generation: u64,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub evidence: Vec<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub result: Option<Value>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub error: Option<StepFailure>,
}

/// What a caller appends; the journal assigns `seq`.
#[derive(Debug, Clone, PartialEq)]
pub struct NewRecord {
    pub kind: EventKind,
    pub job_id: String,
    pub step: u32,
    pub request_id: String,
    pub at: OffsetDateTime,
    pub control_generation: u64,
    pub evidence: Vec<String>,
    pub result: Option<Value>,
    pub error: Option<StepFailure>,
}

/// What the journal knows about one step.
#[derive(Debug, Clone, PartialEq)]
pub enum StepState {
    NotStarted,
    /// `started` without a terminal record after it. `started` is the most
    /// recent one. The runtime decides whether the step is still running in
    /// this process; the journal alone cannot.
    Interrupted {
        started: Record,
    },
    Completed(Record),
    Failed(Record),
}

pub struct Journal {
    dir: PathBuf,
    records: Vec<Record>,
    acked: u64,
}

impl Record {
    /// The protocol `event` message for this record (same seq, kind, ids,
    /// time, evidence, result and error).
    pub fn to_message(&self) -> Message {
        Message::Event {
            seq: self.seq,
            kind: self.kind,
            job_id: self.job_id.clone(),
            step: self.step,
            request_id: self.request_id.clone(),
            at: self.at,
            evidence: self.evidence.clone(),
            result: self.result.clone(),
            error: self.error.clone(),
        }
    }
}

impl Journal {
    /// Opens (or creates) the journal in `dir`.
    ///
    /// Expected results:
    /// - missing `dir` is created with mode 0700; a new journal file is
    ///   created with mode 0600; an empty journal has `last_seq() == 0` and
    ///   `acked() == 0`;
    /// - reopening returns the same records, `acked` and generation;
    /// - a last line that is incomplete (no trailing newline, or not valid
    ///   JSON *and* it is the last line) is a torn write: it is dropped and
    ///   the file is truncated to the end of the last complete line, so the
    ///   next append starts on a clean line;
    /// - an invalid line that is *not* the last line → error naming the line
    ///   number; the file is left untouched (corruption is never repaired
    ///   silently);
    /// - seqs not exactly 1, 2, 3, … → error;
    /// - `acked` file missing → 0; `acked` not a decimal u64 or larger than
    ///   the last seq → error.
    pub fn open(dir: &Path) -> Result<Journal> {
        if !dir.is_dir() {
            fs::DirBuilder::new()
                .recursive(true)
                .mode(0o700)
                .create(dir)
                .with_context(|| format!("create journal directory {}", dir.display()))?;
            sync_dir(parent_of(dir))?;
        }

        let path = dir.join(JOURNAL_FILE);
        let existed = path.exists();
        let mut file = OpenOptions::new()
            .read(true)
            .append(true)
            .create(true)
            .mode(0o600)
            .open(&path)
            .with_context(|| format!("open journal {}", path.display()))?;
        if !existed {
            sync_dir(dir)?;
        }

        let mut bytes = Vec::new();
        file.read_to_end(&mut bytes)
            .with_context(|| format!("read journal {}", path.display()))?;
        let (records, valid_len) = parse_journal(&path, &bytes)?;
        let acked = read_acked(dir, records.len() as u64)?;

        // The torn tail is removed only after every other check has passed,
        // so a file that is refused is left exactly as it was found.
        if valid_len < bytes.len() {
            file.set_len(valid_len as u64)
                .with_context(|| format!("truncate torn tail of {}", path.display()))?;
            file.sync_all()
                .with_context(|| format!("fsync {} after truncate", path.display()))?;
        }

        Ok(Journal {
            dir: dir.to_path_buf(),
            records,
            acked,
        })
    }

    /// Appends one record with `seq = last_seq() + 1`, writes it as one JSON
    /// line and fsyncs before returning.
    ///
    /// Expected results:
    /// - returns the stored record; a reopened journal contains it;
    /// - `Started` is allowed when the step is `NotStarted`, or
    ///   `Interrupted` (a new attempt of a retryable step after the agent
    ///   stopped mid-step; the earlier `started` stays in the file);
    ///   `Started` when the step is `Completed` or `Failed` → error, nothing
    ///   written. (Double starts within one process are prevented by the
    ///   caller, not by the journal.)
    /// - `Completed`/`Failed` when the step is not `Interrupted` (never
    ///   started, or already finished) → error, nothing written;
    /// - records of other (job, step) pairs do not interfere.
    pub fn append(&mut self, rec: NewRecord) -> Result<Record> {
        let last = self.last_kind(&rec.job_id, rec.step);
        if !transition_allowed(last, rec.kind) {
            bail!(
                "journal refuses {:?} for job {} step {}: the step's last record is {:?}",
                rec.kind,
                rec.job_id,
                rec.step,
                last
            );
        }

        let stored = Record {
            seq: self.last_seq() + 1,
            kind: rec.kind,
            job_id: rec.job_id,
            step: rec.step,
            request_id: rec.request_id,
            at: rec.at,
            control_generation: rec.control_generation,
            evidence: rec.evidence,
            result: rec.result,
            error: rec.error,
        };
        let mut line = serde_json::to_string(&stored).context("serialize journal record")?;
        line.push('\n');

        let path = self.dir.join(JOURNAL_FILE);
        let mut file = OpenOptions::new()
            .append(true)
            .open(&path)
            .with_context(|| format!("open journal {} for append", path.display()))?;
        let before = file
            .metadata()
            .with_context(|| format!("stat journal {}", path.display()))?
            .len();
        let written = file
            .write_all(line.as_bytes())
            .and_then(|()| file.sync_data());
        if let Err(e) = written {
            // Cut a partial line back off, so the next append starts on a clean line.
            let _ = file.set_len(before);
            let _ = file.sync_data();
            return Err(e).with_context(|| format!("append to journal {}", path.display()));
        }

        self.records.push(stored.clone());
        Ok(stored)
    }

    /// State of (job_id, step) from the records.
    pub fn step_state(&self, job_id: &str, step: u32) -> StepState {
        match self
            .records
            .iter()
            .rev()
            .find(|r| r.job_id == job_id && r.step == step)
        {
            None => StepState::NotStarted,
            Some(r) => match r.kind {
                EventKind::Started => StepState::Interrupted { started: r.clone() },
                EventKind::Completed => StepState::Completed(r.clone()),
                EventKind::Failed => StepState::Failed(r.clone()),
            },
        }
    }

    /// Kind of the step's most recent record, if it has any.
    fn last_kind(&self, job_id: &str, step: u32) -> Option<EventKind> {
        self.records
            .iter()
            .rev()
            .find(|r| r.job_id == job_id && r.step == step)
            .map(|r| r.kind)
    }

    /// Records with `seq > acked()`, oldest first.
    pub fn unacked(&self) -> Vec<Record> {
        self.records
            .iter()
            .filter(|r| r.seq > self.acked)
            .cloned()
            .collect()
    }

    /// Records the Core's cumulative acknowledgement and persists it
    /// atomically (write temp file, fsync, rename).
    ///
    /// Expected results: `seq <= acked()` is a no-op (acks never go back);
    /// `seq > last_seq()` → error and nothing changes; a reopened journal has
    /// the new watermark.
    pub fn ack(&mut self, seq: u64) -> Result<()> {
        if seq <= self.acked {
            return Ok(());
        }
        if seq > self.last_seq() {
            bail!(
                "cannot ack seq {seq}: journal {} ends at seq {}",
                self.dir.display(),
                self.last_seq()
            );
        }
        write_acked(&self.dir, seq)?;
        self.acked = seq;
        Ok(())
    }

    pub fn acked(&self) -> u64 {
        self.acked
    }

    pub fn last_seq(&self) -> u64 {
        self.records.last().map(|r| r.seq).unwrap_or(0)
    }

    /// Highest `control_generation` in any record (0 when empty). The agent
    /// refuses steps with a lower generation (stale controller).
    pub fn max_control_generation(&self) -> u64 {
        self.records
            .iter()
            .map(|r| r.control_generation)
            .max()
            .unwrap_or(0)
    }

    #[cfg(test)]
    pub fn dir(&self) -> &Path {
        &self.dir
    }

    /// (job_id, step) pairs whose last record is `started` (for the startup
    /// log). Derived from `step_state`, so it needs no slot of its own.
    pub fn interrupted_steps(&self) -> Vec<(String, u32)> {
        let mut seen = std::collections::BTreeSet::new();
        for r in &self.records {
            seen.insert((r.job_id.clone(), r.step));
        }
        seen.into_iter()
            .filter(|(j, s)| matches!(self.step_state(j, *s), StepState::Interrupted { .. }))
            .collect()
    }
}

/// Whether an event of `kind` may follow the step's last record (`None` when
/// the step has no record). A step may start again only after an unfinished
/// start (`Interrupted`); it may finish only while a start is open.
fn transition_allowed(last: Option<EventKind>, kind: EventKind) -> bool {
    matches!(
        (last, kind),
        (None | Some(EventKind::Started), EventKind::Started)
            | (
                Some(EventKind::Started),
                EventKind::Completed | EventKind::Failed
            )
    )
}

/// Parses the journal bytes. Returns the records and the byte length of the
/// valid prefix; anything after that prefix is a torn write to be cut off.
fn parse_journal(path: &Path, bytes: &[u8]) -> Result<(Vec<Record>, usize)> {
    let mut records: Vec<Record> = Vec::new();
    let mut last_kind_of: HashMap<(String, u32), EventKind> = HashMap::new();
    let mut valid_len = 0;
    let mut start = 0;
    let mut line_no = 0usize;
    while start < bytes.len() {
        line_no += 1;
        let rest = &bytes[start..];
        let Some(nl) = rest.iter().position(|&b| b == b'\n') else {
            // A last line without its newline is incomplete, whatever it contains.
            break;
        };
        let end = start + nl + 1;
        let parsed = std::str::from_utf8(&rest[..nl])
            .map_err(|e| e.to_string())
            .and_then(|s| serde_json::from_str::<Record>(s).map_err(|e| e.to_string()));
        let rec = match parsed {
            Ok(rec) => rec,
            // An invalid last line that was newline-terminated is still a torn write.
            Err(_) if end == bytes.len() => break,
            Err(e) => bail!(
                "{}: line {line_no} is not a valid journal record: {e}",
                path.display()
            ),
        };

        let expected = records.len() as u64 + 1;
        if rec.seq != expected {
            bail!(
                "{}: line {line_no} has seq {}, expected {expected}",
                path.display(),
                rec.seq
            );
        }
        let key = (rec.job_id.clone(), rec.step);
        let last = last_kind_of.get(&key).copied();
        if !transition_allowed(last, rec.kind) {
            bail!(
                "{}: line {line_no} ({:?} for job {} step {}) does not follow the step's previous record ({:?})",
                path.display(),
                rec.kind,
                rec.job_id,
                rec.step,
                last
            );
        }
        last_kind_of.insert(key, rec.kind);
        records.push(rec);
        valid_len = end;
        start = end;
    }
    Ok((records, valid_len))
}

/// Reads the acked watermark. A missing file means 0; the value must be a
/// plain decimal number no larger than `last_seq`.
fn read_acked(dir: &Path, last_seq: u64) -> Result<u64> {
    let path = dir.join(ACKED_FILE);
    let text = match fs::read_to_string(&path) {
        Ok(text) => text,
        Err(e) if e.kind() == ErrorKind::NotFound => return Ok(0),
        Err(e) => return Err(e).with_context(|| format!("read {}", path.display())),
    };
    let digits = text.trim();
    if digits.is_empty() || !digits.bytes().all(|b| b.is_ascii_digit()) {
        bail!(
            "{}: acked watermark is not a decimal number",
            path.display()
        );
    }
    let acked: u64 = digits
        .parse()
        .with_context(|| format!("{}: acked watermark is out of range", path.display()))?;
    if acked > last_seq {
        bail!(
            "{}: acked watermark {acked} is beyond the last seq {last_seq}",
            path.display()
        );
    }
    Ok(acked)
}

/// Writes the watermark to a temporary file, fsyncs it, renames it over the
/// acked file and fsyncs the directory, so a crash leaves either the old or
/// the new watermark.
fn write_acked(dir: &Path, seq: u64) -> Result<()> {
    let tmp = dir.join(ACKED_TMP_FILE);
    let dst = dir.join(ACKED_FILE);
    if let Err(e) = stage_acked(&tmp, seq) {
        let _ = fs::remove_file(&tmp);
        return Err(e);
    }
    if let Err(e) = fs::rename(&tmp, &dst)
        .with_context(|| format!("rename {} to {}", tmp.display(), dst.display()))
    {
        let _ = fs::remove_file(&tmp);
        return Err(e);
    }
    sync_dir(dir)
}

fn stage_acked(tmp: &Path, seq: u64) -> Result<()> {
    let mut file = OpenOptions::new()
        .write(true)
        .create(true)
        .truncate(true)
        .mode(0o600)
        .open(tmp)
        .with_context(|| format!("create {}", tmp.display()))?;
    file.write_all(seq.to_string().as_bytes())
        .with_context(|| format!("write {}", tmp.display()))?;
    file.sync_all()
        .with_context(|| format!("fsync {}", tmp.display()))?;
    Ok(())
}

fn sync_dir(dir: &Path) -> Result<()> {
    File::open(dir)
        .and_then(|d| d.sync_all())
        .with_context(|| format!("fsync directory {}", dir.display()))
}

/// The parent of `dir`, using `.` for a bare relative name.
fn parent_of(dir: &Path) -> &Path {
    match dir.parent() {
        Some(p) if !p.as_os_str().is_empty() => p,
        _ => Path::new("."),
    }
}

#[cfg(test)]
mod tests;
