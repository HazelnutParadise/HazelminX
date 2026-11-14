//! Job step actions `compose.check` and `compose.deploy` (ticket 044.2,
//! decision 0035). The Core sends the revision's manifest, its files and a
//! grant bound to the manifest. The agent writes each file to its spool as
//! `<spool>/<revision>/<sha256>`, asks the privileged executor to run
//! `compose_check` or `compose_deploy`, forwards the executor's output as
//! step progress, and removes the spool afterwards. The executor trusts only
//! the hashes it was signed for, so the spool needs no other protection than
//! being the agent's own.

use hazelminx_privileged::action::Action;
use hazelminx_privileged::client::{Client, ClientError};
use hazelminx_privileged::command::StreamKind;
use hazelminx_privileged::compose_manifest::ComposeManifest;
use hazelminx_privileged::grant::Grant;
use hazelminx_protocol::{ExecutorGrant, Resource, StepFailure};
use serde_json::{Map, Value};
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use std::io::Write;
use std::os::unix::fs::{DirBuilderExt, OpenOptionsExt};
use std::path::{Path, PathBuf};

pub const CHECK: &str = "compose.check";
pub const DEPLOY: &str = "compose.deploy";
/// The resource kind a Compose job reserves: the project name.
pub const RESOURCE_KIND: &str = "compose-project";
/// Longest log line forwarded; longer lines are cut.
pub const MAX_LOG_LINE: usize = 2000;

pub fn is_compose(action: &str) -> bool {
    action == CHECK || action == DEPLOY
}

/// Parsed and checked parameters.
pub struct Params {
    pub grant: Grant,
    pub manifest: ComposeManifest,
    /// Content by sha256.
    pub files: HashMap<String, String>,
}

pub fn parse(p: &Map<String, Value>) -> Result<Params, String> {
    for k in p.keys() {
        if !matches!(k.as_str(), "grant" | "manifest" | "files") {
            return Err(format!("unknown param {k:?}"));
        }
    }
    let grant: ExecutorGrant = serde_json::from_value(p.get("grant").cloned().ok_or("grant is missing")?)
        .map_err(|e| format!("grant: {e}"))?;
    let grant: Grant = serde_json::to_value(&grant)
        .and_then(serde_json::from_value)
        .map_err(|e| format!("grant: {e}"))?;
    let manifest: ComposeManifest =
        serde_json::from_value(p.get("manifest").cloned().ok_or("manifest is missing")?)
            .map_err(|e| format!("manifest: {e}"))?;
    let list = p
        .get("files")
        .and_then(Value::as_array)
        .ok_or("files must be an array")?;
    let mut files = HashMap::new();
    for f in list {
        let o = f.as_object().ok_or("a file must be an object")?;
        if o.keys().any(|k| k != "sha256" && k != "content") {
            return Err("a file has keys other than sha256 and content".into());
        }
        let sha = o.get("sha256").and_then(Value::as_str).ok_or("a file has no sha256")?;
        let content = o.get("content").and_then(Value::as_str).ok_or("a file has no content")?;
        if hex::encode(Sha256::digest(content.as_bytes())) != sha {
            return Err("a file's content does not match its sha256".into());
        }
        files.insert(sha.to_string(), content.to_string());
    }
    for f in &manifest.files {
        if !files.contains_key(f.sha256.as_str()) {
            return Err(format!("no content for {}", f.path.as_str()));
        }
    }
    if files.len() > manifest.files.len() {
        return Err("files has content the manifest does not list".into());
    }
    Ok(Params {
        grant,
        manifest,
        files,
    })
}

pub fn validate(p: &Map<String, Value>) -> Result<(), String> {
    parse(p).map(|_| ())
}

pub fn touches(p: &Map<String, Value>) -> Vec<Resource> {
    match p
        .get("manifest")
        .and_then(|m| m.get("project"))
        .and_then(Value::as_str)
    {
        Some(name) => vec![Resource {
            kind: RESOURCE_KIND.into(),
            id: name.into(),
        }],
        None => Vec::new(),
    }
}

fn failure(code: &str, summary: impl Into<String>, retryable: bool) -> StepFailure {
    StepFailure {
        code: code.into(),
        summary: summary.into(),
        retryable,
    }
}

/// Writes the revision's spool: `<spool>/<revision>/<sha256>`, 0700/0600.
fn write_spool(spool: &Path, p: &Params) -> std::io::Result<PathBuf> {
    std::fs::DirBuilder::new().mode(0o700).recursive(true).create(spool)?;
    let dir = spool.join(p.manifest.revision.as_str());
    // A retried step finds its own earlier spool; start it again.
    let _ = std::fs::remove_dir_all(&dir);
    std::fs::DirBuilder::new().mode(0o700).create(&dir)?;
    for (sha, content) in &p.files {
        let mut f = std::fs::OpenOptions::new()
            .write(true)
            .create_new(true)
            .mode(0o600)
            .open(dir.join(sha))?;
        f.write_all(content.as_bytes())?;
    }
    Ok(dir)
}

/// Splits executor output into log lines; a partial line waits for the
/// next chunk.
#[derive(Default)]
pub struct Lines {
    pending: Vec<u8>,
}

