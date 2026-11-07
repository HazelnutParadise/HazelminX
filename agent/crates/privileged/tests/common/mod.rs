//! Shared harness: a server on a temp socket with a configurable executor.

use ed25519_dalek::SigningKey;
use hazelminx_privileged::BoxFuture;
use hazelminx_privileged::action::Action;
use hazelminx_privileged::audit::MemoryLog;
use hazelminx_privileged::client::Client;
use hazelminx_privileged::command::{OutputSink, StreamKind};
use hazelminx_privileged::executor::{ActionError, ActionResult, Executor};
use hazelminx_privileged::grant::Grant;
use hazelminx_privileged::server::{Server, ServerConfig};
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
}

impl Executor for FakeExecutor {
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

pub struct Harness {
    /// Owns the socket directory for the harness's lifetime.
    pub _dir: tempfile::TempDir,
    pub client: Client,
    pub log: Arc<MemoryLog>,
    pub issuer: SigningKey,
    pub executor: Arc<FakeExecutor>,
}

/// uid of the test process, taken from a file it just created.
pub fn current_uid() -> u32 {
    use std::os::unix::fs::MetadataExt;
    let f = tempfile::NamedTempFile::new().unwrap();
    f.as_file().metadata().unwrap().uid()
}

impl Harness {
    pub async fn start(executor: FakeExecutor, allowed_uid: u32, max_actions: usize) -> Harness {
        let dir = tempfile::tempdir().unwrap();
        let sock = dir.path().join("p.sock");
        let issuer = SigningKey::generate(&mut rand::rngs::OsRng);
        let log = Arc::new(MemoryLog::default());
        let executor = Arc::new(executor);
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
