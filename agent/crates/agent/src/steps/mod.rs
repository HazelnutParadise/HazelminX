//! What the agent does with a `step_execute` or `step_reconcile`, without
//! any networking: checks, journal look-ups and journal writes. The session
//! (connection module) sends whatever this decides and runs the action.
//!
//! Order of checks for `step_execute` (first match wins):
//! 1. `targetNode` is not this node → reject `WRONG_NODE`;
//! 2. the journal has a terminal record for (jobId, step) → `Resend` that
//!    record (never run again, even if the request is expired or stale:
//!    re-sending a record executes nothing); a completed step whose
//!    records were compacted (its result is gone) → reject
//!    `OUTCOME_UNKNOWN`, so the Core reconciles;
//! 3. the journal shows the step started and it is still running in this
//!    process → `AlreadyRunning` (its outcome event will follow);
//! 4. the journal shows the step started but it is not running (the agent
//!    restarted mid-step): when the request is `retryable` (running it twice
//!    has no extra side effect, and the Core only re-sends it after
//!    reconciling) it continues to the checks below as a new attempt;
//!    otherwise reject `OUTCOME_UNKNOWN`: it is never re-run without
//!    reconciliation;
//! 5. `expiresAt` is not after `now` → reject `EXPIRED`;
//! 6. `controlGeneration` is below the journal's highest → reject
//!    `STALE_CONTROL_GENERATION`;
//! 7. the action is not in `specs`, or `actionVersion` is higher than the
//!    spec's version → reject `UNSUPPORTED_ACTION` with `supported` set to
//!    the full capability list;
//! 8. the action's `validate` refuses `params` → reject `INVALID_PARAMS`
//!    with the validator's message;
//! 9. otherwise journal `started` (fsynced) and return `Run`.

use crate::actions::ActionSpec;
use crate::journal::{Journal, NewRecord, Record, StepState};
use anyhow::{Result, bail};
use hazelminx_protocol::{Capability, EventKind, Message, StepFailure, step_reject_code};
use serde_json::{Map, Value};
use std::collections::HashSet;
use time::OffsetDateTime;

/// A step that passed every check and is journaled as started.
#[derive(Debug, Clone, PartialEq)]
pub struct RunTicket {
    pub request_id: String,
    pub job_id: String,
    pub step: u32,
    pub action: String,
    pub params: Map<String, Value>,
    pub control_generation: u64,
}

#[derive(Debug, Clone, PartialEq)]
pub enum Decision {
    /// Send this `step_rejected` message; nothing was journaled.
    Reject(Message),
    /// Re-send the `event` of this terminal record; nothing runs.
    Resend(Record),
    /// The step is running in this process; nothing to send now.
    AlreadyRunning,
    /// Run the action; `started` is journaled (its event is the record).
    Run { ticket: RunTicket, started: Record },
}

/// Result of `reconcile`.
#[derive(Debug, Clone, PartialEq)]
pub enum ReconcileAnswer {
    /// Send this message (`reconcile_report`, or `step_rejected` with
    /// `WRONG_NODE`).
    Now(Message),
    /// The step is running in this process: answer when it finishes (call
    /// `reconcile` again after `finish`).
    WhenFinished,
}

pub struct Steps {
    journal: Journal,
    node: String,
    specs: &'static [ActionSpec],
    capabilities: Vec<Capability>,
    running: HashSet<(String, u32)>,
}

impl Steps {
    /// `capabilities` is what `hello` declared (used for `supported` in
    /// `UNSUPPORTED_ACTION`).
    pub fn new(
        journal: Journal,
        node_fingerprint: String,
        specs: &'static [ActionSpec],
        capabilities: Vec<Capability>,
    ) -> Steps {
        Steps {
            journal,
            node: node_fingerprint,
            specs,
            capabilities,
            running: HashSet::new(),
        }
    }

