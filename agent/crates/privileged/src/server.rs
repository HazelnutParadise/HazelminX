//! Socket listener and the per-request pipeline:
//! peer uid → frame size → JSON → grant (signature, window, revocation) →
//! action kind → permission → parameters → ledger declaration → slot →
//! ledger intent (before state) → execute → stream → ledger result.
//!
//! An action that changes the host runs only after its intent lines are
//! synced to the host ledger (spec 55.1). No declaration, no ledger, or an
//! unreadable before-state means the action is refused and the refusal is a
//! security event (T-250).
//!
//! Control requests (`health`, `revoke`) are answered after the peer check
//! without touching the action slots, so a host saturated with long actions
//! or slow readers still answers them (SEC-009).

use crate::action::Action;
use crate::audit::{SecurityEvent, SecurityLog, redact, truncate};
use crate::command::OutputSink;
use crate::executor::Executor;
use crate::grant::{Grant, GrantRejection, Revocations, is_token};
use crate::ledger::recorder::{ChangeContext, LedgerDecl, RecordError, Recorder};
use crate::protocol::{ErrorCode, MAX_REQUEST_BYTES, PROTOCOL_VERSION, Request, Response};
use ed25519_dalek::VerifyingKey;
use std::io;
use std::path::PathBuf;
use std::sync::Arc;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::time::{Duration, Instant};
use time::OffsetDateTime;
use tokio::io::{AsyncBufReadExt, AsyncReadExt, AsyncWriteExt, BufReader};
use tokio::net::{UnixListener, UnixStream};
use tokio::sync::Semaphore;

pub const DEFAULT_MAX_CONCURRENT_ACTIONS: usize = 8;
/// `reason` values of [`SecurityEvent::LedgerRefused`].
pub const REFUSE_NO_DESCRIPTION: &str = "no host-ledger description";
pub const REFUSE_UNAVAILABLE: &str = "host ledger unavailable";
pub const REFUSE_BEFORE_STATE: &str = "before state unreadable";
pub const REFUSE_WRITE: &str = "ledger write failed";
/// How long an action request waits for a free slot before `busy`.
pub const SLOT_WAIT: Duration = Duration::from_secs(30);
pub const SOCKET_MODE: u32 = 0o660;
/// A connection that has not delivered its request line by then is dropped,
/// so idle or stalled callers cannot pin connection tasks and descriptors.
pub const DEFAULT_REQUEST_READ_TIMEOUT: Duration = Duration::from_secs(10);

#[derive(Debug, Clone)]
pub struct ServerConfig {
    pub socket_path: PathBuf,
    /// The only uid allowed to connect (the `hazelminx-agent` account).
    pub allowed_uid: u32,
    pub issuer_key: VerifyingKey,
    pub max_concurrent_actions: usize,
    pub request_read_timeout: Duration,
}

pub struct Server {
    cfg: ServerConfig,
    executor: Arc<dyn Executor>,
    log: Arc<dyn SecurityLog>,
    recorder: Arc<Recorder>,
    revocations: Revocations,
    slots: Semaphore,
    active: AtomicUsize,
}

/// A pipeline stage refused the request. Carries the wire error and the
/// security event to record (if any; malformed frames are logged elsewhere).
struct Refusal {
    code: ErrorCode,
    message: String,
    event: Option<SecurityEvent>,
}

impl Server {
    pub fn new(
        cfg: ServerConfig,
        executor: Arc<dyn Executor>,
        log: Arc<dyn SecurityLog>,
        recorder: Arc<Recorder>,
    ) -> Arc<Self> {
        let max = cfg.max_concurrent_actions.max(1);
        Arc::new(Self {
            cfg,
            executor,
            log,
            recorder,
            revocations: Revocations::default(),
            slots: Semaphore::new(max),
            active: AtomicUsize::new(0),
        })
    }

    pub fn revocations(&self) -> &Revocations {
        &self.revocations
    }

    /// Removes a stale socket file, binds, and sets mode [`SOCKET_MODE`].
    /// The parent directory's owner/group (root:hazelminx-agent, 0750 from
    /// the systemd unit) is what keeps other local users out.
    pub fn bind(&self) -> io::Result<UnixListener> {
        use std::os::unix::fs::PermissionsExt;
        let path = &self.cfg.socket_path;
        match std::fs::remove_file(path) {
            Ok(()) => {}
            Err(e) if e.kind() == io::ErrorKind::NotFound => {}
            Err(e) => return Err(e),
        }
        let listener = UnixListener::bind(path)?;
        std::fs::set_permissions(path, std::fs::Permissions::from_mode(SOCKET_MODE))?;
        Ok(listener)
    }

