//! Wraps one host change in ledger lines: probe every declared target,
//! sync the intent lines, run the change, probe again, write the result
//! lines. If the intents cannot be written, the change does not run.
//!
//! The executor uses this around every mutating action; the installer
//! (ticket 013) uses the same calls around its own steps, so "from the first
//! install step" every change goes through one path.

use super::entry::{ChangeKind, Entry, Outcome, Phase, Target, TargetState, classify};
use super::probe::{Capture, Probe};
use super::store::{Ledger, LedgerError, random_id};
use std::sync::Arc;
use time::OffsetDateTime;

/// Longest error text kept in a result line.
pub const MAX_ERROR_TEXT: usize = 512;

/// What an action declares it will change. Every action declares one or the
/// other; the executor refuses actions that declare neither.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum LedgerDecl {
    /// Reads only; nothing on the host changes, nothing is recorded.
    ReadOnly,
    /// Changes exactly these targets.
    Changes(Vec<Declared>),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Declared {
    pub target: Target,
    pub change: ChangeKind,
}

/// Who and what caused the change.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct ChangeContext {
    pub job_id: Option<String>,
    pub request_id: Option<String>,
    pub grant_id: Option<String>,
    pub subject: Option<String>,
    pub action: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RecordError {
    /// The ledger could not be opened or has stopped accepting writes.
    Unavailable(String),
    /// The state before the change could not be read.
    Probe { target: String, reason: String },
    /// Writing a line failed.
    Write(String),
}

impl std::fmt::Display for RecordError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            RecordError::Unavailable(m) => write!(f, "host ledger unavailable: {m}"),
            RecordError::Probe { target, reason } => {
                write!(
                    f,
                    "cannot read the state of {target} before the change: {reason}"
                )
            }
            RecordError::Write(m) => write!(f, "host ledger write failed: {m}"),
        }
    }
}

impl std::error::Error for RecordError {}

/// Intent lines written; the change may now run. Hand it to
/// [`Recorder::finish`] whatever the change's outcome.
#[derive(Debug, Clone)]
pub struct Pending {
    intents: Vec<Entry>,
}

impl Pending {
    pub fn intents(&self) -> &[Entry] {
        &self.intents
    }
}

pub struct Recorder {
    ledger: Result<Arc<Ledger>, String>,
    probe: Arc<dyn Probe>,
}

impl Recorder {
    pub fn new(ledger: Result<Arc<Ledger>, String>, probe: Arc<dyn Probe>) -> Self {
        Self { ledger, probe }
    }

    pub fn ledger(&self) -> Option<&Arc<Ledger>> {
        self.ledger.as_ref().ok()
    }

    /// `Err(reason)` when no change can be recorded right now.
    pub fn ready(&self) -> Result<(), String> {
        let ledger = self.ledger.as_ref().map_err(Clone::clone)?;
        match ledger.poisoned() {
            Some(reason) => Err(reason),
            None => Ok(()),
        }
    }

    /// Probes each declared target and syncs one intent line per target.
    pub async fn begin(
        &self,
        ctx: &ChangeContext,
        declared: &[Declared],
    ) -> Result<Pending, RecordError> {
        self.ready().map_err(RecordError::Unavailable)?;
        let ledger = self.ledger.as_ref().expect("ready").clone();
        let mut intents = Vec::with_capacity(declared.len());
        for d in declared {
            let before = self
                .probe
                .probe(&d.target, Capture::Before)
                .await
                .map_err(|reason| RecordError::Probe {
                    target: d.target.describe(),
                    reason: truncate_error(&reason),
                })?;
            let prior = ledger.known(&d.target).map(|k| k.ownership);
            intents.push(Entry {
                seq: 0,
                prev_hash: String::new(),
                recorded_at: OffsetDateTime::now_utc(),
                phase: Phase::Intent,
                change_id: random_id().map_err(|e| RecordError::Write(e.to_string()))?,
                job_id: ctx.job_id.clone(),
                request_id: ctx.request_id.clone(),
                grant_id: ctx.grant_id.clone(),
                subject: ctx.subject.clone(),
                action: ctx.action.clone(),
                target: d.target.clone(),
                change: d.change,
                ownership: classify(prior, d.change, &before, None),
                before,
                after: None,
                after_error: None,
                outcome: None,
                intent_seq: None,
            });
        }
        let stored = append(ledger, intents).await?;
        Ok(Pending { intents: stored })
    }

