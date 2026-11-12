//! `hazelminx-agent enroll`: trade a one-time enrollment code for a node
//! certificate (engineering/contracts/README.md, 註冊; decision 0031).
//!
//! 1. Fetch the Core CA. In `pinned` mode the agent has no CA yet, so this
//!    TLS connection is not verified; the answer carries an HMAC keyed by the
//!    code and is only trusted when it verifies.
//! 2. Send the code and this node's public key over TLS verified against
//!    that CA (or the proxy's chain), then ask again until an administrator
//!    approved or rejected the host.
//! 3. Check the certificate and write it, the CA, the privileged executor's
//!    issuer key and the Core URL into the state directory.

use crate::config::CoreTransport;
use crate::connection::tls;
use crate::identity::Identity;
use anyhow::{Context, Result, anyhow, bail};
use hazelminx_protocol::enroll::{
    DISCOVERY_PATH, Discovery, ENROLL_PATH, EnrollRequest, EnrollResponse, EnrollmentCode,
    STATUS_APPROVED, STATUS_PENDING, verify_discovery_mac,
};
use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};
use std::time::Duration;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;
use tracing::info;

/// Where the Core URL learned at enrollment is kept.
pub const ENROLLMENT_FILE: &str = "enrollment.json";

/// What `enrollment.json` holds.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct EnrollmentRecord {
    pub core_url: String,
    pub node_id: String,
    pub enrolled_at: String,
}

/// Reads `state_dir/enrollment.json`; `None` when the node never enrolled.
pub fn load_record(state_dir: &Path) -> Result<Option<EnrollmentRecord>> {
    let p = state_dir.join(ENROLLMENT_FILE);
    match std::fs::read_to_string(&p) {
        Ok(t) => Ok(Some(
            serde_json::from_str(&t).with_context(|| format!("parse {}", p.display()))?,
        )),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(e) => Err(e).with_context(|| format!("read {}", p.display())),
    }
}

pub struct Options {
    pub core_url: String,
    pub code: EnrollmentCode,
    pub group: String,
    pub transport: CoreTransport,
    pub proxy_ca_file: Option<PathBuf>,
    pub state_dir: PathBuf,
    pub agent_version: String,
    /// How long to wait for an administrator.
    pub wait: Duration,
}

/// The host facts an administrator checks before approving.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HostFacts {
    pub hostname: String,
    pub machine_id: String,
    pub os: String,
    pub arch: String,
}

impl HostFacts {
    pub fn read() -> HostFacts {
        let first = |paths: &[&str]| {
            paths
                .iter()
                .filter_map(|p| std::fs::read_to_string(p).ok())
                .map(|s| s.trim().to_string())
                .find(|s| !s.is_empty())
                .unwrap_or_default()
        };
        let mut hostname = first(&["/proc/sys/kernel/hostname", "/etc/hostname"]);
        if hostname.is_empty() {
            hostname = std::env::var("HOSTNAME").unwrap_or_default();
        }
        let os = std::fs::read_to_string("/etc/os-release")
            .ok()
            .and_then(|t| pretty_name(&t))
            .unwrap_or_else(|| std::env::consts::OS.to_string());
        HostFacts {
            hostname,
            machine_id: first(&["/etc/machine-id", "/var/lib/dbus/machine-id"]),
            os,
            arch: std::env::consts::ARCH.to_string(),
        }
    }
}

fn pretty_name(os_release: &str) -> Option<String> {
    os_release.lines().find_map(|l| {
        l.strip_prefix("PRETTY_NAME=")
            .map(|v| v.trim().trim_matches('"').to_string())
            .filter(|v| !v.is_empty())
    })
}

/// What a finished enrollment reports.
#[derive(Debug)]
pub struct Enrolled {
    pub node_id: String,
    pub fingerprint: String,
}

