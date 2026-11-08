//! The ledger on disk: a root-only directory holding `meta.json`, the
//! append-only `ledger.jsonl` and `backups/`.
//!
//! Each line is `{"hash":"<hex>","entry":{…}}` where `hash` is the SHA-256 of
//! the entry bytes exactly as written, and every entry carries the previous
//! line's hash (`prev_hash`). The first entry chains to
//! `sha256("hazelminx-ledger-v1\n" + ledger_id)`, so lines cannot be moved
//! between hosts. Readers verify by hashing the raw entry bytes; they never
//! re-serialize, so Core (Go, `json.RawMessage`) can verify the same way.
//!
//! Appends are one `write` plus `fdatasync` under an exclusive file lock.
//! A final line without its newline is a torn write from a crash; the change
//! it describes never ran (intents are synced before the change), so `open`
//! cuts it off. Any other damage refuses to open: the executor then refuses
//! every host change until an operator looks.

use super::entry::{Entry, Ownership, Phase, Target, TargetState};
use super::sync::{Head, LedgerBatch};
use serde::{Deserialize, Serialize};
use serde_json::value::RawValue;
use sha2::{Digest, Sha256};
use std::collections::BTreeMap;
use std::fs::{File, OpenOptions};
use std::io::{self, Read, Write};
use std::os::unix::fs::{DirBuilderExt, FileExt, MetadataExt, OpenOptionsExt};
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use time::OffsetDateTime;

pub const DEFAULT_LEDGER_DIR: &str = "/var/lib/hazelminx/ledger";
pub const LEDGER_FILE: &str = "ledger.jsonl";
pub const META_FILE: &str = "meta.json";
pub const BACKUP_DIR: &str = "backups";
pub const FORMAT_VERSION: u32 = 1;
/// Most lines one export returns.
pub const MAX_EXPORT_LINES: usize = 500;

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Meta {
    pub format: u32,
    /// Random 128-bit hex id, fixed for the life of this host's ledger.
    pub ledger_id: String,
    #[serde(with = "time::serde::rfc3339")]
    pub created_at: OffsetDateTime,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum LedgerError {
    /// Directory or file ownership/permissions would let a non-root user
    /// read or rewrite the ledger.
    Insecure(String),
    /// Hash chain, sequence or format check failed at `line` (1-based).
    Integrity {
        line: usize,
        reason: String,
    },
    /// Another process holds the ledger.
    Locked,
    Io(String),
    /// An earlier append failed; the file may hold a partial line until it
    /// is reopened.
    Poisoned(String),
    /// Caller asked for something the ledger cannot answer.
    Request(String),
}

impl std::fmt::Display for LedgerError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            LedgerError::Insecure(m) => write!(f, "ledger is not root-only: {m}"),
            LedgerError::Integrity { line, reason } => {
                write!(f, "ledger integrity check failed at line {line}: {reason}")
            }
            LedgerError::Locked => f.write_str("ledger is locked by another process"),
            LedgerError::Io(m) => write!(f, "ledger io: {m}"),
            LedgerError::Poisoned(m) => write!(f, "ledger stopped after a failed write: {m}"),
            LedgerError::Request(m) => f.write_str(m),
        }
    }
}

impl std::error::Error for LedgerError {}

fn io_err(context: &str, path: &Path, e: io::Error) -> LedgerError {
    LedgerError::Io(format!("{context} {}: {e}", path.display()))
}

/// Who may own the ledger and how far up the ancestor check goes.
/// Production is [`Security::root`]; tests use their own uid and a temp dir.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Security {
    pub owner_uid: u32,
    /// Ancestors are checked up to and including this directory.
    pub trust_root: PathBuf,
}

impl Security {
    pub fn root() -> Self {
        Self {
            owner_uid: 0,
            trust_root: PathBuf::from("/"),
        }
    }
}

