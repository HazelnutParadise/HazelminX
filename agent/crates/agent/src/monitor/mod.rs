//! Host monitoring (ticket 020.1): a thread collects a sample every
//! interval into a bounded buffer and refreshes the host facts; the
//! session sends what the Core has not acknowledged (`host.metrics` v1).

pub mod collect;
pub mod parse;

use std::collections::VecDeque;
use std::sync::Mutex;
use std::time::{Duration, Instant};

use hazelminx_protocol::Message;
use hazelminx_protocol::metrics::{HostFacts, MetricsSample};
use time::OffsetDateTime;

/// Collection interval (spec 38.1: monitoring every 10 seconds).
pub const INTERVAL: Duration = Duration::from_secs(10);
/// How often host facts are read again.
pub const FACTS_INTERVAL: Duration = Duration::from_secs(60);
/// Samples kept while the Core is unreachable: one hour at 10 s. Older
/// ones are dropped and counted (spec 38.2: bounded buffer, gaps shown).
pub const BUFFER_SAMPLES: usize = 360;
/// Upper bound of one metrics_report's encoded samples. The body is
/// escaped again inside the frame, so this stays well under the 1 MiB
/// frame limit.
pub const REPORT_BYTES: usize = 192 * 1024;
/// A session that sent samples and got no acknowledgement for this long
/// sends them again (the Core could not store them).
pub const ACK_TIMEOUT: Duration = Duration::from_secs(30);

#[derive(Default)]
struct State {
    samples: VecDeque<MetricsSample>,
    /// Samples dropped because the buffer was full and not yet
    /// acknowledged as reported.
    dropped: u64,
    /// The report carrying a drop count, until its acknowledgement:
    /// (its last seq, the count it carried).
    dropped_sent: Option<(u64, u64)>,
    facts: Option<(OffsetDateTime, HostFacts)>,
    /// Increases whenever the facts change, so each session knows whether
    /// it has sent the current ones.
    facts_version: u64,
}

/// Shared between the collector thread and the session.
pub struct Monitor {
    state: Mutex<State>,
    capacity: usize,
}

impl Default for Monitor {
    fn default() -> Self {
        Monitor::with_capacity(BUFFER_SAMPLES)
    }
}

impl Monitor {
    pub fn with_capacity(capacity: usize) -> Self {
        Monitor {
            state: Mutex::default(),
            capacity: capacity.max(1),
        }
    }

    /// Adds a sample, dropping the oldest when the buffer is full.
    pub fn push(&self, s: MetricsSample) {
        let mut st = self.state.lock().unwrap();
        while st.samples.len() >= self.capacity {
            st.samples.pop_front();
            st.dropped += 1;
        }
        st.samples.push_back(s);
    }

    /// Stores new facts; true when they differ from the current ones (the
    /// collection time alone does not count as a change).
    pub fn set_facts(&self, at: OffsetDateTime, f: HostFacts) -> bool {
        let mut st = self.state.lock().unwrap();
        if st.facts.as_ref().is_some_and(|(_, cur)| *cur == f) {
            return false;
        }
        st.facts = Some((at, f));
        st.facts_version += 1;
        true
    }

    /// The Core stored every sample up to `seq`.
    pub fn ack(&self, seq: u64) {
        let mut st = self.state.lock().unwrap();
        st.samples.retain(|s| s.seq > seq);
        if let Some((through, count)) = st.dropped_sent
            && seq >= through
        {
            st.dropped = st.dropped.saturating_sub(count);
            st.dropped_sent = None;
        }
    }

    /// Whether a sample up to `sent` is still waiting for its
    /// acknowledgement.
    pub fn unacked_through(&self, sent: u64) -> bool {
        self.state
            .lock()
            .unwrap()
            .samples
            .front()
            .is_some_and(|s| s.seq <= sent)
    }

    /// The facts message when the session has not sent version `sent`.
    pub fn facts_after(&self, sent: u64) -> Option<(u64, Message)> {
        let st = self.state.lock().unwrap();
        let (at, f) = st.facts.as_ref()?;
        (st.facts_version > sent).then(|| {
            (
                st.facts_version,
                Message::HostFacts {
                    collected_at: *at,
                    facts: Box::new(f.clone()),
                },
            )
        })
    }

    /// The next report: the oldest unacknowledged samples, at least one
    /// and at most `REPORT_BYTES` of them, with every drop not yet
    /// acknowledged. A session has one report in flight at a time, so an
    /// acknowledgement always covers the report it answers; a resend
    /// after a lost acknowledgement may count drops twice, never zero.
    pub fn next_report(&self) -> Option<(u64, Message)> {
        let mut st = self.state.lock().unwrap();
        let mut size = 0;
        let mut out = Vec::new();
        for s in st.samples.iter() {
            let n = serde_json::to_vec(s)
                .map(|v| v.len())
                .unwrap_or(REPORT_BYTES);
            if !out.is_empty() && size + n > REPORT_BYTES {
                break;
            }
            size += n;
            out.push(s.clone());
        }
        let last = out.last()?.seq;
        let dropped = st.dropped;
        st.dropped_sent = (dropped > 0).then_some((last, dropped));
        Some((
            last,
            Message::MetricsReport {
                samples: out,
                dropped_samples: dropped,
            },
        ))
    }

    #[cfg(test)]
    fn len(&self) -> usize {
        self.state.lock().unwrap().samples.len()
    }
}

/// Collects forever on its own thread; `wake` runs after each new sample
/// or fact change so the session sends it.
pub fn spawn(
    monitor: std::sync::Arc<Monitor>,
    mut collector: collect::Collector,
    wake: impl Fn() + Send + 'static,
) -> std::io::Result<()> {
    std::thread::Builder::new()
        .name("monitor".into())
        .spawn(move || {
            let mut next = Instant::now();
            let mut facts_due = Instant::now();
            loop {
                let now = OffsetDateTime::now_utc();
                let now = now.replace_nanosecond(0).unwrap_or(now);
                if Instant::now() >= facts_due {
                    monitor.set_facts(now, collector.facts());
                    facts_due = Instant::now() + FACTS_INTERVAL;
                }
                monitor.push(collector.sample(now));
                wake();
                next += INTERVAL;
                let now = Instant::now();
                if next <= now {
                    // A round took longer than the interval (or the host
                    // was suspended): skip the missed slots, they are a gap.
                    next = now + INTERVAL;
                }
                std::thread::sleep(next - now);
            }
        })
        .map(|_| ())
}

#[cfg(test)]
mod tests;