    /// Accepts until `shutdown` completes. Each connection is its own task.
    pub async fn serve(
        self: Arc<Self>,
        listener: UnixListener,
        shutdown: impl Future<Output = ()>,
    ) {
        tokio::pin!(shutdown);
        loop {
            tokio::select! {
                _ = &mut shutdown => break,
                accepted = listener.accept() => match accepted {
                    Ok((stream, _)) => {
                        tokio::spawn(self.clone().handle_connection(stream));
                    }
                    Err(e) => {
                        tracing::warn!(error = %e, "accept failed");
                        tokio::time::sleep(Duration::from_millis(50)).await;
                    }
                },
            }
        }
        let _ = std::fs::remove_file(&self.cfg.socket_path);
    }

    pub async fn handle_connection(self: Arc<Self>, stream: UnixStream) {
        // 1. Caller identity, before reading a single byte.
        match stream.peer_cred() {
            Ok(cred) if cred.uid() == self.cfg.allowed_uid => {}
            Ok(cred) => {
                self.log.record(SecurityEvent::CallerRejected {
                    uid: cred.uid(),
                    pid: cred.pid(),
                });
                let mut stream = stream;
                let _ = write_frame(
                    &mut stream,
                    &Response::error(
                        None,
                        ErrorCode::UnauthorizedCaller,
                        "caller uid is not allowed",
                    ),
                )
                .await;
                return;
            }
            Err(e) => {
                tracing::warn!(error = %e, "peer credentials unavailable; dropping connection");
                return;
            }
        }

        // 2. One bounded request line.
        let mut reader = BufReader::new(stream);
        let mut line = Vec::new();
        let mut limited = AsyncReadExt::take(&mut reader, MAX_REQUEST_BYTES as u64 + 1);
        let read = match tokio::time::timeout(
            self.cfg.request_read_timeout,
            limited.read_until(b'\n', &mut line),
        )
        .await
        {
            Ok(Ok(n)) => n,
            Ok(Err(e)) => {
                tracing::debug!(error = %e, "read request failed");
                return;
            }
            Err(_) => {
                self.log.record(SecurityEvent::RequestRejected {
                    reason: "request line not received before timeout".into(),
                    bytes: line.len(),
                });
                return;
            }
        };
        let stream = reader.get_mut();
        if read == 0 {
            return;
        }
        if line.len() > MAX_REQUEST_BYTES {
            self.log.record(SecurityEvent::RequestRejected {
                reason: "request exceeds size limit".into(),
                bytes: line.len(),
            });
            let _ = write_frame(
                stream,
                &Response::error(
                    None,
                    ErrorCode::RequestTooLarge,
                    "request exceeds size limit",
                ),
            )
            .await;
            return;
        }
        let request = match serde_json::from_slice::<Request>(&line) {
            Ok(r) => r,
            Err(_) => {
                self.log.record(SecurityEvent::RequestRejected {
                    reason: "request is not a known JSON frame".into(),
                    bytes: line.len(),
                });
                let _ = write_frame(
                    stream,
                    &Response::error(
                        None,
                        ErrorCode::Malformed,
                        "request is not a known JSON frame",
                    ),
                )
                .await;
                return;
            }
        };

        // 3. Dispatch.
        match request {
            Request::Health => {
                let _ = write_frame(
                    stream,
                    &Response::Health {
                        protocol_version: PROTOCOL_VERSION,
                        version: env!("CARGO_PKG_VERSION").to_string(),
                        active_actions: self.active.load(Ordering::SeqCst),
                        max_actions: self.cfg.max_concurrent_actions.max(1),
                        ledger_ready: self.recorder.ready().is_ok(),
                    },
                )
                .await;
            }
            Request::Revoke { grant_id } => {
                // Only token-shaped ids can ever belong to a valid grant, so
                // anything else is refused instead of stored.
                if !is_token(&grant_id) {
                    self.log.record(SecurityEvent::RequestRejected {
                        reason: "revoke grant_id is not a token".into(),
                        bytes: line.len(),
                    });
                    let _ = write_frame(
                        stream,
                        &Response::error(None, ErrorCode::Malformed, "grant_id is not a token"),
                    )
                    .await;
                    return;
                }
                self.revocations.revoke(&grant_id, now());
                self.log.record(SecurityEvent::GrantRevoked {
                    grant_id: grant_id.clone(),
                });
                let _ = write_frame(stream, &Response::Revoked { grant_id }).await;
            }
            Request::Action {
                request_id,
                job_id,
                grant,
                action,
            } => {
                let outcome = self
                    .run_action(request_id.clone(), job_id, grant, action, stream)
                    .await;
                if let Err(refusal) = outcome {
                    if let Some(event) = refusal.event {
                        self.log.record(event);
                    }
                    let _ = write_frame(
                        stream,
                        &Response::error(Some(request_id), refusal.code, refusal.message),
                    )
                    .await;
                }
            }
        }
    }