    /// Applies the checks in the module docs to a `Message::StepExecute`.
    ///
    /// Expected results, besides the order above:
    /// - any other message variant → `Err` (programming error), nothing
    ///   journaled;
    /// - every `Reject` is a `Message::StepRejected` carrying the request's
    ///   `requestId`, `jobId` and `step`, a non-empty `message`, and
    ///   `supported: None` except for `UNSUPPORTED_ACTION`;
    /// - `Run`: the journal's new record is `started` with the request's
    ///   requestId and controlGeneration and `at = now`; the returned
    ///   `started` equals that record; (jobId, step) is now running;
    /// - a second identical request while running → `AlreadyRunning`, and
    ///   nothing new is journaled;
    /// - journal I/O failure → `Err`.
    pub fn decide(&mut self, msg: &Message, now: OffsetDateTime) -> Result<Decision> {
        let Message::StepExecute {
            request_id,
            job_id,
            step,
            action,
            action_version,
            params,
            target_node,
            control_generation,
            expires_at,
            retryable,
            ..
        } = msg
        else {
            bail!("decide expects a step_execute message, got {msg:?}");
        };

        let reject = |code: &str, message: String, supported: Option<Vec<Capability>>| {
            Decision::Reject(Message::StepRejected {
                request_id: request_id.clone(),
                job_id: job_id.clone(),
                step: *step,
                code: code.to_string(),
                message,
                supported,
            })
        };

        if target_node != &self.node {
            return Ok(reject(
                step_reject_code::WRONG_NODE,
                format!("target node {target_node:?} is not this node"),
                None,
            ));
        }

        match self.journal.step_state(job_id, *step) {
            StepState::Completed(record) | StepState::Failed(record) => {
                return Ok(Decision::Resend(record));
            }
            StepState::Interrupted { .. } => {
                if self.running.contains(&(job_id.clone(), *step)) {
                    return Ok(Decision::AlreadyRunning);
                }
                if !*retryable {
                    return Ok(reject(
                        step_reject_code::OUTCOME_UNKNOWN,
                        "an earlier attempt of this step was interrupted; its outcome is unknown, reconcile before retrying".to_string(),
                        None,
                    ));
                }
            }
            StepState::Compacted(_) => {
                return Ok(reject(
                    step_reject_code::OUTCOME_UNKNOWN,
                    "this step completed earlier and only its outcome is kept; reconcile to read it"
                        .to_string(),
                    None,
                ));
            }
            StepState::NotStarted => {}
        }

        if *expires_at <= now {
            return Ok(reject(
                step_reject_code::EXPIRED,
                format!("step expired at {expires_at}"),
                None,
            ));
        }

        let highest = self.journal.max_control_generation();
        if *control_generation < highest {
            return Ok(reject(
                step_reject_code::STALE_CONTROL_GENERATION,
                format!(
                    "control generation {control_generation} is below the journal's highest {highest}"
                ),
                None,
            ));
        }

        let Some(spec) = self
            .specs
            .iter()
            .find(|s| s.name == action.as_str() && s.version >= *action_version)
        else {
            return Ok(reject(
                step_reject_code::UNSUPPORTED_ACTION,
                format!("action {action:?} version {action_version} is not supported"),
                Some(self.capabilities.clone()),
            ));
        };

        if let Err(message) = (spec.validate)(params) {
            return Ok(reject(step_reject_code::INVALID_PARAMS, message, None));
        }

        let started = self.journal.append(NewRecord {
            kind: EventKind::Started,
            job_id: job_id.clone(),
            step: *step,
            request_id: request_id.clone(),
            at: now,
            control_generation: *control_generation,
            evidence: Vec::new(),
            result: None,
            error: None,
        })?;
        self.running.insert((job_id.clone(), *step));

        Ok(Decision::Run {
            ticket: RunTicket {
                request_id: request_id.clone(),
                job_id: job_id.clone(),
                step: *step,
                action: action.clone(),
                params: params.clone(),
                control_generation: *control_generation,
            },
            started,
        })
    }

