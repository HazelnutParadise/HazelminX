//! The agent dials the Core (NOD-002): no listening port on the node. One
//! WebSocket over TLS carries the handshake, then step requests from the
//! Core and durable events from the agent (engineering/contracts/README.md).
//!
//! Durable events come from the journal, not from the session: an action
//! that finishes while the Core is unreachable is journaled, and the next
//! session sends every record the Core has not acknowledged. Actions run on
//! their own tasks and are not tied to a session, so a dropped connection
//! never stops or repeats them.

pub mod tls;

use crate::actions::{self, AgentFacts};
use crate::config::CoreTransport;
use crate::identity::Identity;
use crate::monitor::Monitor;
use crate::steps::{Decision, ReconcileAnswer, RunTicket, Steps};
use anyhow::{Context, Result, anyhow, bail};
use base64::Engine;
use futures_util::{SinkExt, StreamExt};
use hazelminx_protocol::{self as proto, CONNECT_PATH, Handshake, Message, Role, reject_code};
use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use std::time::Duration;
use time::OffsetDateTime;
use tokio::net::TcpStream;
use tokio::sync::Notify;
use tokio_tungstenite::tungstenite::{self, protocol::WebSocketConfig};
use tracing::{debug, error, info, warn};

/// Everything the connection loop needs.
pub struct Options {
    pub core_url: String,
    pub transport: CoreTransport,
    pub proxy_ca_file: Option<PathBuf>,
    pub identity_dir: PathBuf,
    pub agent_version: String,
}

/// State shared by sessions and action tasks.
pub struct Shared {
    steps: Mutex<Steps>,
    /// Woken when the journal grows or a deferred answer is ready.
    changed: Notify,
    /// Messages ready to send that are not journal records (answers to
    /// reconciles that waited for a running step).
    outbox: Mutex<Vec<Message>>,
    /// step_reconcile requests waiting for a running step to finish.
    deferred: Mutex<Vec<Message>>,
    /// Set when a step outcome could not be journaled: the session is ended
    /// so the Core reconciles instead of waiting for an event that will
    /// never come.
    broken: std::sync::atomic::AtomicBool,
    facts: AgentFacts,
    identity: Identity,
    /// The privileged executor, for `ledger_pull`.
    privileged: hazelminx_privileged::client::Client,
    /// Set when a renewed certificate was stored: the session ends so the
    /// next connection proves it.
    renewed: std::sync::atomic::AtomicBool,
    /// Failed connection attempts in a row with `node.crt.next`.
    next_failures: std::sync::atomic::AtomicU32,
    /// Host facts and samples waiting for the Core (`host.metrics`).
    pub monitor: Arc<Monitor>,
}

impl Shared {
    /// Wakes the session to send what is new (journal records, answers,
    /// host facts and samples).
    pub fn wake(&self) {
        self.changed.notify_one();
    }

    pub fn new(
        steps: Steps,
        identity: Identity,
        agent_version: String,
        privileged: hazelminx_privileged::client::Client,
    ) -> Arc<Shared> {
        let facts = AgentFacts {
            agent_version,
            fingerprint: identity.fingerprint(),
        };
        Arc::new(Shared {
            steps: Mutex::new(steps),
            changed: Notify::new(),
            outbox: Mutex::new(Vec::new()),
            deferred: Mutex::new(Vec::new()),
            broken: std::sync::atomic::AtomicBool::new(false),
            facts,
            identity,
            privileged,
            renewed: std::sync::atomic::AtomicBool::new(false),
            next_failures: std::sync::atomic::AtomicU32::new(0),
            monitor: Arc::default(),
        })
    }
}

/// Why a connection attempt or session ended.
#[derive(Debug)]
enum End {
    /// The Core refused us; `code` decides how long to wait.
    Refused {
        code: String,
        message: String,
        supported: Option<proto::ProtocolRange>,
    },
    /// Network, TLS or protocol failure, or the session ran and ended.
    Lost {
        error: anyhow::Error,
        lived: Duration,
    },
    Shutdown,
}

/// Reconnect delays: exponential from 1 s to 60 s with jitter, reset after a
/// session that stayed up for a minute; refusals that need an operator
/// (incompatible protocol, revoked or unknown identity) wait 10 minutes.
/// Bounded retries without a connection storm (SEC-008).
#[derive(Debug, Default)]
pub struct Backoff {
    failures: u32,
}