pub fn genesis_hash(ledger_id: &str) -> String {
    hex::encode(Sha256::digest(
        format!("hazelminx-ledger-v1\n{ledger_id}").as_bytes(),
    ))
}

pub fn sha256_hex(bytes: &[u8]) -> String {
    hex::encode(Sha256::digest(bytes))
}

/// 16 random bytes from the kernel, hex encoded.
pub fn random_id() -> Result<String, LedgerError> {
    let mut buf = [0u8; 16];
    File::open("/dev/urandom")
        .and_then(|mut f| f.read_exact(&mut buf))
        .map_err(|e| LedgerError::Io(format!("read /dev/urandom: {e}")))?;
    Ok(hex::encode(buf))
}

#[derive(Deserialize)]
struct RawLine<'a> {
    hash: String,
    #[serde(borrow)]
    entry: &'a RawValue,
}

/// A verified ledger file.
#[derive(Debug, Clone)]
pub struct Chain {
    pub meta: Meta,
    pub entries: Vec<Entry>,
    /// `(offset, length without newline)` of each line, index = seq - 1.
    pub spans: Vec<(u64, u64)>,
    /// Bytes covered by complete, verified lines.
    pub valid_len: u64,
    /// Bytes after the last newline (a torn final write), 0 normally.
    pub torn_tail: u64,
    pub head: Head,
}

/// Checks `bytes` against `meta`: hash per line, `prev_hash` linkage and
/// gap-free `seq`.
pub fn parse_chain(meta: Meta, bytes: &[u8]) -> Result<Chain, LedgerError> {
    if meta.format != FORMAT_VERSION {
        return Err(LedgerError::Integrity {
            line: 0,
            reason: format!("unsupported ledger format {}", meta.format),
        });
    }
    let mut head = Head {
        seq: 0,
        hash: genesis_hash(&meta.ledger_id),
    };
    let mut entries = Vec::new();
    let mut spans = Vec::new();
    let mut offset = 0usize;
    while let Some(nl) = bytes[offset..].iter().position(|&b| b == b'\n') {
        let line_no = entries.len() + 1;
        let line = &bytes[offset..offset + nl];
        let bad = |reason: String| LedgerError::Integrity {
            line: line_no,
            reason,
        };
        let raw: RawLine =
            serde_json::from_slice(line).map_err(|e| bad(format!("not a ledger line: {e}")))?;
        let actual = sha256_hex(raw.entry.get().as_bytes());
        if actual != raw.hash {
            return Err(bad("entry hash does not match its content".into()));
        }
        let entry: Entry = serde_json::from_str(raw.entry.get())
            .map_err(|e| bad(format!("entry does not parse: {e}")))?;
        if entry.seq != head.seq + 1 {
            return Err(bad(format!("sequence {} follows {}", entry.seq, head.seq)));
        }
        if entry.prev_hash != head.hash {
            return Err(bad("prev_hash does not match the previous line".into()));
        }
        head = Head {
            seq: entry.seq,
            hash: raw.hash,
        };
        spans.push((offset as u64, nl as u64));
        entries.push(entry);
        offset += nl + 1;
    }
    Ok(Chain {
        meta,
        entries,
        spans,
        valid_len: offset as u64,
        torn_tail: (bytes.len() - offset) as u64,
        head,
    })
}

/// What the ledger knows about one target.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Known {
    /// Strongest ownership any line has recorded.
    pub ownership: Ownership,
    /// State before the platform first touched the target.
    pub first_before: TargetState,
    /// Latest result line, if any.
    pub last_result: Option<Entry>,
}

/// Per-target knowledge from a verified chain (for read-only callers).
pub fn known_from(entries: &[Entry]) -> Vec<(Target, Known)> {
    index(entries).into_iter().collect()
}

fn index(entries: &[Entry]) -> BTreeMap<Target, Known> {
    let mut map: BTreeMap<Target, Known> = BTreeMap::new();
    for e in entries {
        add_to_index(&mut map, e);
    }
    map
}

