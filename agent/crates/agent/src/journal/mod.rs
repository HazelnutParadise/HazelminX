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
//!
//! Compaction (ticket 122) keeps the file and the memory bounded: a job
//! whose records are all acknowledged and whose steps all finished is
//! *settled*; settled jobs older than [`RETAIN_AGE`], or beyond the newest
//! [`RETAIN_MAX_RECORDS`] settled records, move to `dir/journal.archive.jsonl`.
//! The journal is then rewritten through a temporary file (write, fsync,
//! rename, fsync the directory) with a first line that carries what the
//! moved records leave behind: the highest seq and control generation. A
//! crash at any point leaves either the old or the new journal, never a
//! mix; records copied to the archive by an unfinished compaction are not
//! copied twice. The archive is an audit copy the agent never reads to
//! answer the Core; it is rotated to `journal.archive.1.jsonl` when it
//! reaches [`ARCHIVE_MAX_BYTES`], and the older generation is dropped.

use anyhow::{Context, Result, bail};
use hazelminx_protocol::{EventKind, Message, StepFailure};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::collections::{HashMap, HashSet};
use std::fs::{self, File, OpenOptions};
use std::io::{ErrorKind, Read, Write};
use std::os::unix::fs::{DirBuilderExt, OpenOptionsExt};
use std::path::{Path, PathBuf};
use std::time::Instant;
use time::OffsetDateTime;
use tracing::warn;

pub const JOURNAL_FILE: &str = "journal.jsonl";
pub const ACKED_FILE: &str = "acked";
/// Staging name for the acked watermark; renamed over `ACKED_FILE`.
const ACKED_TMP_FILE: &str = "acked.tmp";
/// Staging name for a compacted journal; renamed over `JOURNAL_FILE`.
pub const JOURNAL_TMP_FILE: &str = "journal.jsonl.tmp";
pub const ARCHIVE_FILE: &str = "journal.archive.jsonl";
/// The previous archive generation, replaced at each rotation.
pub const ARCHIVE_PREV_FILE: &str = "journal.archive.1.jsonl";
/// Settled jobs stay in the journal at least this long after their newest
/// record, so the Core can still reconcile them after a long outage.
pub const RETAIN_AGE: time::Duration = time::Duration::days(7);
/// At most this many settled records stay in the journal, newest jobs first.
pub const RETAIN_MAX_RECORDS: usize = 10_000;
/// The archive is rotated once it reaches this size; with the previous
/// generation, the archive takes at most about twice this on disk.
pub const ARCHIVE_MAX_BYTES: u64 = 16 * 1024 * 1024;
/// How often `ack` tries to compact.
const COMPACT_INTERVAL: std::time::Duration = std::time::Duration::from_secs(3600);

/// The clock compaction uses for [`RETAIN_AGE`]; fixed in tests so records
/// written with fixed dates do not age out as the calendar moves.
#[cfg(not(test))]
fn clock() -> OffsetDateTime {
    OffsetDateTime::now_utc()
}

#[cfg(test)]
const TEST_NOW: OffsetDateTime = time::macros::datetime!(2026-11-08 12:00:00 UTC);

#[cfg(test)]
fn clock() -> OffsetDateTime {
    TEST_NOW
}

/// Where a test makes compaction stop as if the power went out.
#[cfg(test)]
#[derive(Debug, Clone, Copy, PartialEq)]
enum CrashPoint {
    /// Half of the archive batch is written.
    ArchiveTorn,
    /// The archive batch is fsynced; the journal is not rewritten yet.
    AfterArchive,
    /// Half of the temporary journal is written.
    TmpTorn,
    /// The temporary journal is fsynced but not renamed.
    AfterTmp,
    /// The temporary journal is renamed; the directory is not fsynced.
    AfterRename,
    /// A full archive was rotated; nothing new is archived yet.
    AfterRotate,
}