impl Lines {
    pub fn push(&mut self, data: &[u8]) -> Vec<String> {
        self.pending.extend_from_slice(data);
        let mut out = Vec::new();
        while let Some(i) = self.pending.iter().position(|b| *b == b'\n') {
            let line: Vec<u8> = self.pending.drain(..=i).collect();
            out.push(clean(&line[..line.len() - 1]));
        }
        out
    }

    pub fn finish(&mut self) -> Option<String> {
        (!self.pending.is_empty()).then(|| clean(&std::mem::take(&mut self.pending)))
    }
}

fn clean(b: &[u8]) -> String {
    String::from_utf8_lossy(b)
        .chars()
        .filter(|c| *c == '\t' || !c.is_control())
        .take(MAX_LOG_LINE)
        .collect()
}

/// Runs one Compose step. `log` receives the executor's output line by line.
pub async fn run(
    client: &Client,
    spool: &Path,
    request_id: &str,
    job_id: &str,
    action: &str,
    params: &Map<String, Value>,
    mut log: impl FnMut(Vec<String>),
) -> Result<Value, StepFailure> {
    let p = parse(params).map_err(|e| failure("INVALID_PARAMS", e, false))?;
    let dir = write_spool(spool, &p)
        .map_err(|e| failure("SPOOL_FAILED", format!("cannot stage the revision's files: {e}"), true))?;
    let manifest = p.manifest.clone();
    let act = if action == DEPLOY {
        Action::ComposeDeploy { manifest }
    } else {
        Action::ComposeCheck { manifest }
    };
    let mut lines = Lines::default();
    let res = client
        .execute_for_job(request_id, Some(job_id), &p.grant, &act, |stream, data| {
            if matches!(stream, StreamKind::Stdout | StreamKind::Stderr) {
                let l = lines.push(&data);
                if !l.is_empty() {
                    log(l);
                }
            }
        })
        .await;
    if let Some(l) = lines.finish() {
        log(vec![l]);
    }
    let _ = std::fs::remove_dir_all(&dir);
    res.map_err(|e| match e {
        ClientError::Rejected(body) => {
            let code = serde_json::to_value(body.code)
                .ok()
                .and_then(|v| v.as_str().map(str::to_uppercase))
                .unwrap_or_else(|| "EXECUTOR_REFUSED".into());
            failure(&format!("EXECUTOR_{code}"), body.message, false)
        }
        other => failure(
            "EXECUTOR_UNREACHABLE",
            format!("the privileged executor did not answer: {other}"),
            false,
        ),
    })
}

/// Example params for the tests of every action.
#[cfg(test)]
pub fn sample_params() -> Map<String, Value> {
    tests::params()
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn sha(s: &str) -> String {
        hex::encode(Sha256::digest(s.as_bytes()))
    }

    pub fn params() -> Map<String, Value> {
        let c = "services: {}\n";
        json!({
            "grant": {"id": "g", "subject": "u", "permissions": ["host.compose.deploy"],
                      "not_before": "2026-11-14T00:00:00Z", "expires_at": "2026-11-14T00:05:00Z",
                      "binding": "a".repeat(64), "signature": "00"},
            "manifest": {"project": "shop-prod-web", "revision": "r1",
                         "environment": "env:/var/run/docker.sock", "main_file": "compose.yaml",
                         "files": [{"path": "compose.yaml", "size": c.len(), "sha256": sha(c)}]},
            "files": [{"sha256": sha(c), "content": c}]
        })
        .as_object()
        .cloned()
        .unwrap()
    }

    #[test]
    fn parses_and_names_the_project_it_touches() {
        let p = params();
        assert!(validate(&p).is_ok());
        assert_eq!(
            touches(&p),
            vec![Resource {
                kind: RESOURCE_KIND.into(),
                id: "shop-prod-web".into()
            }]
        );
    }

    #[test]
    fn refuses_content_that_does_not_match_the_manifest() {
        let mut p = params();
        p["files"][0]["content"] = json!("services: {x: {}}\n");
        assert!(validate(&p).is_err());
        let mut p = params();
        p["files"].as_array_mut().unwrap().push(json!({"sha256": sha("x"), "content": "x"}));
        assert!(validate(&p).is_err());
        let mut p = params();
        p.insert("cmd".into(), json!("id"));
        assert!(validate(&p).is_err());
        let mut p = params();
        p["manifest"]["main_file"] = json!("../compose.yaml");
        assert!(validate(&p).is_err());
    }

    #[test]
    fn spool_is_content_addressed_and_private() {
        use std::os::unix::fs::PermissionsExt;
        let d = tempfile::tempdir().unwrap();
        let spool = d.path().join("compose-spool");
        let p = parse(&params()).unwrap();
        let dir = write_spool(&spool, &p).unwrap();
        let c = "services: {}\n";
        assert_eq!(std::fs::read_to_string(dir.join(sha(c))).unwrap(), c);
        assert_eq!(std::fs::metadata(&dir).unwrap().permissions().mode() & 0o777, 0o700);
        // Written again on a retried step.
        write_spool(&spool, &p).unwrap();
    }

    #[test]
    fn output_becomes_clean_lines() {
        let mut l = Lines::default();
        assert_eq!(l.push(b"a\x1b[1mb\nc"), vec!["a[1mb".to_string()]);
        assert_eq!(l.push(b"d\n"), vec!["cd".to_string()]);
        assert_eq!(l.push(b"tail"), Vec::<String>::new());
        assert_eq!(l.finish(), Some("tail".into()));
    }
}