fn add_to_index(map: &mut BTreeMap<Target, Known>, e: &Entry) {
    let known = map.entry(e.target.clone()).or_insert_with(|| Known {
        ownership: e.ownership,
        first_before: e.before.clone(),
        last_result: None,
    });
    known.ownership = known.ownership.max(e.ownership);
    if e.phase == Phase::Result {
        known.last_result = Some(e.clone());
    }
}

/// Owner, type and mode checks. `dir` must be a real directory (not a
/// symlink) owned by `owner_uid` with no group/other bits; every ancestor up
/// to the trust root must be owned by root or `owner_uid` and not writable by
/// group or others.
pub fn check_dir(dir: &Path, sec: &Security) -> Result<(), LedgerError> {
    let md = std::fs::symlink_metadata(dir).map_err(|e| io_err("stat", dir, e))?;
    if !md.file_type().is_dir() {
        return Err(LedgerError::Insecure(format!(
            "{} is not a directory",
            dir.display()
        )));
    }
    check_owner_mode(dir, &md, sec, 0o077)?;
    let mut cur = dir.parent();
    let stop = sec.trust_root.as_path();
    if dir.starts_with(stop) && dir != stop {
        while let Some(p) = cur {
            let md = std::fs::metadata(p).map_err(|e| io_err("stat", p, e))?;
            if md.uid() != 0 && md.uid() != sec.owner_uid {
                return Err(LedgerError::Insecure(format!(
                    "ancestor {} is owned by uid {}",
                    p.display(),
                    md.uid()
                )));
            }
            if md.mode() & 0o022 != 0 {
                return Err(LedgerError::Insecure(format!(
                    "ancestor {} is writable by group or others",
                    p.display()
                )));
            }
            if p == stop {
                break;
            }
            cur = p.parent();
        }
    }
    Ok(())
}

fn check_owner_mode(
    path: &Path,
    md: &std::fs::Metadata,
    sec: &Security,
    forbidden: u32,
) -> Result<(), LedgerError> {
    if md.uid() != sec.owner_uid {
        return Err(LedgerError::Insecure(format!(
            "{} is owned by uid {}, expected {}",
            path.display(),
            md.uid(),
            sec.owner_uid
        )));
    }
    if md.mode() & forbidden != 0 {
        return Err(LedgerError::Insecure(format!(
            "{} has mode {:o}; group and others must have no access",
            path.display(),
            md.mode() & 0o7777
        )));
    }
    Ok(())
}

fn check_file(path: &Path, sec: &Security) -> Result<(), LedgerError> {
    let md = std::fs::symlink_metadata(path).map_err(|e| io_err("stat", path, e))?;
    if !md.file_type().is_file() {
        return Err(LedgerError::Insecure(format!(
            "{} is not a regular file",
            path.display()
        )));
    }
    check_owner_mode(path, &md, sec, 0o077)
}

fn read_meta(dir: &Path, sec: &Security) -> Result<Option<Meta>, LedgerError> {
    let path = dir.join(META_FILE);
    match std::fs::symlink_metadata(&path) {
        Err(e) if e.kind() == io::ErrorKind::NotFound => return Ok(None),
        Err(e) => return Err(io_err("stat", &path, e)),
        Ok(_) => {}
    }
    check_file(&path, sec)?;
    let text = std::fs::read(&path).map_err(|e| io_err("read", &path, e))?;
    serde_json::from_slice(&text)
        .map(Some)
        .map_err(|e| LedgerError::Integrity {
            line: 0,
            reason: format!("{META_FILE} does not parse: {e}"),
        })
}