    /// Journals the outcome of a ticket and marks it no longer running.
    ///
    /// Expected results: `Ok(value)` → `completed` record with
    /// `result: Some(value)`; `Err(failure)` → `failed` record with
    /// `error: Some(failure)`; both carry the ticket's requestId and
    /// controlGeneration, `at = now`, and `evidence` =
    /// [`agent://journal/<seq of the latest started record>`]; afterwards
    /// `decide` on the same (jobId, step) gives `Resend` of this record.
    pub fn finish(
        &mut self,
        ticket: &RunTicket,
        outcome: std::result::Result<Value, StepFailure>,
        now: OffsetDateTime,
    ) -> Result<Record> {
        let started_seq = match self.journal.step_state(&ticket.job_id, ticket.step) {
            StepState::Interrupted { started } => started.seq,
            other => {
                self.running.remove(&(ticket.job_id.clone(), ticket.step));
                bail!(
                    "cannot finish job {} step {}: the step is not open in the journal ({other:?})",
                    ticket.job_id,
                    ticket.step
                );
            }
        };

        let (kind, result, error) = match outcome {
            Ok(value) => (EventKind::Completed, Some(value), None),
            Err(failure) => (EventKind::Failed, None, Some(failure)),
        };

        let appended = self.journal.append(NewRecord {
            kind,
            job_id: ticket.job_id.clone(),
            step: ticket.step,
            request_id: ticket.request_id.clone(),
            at: now,
            control_generation: ticket.control_generation,
            evidence: vec![format!("agent://journal/{started_seq}")],
            result,
            error,
        });
        self.running.remove(&(ticket.job_id.clone(), ticket.step));
        appended
    }

    /// Answers a `Message::StepReconcile` from the journal. Never runs or
    /// journals anything.
    ///
    /// Expected results:
    /// - other variants → `Err`;
    /// - wrong `targetNode` → `Now(StepRejected WRONG_NODE)`;
    /// - never started → report started=false, completed=false,
    ///   failed=false, no evidence, no error;
    /// - started and running here → `WhenFinished`;
    /// - started, not running (interrupted) → started=true,
    ///   completed=false, failed=false, evidence
    ///   [`agent://journal/<seq of started>`];
    /// - completed → started=true, completed=true, evidence of the record;
    /// - failed → started=true, failed=true, the record's evidence and
    ///   error;
    /// - compacted → the same as completed, from the outcome;
    /// - every report carries the reconcile's requestId, jobId and step.
    pub fn reconcile(&self, msg: &Message) -> Result<ReconcileAnswer> {
        let Message::StepReconcile {
            request_id,
            job_id,
            step,
            target_node,
        } = msg
        else {
            bail!("reconcile expects a step_reconcile message, got {msg:?}");
        };

        if target_node != &self.node {
            return Ok(ReconcileAnswer::Now(Message::StepRejected {
                request_id: request_id.clone(),
                job_id: job_id.clone(),
                step: *step,
                code: step_reject_code::WRONG_NODE.to_string(),
                message: format!("target node {target_node:?} is not this node"),
                supported: None,
            }));
        }

        let report = |started: bool,
                      completed: bool,
                      failed: bool,
                      evidence: Vec<String>,
                      error: Option<StepFailure>| {
            ReconcileAnswer::Now(Message::ReconcileReport {
                request_id: request_id.clone(),
                job_id: job_id.clone(),
                step: *step,
                started,
                completed,
                failed,
                evidence,
                error,
            })
        };

        match self.journal.step_state(job_id, *step) {
            StepState::NotStarted => Ok(report(false, false, false, Vec::new(), None)),
            StepState::Interrupted { started } => {
                if self.running.contains(&(job_id.clone(), *step)) {
                    Ok(ReconcileAnswer::WhenFinished)
                } else {
                    Ok(report(
                        true,
                        false,
                        false,
                        vec![format!("agent://journal/{}", started.seq)],
                        None,
                    ))
                }
            }
            StepState::Completed(record) => Ok(report(true, true, false, record.evidence, None)),
            StepState::Failed(record) => {
                Ok(report(true, false, true, record.evidence, record.error))
            }
            StepState::Compacted(outcome) => Ok(report(
                true,
                outcome.kind == EventKind::Completed,
                outcome.kind == EventKind::Failed,
                outcome.evidence,
                outcome.error,
            )),
        }
    }

    pub fn journal(&self) -> &Journal {
        &self.journal
    }

    pub fn journal_mut(&mut self) -> &mut Journal {
        &mut self.journal
    }

    #[cfg(test)]
    /// True while (job_id, step) is running in this process.
    pub fn is_running(&self, job_id: &str, step: u32) -> bool {
        self.running.contains(&(job_id.to_string(), step))
    }
}

#[cfg(test)]
mod tests;