impl Backoff {
    pub const BASE: Duration = Duration::from_secs(2);
    pub const CAP: Duration = Duration::from_secs(60);
    pub const OPERATOR: Duration = Duration::from_secs(600);
    pub const STABLE: Duration = Duration::from_secs(60);

    /// Delay before the next attempt; `jitter` in [0, 1).
    pub fn next(&mut self, lived: Duration, needs_operator: bool, jitter: f64) -> Duration {
        if lived >= Self::STABLE {
            self.failures = 0;
        }
        let ceiling = if needs_operator {
            Self::OPERATOR
        } else {
            let exp = Self::BASE.saturating_mul(1u32 << self.failures.min(6));
            exp.min(Self::CAP)
        };
        self.failures = self.failures.saturating_add(1);
        let jitter = jitter.clamp(0.0, 1.0);
        if needs_operator {
            // Waiting for a person: at least half of the long delay.
            return ceiling.mul_f64(0.5 + 0.5 * jitter);
        }
        // Anywhere up to the ceiling, so agents dropped by the same Core
        // restart spread their reconnects over the whole window instead of
        // arriving together.
        ceiling.mul_f64(jitter).max(Duration::from_millis(100))
    }
}

/// Runs until `shutdown` resolves.
pub async fn run(
    opts: Options,
    shared: Arc<Shared>,
    shutdown: impl std::future::Future<Output = ()>,
) {
    tokio::pin!(shutdown);
    let mut backoff = Backoff::default();
    loop {
        let end = tokio::select! {
            end = connect_and_serve(&opts, &shared) => end,
            _ = &mut shutdown => End::Shutdown,
        };
        let (lived, needs_operator) = match &end {
            End::Shutdown => return,
            End::Refused {
                code,
                message,
                supported,
            } => {
                let op = matches!(
                    code.as_str(),
                    reject_code::PROTOCOL_INCOMPATIBLE
                        | reject_code::NODE_REVOKED
                        | reject_code::IDENTITY_INVALID
                        | reject_code::IDENTITY_CONFLICT
                );
                let ours = proto::supported_range();
                match supported {
                    Some(r) => {
                        error!(code, %message, agent_protocol = %format!("{}..{}", ours.min, ours.max), core_protocol = %format!("{}..{}", r.min, r.max), "core refused this agent; no operation will run until this is fixed")
                    }
                    None => error!(code, %message, "core refused this agent"),
                }
                (Duration::ZERO, op)
            }
            End::Lost { error, lived } => {
                warn!(error = %format!("{error:#}"), lived_secs = lived.as_secs(), "core connection lost");
                (*lived, false)
            }
        };
        let delay = backoff.next(lived, needs_operator, rand::random::<f64>());
        info!(retry_in_secs = delay.as_secs_f64(), "reconnecting to core");
        tokio::select! {
            _ = tokio::time::sleep(delay) => {}
            _ = &mut shutdown => return,
        }
    }
}

type Ws = tokio_tungstenite::WebSocketStream<tokio_rustls::client::TlsStream<TcpStream>>;

async fn connect_and_serve(opts: &Options, shared: &Arc<Shared>) -> End {
    let started = std::time::Instant::now();
    let lost = |error: anyhow::Error| End::Lost {
        error,
        lived: started.elapsed(),
    };
    let material = match tls::TrustMaterial::load(&opts.identity_dir) {
        Ok(m) => m,
        Err(e) => return lost(e),
    };
    // A renewed certificate the Core refuses, or that fails several times in
    // a row, is dropped and the current one is used again (the Core still
    // accepts it); a single network failure is not held against it.
    let give_up_next = |why: &dyn std::fmt::Display, definite: bool| {
        if !material.using_next {
            return;
        }
        let before = shared
            .next_failures
            .fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        if discard_renewed(before, definite) {
            warn!(reason = %why, "the renewed certificate did not work; going back to the current one");
            shared
                .next_failures
                .store(0, std::sync::atomic::Ordering::SeqCst);
            if let Err(e) = tls::discard_next(&opts.identity_dir) {
                error!(error = %format!("{e:#}"), "cannot remove node.crt.next");
            }
        }
    };
    let mut ws = match dial(opts, &material).await {
        Ok(ws) => ws,
        Err(DialError::Tcp(e)) => return lost(e),
        Err(DialError::Tls(e)) => {
            give_up_next(&format!("{e:#}"), false);
            return lost(e);
        }
    };
    let ready = match handshake(&mut ws, &material, shared).await {
        Ok(r) => r,
        Err(HandshakeError::Refused {
            code,
            message,
            supported,
        }) => {
            give_up_next(&code, true);
            return End::Refused {
                code,
                message,
                supported,
            };
        }
        Err(HandshakeError::Other(e)) => {
            give_up_next(&format!("{e:#}"), false);
            return lost(e);
        }
    };
    if material.using_next {
        shared
            .next_failures
            .store(0, std::sync::atomic::Ordering::SeqCst);
        match tls::promote_next(&opts.identity_dir) {
            Ok(()) => info!("renewed node certificate is now in use"),
            Err(e) => {
                error!(error = %format!("{e:#}"), "cannot switch to the renewed certificate file")
            }
        }
    }
    info!(session = %ready.session_id, protocol = ready.protocol_version, core_version = %ready.core_version, "connected to core");
    let err = serve(ws, shared, ready.keys, ready.heartbeat, ready.metrics).await;
    End::Lost {
        error: err,
        lived: started.elapsed(),
    }
}