/// Writes `bytes` to `dir/name` via a temp file and rename, both synced,
/// mode 0600. Refuses to replace an existing file.
pub fn write_new_file(dir: &Path, name: &str, bytes: &[u8]) -> Result<(), LedgerError> {
    let target = dir.join(name);
    if target.exists() {
        return Err(LedgerError::Request(format!(
            "{} already exists",
            target.display()
        )));
    }
    let tmp = dir.join(format!(".{name}.{}.tmp", random_id()?));
    let mut f = OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(&tmp)
        .map_err(|e| io_err("create", &tmp, e))?;
    f.write_all(bytes)
        .and_then(|_| f.sync_all())
        .map_err(|e| io_err("write", &tmp, e))?;
    drop(f);
    // hard_link + unlink gives "fail if exists" semantics without a race.
    std::fs::hard_link(&tmp, &target).map_err(|e| io_err("link", &target, e))?;
    let _ = std::fs::remove_file(&tmp);
    sync_dir(dir)
}

pub fn sync_dir(dir: &Path) -> Result<(), LedgerError> {
    File::open(dir)
        .and_then(|d| d.sync_all())
        .map_err(|e| io_err("sync", dir, e))
}

/// Creates `dir` (mode 0700, parent must exist) when missing, then
/// [`check_dir`].
pub fn ensure_dir(dir: &Path, sec: &Security) -> Result<(), LedgerError> {
    match std::fs::symlink_metadata(dir) {
        Err(e) if e.kind() == io::ErrorKind::NotFound => {
            std::fs::DirBuilder::new()
                .mode(0o700)
                .create(dir)
                .map_err(|e| io_err("create", dir, e))?;
        }
        Err(e) => return Err(io_err("stat", dir, e)),
        Ok(_) => {}
    }
    check_dir(dir, sec)
}

/// Read-only verification (CLI `ledger verify`, Core-independent checks).
/// Does not take the lock and does not cut a torn tail; it reports it.
pub fn verify(dir: &Path, sec: &Security) -> Result<Chain, LedgerError> {
    check_dir(dir, sec)?;
    let meta = read_meta(dir, sec)?.ok_or_else(|| LedgerError::Integrity {
        line: 0,
        reason: format!("{META_FILE} is missing"),
    })?;
    let path = dir.join(LEDGER_FILE);
    let bytes = match std::fs::symlink_metadata(&path) {
        Err(e) if e.kind() == io::ErrorKind::NotFound => Vec::new(),
        Err(e) => return Err(io_err("stat", &path, e)),
        Ok(_) => {
            check_file(&path, sec)?;
            std::fs::read(&path).map_err(|e| io_err("read", &path, e))?
        }
    };
    parse_chain(meta, &bytes)
}

struct Inner {
    file: File,
    head: Head,
    spans: Vec<(u64, u64)>,
    end: u64,
    index: BTreeMap<Target, Known>,
    poisoned: Option<String>,
}