    /// Authorizes and executes; on success the result frame is written here.
    async fn run_action(
        &self,
        request_id: String,
        job_id: Option<String>,
        grant: Grant,
        action: serde_json::Value,
        stream: &mut UnixStream,
    ) -> Result<(), Refusal> {
        let kind_hint = action
            .get("kind")
            .and_then(|k| k.as_str())
            .map(str::to_string);

        // Grant: signature, window, lifetime, revocation.
        let now = now();
        let rejection = match grant.verify(&self.cfg.issuer_key, now) {
            Err(r) => Some(r),
            Ok(()) if self.revocations.is_revoked(&grant.id, now) => Some(GrantRejection::Revoked),
            Ok(()) => None,
        };
        if let Some(r) = rejection {
            return Err(Refusal {
                code: ErrorCode::GrantInvalid,
                message: r.to_string(),
                event: Some(SecurityEvent::GrantRejected {
                    grant_id: (r != GrantRejection::MalformedField).then(|| grant.id.clone()),
                    action: kind_hint.as_deref().map(truncate),
                    reason: serde_json::to_value(r)
                        .ok()
                        .and_then(|v| v.as_str().map(str::to_string))
                        .unwrap_or_default(),
                }),
            });
        }

        // Action kind and permission, before parameter parsing so that a
        // caller without the permission learns nothing about parameter rules.
        let kind = kind_hint.unwrap_or_default();
        let Some(required) = Action::permission_for_kind(&kind) else {
            return Err(Refusal {
                code: ErrorCode::UnknownAction,
                message: "unknown action kind".into(),
                event: Some(SecurityEvent::ParamRejected {
                    grant_id: grant.id.clone(),
                    action: truncate(&kind),
                    reason: "unknown action kind".into(),
                }),
            });
        };
        if !grant.has_permission(required) {
            return Err(Refusal {
                code: ErrorCode::PermissionDenied,
                message: format!("grant does not include permission {required}"),
                event: Some(SecurityEvent::PermissionDenied {
                    grant_id: grant.id.clone(),
                    subject: grant.subject.clone(),
                    action: kind,
                    required: required.to_string(),
                }),
            });
        }
        let action = match serde_json::from_value::<Action>(action) {
            Ok(a) => a,
            Err(e) => {
                let reason = e.to_string();
                return Err(Refusal {
                    code: ErrorCode::InvalidParam,
                    message: reason.clone(),
                    event: Some(SecurityEvent::ParamRejected {
                        grant_id: grant.id.clone(),
                        action: kind,
                        reason: redact(&reason),
                    }),
                });
            }
        };

        if job_id.as_deref().is_some_and(|j| !is_token(j)) {
            return Err(Refusal {
                code: ErrorCode::InvalidParam,
                message: "job_id is not a token".into(),
                event: Some(SecurityEvent::ParamRejected {
                    grant_id: grant.id.clone(),
                    action: kind,
                    reason: "job_id is not a token".into(),
                }),
            });
        }

        // Ledger declaration: what this action changes on the host.
        let action_kind = action.kind().to_string();
        // The caller gets the detail; the security event gets the rule name
        // only, since probe errors can quote parameter values (SEC-010).
        let ledger_refusal = |rule: &'static str, message: String| Refusal {
            code: ErrorCode::LedgerRefused,
            message,
            event: Some(SecurityEvent::LedgerRefused {
                grant_id: grant.id.clone(),
                action: action_kind.clone(),
                reason: rule.to_string(),
            }),
        };
        let declared = match self.executor.declare(&action) {
            None => {
                return Err(ledger_refusal(
                    REFUSE_NO_DESCRIPTION,
                    "action has no host-ledger description".into(),
                ));
            }
            Some(LedgerDecl::ReadOnly) => None,
            Some(LedgerDecl::Changes(d)) if d.is_empty() => {
                return Err(ledger_refusal(
                    REFUSE_NO_DESCRIPTION,
                    "action declares a change but names no target".into(),
                ));
            }
            Some(LedgerDecl::Changes(d)) => {
                if let Err(reason) = self.recorder.ready() {
                    return Err(ledger_refusal(
                        REFUSE_UNAVAILABLE,
                        format!("host ledger unavailable: {reason}"),
                    ));
                }
                Some(d)
            }
        };