/// Connection attempts in a row a renewed certificate may fail before it
/// is dropped (a refusal by the Core drops it at once).
const RENEWED_CERT_ATTEMPTS: u32 = 3;

/// Whether to drop `node.crt.next` after a failure, given how many
/// failures came before it.
fn discard_renewed(failures_before: u32, definite: bool) -> bool {
    definite || failures_before + 1 >= RENEWED_CERT_ATTEMPTS
}

/// Why dialing failed: before TLS (network) or from TLS on.
enum DialError {
    Tcp(anyhow::Error),
    Tls(anyhow::Error),
}

async fn dial(opts: &Options, material: &tls::TrustMaterial) -> std::result::Result<Ws, DialError> {
    let tcp_err = DialError::Tcp;
    let url = url::Url::parse(&opts.core_url)
        .with_context(|| format!("core_url {:?}", opts.core_url))
        .map_err(tcp_err)?;
    let host = url
        .host_str()
        .ok_or_else(|| anyhow!("core_url has no host"))
        .map_err(tcp_err)?
        .to_string();
    let port = url.port().unwrap_or(443);
    let tcp = tokio::time::timeout(
        Duration::from_secs(10),
        TcpStream::connect((host.as_str(), port)),
    )
    .await
    .map_err(|_| DialError::Tcp(anyhow!("TCP connect to {host}:{port} timed out")))?
    .with_context(|| format!("TCP connect to {host}:{port}"))
    .map_err(tcp_err)?;
    dial_tls(opts, material, &url, host, tcp)
        .await
        .map_err(DialError::Tls)
}

async fn dial_tls(
    opts: &Options,
    material: &tls::TrustMaterial,
    url: &url::Url,
    host: String,
    tcp: TcpStream,
) -> Result<Ws> {
    let _ = tcp.set_nodelay(true);
    let port = url.port().unwrap_or(443);
    let config = material.client_config(opts.transport, opts.proxy_ca_file.as_deref())?;
    let name = tls::server_name(opts.transport, &host)?;
    let tls = tokio::time::timeout(
        Duration::from_secs(10),
        tokio_rustls::TlsConnector::from(config).connect(name, tcp),
    )
    .await
    .map_err(|_| anyhow!("TLS handshake with {host}:{port} timed out"))?
    .with_context(|| format!("TLS handshake with {host}:{port}"))?;
    let authority = match url.port() {
        Some(p) => format!("{host}:{p}"),
        None => host.clone(),
    };
    let ws_url = format!("wss://{authority}{CONNECT_PATH}");
    let cfg = WebSocketConfig::default()
        .max_message_size(Some(1 << 20))
        .max_frame_size(Some(1 << 20));
    let (ws, _) = tokio::time::timeout(
        Duration::from_secs(10),
        tokio_tungstenite::client_async_with_config(ws_url.as_str(), tls, Some(cfg)),
    )
    .await
    .map_err(|_| anyhow!("WebSocket upgrade timed out"))?
    .context("WebSocket upgrade")?;
    Ok(ws)
}

enum HandshakeError {
    Refused {
        code: String,
        message: String,
        supported: Option<proto::ProtocolRange>,
    },
    Other(anyhow::Error),
}

impl From<anyhow::Error> for HandshakeError {
    fn from(e: anyhow::Error) -> Self {
        HandshakeError::Other(e)
    }
}

