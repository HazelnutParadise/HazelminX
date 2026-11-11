//! Actions this agent can run for a job step, and the capabilities it
//! declares in `hello`. Every action has a fixed name, a version and a
//! strict parameter check; anything else is refused before it runs.

use hazelminx_protocol::{
    CAP_AGENT_INFO, CAP_HOST_LEDGER, CAP_JOBS, CAP_NODE_CERTIFICATE, Capability, JOBS_VERSION,
    Resource, StepFailure,
};
use serde_json::{Map, Value, json};

/// One action the agent implements.
#[derive(Debug, Clone, Copy)]
pub struct ActionSpec {
    pub name: &'static str,
    pub version: u32,
    /// Rejects params this version does not define (unknown keys, wrong
    /// types). Returning `Err` means the step is refused, not run.
    pub validate: fn(&Map<String, Value>) -> Result<(), String>,
    /// The resources the (validated) params act on. A step is refused when
    /// any of them is not in its `resources` (SEC-004).
    pub touches: fn(&Map<String, Value>) -> Vec<Resource>,
}

/// Actions that only read; they touch nothing. Every other action must name
/// what it touches (checked by a test).
pub const READ_ONLY: &[&str] = &[CAP_AGENT_INFO];

fn touches_nothing(_: &Map<String, Value>) -> Vec<Resource> {
    Vec::new()
}

/// Example params of each writing action, for the test that it names what
/// it touches.
#[cfg(test)]
pub fn sample_params(name: &str) -> Option<Map<String, Value>> {
    match name {
        #[cfg(feature = "test-actions")]
        test_actions::SLEEP => json!({"millis": 1, "counterFile": "/tmp/c"})
            .as_object()
            .cloned(),
        _ => None,
    }
}

fn no_params(p: &Map<String, Value>) -> Result<(), String> {
    match p.keys().next() {
        None => Ok(()),
        Some(k) => Err(format!("unknown param {k:?}; this action takes none")),
    }
}

/// Actions of this build.
pub fn specs() -> &'static [ActionSpec] {
    &[
        ActionSpec {
            name: CAP_AGENT_INFO,
            version: 1,
            validate: no_params,
            touches: touches_nothing,
        },
        #[cfg(feature = "test-actions")]
        ActionSpec {
            name: test_actions::SLEEP,
            version: 1,
            validate: test_actions::validate_sleep,
            touches: test_actions::touches_sleep,
        },
    ]
}

/// Capabilities declared in `hello`: the jobs protocol, the host change
/// ledger relay (`ledger_pull`), certificate rotation, and every action.
pub fn capabilities() -> Vec<Capability> {
    let mut caps = vec![
        Capability {
            name: CAP_JOBS.to_string(),
            version: JOBS_VERSION,
        },
        Capability {
            name: CAP_HOST_LEDGER.to_string(),
            version: 1,
        },
        Capability {
            name: CAP_NODE_CERTIFICATE.to_string(),
            version: 1,
        },
    ];
    caps.extend(specs().iter().map(|s| Capability {
        name: s.name.to_string(),
        version: s.version,
    }));
    caps
}

/// Facts an action may need about this agent.
#[derive(Debug, Clone)]
pub struct AgentFacts {
    pub agent_version: String,
    pub fingerprint: String,
}

/// Runs a validated action. Unknown names cannot reach here (the step
/// decision refuses them), so they are reported as a failure rather than a
/// panic.
pub async fn run(
    facts: &AgentFacts,
    name: &str,
    params: &Map<String, Value>,
) -> Result<Value, StepFailure> {
    let _ = params;
    match name {
        CAP_AGENT_INFO => Ok(agent_info(facts)),
        #[cfg(feature = "test-actions")]
        test_actions::SLEEP => test_actions::sleep(params).await,
        other => Err(StepFailure {
            code: "UNSUPPORTED_ACTION".into(),
            summary: format!("action {other:?} is not implemented"),
            retryable: false,
        }),
    }
}

fn agent_info(facts: &AgentFacts) -> Value {
    let hostname = std::fs::read_to_string("/etc/hostname")
        .ok()
        .map(|s| s.trim().to_string())
        .filter(|s| !s.is_empty())
        .or_else(|| std::env::var("HOSTNAME").ok())
        .unwrap_or_default();
    json!({
        "agentVersion": facts.agent_version,
        "fingerprint": facts.fingerprint,
        "os": std::env::consts::OS,
        "arch": std::env::consts::ARCH,
        "hostname": hostname,
    })
}

/// Actions compiled only into test builds of the agent (`--features
/// test-actions`), used by the Core↔Agent interop test to hold a step open
/// and count how often it really ran. Never part of a release build.
#[cfg(feature = "test-actions")]
pub mod test_actions {
    use super::*;

    pub const SLEEP: &str = "test.sleep";

    pub fn validate_sleep(p: &Map<String, Value>) -> Result<(), String> {
        for (k, v) in p {
            match (k.as_str(), v) {
                ("millis", Value::Number(n)) if n.as_u64().is_some() => {}
                ("counterFile", Value::String(s)) if s.starts_with('/') => {}
                _ => return Err(format!("bad param {k:?}")),
            }
        }
        if !p.contains_key("millis") {
            return Err("millis is required".into());
        }
        Ok(())
    }

    /// The counter file it writes.
    pub fn touches_sleep(p: &Map<String, Value>) -> Vec<Resource> {
        match p.get("counterFile") {
            Some(Value::String(f)) => vec![Resource {
                kind: "path".into(),
                id: f.clone(),
            }],
            _ => Vec::new(),
        }
    }

    /// Appends one line to `counterFile` (if given) when it starts, then
    /// sleeps `millis`.
    pub async fn sleep(p: &Map<String, Value>) -> Result<Value, StepFailure> {
        let millis = p.get("millis").and_then(Value::as_u64).unwrap_or(0);
        if let Some(Value::String(path)) = p.get("counterFile") {
            use std::io::Write;
            let mut f = std::fs::OpenOptions::new()
                .create(true)
                .append(true)
                .open(path)
                .map_err(|e| StepFailure {
                    code: "TEST_COUNTER".into(),
                    summary: e.to_string(),
                    retryable: false,
                })?;
            let _ = writeln!(f, "ran");
            let _ = f.sync_all();
        }
        tokio::time::sleep(std::time::Duration::from_millis(millis)).await;
        Ok(json!({ "sleptMillis": millis }))
    }
}