/// The open, locked ledger. One per host; the executor holds it.
pub struct Ledger {
    dir: PathBuf,
    meta: Meta,
    inner: Mutex<Inner>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct OpenReport {
    pub created: bool,
    pub entries: usize,
    /// Bytes of a torn final write that were cut off.
    pub torn_tail_cut: u64,
}

impl Ledger {
    /// Opens (creating on first use) the ledger in `dir`. The parent of
    /// `dir` must already exist; `dir` itself is created with mode 0700.
    pub fn open(dir: &Path, sec: &Security) -> Result<(Ledger, OpenReport), LedgerError> {
        ensure_dir(dir, sec)?;
        ensure_dir(&dir.join(BACKUP_DIR), sec)?;

        let path = dir.join(LEDGER_FILE);
        let ledger_exists = path.exists();
        let (meta, created) = match read_meta(dir, sec)? {
            Some(m) => (m, false),
            None if ledger_exists => {
                return Err(LedgerError::Integrity {
                    line: 0,
                    reason: format!("{LEDGER_FILE} exists but {META_FILE} is missing"),
                });
            }
            None => {
                let meta = Meta {
                    format: FORMAT_VERSION,
                    ledger_id: random_id()?,
                    created_at: OffsetDateTime::now_utc(),
                };
                let bytes = serde_json::to_vec_pretty(&meta).expect("Meta serializes");
                write_new_file(dir, META_FILE, &bytes)?;
                (meta, true)
            }
        };

        let mut file = OpenOptions::new()
            .read(true)
            .append(true)
            .create(true)
            .mode(0o600)
            .open(&path)
            .map_err(|e| io_err("open", &path, e))?;
        check_file(&path, sec)?;
        match file.try_lock() {
            Ok(()) => {}
            Err(std::fs::TryLockError::WouldBlock) => return Err(LedgerError::Locked),
            Err(std::fs::TryLockError::Error(e)) => return Err(io_err("lock", &path, e)),
        }
        let mut bytes = Vec::new();
        file.read_to_end(&mut bytes)
            .map_err(|e| io_err("read", &path, e))?;
        let chain = parse_chain(meta.clone(), &bytes)?;
        if chain.torn_tail > 0 {
            file.set_len(chain.valid_len)
                .and_then(|_| file.sync_all())
                .map_err(|e| io_err("truncate torn tail of", &path, e))?;
        }
        let report = OpenReport {
            created,
            entries: chain.entries.len(),
            torn_tail_cut: chain.torn_tail,
        };
        let inner = Inner {
            file,
            head: chain.head,
            spans: chain.spans,
            end: chain.valid_len,
            index: index(&chain.entries),
            poisoned: None,
        };
        Ok((
            Ledger {
                dir: dir.to_path_buf(),
                meta,
                inner: Mutex::new(inner),
            },
            report,
        ))
    }

    pub fn dir(&self) -> &Path {
        &self.dir
    }

    pub fn backup_dir(&self) -> PathBuf {
        self.dir.join(BACKUP_DIR)
    }

    pub fn meta(&self) -> &Meta {
        &self.meta
    }

    pub fn head(&self) -> Head {
        self.inner.lock().unwrap().head.clone()
    }

    /// `Some(reason)` once an append has failed.
    pub fn poisoned(&self) -> Option<String> {
        self.inner.lock().unwrap().poisoned.clone()
    }

    pub fn known(&self, target: &Target) -> Option<Known> {
        self.inner.lock().unwrap().index.get(target).cloned()
    }

    pub fn known_all(&self) -> Vec<(Target, Known)> {
        let inner = self.inner.lock().unwrap();
        inner
            .index
            .iter()
            .map(|(t, k)| (t.clone(), k.clone()))
            .collect()
    }

    /// Appends `entries` as one write followed by `fdatasync`. `seq` and
    /// `prev_hash` are assigned here (whatever the caller put there is
    /// replaced). Returns the entries as stored. Blocking; async callers use
    /// `spawn_blocking`.
    pub fn append(&self, entries: Vec<Entry>) -> Result<Vec<Entry>, LedgerError> {
        let mut inner = self.inner.lock().unwrap();
        if let Some(reason) = &inner.poisoned {
            return Err(LedgerError::Poisoned(reason.clone()));
        }
        let mut head = inner.head.clone();
        let mut end = inner.end;
        let mut buf = Vec::new();
        let mut spans = Vec::new();
        let mut stored = Vec::with_capacity(entries.len());
        for mut e in entries {
            e.seq = head.seq + 1;
            e.prev_hash = head.hash.clone();
            let json = serde_json::to_string(&e).expect("Entry serializes");
            let hash = sha256_hex(json.as_bytes());
            let line = format!("{{\"hash\":\"{hash}\",\"entry\":{json}}}");
            spans.push((end, line.len() as u64));
            end += line.len() as u64 + 1;
            buf.extend_from_slice(line.as_bytes());
            buf.push(b'\n');
            head = Head { seq: e.seq, hash };
            stored.push(e);
        }
        if let Err(e) = inner
            .file
            .write_all(&buf)
            .and_then(|_| inner.file.sync_data())
        {
            let reason = format!("append to {}: {e}", self.dir.join(LEDGER_FILE).display());
            inner.poisoned = Some(reason.clone());
            return Err(LedgerError::Poisoned(reason));
        }
        inner.head = head;
        inner.end = end;
        inner.spans.extend(spans);
        for e in &stored {
            add_to_index(&mut inner.index, e);
        }
        Ok(stored)
    }

