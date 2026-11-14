//! The only way this crate starts a process. A [`CommandSpec`] is a program
//! plus a vector of argv entries and environment pairs; it is handed to the
//! OS as separate strings, never joined and never parsed by a shell. The
//! [`CommandRunner`] trait is the test seam: the real runner spawns, the
//! recording runner just remembers the spec (engineering/architecture.md "Test seams").

use crate::BoxFuture;
use std::collections::VecDeque;
use std::ffi::{OsStr, OsString};
use std::io;
use std::path::PathBuf;
use std::process::Stdio;
use std::sync::Mutex;
use tokio::io::{AsyncRead, AsyncReadExt};
use tokio::sync::mpsc;

/// Fixed environment for every spawned command; the caller's environment is
/// never inherited.
pub const BASE_PATH: &str = "/usr/sbin:/usr/bin:/sbin:/bin";
/// Bytes of stdout/stderr kept for the handler to parse. Streaming to the
/// client is unaffected by this cap.
pub const CAPTURE_LIMIT: usize = 4 * 1024 * 1024;
/// Bounded output channel: a slow reader stalls the producer instead of
/// growing memory (SEC-009).
pub const OUTPUT_CHANNEL_CAPACITY: usize = 32;
/// Largest chunk the runner forwards at once.
pub const OUTPUT_CHUNK_BYTES: usize = 64 * 1024;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CommandSpec {
    pub program: PathBuf,
    pub args: Vec<OsString>,
    pub env: Vec<(OsString, OsString)>,
    /// Start with an empty environment: no PATH or locale is added. For
    /// programs that substitute environment variables into user content
    /// (Compose), so they see exactly what Core analysed.
    pub bare_env: bool,
}

