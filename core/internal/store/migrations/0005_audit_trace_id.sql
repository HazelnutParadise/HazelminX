-- 0005: rename the audit trace column (ticket 010 error format).
-- audit_events.request_id held the HTTP call's trace id (X-Trace-ID), not the
-- operation request identifier that jobs.request_id deduplicates (spec 33).
ALTER TABLE audit_events RENAME COLUMN request_id TO trace_id;