/// Runs the whole enrollment. Nothing is written unless the Core approved
/// the host and the certificate checks out.
pub async fn run(opts: &Options, id: &Identity, facts: &HostFacts) -> Result<Enrolled> {
    let target = Target::parse(&opts.core_url)?;
    let ca_pem = discover(opts, &target).await?;
    let ca = tls::parse_certificate(&ca_pem).context("discovered Core CA")?;
    let req = EnrollRequest {
        code: opts.code.normalized(),
        node_public_key: {
            use base64::Engine;
            base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(id.verifying_key().as_bytes())
        },
        hostname: facts.hostname.clone(),
        machine_id: facts.machine_id.clone(),
        os: facts.os.clone(),
        arch: facts.arch.clone(),
        agent_version: opts.agent_version.clone(),
        group: opts.group.clone(),
    };
    let body = serde_json::to_vec(&req)?;
    let config =
        tls::enroll_client_config(opts.transport, opts.proxy_ca_file.as_deref(), Some(&ca))?;
    let deadline = tokio::time::Instant::now() + opts.wait;
    let mut announced = false;
    let approved = loop {
        let (status, text) = target
            .request(
                config.clone(),
                opts.transport,
                "POST",
                ENROLL_PATH,
                Some(&body),
            )
            .await?;
        let r: EnrollResponse = serde_json::from_str(&text)
            .with_context(|| format!("core answered {status} with an unreadable body"))?;
        match (status, r.status.as_str()) {
            (200, STATUS_APPROVED) => break r,
            (202, STATUS_PENDING) => {
                if !announced {
                    println!(
                        "Waiting for an administrator to approve this host.\n  enrollment: {}\n  host name:  {}\n  machine-id: {}\n  key:        SHA256:{}",
                        r.enrollment_id,
                        facts.hostname,
                        facts.machine_id,
                        id.fingerprint()
                    );
                    announced = true;
                }
                let wait = Duration::from_secs(r.retry_after_seconds.clamp(2, 30));
                if tokio::time::Instant::now() + wait > deadline {
                    bail!(
                        "no decision within {}s; run the same command again to keep waiting (the code stays reserved for this host for 24 hours)",
                        opts.wait.as_secs()
                    );
                }
                tokio::time::sleep(wait).await;
            }
            _ => bail!("core refused the enrollment: {}: {}", r.code, r.message),
        }
    };
    anyhow::ensure!(
        approved.ca_certificate == ca_pem,
        "the approved answer names another Core CA than the one discovered; not writing anything"
    );
    tls::verify_node_certificate(
        &approved.node_certificate,
        &ca,
        id.verifying_key().as_bytes(),
    )
    .context("the certificate from the core does not check out; not writing anything")?;
    write_files(opts, id, &approved)?;
    info!(node = %approved.node_id, fingerprint = %id.fingerprint(), "enrolled");
    Ok(Enrolled {
        node_id: approved.node_id,
        fingerprint: id.fingerprint(),
    })
}

/// Step 1: the CA, authenticated by the code.
async fn discover(opts: &Options, target: &Target) -> Result<String> {
    let config = tls::enroll_client_config(opts.transport, opts.proxy_ca_file.as_deref(), None)?;
    let path = format!("{DISCOVERY_PATH}?codeId={}", opts.code.id());
    let (status, text) = target
        .request(config, opts.transport, "GET", &path, None)
        .await?;
    if status != 200 {
        let r: EnrollResponse = serde_json::from_str(&text).unwrap_or_default();
        bail!(
            "core has no usable enrollment code {} (HTTP {status} {}); check the code, or create a new one: codes expire after 10 minutes and work once",
            opts.code.id(),
            r.code
        );
    }
    let d: Discovery = serde_json::from_str(&text).context("discovery answer")?;
    anyhow::ensure!(
        verify_discovery_mac(&opts.code.key(), d.ca_certificate.as_bytes(), &d.mac),
        "the Core CA offered at {} is not authenticated by this code; either the code is wrong or something between this host and the Core is intercepting the connection. Nothing was sent.",
        opts.core_url
    );
    Ok(d.ca_certificate)
}