struct Ready {
    keys: proto::SessionKeys,
    session_id: String,
    protocol_version: u32,
    core_version: String,
    heartbeat: Duration,
    /// The Core declared `host.metrics` v1, so it reads host facts and
    /// samples; an older Core would answer them with protocol_error.
    metrics: bool,
}

async fn handshake(
    ws: &mut Ws,
    material: &tls::TrustMaterial,
    shared: &Arc<Shared>,
) -> std::result::Result<Ready, HandshakeError> {
    let deadline = Duration::from_secs(10);
    let agent_nonce = proto::new_nonce();
    let (kex_secret, agent_kex) = proto::new_kex();
    let hello = Handshake::Hello {
        protocol: proto::supported_range(),
        agent_version: shared.facts.agent_version.clone(),
        node_certificate: material.node_cert_pem.clone(),
        nonce: agent_nonce.clone(),
        kex: agent_kex.clone(),
        capabilities: actions::capabilities(),
    };
    // The hello digest is taken over the exact text sent.
    let hello_text = proto::encode(&hello);
    ws.send(tungstenite::Message::text(hello_text.clone()))
        .await
        .context("send to core")?;
    let welcome = recv_handshake(ws, deadline).await?;
    let (
        version,
        core_version,
        core_cert,
        core_nonce,
        core_kex,
        session_id,
        proof,
        heartbeat,
        core_caps,
    ) = match welcome {
        Handshake::Welcome {
            protocol_version,
            core_version,
            core_certificate,
            nonce,
            kex,
            session_id,
            proof,
            heartbeat_seconds,
            capabilities,
        } => (
            protocol_version,
            core_version,
            core_certificate,
            nonce,
            kex,
            session_id,
            proof,
            heartbeat_seconds,
            capabilities,
        ),
        Handshake::Reject {
            code,
            message,
            supported,
        } => {
            return Err(HandshakeError::Refused {
                code,
                message,
                supported,
            });
        }
        other => return Err(anyhow!("expected welcome, got {other:?}").into()),
    };
    let ours = proto::supported_range();
    if version < ours.min || version > ours.max {
        return Err(anyhow!(
            "core chose protocol {version}, outside {}..{}",
            ours.min,
            ours.max
        )
        .into());
    }
    if !proto::valid_nonce(&core_nonce) {
        return Err(anyhow!("core nonce is not 32 bytes of base64url").into());
    }
    let fp = shared.identity.fingerprint();
    let welcome_digest = proto::welcome_digest(&core_version, heartbeat, &core_caps);
    let hello_digest = proto::hello_digest(&hello_text);
    let tin = proto::TranscriptInput {
        agent_nonce: &agent_nonce,
        core_nonce: &core_nonce,
        agent_kex: &agent_kex,
        core_kex: &core_kex,
        protocol_version: version,
        node_fingerprint: &fp,
        session_id: &session_id,
        hello_digest: &hello_digest,
        welcome_digest: &welcome_digest,
    };
    let core_t = proto::transcript(Role::Core, &tin);
    let proof = base64::engine::general_purpose::STANDARD
        .decode(proof.as_bytes())
        .map_err(|_| anyhow!("core proof is not base64"))?;
    material
        .verify_core(&core_cert, core_t.as_bytes(), &proof)
        .context("core identity check failed; not continuing")?;
    let keys = proto::derive_keys(Role::Agent, &kex_secret, &core_kex, &session_id)
        .map_err(|e| anyhow!("welcome kex: {e}"))?;
    let agent_t = proto::transcript(Role::Agent, &tin);
    let sig = shared.identity.sign(agent_t.as_bytes());
    send(
        ws,
        &Handshake::Auth {
            proof: base64::engine::general_purpose::STANDARD.encode(sig),
        },
    )
    .await?;
    match recv_handshake(ws, deadline).await? {
        Handshake::Ready { session_id: sid } if sid == session_id => {}
        Handshake::Ready { session_id: sid } => {
            return Err(anyhow!("ready for session {sid}, expected {session_id}").into());
        }
        Handshake::Reject {
            code,
            message,
            supported,
        } => {
            return Err(HandshakeError::Refused {
                code,
                message,
                supported,
            });
        }
        other => return Err(anyhow!("expected ready, got {other:?}").into()),
    }
    Ok(Ready {
        keys,
        session_id,
        protocol_version: version,
        core_version,
        heartbeat: Duration::from_secs(u64::from(heartbeat.clamp(1, 300))),
        metrics: core_caps
            .iter()
            .any(|c| c.name == proto::CAP_HOST_METRICS && c.version >= 1),
    })
}