impl CommandSpec {
    pub fn new(program: impl Into<PathBuf>) -> Self {
        Self {
            program: program.into(),
            args: Vec::new(),
            env: Vec::new(),
            bare_env: false,
        }
    }
    pub fn bare_env(mut self) -> Self {
        self.bare_env = true;
        self
    }
    pub fn arg(mut self, a: impl AsRef<OsStr>) -> Self {
        self.args.push(a.as_ref().to_os_string());
        self
    }
    pub fn env(mut self, k: impl AsRef<OsStr>, v: impl AsRef<OsStr>) -> Self {
        self.env
            .push((k.as_ref().to_os_string(), v.as_ref().to_os_string()));
        self
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct CommandOutput {
    /// `None` when killed by a signal.
    pub exit_code: Option<i32>,
    pub stdout: Vec<u8>,
    pub stderr: Vec<u8>,
    /// True when `stdout`/`stderr` were cut at [`CAPTURE_LIMIT`].
    pub truncated: bool,
}

impl CommandOutput {
    pub fn success(&self) -> bool {
        self.exit_code == Some(0)
    }
    pub fn stdout_lossy(&self) -> String {
        String::from_utf8_lossy(&self.stdout).into_owned()
    }
    pub fn stderr_lossy(&self) -> String {
        String::from_utf8_lossy(&self.stderr).into_owned()
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum StreamKind {
    Stdout,
    Stderr,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct OutputChunk {
    pub stream: StreamKind,
    pub data: Vec<u8>,
}

/// Producer end of the bounded output channel handed to handlers.
#[derive(Clone)]
pub struct OutputSink {
    tx: mpsc::Sender<OutputChunk>,
}

impl OutputSink {
    pub fn channel() -> (OutputSink, mpsc::Receiver<OutputChunk>) {
        let (tx, rx) = mpsc::channel(OUTPUT_CHANNEL_CAPACITY);
        (OutputSink { tx }, rx)
    }

    /// A sink whose receiver is dropped; sends are no-ops. For callers that
    /// do not want the output stream.
    pub fn discard() -> OutputSink {
        let (tx, _rx) = mpsc::channel(1);
        OutputSink { tx }
    }

    /// Waits for channel space (back-pressure). Returns `false` once the
    /// receiver is gone; producers may keep running but stop forwarding.
    pub async fn send(&self, stream: StreamKind, data: Vec<u8>) -> bool {
        self.tx.send(OutputChunk { stream, data }).await.is_ok()
    }
}

pub trait CommandRunner: Send + Sync {
    fn run(
        &self,
        spec: CommandSpec,
        sink: OutputSink,
    ) -> BoxFuture<'static, io::Result<CommandOutput>>;
}

/// Spawns with `tokio::process::Command`: cleared environment plus
/// [`BASE_PATH`] and the spec's pairs, stdin closed, argv passed verbatim.
#[derive(Debug, Default, Clone, Copy)]
pub struct HostRunner;

impl CommandRunner for HostRunner {
    fn run(
        &self,
        spec: CommandSpec,
        sink: OutputSink,
    ) -> BoxFuture<'static, io::Result<CommandOutput>> {
        Box::pin(async move {
            let mut cmd = tokio::process::Command::new(&spec.program);
            cmd.args(&spec.args).env_clear();
            if !spec.bare_env {
                cmd.env("PATH", BASE_PATH)
                    .env("LANG", "C.UTF-8")
                    .env("LC_ALL", "C.UTF-8");
            }
            cmd.envs(spec.env.iter().map(|(k, v)| (k.as_os_str(), v.as_os_str())))
                .stdin(Stdio::null())
                .stdout(Stdio::piped())
                .stderr(Stdio::piped())
                .kill_on_drop(true);
            let mut child = cmd.spawn()?;
            let stdout = child.stdout.take().expect("stdout piped");
            let stderr = child.stderr.take().expect("stderr piped");
            let out_task = tokio::spawn(pump(stdout, StreamKind::Stdout, sink.clone()));
            let err_task = tokio::spawn(pump(stderr, StreamKind::Stderr, sink));
            let status = child.wait().await?;
            let (stdout, out_trunc) = out_task.await.map_err(io::Error::other)??;
            let (stderr, err_trunc) = err_task.await.map_err(io::Error::other)??;
            Ok(CommandOutput {
                exit_code: status.code(),
                stdout,
                stderr,
                truncated: out_trunc || err_trunc,
            })
        })
    }
}

/// Forwards a pipe to the sink chunk by chunk and keeps up to
/// [`CAPTURE_LIMIT`] bytes for parsing.
async fn pump<R: AsyncRead + Unpin>(
    mut pipe: R,
    stream: StreamKind,
    sink: OutputSink,
) -> io::Result<(Vec<u8>, bool)> {
    let mut captured = Vec::new();
    let mut truncated = false;
    let mut buf = vec![0u8; OUTPUT_CHUNK_BYTES];
    loop {
        let n = pipe.read(&mut buf).await?;
        if n == 0 {
            break;
        }
        let chunk = &buf[..n];
        if captured.len() < CAPTURE_LIMIT {
            let room = CAPTURE_LIMIT - captured.len();
            captured.extend_from_slice(&chunk[..n.min(room)]);
            if n > room {
                truncated = true;
            }
        } else {
            truncated = true;
        }
        // A departed reader stops forwarding but not the command.
        sink.send(stream, chunk.to_vec()).await;
    }
    Ok((captured, truncated))
}

/// Records every spec and replies with queued outputs (default: exit 0, empty).
#[derive(Default)]
pub struct RecordingRunner {
    calls: Mutex<Vec<CommandSpec>>,
    replies: Mutex<VecDeque<CommandOutput>>,
}

impl RecordingRunner {
    pub fn with_replies(replies: impl IntoIterator<Item = CommandOutput>) -> Self {
        Self {
            calls: Mutex::new(Vec::new()),
            replies: Mutex::new(replies.into_iter().collect()),
        }
    }
    pub fn calls(&self) -> Vec<CommandSpec> {
        self.calls.lock().unwrap().clone()
    }
}

impl CommandRunner for RecordingRunner {
    fn run(
        &self,
        spec: CommandSpec,
        sink: OutputSink,
    ) -> BoxFuture<'static, io::Result<CommandOutput>> {
        self.calls.lock().unwrap().push(spec);
        let reply = self.replies.lock().unwrap().pop_front().unwrap_or_default();
        Box::pin(async move {
            if !reply.stdout.is_empty() {
                sink.send(StreamKind::Stdout, reply.stdout.clone()).await;
            }
            if !reply.stderr.is_empty() {
                sink.send(StreamKind::Stderr, reply.stderr.clone()).await;
            }
            Ok(reply)
        })
    }
}

pub fn output(exit_code: i32, stdout: &str) -> CommandOutput {
    CommandOutput {
        exit_code: Some(exit_code),
        stdout: stdout.as_bytes().to_vec(),
        stderr: Vec::new(),
        truncated: false,
    }
}