/// First line of a compacted journal: what the moved records leave behind.
#[derive(Debug, Clone, Copy, Default, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct Header {
    compacted: Floor,
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct Floor {
    /// Highest seq moved out (0 when nothing was).
    through_seq: u64,
    /// Highest control generation among every record moved out.
    max_control_generation: u64,
}

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
    /// Records still in `journal.jsonl`, in seq order.
    records: Vec<Record>,
    acked: u64,
    floor: Floor,
    /// When `ack` next tries to compact.
    compact_due: Instant,
    /// False until a compaction succeeds in this process: an earlier one
    /// may have stopped after copying records to the archive.
    archive_clean: bool,
    #[cfg(test)]
    crash_at: Option<CrashPoint>,
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
    /// Opens (or creates) the journal in `dir`, then compacts it.
    ///
    /// Expected results:
    /// - missing `dir` is created with mode 0700; a new journal file is
    ///   created with mode 0600; an empty journal has `last_seq() == 0` and
    ///   `acked() == 0`;
    /// - reopening returns the same records, `acked` and generation (minus
    ///   what compaction moves out, see [`Journal::compact`]);
    /// - a last line that is incomplete (no trailing newline, or not valid
    ///   JSON *and* it is the last line) is a torn write: it is dropped and
    ///   the file is truncated to the end of the last complete line, so the
    ///   next append starts on a clean line;
    /// - an invalid line that is *not* the last line → error naming the line
    ///   number; the file is left untouched (corruption is never repaired
    ///   silently);
    /// - a compaction header is accepted only as the first line;
    /// - seqs not strictly increasing, or seqs above the header's
    ///   `throughSeq` not exactly `throughSeq + 1`, `+ 2`, … (1, 2, 3, …
    ///   without a header) → error;
    /// - `acked` file missing → 0; `acked` not a decimal u64, larger than
    ///   the last seq, or below the header's `throughSeq` → error;
    /// - a leftover `journal.jsonl.tmp` (a compaction cut off before its
    ///   rename) is deleted; the journal it would have replaced stays;
    /// - a failed compaction is logged and does not fail `open`.
    pub fn open(dir: &Path) -> Result<Journal> {
        let mut journal = Journal::load(dir)?;
        if let Err(e) = journal.compact(clock()) {
            warn!(error = %format!("{e:#}"), "journal compaction failed; the journal is unchanged");
        }
        Ok(journal)
    }

    /// `open` without the compaction.
    fn load(dir: &Path) -> Result<Journal> {
        if !dir.is_dir() {
            fs::DirBuilder::new()
                .recursive(true)
                .mode(0o700)
                .create(dir)
                .with_context(|| format!("create journal directory {}", dir.display()))?;
            sync_dir(parent_of(dir))?;
        }

        let tmp = dir.join(JOURNAL_TMP_FILE);
        match fs::remove_file(&tmp) {
            Ok(()) => sync_dir(dir)?,
            Err(e) if e.kind() == ErrorKind::NotFound => {}
            Err(e) => return Err(e).with_context(|| format!("remove {}", tmp.display())),
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
        let (floor, records, valid_len) = parse_journal(&path, &bytes)?;
        let last_seq = records
            .last()
            .map_or(floor.through_seq, |r| r.seq.max(floor.through_seq));
        let acked = read_acked(dir, last_seq)?;
        if acked < floor.through_seq {
            bail!(
                "{}: acked watermark {acked} is below seq {} that compaction already moved out",
                dir.join(ACKED_FILE).display(),
                floor.through_seq
            );
        }

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
            floor,
            compact_due: Instant::now() + COMPACT_INTERVAL,
            archive_clean: false,
            #[cfg(test)]
            crash_at: None,
        })
    }

    /// Moves settled jobs out of the journal and into the archive; returns
    /// how many records moved.
    ///
    /// Expected results:
    /// - a job moves only as a whole, and only when every one of its
    ///   records has `seq <= acked()` and every one of its steps ends with
    ///   `completed` or `failed`; an interrupted step or an unacknowledged
    ///   record keeps the whole job;
    /// - a settled job moves when its newest record is at or before
    ///   `now - RETAIN_AGE`, or when newer settled jobs already fill
    ///   `RETAIN_MAX_RECORDS`; the rest stay and still answer `step_state`;
    /// - nothing to move → `Ok(0)` and no file is touched;
    /// - moved records leave memory and `journal.jsonl`, and are appended to
    ///   the archive (mode 0600) with every field intact; `last_seq`,
    ///   `acked` and `max_control_generation` do not change;
    /// - the archive is rotated before appending once it is at least
    ///   `ARCHIVE_MAX_BYTES`;
    /// - after an earlier compaction was cut off, records already in the
    ///   archive are not appended again, and a torn last archive line is cut
    ///   off first; an invalid archive line elsewhere → error, nothing
    ///   changes;
    /// - an error before the rename leaves the journal file and memory as
    ///   they were.
    pub fn compact(&mut self, now: OffsetDateTime) -> Result<usize> {
        let movable = self.movable_jobs(now);
        if movable.is_empty() {
            return Ok(0);
        }
        let (moved, kept): (Vec<Record>, Vec<Record>) = self
            .records
            .iter()
            .cloned()
            .partition(|r| movable.contains(r.job_id.as_str()));
        let floor = Floor {
            through_seq: moved
                .iter()
                .map(|r| r.seq)
                .fold(self.floor.through_seq, u64::max),
            max_control_generation: moved
                .iter()
                .map(|r| r.control_generation)
                .fold(self.floor.max_control_generation, u64::max),
        };

        if let Err(e) = self.archive(&moved) {
            self.archive_clean = false;
            return Err(e);
        }
        if let Err(e) = self.replace_journal(floor, &kept) {
            self.archive_clean = false;
            return Err(e);
        }
        // The new file is in place: memory follows it even if the directory
        // fsync below fails.
        self.records = kept;
        self.floor = floor;
        #[cfg(test)]
        self.crash(CrashPoint::AfterRename)?;
        sync_dir(&self.dir)?;
        self.archive_clean = true;
        Ok(moved.len())
    }

    /// Job ids that `compact` moves out at `now`.
    fn movable_jobs(&self, now: OffsetDateTime) -> HashSet<&str> {
        struct Job {
            newest_seq: u64,
            newest_at: OffsetDateTime,
            count: usize,
            unacked: bool,
            open_steps: HashSet<u32>,
        }
        let mut jobs: HashMap<&str, Job> = HashMap::new();
        for r in &self.records {
            let job = jobs.entry(r.job_id.as_str()).or_insert(Job {
                newest_seq: 0,
                newest_at: r.at,
                count: 0,
                unacked: false,
                open_steps: HashSet::new(),
            });
            job.newest_seq = r.seq;
            job.newest_at = job.newest_at.max(r.at);
            job.count += 1;
            job.unacked |= r.seq > self.acked;
            match r.kind {
                EventKind::Started => job.open_steps.insert(r.step),
                EventKind::Completed | EventKind::Failed => job.open_steps.remove(&r.step),
            };
        }

        let mut settled: Vec<(&str, Job)> = jobs
            .into_iter()
            .filter(|(_, j)| !j.unacked && j.open_steps.is_empty())
            .collect();
        settled.sort_by_key(|(_, job)| std::cmp::Reverse(job.newest_seq));
        let cutoff = now - RETAIN_AGE;
        let mut kept = 0usize;
        let mut movable = HashSet::new();
        for (id, job) in settled {
            if job.newest_at > cutoff && kept + job.count <= RETAIN_MAX_RECORDS {
                kept += job.count;
            } else {
                movable.insert(id);
            }
        }
        movable
    }

    /// Appends `moved` to the archive and fsyncs it, rotating a full
    /// archive first and skipping records an interrupted compaction
    /// already copied.
    fn archive(&mut self, moved: &[Record]) -> Result<()> {
        let cur = self.dir.join(ARCHIVE_FILE);
        let prev = self.dir.join(ARCHIVE_PREV_FILE);

        let mut present = HashSet::new();
        if !self.archive_clean {
            scan_archive(&prev, false, &mut present)?;
            scan_archive(&cur, true, &mut present)?;
        }

        let size = match fs::metadata(&cur) {
            Ok(m) => m.len(),
            Err(e) if e.kind() == ErrorKind::NotFound => 0,
            Err(e) => return Err(e).with_context(|| format!("stat {}", cur.display())),
        };
        if size >= ARCHIVE_MAX_BYTES {
            fs::rename(&cur, &prev)
                .with_context(|| format!("rotate {} to {}", cur.display(), prev.display()))?;
            sync_dir(&self.dir)?;
            #[cfg(test)]
            self.crash(CrashPoint::AfterRotate)?;
        }

        let mut batch = String::new();
        for r in moved.iter().filter(|r| !present.contains(&r.seq)) {
            batch.push_str(&serde_json::to_string(r).context("serialize archive record")?);
            batch.push('\n');
        }
        if batch.is_empty() {
            return Ok(());
        }

        let existed = cur.exists();
        let mut file = OpenOptions::new()
            .append(true)
            .create(true)
            .mode(0o600)
            .open(&cur)
            .with_context(|| format!("open archive {}", cur.display()))?;
        if !existed {
            sync_dir(&self.dir)?;
        }
        #[cfg(test)]
        if self.crash_at == Some(CrashPoint::ArchiveTorn) {
            file.write_all(&batch.as_bytes()[..batch.len() / 2])?;
            bail!("simulated crash at {:?}", CrashPoint::ArchiveTorn);
        }
        let before = file
            .metadata()
            .with_context(|| format!("stat archive {}", cur.display()))?
            .len();
        let written = file
            .write_all(batch.as_bytes())
            .and_then(|()| file.sync_data());
        if let Err(e) = written {
            let _ = file.set_len(before);
            let _ = file.sync_data();
            return Err(e).with_context(|| format!("append to archive {}", cur.display()));
        }
        #[cfg(test)]
        self.crash(CrashPoint::AfterArchive)?;
        Ok(())
    }

    /// Writes `floor` and `kept` to the temporary journal, fsyncs it and
    /// renames it over `journal.jsonl`. The directory fsync is the caller's.
    fn replace_journal(&self, floor: Floor, kept: &[Record]) -> Result<()> {
        let mut text = serde_json::to_string(&Header { compacted: floor })
            .context("serialize journal header")?;
        text.push('\n');
        for r in kept {
            text.push_str(&serde_json::to_string(r).context("serialize journal record")?);
            text.push('\n');
        }

        let tmp = self.dir.join(JOURNAL_TMP_FILE);
        let dst = self.dir.join(JOURNAL_FILE);
        #[cfg(test)]
        if self.crash_at == Some(CrashPoint::TmpTorn) {
            fs::write(&tmp, &text.as_bytes()[..text.len() / 2])?;
            bail!("simulated crash at {:?}", CrashPoint::TmpTorn);
        }
        let staged = stage_file(&tmp, text.as_bytes());
        #[cfg(test)]
        if staged.is_ok() {
            self.crash(CrashPoint::AfterTmp)?;
        }
        let renamed = staged.and_then(|()| {
            fs::rename(&tmp, &dst)
                .with_context(|| format!("rename {} to {}", tmp.display(), dst.display()))
        });
        if renamed.is_err() {
            let _ = fs::remove_file(&tmp);
        }
        renamed
    }

    #[cfg(test)]
    fn crash(&self, point: CrashPoint) -> Result<()> {
        if self.crash_at == Some(point) {
            bail!("simulated crash at {point:?}");
        }
        Ok(())
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
    /// the new watermark. Once `COMPACT_INTERVAL` has passed since the last
    /// try (or since `open`), a stored watermark is followed by a
    /// compaction; its failure is logged and does not fail the ack.
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

        if Instant::now() >= self.compact_due {
            self.compact_due = Instant::now() + COMPACT_INTERVAL;
            if let Err(e) = self.compact(clock()) {
                warn!(error = %format!("{e:#}"), "journal compaction failed; will try again later");
            }
        }
        Ok(())
    }

    pub fn acked(&self) -> u64 {
        self.acked
    }

    /// Highest seq ever journaled, including records compaction moved out.
    pub fn last_seq(&self) -> u64 {
        self.records.last().map_or(self.floor.through_seq, |r| {
            r.seq.max(self.floor.through_seq)
        })
    }

    /// Highest `control_generation` in any record ever journaled, including
    /// records compaction moved out (0 when empty). The agent refuses steps
    /// with a lower generation (stale controller).
    pub fn max_control_generation(&self) -> u64 {
        self.records
            .iter()
            .map(|r| r.control_generation)
            .fold(self.floor.max_control_generation, u64::max)
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

/// Parses the journal bytes. Returns the compaction floor (from the header
/// line, or zero without one), the records and the byte length of the valid
/// prefix; anything after that prefix is a torn write to be cut off.
fn parse_journal(path: &Path, bytes: &[u8]) -> Result<(Floor, Vec<Record>, usize)> {
    let mut floor = Floor::default();
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
        let text = std::str::from_utf8(&rest[..nl]).map_err(|e| e.to_string());
        if line_no == 1
            && let Some(header) = text
                .as_ref()
                .ok()
                .and_then(|s| serde_json::from_str::<Header>(s).ok())
        {
            floor = header.compacted;
            valid_len = end;
            start = end;
            continue;
        }
        let parsed =
            text.and_then(|s| serde_json::from_str::<Record>(s).map_err(|e| e.to_string()));
        let rec = match parsed {
            Ok(rec) => rec,
            // An invalid last line that was newline-terminated is still a torn write.
            Err(_) if end == bytes.len() => break,
            Err(e) => bail!(
                "{}: line {line_no} is not a valid journal record: {e}",
                path.display()
            ),
        };

        // Below the floor, compaction leaves gaps; above it, none.
        let prev = records.last().map_or(0, |r| r.seq);
        if rec.seq > floor.through_seq {
            let expected = prev.max(floor.through_seq) + 1;
            if rec.seq != expected {
                bail!(
                    "{}: line {line_no} has seq {}, expected {expected}",
                    path.display(),
                    rec.seq
                );
            }
        } else if rec.seq <= prev {
            bail!(
                "{}: line {line_no} has seq {}, expected more than {prev}",
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
    Ok((floor, records, valid_len))
}

/// Adds the seqs in an archive file to `present`. A missing file adds
/// nothing; blank lines are skipped; a torn last line is ignored, and cut
/// off when `truncate_torn` (the archive about to be appended to). Any
/// other invalid line → error.
fn scan_archive(path: &Path, truncate_torn: bool, present: &mut HashSet<u64>) -> Result<()> {
    let bytes = match fs::read(path) {
        Ok(b) => b,
        Err(e) if e.kind() == ErrorKind::NotFound => return Ok(()),
        Err(e) => return Err(e).with_context(|| format!("read archive {}", path.display())),
    };
    let mut valid_len = 0;
    let mut start = 0;
    let mut line_no = 0usize;
    while start < bytes.len() {
        line_no += 1;
        let rest = &bytes[start..];
        let Some(nl) = rest.iter().position(|&b| b == b'\n') else {
            break;
        };
        let end = start + nl + 1;
        if nl > 0 {
            let parsed = std::str::from_utf8(&rest[..nl])
                .map_err(|e| e.to_string())
                .and_then(|s| serde_json::from_str::<Record>(s).map_err(|e| e.to_string()));
            match parsed {
                Ok(rec) => {
                    present.insert(rec.seq);
                }
                Err(_) if end == bytes.len() => break,
                Err(e) => bail!(
                    "{}: line {line_no} is not a valid archive record: {e}",
                    path.display()
                ),
            }
        }
        valid_len = end;
        start = end;
    }
    if truncate_torn && valid_len < bytes.len() {
        let file = OpenOptions::new()
            .write(true)
            .open(path)
            .with_context(|| format!("open archive {}", path.display()))?;
        file.set_len(valid_len as u64)
            .with_context(|| format!("truncate torn tail of {}", path.display()))?;
        file.sync_all()
            .with_context(|| format!("fsync {} after truncate", path.display()))?;
    }
    Ok(())
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
    if let Err(e) = stage_file(&tmp, seq.to_string().as_bytes()) {
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

/// Creates `tmp` with mode 0600 (replacing any earlier one), writes `bytes`
/// and fsyncs it.
fn stage_file(tmp: &Path, bytes: &[u8]) -> Result<()> {
    match fs::remove_file(tmp) {
        Ok(()) => {}
        Err(e) if e.kind() == ErrorKind::NotFound => {}
        Err(e) => return Err(e).with_context(|| format!("remove {}", tmp.display())),
    }
    let mut file = OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(tmp)
        .with_context(|| format!("create {}", tmp.display()))?;
    file.write_all(bytes)
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
mod compaction_tests;
#[cfg(test)]
mod tests;