fn write_files(opts: &Options, id: &Identity, r: &EnrollResponse) -> Result<()> {
    let dir = id.dir();
    tls::write_atomic(dir, tls::CORE_CA_FILE, r.ca_certificate.as_bytes())?;
    tls::discard_next(dir)?;
    tls::write_atomic(dir, tls::NODE_CERT_FILE, r.node_certificate.as_bytes())?;
    if !r.privileged_issuer_key.is_empty() {
        tls::write_atomic(
            dir,
            tls::ISSUER_KEY_FILE,
            r.privileged_issuer_key.as_bytes(),
        )?;
    }
    let rec = EnrollmentRecord {
        core_url: opts.core_url.clone(),
        node_id: r.node_id.clone(),
        enrolled_at: time::OffsetDateTime::now_utc()
            .format(&time::format_description::well_known::Rfc3339)
            .unwrap_or_default(),
    };
    tls::write_atomic(
        &opts.state_dir,
        ENROLLMENT_FILE,
        serde_json::to_string_pretty(&rec)?.as_bytes(),
    )
}

/// The agent listener address from `core_url`.
struct Target {
    host: String,
    port: u16,
}

impl Target {
    fn parse(core_url: &str) -> Result<Target> {
        let url = url::Url::parse(core_url).with_context(|| format!("core_url {core_url:?}"))?;
        anyhow::ensure!(
            matches!(url.scheme(), "https" | "wss"),
            "core_url {core_url:?} must start with https:// or wss://"
        );
        let host = url
            .host_str()
            .ok_or_else(|| anyhow!("core_url has no host"))?
            .trim_start_matches('[')
            .trim_end_matches(']')
            .to_string();
        Ok(Target {
            host,
            port: url.port().unwrap_or(443),
        })
    }

    /// One HTTP/1.1 request with `Connection: close`; returns the status and
    /// the body (the Core always sends Content-Length).
    async fn request(
        &self,
        config: std::sync::Arc<rustls::ClientConfig>,
        transport: CoreTransport,
        method: &str,
        path: &str,
        body: Option<&[u8]>,
    ) -> Result<(u16, String)> {
        let tcp = tokio::time::timeout(
            Duration::from_secs(10),
            TcpStream::connect((self.host.as_str(), self.port)),
        )
        .await
        .map_err(|_| anyhow!("TCP connect to {}:{} timed out", self.host, self.port))?
        .with_context(|| format!("TCP connect to {}:{}", self.host, self.port))?;
        let name = tls::server_name(transport, &self.host)?;
        let mut tls_stream = tokio::time::timeout(
            Duration::from_secs(10),
            tokio_rustls::TlsConnector::from(config).connect(name, tcp),
        )
        .await
        .map_err(|_| anyhow!("TLS handshake with {}:{} timed out", self.host, self.port))?
        .with_context(|| format!("TLS handshake with {}:{}", self.host, self.port))?;
        let host_header = if self.host.contains(':') {
            format!("[{}]:{}", self.host, self.port)
        } else {
            format!("{}:{}", self.host, self.port)
        };
        let mut req = format!(
            "{method} {path} HTTP/1.1\r\nHost: {host_header}\r\nConnection: close\r\nAccept: application/json\r\nUser-Agent: hazelminx-agent\r\n"
        );
        if let Some(b) = body {
            req.push_str(&format!(
                "Content-Type: application/json\r\nContent-Length: {}\r\n",
                b.len()
            ));
        }
        req.push_str("\r\n");
        let mut out = req.into_bytes();
        out.extend_from_slice(body.unwrap_or_default());
        let io = async {
            tls_stream.write_all(&out).await?;
            tls_stream.flush().await?;
            let mut buf = Vec::new();
            // A peer that closes without close_notify still delivered the body.
            match (&mut tls_stream)
                .take(MAX_RESPONSE as u64 + 1)
                .read_to_end(&mut buf)
                .await
            {
                Ok(_) => {}
                Err(e) if e.kind() == std::io::ErrorKind::UnexpectedEof && !buf.is_empty() => {}
                Err(e) => return Err(e),
            }
            Ok::<_, std::io::Error>(buf)
        };
        let buf = tokio::time::timeout(Duration::from_secs(30), io)
            .await
            .map_err(|_| anyhow!("core did not answer within 30s"))?
            .context("talk to core")?;
        parse_response(&buf)
    }
}

const MAX_RESPONSE: usize = 64 << 10;