    /// Raw lines after `after_seq`, at most `limit` (capped at
    /// [`MAX_EXPORT_LINES`]) and, when `max_bytes` is set, no more line bytes
    /// than that (at least one line), for Core's copy.
    pub fn export(
        &self,
        after_seq: u64,
        limit: usize,
        max_bytes: Option<usize>,
    ) -> Result<LedgerBatch, LedgerError> {
        let inner = self.inner.lock().unwrap();
        if after_seq > inner.head.seq {
            return Err(LedgerError::Request(format!(
                "after_seq {after_seq} is beyond the ledger head {}",
                inner.head.seq
            )));
        }
        let limit = limit.clamp(1, MAX_EXPORT_LINES);
        let from = after_seq as usize;
        let mut to = (from + limit).min(inner.spans.len());
        if let Some(max) = max_bytes {
            let mut total = 0usize;
            for (i, &(_, len)) in inner.spans[from..to].iter().enumerate() {
                total += len as usize;
                if total > max && i > 0 {
                    to = from + i;
                    break;
                }
            }
        }
        let mut lines = Vec::with_capacity(to - from);
        for &(offset, len) in &inner.spans[from..to] {
            let mut buf = vec![0u8; len as usize];
            inner
                .file
                .read_exact_at(&mut buf, offset)
                .map_err(|e| LedgerError::Io(format!("read ledger line: {e}")))?;
            lines.push(
                String::from_utf8(buf)
                    .map_err(|_| LedgerError::Io("ledger line is not UTF-8".into()))?,
            );
        }
        Ok(LedgerBatch {
            ledger_id: self.meta.ledger_id.clone(),
            after_seq,
            lines,
            head: inner.head.clone(),
            more: (to as u64) < inner.head.seq,
        })
    }
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use crate::ledger::entry::{ChangeKind, Outcome, UnitState};
    use crate::params::UnitName;
    use std::os::unix::fs::PermissionsExt;

    pub fn current_uid() -> u32 {
        let f = tempfile::NamedTempFile::new().unwrap();
        f.as_file().metadata().unwrap().uid()
    }

    /// A security policy for a ledger under `root` (a fresh temp dir).
    pub fn sec_for(root: &Path) -> Security {
        Security {
            owner_uid: current_uid(),
            trust_root: root.to_path_buf(),
        }
    }

    pub fn tight_tempdir() -> tempfile::TempDir {
        let d = tempfile::tempdir().unwrap();
        std::fs::set_permissions(d.path(), std::fs::Permissions::from_mode(0o700)).unwrap();
        d
    }

    pub fn sample(phase: Phase, unit: &str) -> Entry {
        let state = TargetState::SystemdUnit(UnitState {
            exists: true,
            load_state: Some("loaded".into()),
            unit_file_state: Some("enabled".into()),
            active_state: Some("active".into()),
            sub_state: Some("running".into()),
        });
        Entry {
            seq: 0,
            prev_hash: String::new(),
            recorded_at: OffsetDateTime::now_utc(),
            phase,
            change_id: "c1".into(),
            job_id: Some("job-1".into()),
            request_id: Some("r1".into()),
            grant_id: Some("g1".into()),
            subject: Some("user-1".into()),
            action: "restart_service".into(),
            target: Target::SystemdUnit {
                unit: UnitName::try_from(unit.to_string()).unwrap(),
            },
            change: ChangeKind::Restart,
            ownership: Ownership::Existing,
            before: state.clone(),
            after: (phase == Phase::Result).then_some(state),
            after_error: None,
            outcome: (phase == Phase::Result).then_some(Outcome::Succeeded),
            intent_seq: None,
        }
    }