    /// Probes each target again and writes one result line per target.
    pub async fn finish(
        &self,
        pending: Pending,
        outcome: &Result<(), String>,
    ) -> Result<Vec<Entry>, RecordError> {
        let ledger = self
            .ledger
            .as_ref()
            .map_err(|e| RecordError::Unavailable(e.clone()))?
            .clone();
        let outcome = match outcome {
            Ok(()) => Outcome::Succeeded,
            Err(e) => Outcome::Failed {
                error: truncate_error(e),
            },
        };
        let mut results = Vec::with_capacity(pending.intents.len());
        for intent in pending.intents {
            let (after, after_error) = match self.probe.probe(&intent.target, Capture::After).await
            {
                Ok(s) => (Some(s), None),
                Err(e) => (None, Some(truncate_error(&e))),
            };
            let prior = ledger.known(&intent.target).map(|k| k.ownership);
            let ownership = match &after {
                Some(a) => classify(prior, intent.change, &intent.before, Some(a)),
                None => intent.ownership,
            };
            results.push(Entry {
                seq: 0,
                prev_hash: String::new(),
                recorded_at: OffsetDateTime::now_utc(),
                phase: Phase::Result,
                ownership,
                after,
                after_error,
                outcome: Some(outcome.clone()),
                intent_seq: Some(intent.seq),
                ..intent
            });
        }
        append(ledger, results).await
    }
}

async fn append(ledger: Arc<Ledger>, entries: Vec<Entry>) -> Result<Vec<Entry>, RecordError> {
    tokio::task::spawn_blocking(move || ledger.append(entries))
        .await
        .map_err(|e| RecordError::Write(format!("ledger task: {e}")))?
        .map_err(|e| match e {
            LedgerError::Poisoned(m) => RecordError::Unavailable(m),
            other => RecordError::Write(other.to_string()),
        })
}

fn truncate_error(s: &str) -> String {
    if s.chars().count() <= MAX_ERROR_TEXT {
        s.to_string()
    } else {
        let cut: String = s.chars().take(MAX_ERROR_TEXT).collect();
        format!("{cut}…")
    }
}

/// The state a probe returns for tests and fakes.
pub type ProbeResult = Result<TargetState, String>;

#[cfg(test)]
mod tests {
    use super::*;
    use crate::BoxFuture;
    use crate::ledger::entry::{Ownership, UnitState};
    use crate::ledger::store::tests::{sec_for, tight_tempdir};
    use crate::params::UnitName;
    use std::collections::VecDeque;
    use std::sync::Mutex;

    /// Replies with queued states in order.
    #[derive(Default)]
    struct Scripted(Mutex<VecDeque<ProbeResult>>);