/// Sends a handshake message (before ready, nothing is framed yet).
async fn send<T: serde::Serialize>(ws: &mut Ws, msg: &T) -> Result<()> {
    ws.send(tungstenite::Message::text(proto::encode(msg)))
        .await
        .context("send to core")
}

async fn recv_text(ws: &mut Ws, within: Duration) -> Result<String> {
    loop {
        let next = tokio::time::timeout(within, ws.next())
            .await
            .map_err(|_| anyhow!("nothing from core for {}s", within.as_secs()))?;
        match next {
            None => bail!("core closed the connection"),
            Some(Err(e)) => return Err(e).context("read from core"),
            Some(Ok(tungstenite::Message::Text(t))) => return Ok(t.as_str().to_string()),
            Some(Ok(tungstenite::Message::Close(f))) => bail!("core closed the connection: {f:?}"),
            Some(Ok(tungstenite::Message::Binary(_))) => bail!("core sent a binary frame"),
            Some(Ok(_)) => continue, // ping/pong
        }
    }
}

async fn recv_handshake(ws: &mut Ws, within: Duration) -> Result<Handshake> {
    let text = recv_text(ws, within).await?;
    proto::decode_handshake(&text).map_err(|e| anyhow!("core handshake message: {e}"))
}

/// The authenticated message stream of one session: every frame carries
/// the next number for its direction and a MAC under that direction's key.
struct Channel {
    keys: proto::SessionKeys,
    send_n: u64,
    recv_n: u64,
}

impl Channel {
    async fn send(&mut self, ws: &mut Ws, msg: &Message) -> Result<()> {
        self.send_n += 1;
        let frame = proto::seal_frame(&self.keys.send, self.send_n, &proto::encode(msg));
        ws.send(tungstenite::Message::text(frame))
            .await
            .context("send to core")
    }

    /// Authenticates a frame from the core; any failure ends the session.
    fn open(&mut self, frame: &str) -> Result<String> {
        let body = proto::open_frame(&self.keys.recv, self.recv_n + 1, frame)
            .map_err(|e| anyhow!("{e}; closing the session"))?;
        self.recv_n += 1;
        Ok(body)
    }
}

/// Serves one ready session until it ends; returns why.
async fn serve(
    mut ws: Ws,
    shared: &Arc<Shared>,
    keys: proto::SessionKeys,
    heartbeat: Duration,
    metrics: bool,
) -> anyhow::Error {
    let mut ch = Channel {
        keys,
        send_n: 0,
        recv_n: 0,
    };
    // A journal failure that happened while no session was up is handled by
    // this session's replay and the Core's reconcile; it must not end it.
    shared
        .broken
        .store(false, std::sync::atomic::Ordering::SeqCst);
    // Replay every event the Core has not acknowledged, then follow the journal.
    let mut sent_up_to = shared.steps.lock().unwrap().journal().acked();
    // Every unacknowledged sample and the current facts go out again on a
    // new session.
    let mut host = HostSent {
        enabled: metrics,
        facts: 0,
        in_flight: None,
    };
    let mut tick = tokio::time::interval(heartbeat);
    tick.tick().await;
    let idle = heartbeat * 3;
    let mut last_rx = tokio::time::Instant::now();
    shared.changed.notify_one();
    loop {
        tokio::select! {
            biased;
            _ = shared.changed.notified() => {
                if shared.broken.swap(false, std::sync::atomic::Ordering::SeqCst) {
                    return anyhow!("a step outcome could not be journaled; reconnecting so the core reconciles");
                }
                if let Err(e) = flush(&mut ws, &mut ch, shared, &mut sent_up_to, &mut host).await {
                    return e;
                }
            }
            _ = tick.tick() => {
                if last_rx.elapsed() > idle {
                    return anyhow!("no message from core for three heartbeat intervals");
                }
                if let Err(e) = ch.send(&mut ws, &Message::Heartbeat { at: now_secs() }).await {
                    return e;
                }
            }
            next = ws.next() => {
                let frame = match next {
                    None => return anyhow!("core closed the connection"),
                    Some(Err(e)) => return anyhow!(e).context("read from core"),
                    Some(Ok(tungstenite::Message::Text(t))) => t.as_str().to_string(),
                    Some(Ok(tungstenite::Message::Close(f))) => return anyhow!("core closed the connection: {f:?}"),
                    Some(Ok(tungstenite::Message::Binary(_))) => return anyhow!("binary frame in an authenticated session; closing it"),
                    Some(Ok(_)) => { last_rx = tokio::time::Instant::now(); continue; }
                };
                let text = match ch.open(&frame) {
                    Ok(t) => t,
                    Err(e) => return e,
                };
                last_rx = tokio::time::Instant::now();
                match proto::decode(&text) {
                    Ok(msg) => {
                        for out in handle(shared, msg) {
                            if let Err(e) = ch.send(&mut ws, &out).await {
                                return e;
                            }
                        }
                        if shared.renewed.swap(false, std::sync::atomic::Ordering::SeqCst) {
                            let _ = ws.close(None).await;
                            return anyhow!("reconnecting with the renewed node certificate");
                        }
                    }
                    Err(e) => {
                        // Never act on a message we cannot read exactly (NOD-011).
                        warn!(error = %e, "core sent an unreadable message; ignoring it");
                        let pe = Message::ProtocolError { code: e.protocol_error_code().into(), message: e.to_string() };
                        if let Err(e) = ch.send(&mut ws, &pe).await {
                            return e;
                        }
                    }
                }
            }
        }
    }
}