    #[test]
    fn create_append_reopen_and_verify() {
        let root = tight_tempdir();
        let sec = sec_for(root.path());
        let dir = root.path().join("ledger");
        let (l, rep) = Ledger::open(&dir, &sec).unwrap();
        assert!(rep.created);
        assert_eq!(l.head().seq, 0);
        assert_eq!(l.head().hash, genesis_hash(&l.meta().ledger_id));
        let stored = l
            .append(vec![
                sample(Phase::Intent, "a.service"),
                sample(Phase::Result, "a.service"),
            ])
            .unwrap();
        assert_eq!(stored[0].seq, 1);
        assert_eq!(stored[1].seq, 2);
        l.append(vec![sample(Phase::Intent, "b.service")]).unwrap();
        // Root-only on disk.
        for (p, m) in [
            (dir.clone(), 0o700),
            (dir.join(LEDGER_FILE), 0o600),
            (dir.join(META_FILE), 0o600),
            (dir.join(BACKUP_DIR), 0o700),
        ] {
            assert_eq!(
                std::fs::metadata(&p).unwrap().mode() & 0o777,
                m,
                "{}",
                p.display()
            );
        }
        // A second opener is refused while the first holds the lock.
        assert_eq!(Ledger::open(&dir, &sec).err(), Some(LedgerError::Locked));
        drop(l);
        let (l, rep) = Ledger::open(&dir, &sec).unwrap();
        assert!(!rep.created);
        assert_eq!(rep.entries, 3);
        assert_eq!(l.head().seq, 3);
        let chain = verify(&dir, &sec).unwrap();
        assert_eq!(chain.head, l.head());
        let k = l
            .known(&Target::SystemdUnit {
                unit: UnitName::try_from("a.service".to_string()).unwrap(),
            })
            .unwrap();
        assert_eq!(k.last_result.unwrap().seq, 2);
    }

    #[test]
    fn tampering_is_detected() {
        let root = tight_tempdir();
        let sec = sec_for(root.path());
        let dir = root.path().join("ledger");
        let (l, _) = Ledger::open(&dir, &sec).unwrap();
        l.append(vec![
            sample(Phase::Intent, "a.service"),
            sample(Phase::Result, "a.service"),
            sample(Phase::Intent, "b.service"),
        ])
        .unwrap();
        drop(l);
        let path = dir.join(LEDGER_FILE);
        let original = std::fs::read_to_string(&path).unwrap();
        let lines: Vec<&str> = original.lines().collect();

        // Edited content, hash left alone.
        let edited = original.replacen("\"active\"", "\"failed\"", 1);
        std::fs::write(&path, &edited).unwrap();
        assert!(matches!(
            verify(&dir, &sec),
            Err(LedgerError::Integrity { line: 1, .. })
        ));
        assert!(matches!(
            Ledger::open(&dir, &sec),
            Err(LedgerError::Integrity { line: 1, .. })
        ));

        // A line removed from the middle.
        std::fs::write(&path, format!("{}\n{}\n", lines[0], lines[2])).unwrap();
        let e = verify(&dir, &sec).unwrap_err();
        assert!(
            matches!(&e, LedgerError::Integrity { line: 2, reason } if reason.contains("sequence")),
            "{e}"
        );

        // Lines reordered.
        std::fs::write(&path, format!("{}\n{}\n", lines[1], lines[0])).unwrap();
        assert!(matches!(
            verify(&dir, &sec),
            Err(LedgerError::Integrity { line: 1, .. })
        ));

        // Same content under another ledger id (moved from another host).
        std::fs::write(&path, &original).unwrap();
        let mut meta: Meta =
            serde_json::from_slice(&std::fs::read(dir.join(META_FILE)).unwrap()).unwrap();
        meta.ledger_id = random_id().unwrap();
        std::fs::write(dir.join(META_FILE), serde_json::to_vec(&meta).unwrap()).unwrap();
        let e = verify(&dir, &sec).unwrap_err();
        assert!(
            matches!(&e, LedgerError::Integrity { line: 1, reason } if reason.contains("prev_hash")),
            "{e}"
        );
    }

