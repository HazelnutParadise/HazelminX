//! Agent-side client. One connection per request; `execute` streams output
//! chunks to the callback and returns the terminal frame.

use crate::action::Action;
use crate::command::StreamKind;
use crate::grant::Grant;
use crate::protocol::{ErrorBody, ErrorCode, Request, Response};
use std::io;
use std::path::{Path, PathBuf};
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::net::UnixStream;

#[derive(Debug, Clone)]
pub struct Client {
    path: PathBuf,
}

#[derive(Debug)]
pub enum ClientError {
    Io(io::Error),
    /// The server answered with an `error` frame.
    Rejected(ErrorBody),
    /// Unexpected or unparsable frame, or the connection ended early.
    Protocol(String),
}

impl std::fmt::Display for ClientError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ClientError::Io(e) => write!(f, "io: {e}"),
            ClientError::Rejected(e) => write!(f, "{:?}: {}", e.code, e.message),
            ClientError::Protocol(m) => write!(f, "protocol: {m}"),
        }
    }
}

impl std::error::Error for ClientError {}

impl From<io::Error> for ClientError {
    fn from(e: io::Error) -> Self {
        ClientError::Io(e)
    }
}

impl ClientError {
    pub fn code(&self) -> Option<ErrorCode> {
        match self {
            ClientError::Rejected(b) => Some(b.code),
            _ => None,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HealthReport {
    pub protocol_version: u32,
    pub version: String,
    pub active_actions: usize,
    pub max_actions: usize,
    pub ledger_ready: bool,
}

impl Client {
    pub fn new(path: impl AsRef<Path>) -> Self {
        Self {
            path: path.as_ref().to_path_buf(),
        }
    }

    pub async fn health(&self) -> Result<HealthReport, ClientError> {
        let mut reader = self.open(&Request::Health).await?;
        match read_frame(&mut reader).await? {
            Response::Health {
                protocol_version,
                version,
                active_actions,
                max_actions,
                ledger_ready,
            } => Ok(HealthReport {
                protocol_version,
                version,
                active_actions,
                max_actions,
                ledger_ready,
            }),
            Response::Error { error, .. } => Err(ClientError::Rejected(error)),
            other => Err(ClientError::Protocol(format!("unexpected frame {other:?}"))),
        }
    }

    pub async fn revoke(&self, grant_id: &str) -> Result<(), ClientError> {
        let mut reader = self
            .open(&Request::Revoke {
                grant_id: grant_id.to_string(),
            })
            .await?;
        match read_frame(&mut reader).await? {
            Response::Revoked { .. } => Ok(()),
            Response::Error { error, .. } => Err(ClientError::Rejected(error)),
            other => Err(ClientError::Protocol(format!("unexpected frame {other:?}"))),
        }
    }

    /// Runs `action` under `grant`; `on_output` receives each chunk as it
    /// arrives. Returns the action's result value.
    pub async fn execute(
        &self,
        request_id: &str,
        grant: &Grant,
        action: &Action,
        on_output: impl FnMut(StreamKind, Vec<u8>),
    ) -> Result<serde_json::Value, ClientError> {
        self.execute_for_job(request_id, None, grant, action, on_output)
            .await
    }

    /// [`Self::execute`] on behalf of a job; `job_id` is recorded in the
    /// host ledger for every change the action makes.
    pub async fn execute_for_job(
        &self,
        request_id: &str,
        job_id: Option<&str>,
        grant: &Grant,
        action: &Action,
        mut on_output: impl FnMut(StreamKind, Vec<u8>),
    ) -> Result<serde_json::Value, ClientError> {
        let mut reader = self
            .open(&Request::Action {
                request_id: request_id.to_string(),
                job_id: job_id.map(str::to_string),
                grant: grant.clone(),
                action: serde_json::to_value(action).expect("Action serializes"),
            })
            .await?;
        loop {
            match read_frame(&mut reader).await? {
                Response::Output { stream, data } => {
                    let bytes = Response::output_bytes(&data).ok_or_else(|| {
                        ClientError::Protocol("output frame is not base64".into())
                    })?;
                    on_output(stream, bytes);
                }
                Response::Result { result, .. } => return Ok(result),
                Response::Error { error, .. } => return Err(ClientError::Rejected(error)),
                other => return Err(ClientError::Protocol(format!("unexpected frame {other:?}"))),
            }
        }
    }

    /// Sends one raw line and returns every frame the server answers with.
    /// Tests use it for malformed and oversized requests.
    pub async fn send_raw(&self, line: &[u8]) -> Result<Vec<Response>, ClientError> {
        let mut stream = UnixStream::connect(&self.path).await?;
        stream.write_all(line).await?;
        if line.last() != Some(&b'\n') {
            stream.write_all(b"\n").await?;
        }
        stream.flush().await?;
        let mut reader = BufReader::new(stream);
        let mut frames = Vec::new();
        loop {
            let mut buf = String::new();
            if reader.read_line(&mut buf).await? == 0 {
                return Ok(frames);
            }
            let frame = serde_json::from_str::<Response>(buf.trim_end())
                .map_err(|e| ClientError::Protocol(format!("bad frame: {e}")))?;
            frames.push(frame);
        }
    }

    /// Opens a connection, writes the request, and returns the reader
    /// without consuming any frame. Tests use it to hold a slow connection.
    pub async fn open(&self, req: &Request) -> Result<BufReader<UnixStream>, ClientError> {
        let mut stream = UnixStream::connect(&self.path).await?;
        let mut bytes = serde_json::to_vec(req).expect("Request serializes");
        bytes.push(b'\n');
        stream.write_all(&bytes).await?;
        stream.flush().await?;
        Ok(BufReader::new(stream))
    }
}

async fn read_frame(reader: &mut BufReader<UnixStream>) -> Result<Response, ClientError> {
    let mut buf = String::new();
    if reader.read_line(&mut buf).await? == 0 {
        return Err(ClientError::Protocol(
            "connection closed before a terminal frame".into(),
        ));
    }
    serde_json::from_str::<Response>(buf.trim_end())
        .map_err(|e| ClientError::Protocol(format!("bad frame: {e}")))
}