/// What this session has sent of the monitor's data.
struct HostSent {
    enabled: bool,
    /// Facts version sent.
    facts: u64,
    /// The report waiting for its acknowledgement: its last seq and when
    /// it went out. The next one is sent only after it is answered.
    in_flight: Option<(u64, std::time::Instant)>,
}

/// Sends journal records after `sent_up_to`, queued answers, and host
/// facts and samples this session has not sent.
async fn flush(
    ws: &mut Ws,
    ch: &mut Channel,
    shared: &Arc<Shared>,
    sent_up_to: &mut u64,
    host: &mut HostSent,
) -> Result<()> {
    let (records, outbox) = {
        let steps = shared.steps.lock().unwrap();
        let recs: Vec<_> = steps
            .journal()
            .unacked()
            .into_iter()
            .filter(|r| r.seq > *sent_up_to)
            .collect();
        (recs, std::mem::take(&mut *shared.outbox.lock().unwrap()))
    };
    for r in records {
        ch.send(ws, &r.to_message()).await?;
        *sent_up_to = r.seq;
    }
    for m in outbox {
        ch.send(ws, &m).await?;
    }
    if host.enabled {
        if let Some((v, m)) = shared.monitor.facts_after(host.facts) {
            ch.send(ws, &m).await?;
            host.facts = v;
        }
        if let Some((last, at)) = host.in_flight {
            if !shared.monitor.unacked_through(last) {
                host.in_flight = None;
            } else if at.elapsed() > crate::monitor::ACK_TIMEOUT {
                warn!(
                    up_to = last,
                    "core did not acknowledge host samples; sending them again"
                );
                host.in_flight = None;
            }
        }
        if host.in_flight.is_none()
            && let Some((last, m)) = shared.monitor.next_report()
        {
            ch.send(ws, &m).await?;
            host.in_flight = Some((last, std::time::Instant::now()));
        }
    }
    Ok(())
}