        // Slot, then the ledger intent, then execute while forwarding output.
        let permit = match tokio::time::timeout(SLOT_WAIT, self.slots.acquire()).await {
            Ok(Ok(p)) => p,
            _ => {
                return Err(Refusal {
                    code: ErrorCode::Busy,
                    message: "all action slots are busy".into(),
                    event: None,
                });
            }
        };
        self.active.fetch_add(1, Ordering::SeqCst);
        let started = Instant::now();
        let kind = action.kind().to_string();
        let pending = match &declared {
            None => None,
            Some(d) => {
                let ctx = ChangeContext {
                    job_id: job_id.clone(),
                    request_id: Some(request_id.clone()),
                    grant_id: Some(grant.id.clone()),
                    subject: Some(grant.subject.clone()),
                    action: kind.clone(),
                };
                match self.recorder.begin(&ctx, d).await {
                    Ok(p) => Some(p),
                    Err(e) => {
                        self.active.fetch_sub(1, Ordering::SeqCst);
                        drop(permit);
                        let rule = match e {
                            RecordError::Unavailable(_) => REFUSE_UNAVAILABLE,
                            RecordError::Probe { .. } => REFUSE_BEFORE_STATE,
                            RecordError::Write(_) => REFUSE_WRITE,
                        };
                        return Err(ledger_refusal(rule, e.to_string()));
                    }
                }
            }
        };
        let (sink, mut rx) = OutputSink::channel();
        let exec = tokio::spawn(self.executor.execute(action, sink));
        let mut client_gone = false;
        while let Some(chunk) = rx.recv().await {
            if client_gone {
                continue;
            }
            if write_frame(stream, &Response::output(chunk.stream, &chunk.data))
                .await
                .is_err()
            {
                client_gone = true;
            }
        }
        let result = exec.await;
        if let Some(pending) = pending {
            let outcome = match &result {
                Ok(Ok(_)) => Ok(()),
                Ok(Err(e)) => Err(e.message.clone()),
                Err(join) => Err(format!("action task failed: {join}")),
            };
            if let Err(e) = self.recorder.finish(pending, &outcome).await {
                // The change ran and its intent is on disk; only the result
                // line is missing, which reads as "outcome unknown".
                self.log.record(SecurityEvent::LedgerResultUnrecorded {
                    grant_id: grant.id.clone(),
                    action: kind.clone(),
                    reason: match e {
                        RecordError::Unavailable(_) => REFUSE_UNAVAILABLE,
                        RecordError::Probe { .. } => "after state unreadable",
                        RecordError::Write(_) => REFUSE_WRITE,
                    }
                    .to_string(),
                });
            }
        }
        self.active.fetch_sub(1, Ordering::SeqCst);
        drop(permit);

        let (ok, response) = match result {
            Ok(Ok(value)) => (
                true,
                Response::Result {
                    request_id: request_id.clone(),
                    result: value,
                },
            ),
            Ok(Err(e)) => (
                false,
                Response::error(Some(request_id.clone()), ErrorCode::ActionFailed, e.message),
            ),
            Err(join) => (
                false,
                Response::error(
                    Some(request_id.clone()),
                    ErrorCode::ActionFailed,
                    format!("action task failed: {join}"),
                ),
            ),
        };
        self.log.record(SecurityEvent::ActionExecuted {
            grant_id: grant.id,
            subject: grant.subject,
            action: kind,
            ok,
            duration_ms: started.elapsed().as_millis() as u64,
        });
        if !client_gone {
            let _ = write_frame(stream, &response).await;
        }
        Ok(())
    }
}

async fn write_frame(stream: &mut UnixStream, frame: &Response) -> io::Result<()> {
    let mut bytes = serde_json::to_vec(frame).expect("Response serializes");
    bytes.push(b'\n');
    stream.write_all(&bytes).await?;
    stream.flush().await
}

fn now() -> OffsetDateTime {
    OffsetDateTime::now_utc()
}