/// Parses an HTTP/1.x response that carries Content-Length.
fn parse_response(buf: &[u8]) -> Result<(u16, String)> {
    anyhow::ensure!(
        buf.len() <= MAX_RESPONSE,
        "core answer is larger than 64 KiB"
    );
    let split = buf
        .windows(4)
        .position(|w| w == b"\r\n\r\n")
        .ok_or_else(|| anyhow!("core answer has no HTTP header end"))?;
    let head = std::str::from_utf8(&buf[..split]).context("core answer header is not text")?;
    let body = &buf[split + 4..];
    let mut lines = head.split("\r\n");
    let status_line = lines.next().unwrap_or_default();
    let mut parts = status_line.splitn(3, ' ');
    let (proto, code) = (parts.next().unwrap_or(""), parts.next().unwrap_or(""));
    anyhow::ensure!(
        proto.starts_with("HTTP/1."),
        "core answer is not HTTP/1.x: {status_line:?}"
    );
    let status: u16 = code
        .parse()
        .map_err(|_| anyhow!("bad HTTP status line {status_line:?}"))?;
    let mut length = None;
    for l in lines {
        if let Some((k, v)) = l.split_once(':') {
            if k.trim().eq_ignore_ascii_case("content-length") {
                length = Some(
                    v.trim()
                        .parse::<usize>()
                        .map_err(|_| anyhow!("bad Content-Length {v:?}"))?,
                );
            } else if k.trim().eq_ignore_ascii_case("transfer-encoding") {
                bail!("core answer is chunked; expected Content-Length");
            }
        }
    }
    let length = length.ok_or_else(|| anyhow!("core answer has no Content-Length"))?;
    anyhow::ensure!(
        body.len() >= length,
        "core answer was cut short ({} of {length} bytes)",
        body.len()
    );
    let text = std::str::from_utf8(&body[..length]).context("core answer body is not UTF-8")?;
    Ok((status, text.to_string()))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_responses_with_content_length_only() {
        let ok = b"HTTP/1.1 202 Accepted\r\nContent-Type: application/json\r\nContent-Length: 2\r\n\r\n{}";
        assert_eq!(parse_response(ok).unwrap(), (202, "{}".to_string()));
        let extra = b"HTTP/1.1 200 OK\r\ncontent-length: 2\r\n\r\n{}trailing";
        assert_eq!(parse_response(extra).unwrap().1, "{}");
        for bad in [
            &b"HTTP/1.1 200 OK\r\n\r\n{}"[..],
            b"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\nContent-Length: 2\r\n\r\n{}",
            b"HTTP/1.1 200 OK\r\nContent-Length: 10\r\n\r\n{}",
            b"SSH-2.0-OpenSSH\r\n\r\n",
            b"no header end",
        ] {
            assert!(
                parse_response(bad).is_err(),
                "{:?}",
                String::from_utf8_lossy(bad)
            );
        }
    }

    #[test]
    fn reads_the_pretty_os_name() {
        assert_eq!(
            pretty_name(
                "NAME=\"Debian GNU/Linux\"\nPRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\n"
            )
            .as_deref(),
            Some("Debian GNU/Linux 13 (trixie)")
        );
        assert_eq!(pretty_name("NAME=x\n"), None);
    }

    #[test]
    fn target_takes_host_and_port_from_the_url() {
        let t = Target::parse("https://core.example:9801").unwrap();
        assert_eq!((t.host.as_str(), t.port), ("core.example", 9801));
        let t = Target::parse("wss://[::1]:9801/x").unwrap();
        assert_eq!((t.host.as_str(), t.port), ("::1", 9801));
        assert!(Target::parse("http://core:9801").is_err());
    }

    #[test]
    fn enrollment_record_round_trips() {
        let dir = tempfile::tempdir().unwrap();
        assert_eq!(load_record(dir.path()).unwrap(), None);
        let rec = EnrollmentRecord {
            core_url: "https://core:9801".into(),
            node_id: "nod_1".into(),
            enrolled_at: "2026-11-12T00:00:00Z".into(),
        };
        tls::write_atomic(
            dir.path(),
            ENROLLMENT_FILE,
            serde_json::to_string(&rec).unwrap().as_bytes(),
        )
        .unwrap();
        assert_eq!(load_record(dir.path()).unwrap(), Some(rec));
    }
}