/// Handles one decoded message; returns messages to send right away.
fn handle(shared: &Arc<Shared>, msg: Message) -> Vec<Message> {
    match &msg {
        Message::Heartbeat { .. } => vec![],
        Message::Ack { seq } => {
            if let Err(e) = shared.steps.lock().unwrap().journal_mut().ack(*seq) {
                warn!(seq, error = %format!("{e:#}"), "core acknowledged an event this agent never sent");
            }
            vec![]
        }
        Message::MetricsAck { seq } => {
            shared.monitor.ack(*seq);
            // Send the next report, if any, now.
            shared.wake();
            vec![]
        }
        Message::StepExecute { .. } => {
            let decision = shared
                .steps
                .lock()
                .unwrap()
                .decide(&msg, OffsetDateTime::now_utc());
            match decision {
                Ok(Decision::Reject(m)) => vec![m],
                Ok(Decision::Resend(rec)) => vec![rec.to_message()],
                Ok(Decision::AlreadyRunning) => vec![],
                Ok(Decision::Run { ticket, started: _ }) => {
                    shared.changed.notify_one(); // send the started record
                    spawn_action(shared.clone(), ticket);
                    vec![]
                }
                Err(e) => {
                    error!(error = %format!("{e:#}"), "cannot journal the step; refusing it");
                    match &msg {
                        Message::StepExecute {
                            request_id,
                            job_id,
                            step,
                            ..
                        } => vec![Message::StepRejected {
                            request_id: request_id.clone(),
                            job_id: job_id.clone(),
                            step: *step,
                            code: proto::step_reject_code::JOURNAL_UNAVAILABLE.into(),
                            message: "the agent cannot write its step journal, so it runs nothing; nothing was started".into(),
                            supported: None,
                        }],
                        _ => vec![],
                    }
                }
            }
        }
        Message::StepReconcile { .. } => match shared.steps.lock().unwrap().reconcile(&msg) {
            Ok(ReconcileAnswer::Now(m)) => vec![m],
            Ok(ReconcileAnswer::WhenFinished) => {
                // Keep one waiting request per step: the newest one is the
                // one the Core is still waiting on.
                let mut deferred = shared.deferred.lock().unwrap();
                if let Message::StepReconcile { job_id, step, .. } = &msg {
                    deferred.retain(|m| !matches!(m, Message::StepReconcile { job_id: j, step: s, .. } if j == job_id && s == step));
                }
                deferred.push(msg.clone());
                vec![]
            }
            Err(e) => {
                error!(error = %format!("{e:#}"), "cannot answer reconcile");
                vec![]
            }
        },
        Message::LedgerPull {
            request_id,
            after_seq,
            limit,
            grant,
        } => {
            // The executor round trip runs off the session loop; the answer
            // goes out through the outbox.
            let (shared, request_id, after_seq, limit, grant) = (
                shared.clone(),
                request_id.clone(),
                *after_seq,
                *limit,
                grant.clone(),
            );
            tokio::spawn(async move {
                let answer =
                    crate::ledger::answer(&shared.privileged, request_id, after_seq, limit, grant)
                        .await;
                if let Message::LedgerUnavailable { code, message, .. } = &answer {
                    warn!(%code, %message, "cannot export the host ledger for the core");
                }
                shared.outbox.lock().unwrap().push(answer);
                shared.changed.notify_one();
            });
            vec![]
        }
        Message::CertificateRenew {
            request_id,
            certificate,
        } => vec![renew_certificate(shared, request_id, certificate)],
        Message::ProtocolError { code, message } => {
            warn!(%code, %message, "core could not read an agent message");
            vec![]
        }
        _ => vec![Message::ProtocolError {
            code: proto::protocol_error_code::UNKNOWN_MESSAGE.into(),
            message: "message is not valid in the core→agent direction".into(),
        }],
    }
}

/// Checks a renewed certificate against the pinned Core CA and this node's
/// key and stores it as `node.crt.next`; the session then ends and the next
/// connection uses it (engineering/contracts/README.md, 憑證輪替).
fn renew_certificate(shared: &Arc<Shared>, request_id: &str, certificate: &str) -> Message {
    let dir = shared.identity.dir();
    let refused = |code: &str, message: String| {
        warn!(%code, %message, "keeping the current node certificate");
        Message::CertificateRefused {
            request_id: request_id.to_string(),
            code: code.to_string(),
            message,
        }
    };
    let ca = match std::fs::read_to_string(dir.join(tls::CORE_CA_FILE))
        .map_err(anyhow::Error::from)
        .and_then(|p| tls::parse_certificate(&p))
    {
        Ok(c) => c,
        Err(e) => {
            return refused(
                proto::certificate_refused_code::WRITE_FAILED,
                format!("cannot read core-ca.crt: {e:#}"),
            );
        }
    };
    if let Err(e) =
        tls::verify_node_certificate(certificate, &ca, shared.identity.verifying_key().as_bytes())
    {
        return refused(
            proto::certificate_refused_code::CERTIFICATE_INVALID,
            format!("{e:#}"),
        );
    }
    if let Err(e) = tls::write_atomic(dir, tls::NEXT_CERT_FILE, certificate.as_bytes()) {
        return refused(
            proto::certificate_refused_code::WRITE_FAILED,
            format!("{e:#}"),
        );
    }
    info!("renewed node certificate stored; reconnecting to use it");
    shared
        .renewed
        .store(true, std::sync::atomic::Ordering::SeqCst);
    Message::CertificateInstalled {
        request_id: request_id.to_string(),
    }
}