    impl Probe for Scripted {
        fn probe(&self, _: &Target, _: Capture) -> BoxFuture<'static, ProbeResult> {
            let r = self
                .0
                .lock()
                .unwrap()
                .pop_front()
                .unwrap_or(Err("no state".into()));
            Box::pin(async move { r })
        }
    }

    fn unit_state(active: &str) -> TargetState {
        TargetState::SystemdUnit(UnitState {
            exists: true,
            load_state: Some("loaded".into()),
            unit_file_state: Some("enabled".into()),
            active_state: Some(active.into()),
            sub_state: None,
        })
    }

    fn restart(unit: &str) -> Declared {
        Declared {
            target: Target::SystemdUnit {
                unit: UnitName::try_from(unit.to_string()).unwrap(),
            },
            change: ChangeKind::Restart,
        }
    }

    #[tokio::test]
    async fn intent_then_result_with_observed_states() {
        let root = tight_tempdir();
        let (ledger, _) = Ledger::open(&root.path().join("l"), &sec_for(root.path())).unwrap();
        let ledger = Arc::new(ledger);
        let probe = Arc::new(Scripted(Mutex::new(
            [Ok(unit_state("inactive")), Ok(unit_state("active"))].into(),
        )));
        let rec = Recorder::new(Ok(ledger.clone()), probe);
        let ctx = ChangeContext {
            job_id: Some("job-9".into()),
            request_id: Some("r1".into()),
            grant_id: Some("g1".into()),
            subject: Some("user-1".into()),
            action: "restart_service".into(),
        };
        let pending = rec.begin(&ctx, &[restart("nginx.service")]).await.unwrap();
        assert_eq!(ledger.head().seq, 1, "intent is on disk before the change");
        let intent = &pending.intents()[0];
        assert_eq!(intent.ownership, Ownership::Existing);
        let results = rec.finish(pending, &Ok(())).await.unwrap();
        let r = &results[0];
        assert_eq!(r.seq, 2);
        assert_eq!(r.intent_seq, Some(1));
        assert_eq!(r.job_id.as_deref(), Some("job-9"));
        assert_eq!(r.before, unit_state("inactive"));
        assert_eq!(r.after, Some(unit_state("active")));
        assert_eq!(r.outcome, Some(Outcome::Succeeded));
        assert_eq!(r.ownership, Ownership::PlatformModified);
    }

    #[tokio::test]
    async fn unreadable_before_state_writes_nothing() {
        let root = tight_tempdir();
        let (ledger, _) = Ledger::open(&root.path().join("l"), &sec_for(root.path())).unwrap();
        let ledger = Arc::new(ledger);
        let probe = Arc::new(Scripted(Mutex::new(
            [
                Ok(unit_state("active")),
                Err("System has not been booted with systemd".into()),
            ]
            .into(),
        )));
        let rec = Recorder::new(Ok(ledger.clone()), probe);
        let e = rec
            .begin(
                &ChangeContext::default(),
                &[restart("a.service"), restart("b.service")],
            )
            .await
            .unwrap_err();
        assert!(matches!(&e, RecordError::Probe { target, .. } if target == "unit b.service"));
        assert_eq!(ledger.head().seq, 0, "no partial intents");
    }

    #[tokio::test]
    async fn failed_change_and_unreadable_after_state_are_recorded() {
        let root = tight_tempdir();
        let (ledger, _) = Ledger::open(&root.path().join("l"), &sec_for(root.path())).unwrap();
        let probe = Arc::new(Scripted(Mutex::new(
            [Ok(unit_state("active")), Err("timeout".into())].into(),
        )));
        let rec = Recorder::new(Ok(Arc::new(ledger)), probe);
        let p = rec
            .begin(&ChangeContext::default(), &[restart("a.service")])
            .await
            .unwrap();
        let r = rec
            .finish(p, &Err("Job for a.service failed".into()))
            .await
            .unwrap();
        assert_eq!(
            r[0].outcome,
            Some(Outcome::Failed {
                error: "Job for a.service failed".into()
            })
        );
        assert_eq!(r[0].after, None);
        assert_eq!(r[0].after_error.as_deref(), Some("timeout"));
    }

    #[tokio::test]
    async fn unavailable_ledger_refuses_before_probing() {
        let probe = Arc::new(Scripted::default());
        let rec = Recorder::new(Err("ledger is locked".into()), probe);
        assert!(rec.ready().is_err());
        let e = rec
            .begin(&ChangeContext::default(), &[restart("a.service")])
            .await
            .unwrap_err();
        assert!(matches!(e, RecordError::Unavailable(_)));
    }
}
