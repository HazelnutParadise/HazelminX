package iam

import (
	"strconv"

	"encoding/json"
	"github.com/HazelnutParadise/hazelminx/core/internal/paging"
	"strings"
	"time"
)

// AuditEvent is one row of the ledger (IAM-006): who did what, on which
// scope and resource, tied to the job and node when there is one, with
// before/after summaries and the outcome.
type AuditEvent struct {
	Seq          int64          `json:"seq"`
	ID           string         `json:"id"`
	At           time.Time      `json:"at"`
	ActorID      string         `json:"actorId,omitempty"`
	ActorName    string         `json:"actorName"`
	Action       string         `json:"action"`
	Scope        Scope          `json:"scope"`
	ResourceKind string         `json:"resourceKind,omitempty"`
	ResourceID   string         `json:"resourceId,omitempty"`
	JobID        string         `json:"jobId,omitempty"`
	NodeID       string         `json:"nodeId,omitempty"`
	Result       string         `json:"result"` // ok | denied | failed
	Message      string         `json:"message,omitempty"`
	Before       map[string]any `json:"before,omitempty"`
	After        map[string]any `json:"after,omitempty"`
	TraceID      string         `json:"traceId,omitempty"`
	SourceIP     string         `json:"sourceIp,omitempty"`
}

// Audit results.
const (
	ResultOK     = "ok"
	ResultDenied = "denied"
	ResultFailed = "failed"
)

// AuditFilter narrows ListAudit. Scope filters are applied on top of what
// the principal may see; an auditor scoped to project A asking for project B
// simply gets an empty page.
type AuditFilter struct {
	Scope      *Scope
	ActorID    string
	JobID      string
	ResourceID string
	AfterSeq   int64
	Limit      int
	// Page, when it has a Sort, pages by keyset instead of AfterSeq order
	// and Limit (sort fields: seq, at). AfterSeq still filters.
	Page paging.Query
}

// AuditSortColumns maps list sort fields to audit columns.
var AuditSortColumns = map[string]paging.Column{
	"seq": {Expr: "seq", Int: true},
	"at":  {Expr: "at"},
}

// SortValue returns the value of a sort field of ev.
func (ev AuditEvent) SortValue(field string) string {
	if field == "at" {
		return ts(ev.At)
	}
	return strconv.FormatInt(ev.Seq, 10)
}

// MaxAuditPage bounds one page (36.1: reads have an upper limit).
const MaxAuditPage = 200

// Mask replaces values whose key looks like a secret. It is applied to
// before/after summaries on write so the ledger never holds a password,
// token or key, whatever the caller passed in.
func Mask(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if secretKey(k) {
			out[k] = "***"
			continue
		}
		if sub, ok := v.(map[string]any); ok {
			out[k] = Mask(sub)
			continue
		}
		out[k] = v
	}
	return out
}

var secretHints = []string{"password", "passwd", "secret", "token", "private", "credential", "apikey", "api_key", "recovery"}

func secretKey(k string) bool {
	l := strings.ToLower(k)
	for _, h := range secretHints {
		if strings.Contains(l, h) {
			return true
		}
	}
	return false
}

func marshalMasked(m map[string]any) (*string, error) {
	if m == nil {
		return nil, nil
	}
	b, err := json.Marshal(Mask(m))
	if err != nil {
		return nil, err
	}
	s := string(b)
	return &s, nil
}

func unmarshalSummary(s *string) map[string]any {
	if s == nil || *s == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(*s), &m); err != nil {
		return map[string]any{"_unreadable": true}
	}
	return m
}