    #[test]
    fn torn_tail_is_cut_on_open() {
        let root = tight_tempdir();
        let sec = sec_for(root.path());
        let dir = root.path().join("ledger");
        let (l, _) = Ledger::open(&dir, &sec).unwrap();
        l.append(vec![sample(Phase::Intent, "a.service")]).unwrap();
        drop(l);
        let path = dir.join(LEDGER_FILE);
        let mut f = OpenOptions::new().append(true).open(&path).unwrap();
        f.write_all(b"{\"hash\":\"abc\",\"entry\":{\"seq\":2")
            .unwrap();
        drop(f);
        let chain = verify(&dir, &sec).unwrap();
        assert!(chain.torn_tail > 0);
        let (l, rep) = Ledger::open(&dir, &sec).unwrap();
        assert!(rep.torn_tail_cut > 0);
        assert_eq!(l.head().seq, 1);
        l.append(vec![sample(Phase::Result, "a.service")]).unwrap();
        drop(l);
        assert_eq!(verify(&dir, &sec).unwrap().entries.len(), 2);
    }

    #[test]
    fn permissive_directory_or_ancestor_is_refused() {
        let root = tight_tempdir();
        let sec = sec_for(root.path());
        let dir = root.path().join("ledger");
        std::fs::create_dir(&dir).unwrap();
        std::fs::set_permissions(&dir, std::fs::Permissions::from_mode(0o750)).unwrap();
        let e = Ledger::open(&dir, &sec).err().unwrap();
        assert!(matches!(e, LedgerError::Insecure(_)), "{e}");

        let open = root.path().join("open");
        std::fs::create_dir(&open).unwrap();
        std::fs::set_permissions(&open, std::fs::Permissions::from_mode(0o777)).unwrap();
        let e = Ledger::open(&open.join("ledger"), &sec).err().unwrap();
        assert!(
            matches!(&e, LedgerError::Insecure(m) if m.contains("ancestor") && m.contains("open")),
            "{e}"
        );

        let wrong_owner = Security {
            owner_uid: sec.owner_uid.wrapping_add(1),
            ..sec.clone()
        };
        let e = Ledger::open(&root.path().join("x"), &wrong_owner)
            .err()
            .unwrap();
        assert!(matches!(e, LedgerError::Insecure(_)), "{e}");
    }

    #[test]
    fn export_returns_verifiable_raw_lines_in_pages() {
        let root = tight_tempdir();
        let sec = sec_for(root.path());
        let (l, _) = Ledger::open(&root.path().join("ledger"), &sec).unwrap();
        for i in 0..5 {
            l.append(vec![sample(Phase::Intent, &format!("u{i}.service"))])
                .unwrap();
        }
        let b = l.export(0, 2, None).unwrap();
        assert_eq!(b.lines.len(), 2);
        assert!(b.more);
        let head = b.verify(None).unwrap();
        assert_eq!(head.seq, 2);
        let b2 = l.export(2, 10, None).unwrap();
        assert_eq!(b2.lines.len(), 3);
        assert!(!b2.more);
        assert_eq!(b2.verify(Some(&head)).unwrap(), l.head());
        assert!(l.export(6, 1, None).is_err());
        assert_eq!(l.export(5, 1, None).unwrap().lines.len(), 0);
        // A byte cap stops the page early but never returns nothing.
        let one = l.export(0, 10, Some(10)).unwrap();
        assert_eq!(one.lines.len(), 1);
        assert!(one.more);
        let line = one.lines[0].len();
        let two = l.export(0, 10, Some(line * 2 + 1)).unwrap();
        assert_eq!(two.lines.len(), 2);
        assert_eq!(two.verify(None).unwrap().seq, 2);
    }
}