fn spawn_action(shared: Arc<Shared>, ticket: RunTicket) {
    tokio::spawn(async move {
        debug!(job = %ticket.job_id, step = ticket.step, action = %ticket.action, "running step");
        let outcome = actions::run(&shared.facts, &ticket.action, &ticket.params)
            .await
            .and_then(limit_result);
        let mut answers = Vec::new();
        {
            let mut steps = shared.steps.lock().unwrap();
            if let Err(e) = steps.finish(&ticket, outcome, OffsetDateTime::now_utc()) {
                // The step ran but its outcome is not durable: it will be
                // reported as interrupted, never as done.
                error!(job = %ticket.job_id, step = ticket.step, error = %format!("{e:#}"), "cannot journal step outcome");
                shared
                    .broken
                    .store(true, std::sync::atomic::Ordering::SeqCst);
            }
            let mut deferred = shared.deferred.lock().unwrap();
            let mut keep = Vec::new();
            for m in deferred.drain(..) {
                let same = matches!(&m, Message::StepReconcile { job_id, step, .. } if *job_id == ticket.job_id && *step == ticket.step);
                if same {
                    if let Ok(ReconcileAnswer::Now(a)) = steps.reconcile(&m) {
                        answers.push(a);
                    }
                } else {
                    keep.push(m);
                }
            }
            *deferred = keep;
        }
        shared.outbox.lock().unwrap().extend(answers);
        shared.changed.notify_one();
    });
}

/// Largest step result the agent reports. Bigger results would not fit in
/// one Core message once framed, and a stuck event would block every later
/// one, so they become a known failure instead.
pub const MAX_RESULT_BYTES: usize = 256 << 10;

fn limit_result(
    v: serde_json::Value,
) -> std::result::Result<serde_json::Value, proto::StepFailure> {
    let size = serde_json::to_vec(&v)
        .map(|b| b.len())
        .unwrap_or(usize::MAX);
    if size > MAX_RESULT_BYTES {
        return Err(proto::StepFailure {
            code: "RESULT_TOO_LARGE".into(),
            summary: format!("the step result is {size} bytes; the limit is {MAX_RESULT_BYTES}"),
            retryable: false,
        });
    }
    Ok(v)
}

fn now_secs() -> OffsetDateTime {
    let now = OffsetDateTime::now_utc();
    now.replace_nanosecond(0).unwrap_or(now)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn backoff_grows_to_cap_with_full_jitter_and_resets_after_stable_session() {
        let mut b = Backoff::default();
        let d: Vec<_> = (0..10)
            .map(|_| b.next(Duration::ZERO, false, 0.999_999))
            .collect();
        assert!(d[0] <= Backoff::BASE);
        assert!(d[1] > d[0] && d[2] > d[1]);
        assert!(d.iter().all(|x| *x <= Backoff::CAP));
        assert!(d[9] > Duration::from_secs(59));
        // Full jitter: the delay may be anywhere from 100 ms to the ceiling.
        let mut b2 = Backoff::default();
        assert_eq!(
            b2.next(Duration::ZERO, false, 0.0),
            Duration::from_millis(100)
        );
        let mut b3 = Backoff::default();
        for _ in 0..8 {
            b3.next(Duration::ZERO, false, 0.5);
        }
        assert_eq!(b3.next(Duration::ZERO, false, 0.5), Backoff::CAP / 2);
        // A session that stayed up resets the ladder.
        assert!(b.next(Backoff::STABLE, false, 0.999_999) <= Backoff::BASE);
    }

    #[test]
    fn oversized_results_become_a_known_failure() {
        let small = serde_json::json!({"ok": true});
        assert_eq!(limit_result(small.clone()).unwrap(), small);
        let big = serde_json::Value::String("x".repeat(MAX_RESULT_BYTES));
        let err = limit_result(big).unwrap_err();
        assert_eq!(err.code, "RESULT_TOO_LARGE");
        assert!(!err.retryable);
    }

    #[test]
    fn renewed_certificate_survives_two_blips_but_not_a_refusal() {
        assert!(!discard_renewed(0, false));
        assert!(!discard_renewed(1, false));
        assert!(discard_renewed(2, false));
        assert!(discard_renewed(0, true));
    }

    #[test]
    fn operator_refusals_wait_ten_minutes() {
        let mut b = Backoff::default();
        let d = b.next(Duration::ZERO, true, 0.999_999);
        assert!(d > Duration::from_secs(590) && d <= Backoff::OPERATOR);
    }
}
