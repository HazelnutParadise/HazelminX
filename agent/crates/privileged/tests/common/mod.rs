#![allow(dead_code)] // each test binary uses a different subset
//! Shared harness: a server on a temp socket with a configurable executor
//! and a real host ledger in the temp dir (probed by a fake).

use ed25519_dalek::SigningKey;
use hazelminx_privileged::BoxFuture;
use hazelminx_privileged::action::Action;
use hazelminx_privileged::audit::MemoryLog;
use hazelminx_privileged::client::Client;
use hazelminx_privileged::command::{OutputSink, StreamKind};
use hazelminx_privileged::executor::{ActionError, ActionResult, Executor};
use hazelminx_privileged::grant::Grant;
use hazelminx_privileged::ledger::entry::{Target, TargetState, UnitState};
use hazelminx_privileged::ledger::probe::{Capture, Probe};
use hazelminx_privileged::ledger::recorder::{LedgerDecl, Recorder};
use hazelminx_privileged::ledger::store::{Ledger, Security};
use hazelminx_privileged::server::{Server, ServerConfig};
use std::os::unix::fs::PermissionsExt;
use std::sync::Arc;
use std::sync::atomic::{AtomicUsize, Ordering};
use time::{Duration, OffsetDateTime};
use tokio::sync::Notify;

/// Fake executor: counts actions, optionally blocks until released, and can
/// stream a large output or fail.
#[derive(Default)]
pub struct FakeExecutor {
    pub started: Arc<AtomicUsize>,
    pub finished: Arc<AtomicUsize>,
    pub release: Arc<Notify>,
    pub block: bool,
    /// Bytes of stdout to stream before returning.
    pub stream_bytes: usize,
    pub fail_with: Option<String>,
    /// Behave like an executor that changes the host without being able to
    /// describe it in the ledger.
    pub no_ledger_description: bool,
}

impl Executor for FakeExecutor {
    fn declare(&self, action: &Action) -> Option<LedgerDecl> {
        (!self.no_ledger_description).then(|| action.ledger_decl())
    }

    fn execute(&self, action: Action, sink: OutputSink) -> BoxFuture<'static, ActionResult> {
        self.started.fetch_add(1, Ordering::SeqCst);
        let block = self.block;
        let release = self.release.clone();
        let finished = self.finished.clone();
        let stream_bytes = self.stream_bytes;
        let fail = self.fail_with.clone();
        Box::pin(async move {
            if block {
                release.notified().await;
            }
            let mut sent = 0usize;
            while sent < stream_bytes {
                let n = (stream_bytes - sent).min(64 * 1024);
                if !sink.send(StreamKind::Stdout, vec![b'x'; n]).await {
                    break;
                }
                sent += n;
            }
            finished.fetch_add(1, Ordering::SeqCst);
            if let Some(m) = fail {
                return Err(ActionError::new(m));
            }
            Ok(serde_json::json!({ "kind": action.kind(), "streamed": sent }))
        })
    }
}

/// Reports every unit as loaded and active, or fails when `fail` is set.
#[derive(Default)]
pub struct FakeProbe {
    pub fail: bool,
    pub calls: AtomicUsize,
}

impl Probe for FakeProbe {
    fn probe(
        &self,
        _: &Target,
        _: Capture,
    ) -> BoxFuture<'static, Result<TargetState, String>> {
        self.calls.fetch_add(1, Ordering::SeqCst);
        let fail = self.fail;
        Box::pin(async move {
            if fail {
                return Err("System has not been booted with systemd".into());
            }
            Ok(TargetState::SystemdUnit(UnitState {
                exists: true,
                load_state: Some("loaded".into()),
                unit_file_state: Some("enabled".into()),
                active_state: Some("active".into()),
                sub_state: Some("running".into()),
            }))
        })
    }
}

#[derive(Default)]
pub struct LedgerSetup {
    pub probe_fails: bool,
    /// Start without a usable ledger (as when it fails its integrity check).
    pub unavailable: bool,
}

pub struct Harness {
    /// Owns the socket directory for the harness's lifetime.
    pub _dir: tempfile::TempDir,
    pub client: Client,
    pub log: Arc<MemoryLog>,
    pub issuer: SigningKey,
    pub executor: Arc<FakeExecutor>,
    pub ledger: Option<Arc<Ledger>>,
    pub probe: Arc<FakeProbe>,
}

/// uid of the test process, taken from a file it just created.
pub fn current_uid() -> u32 {
    use std::os::unix::fs::MetadataExt;
    let f = tempfile::NamedTempFile::new().unwrap();
    f.as_file().metadata().unwrap().uid()
}

impl Harness {
    pub async fn start(executor: FakeExecutor, allowed_uid: u32, max_actions: usize) -> Harness {
        Self::start_with(executor, allowed_uid, max_actions, LedgerSetup::default()).await
    }

    pub async fn start_with(
        executor: FakeExecutor,
        allowed_uid: u32,
        max_actions: usize,
        setup: LedgerSetup,
    ) -> Harness {
        let dir = tempfile::tempdir().unwrap();
        std::fs::set_permissions(dir.path(), std::fs::Permissions::from_mode(0o700)).unwrap();
        let sock = dir.path().join("p.sock");
        let issuer = SigningKey::generate(&mut rand::rngs::OsRng);
        let log = Arc::new(MemoryLog::default());
        let executor = Arc::new(executor);
        let ledger = (!setup.unavailable).then(|| {
            let sec = Security {
                owner_uid: current_uid(),
                trust_root: dir.path().to_path_buf(),
            };
            Arc::new(Ledger::open(&dir.path().join("ledger"), &sec).unwrap().0)
        });
        let probe = Arc::new(FakeProbe {
            fail: setup.probe_fails,
            ..Default::default()
        });
        let recorder = Arc::new(Recorder::new(
            ledger
                .clone()
                .ok_or_else(|| "ledger integrity check failed at line 3".to_string()),
            probe.clone(),
        ));
        let server = Server::new(
            ServerConfig {
                socket_path: sock.clone(),
                allowed_uid,
                issuer_key: issuer.verifying_key(),
                max_concurrent_actions: max_actions,
                request_read_timeout: std::time::Duration::from_millis(300),
            },
            executor.clone(),
            log.clone(),
            recorder,
        );
        let listener = server.bind().unwrap();
        let s2 = server.clone();
        tokio::spawn(async move { s2.serve(listener, std::future::pending()).await });
        Harness {
            client: Client::new(&sock),
            _dir: dir,
            log,
            issuer,
            executor,
            ledger,
            probe,
        }
    }

    pub fn client_path(&self) -> std::path::PathBuf {
        self._dir.path().join("p.sock")
    }

    pub fn grant(&self, id: &str, perms: &[&str]) -> Grant {
        let mut g = Grant::new(
            id,
            "user-1",
            perms.iter().map(|s| s.to_string()).collect(),
            OffsetDateTime::now_utc(),
            Duration::minutes(5),
        );
        g.sign(&self.issuer);
        g
    }
}
