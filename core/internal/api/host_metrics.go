package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HazelnutParadise/hazelminx/core/internal/hostmetrics"
	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodereg"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// Host facts and resource metrics (ticket 020.1, SYS-001, MON-001,
// MON-005, ROB-009). Read-only; nodes:read on the node.

type hostFactsResponse struct {
	// Reported is false until the node's agent sent facts; the other
	// fields are then null.
	Reported    bool                    `json:"reported"`
	CollectedAt *time.Time              `json:"collectedAt"`
	ReceivedAt  *time.Time              `json:"receivedAt"`
	Facts       *protocol.HostFactsData `json:"facts"`
}

type latestMetricsResponse struct {
	Reported        bool       `json:"reported"`
	CollectedAt     *time.Time `json:"collectedAt"`
	ReceivedAt      *time.Time `json:"receivedAt"`
	IntervalSeconds *int       `json:"intervalSeconds"`
	// Stale: Core received no sample for more than three intervals (the
	// agent is offline or cannot report), by Core's clock, so an agent
	// clock running behind or ahead does not change it; the sample is the
	// last one received.
	Stale          bool                    `json:"stale"`
	DroppedSamples int64                   `json:"droppedSamples"`
	Sample         *protocol.MetricsSample `json:"sample"`
	Exhaustion     *hostmetrics.Exhaustion `json:"exhaustion"`
}

// History windows: the default when from is omitted, and the longest one
// a resolution keeps.
var historyWindows = map[string][2]time.Duration{
	hostmetrics.ResolutionRaw: {time.Hour, hostmetrics.RawRetention},
	hostmetrics.Resolution5m:  {24 * time.Hour, hostmetrics.RollupRetention},
}

var metricsHistoryList = ListSpec{Name: "host-metrics-history", Sorts: []string{"at"},
	DefaultSort: []SortKey{{Field: "at"}}, TieBreaker: "at", DefaultLimit: 500, MaxLimit: 2000, QueryCap: 30000,
	Filters: []FilterSpec{
		{Name: "series", Summary: "required; the series to read (units: cpu.* and *.utilization and pressure.* percent, *Bytes bytes, *BytesPerSecond bytes per second, *PerSecond per second, *LatencyMs milliseconds, temperature.celsius °C, limits.* and filesystem.inodesUsed counts, load.* run-queue averages)", Enum: hostmetrics.SeriesNames()},
		{Name: "key", Summary: "what the series belongs to: empty for host-wide series, the CPU number for cpu.core.usage, the mount point for filesystem.*, the block device name for disk.*, the interface name for network.*, chip/label for temperature.celsius"},
		{Name: "resolution", Summary: "raw (the agent's sampling interval, kept 48 hours; default) or 5m (average, minimum and maximum per 5 minutes, kept 90 days)", Enum: []string{hostmetrics.ResolutionRaw, hostmetrics.Resolution5m}},
		{Name: "from", Summary: "RFC 3339 start (inclusive); default one hour (raw) or one day (5m) before to"},
		{Name: "to", Summary: "RFC 3339 end (exclusive); default now"},
	}}

// RegisterHostMetrics mounts the host facts and metrics routes. MountIAM
// must run first.
func (s *Server) RegisterHostMetrics(reg *nodereg.Registry, store *hostmetrics.Store) {
	if s.protected == nil {
		panic("api: RegisterHostMetrics called before MountIAM")
	}
	const base = "/management/v1"
	nodeScope := func(c *gin.Context) (iam.Scope, bool, error) {
		if _, err := reg.GetNode(c.Request.Context(), c.Param("id")); err != nil {
			if errors.Is(err, nodereg.ErrNotFound) {
				return iam.Scope{}, false, nil
			}
			return iam.Scope{}, false, err
		}
		return iam.Scope{Kind: iam.ScopeNode, ID: c.Param("id")}, true, nil
	}
	read := s.RequireScoped(iam.ActionNodesRead, nodeScope)
	h := &hostMetricsRoutes{store: store, now: time.Now}
	op := func(o Operation) Operation {
		o.Auth, o.Tag, o.Permission = true, "nodes", "nodes:read on the node"
		return o
	}
	s.handle(s.protected, base, op(Operation{Method: "GET", Path: base + "/nodes/{id}/host", ID: "getNodeHost",
		Summary:   "what the host is: OS, kernel, architecture, boot time, time zone, clock synchronization, CPU model and count, memory; every field is {value, source} and value is null when the host did not provide it (never a guess); collectedAt is when the agent read them",
		Responses: []Response{{Status: 200, Body: hostFactsResponse{}}}}), read, h.facts)
	s.handle(s.protected, base, op(Operation{Method: "GET", Path: base + "/nodes/{id}/metrics/latest", ID: "getNodeMetricsLatest",
		Summary:   "the latest sample: per-core CPU, memory (used is total minus available, so cache is not counted as used), swap, load, pressure, PID and file handle use, filesystems with inodes, disks with latency and I/O, interfaces, temperature sensors; null is not provided; exhaustion flags limits that run out before space does (PIDs over 90%, file handles over 90%, memory pressure some avg60 over 10%, inodes exhausted, read-only filesystems); stale when no sample arrived for three intervals",
		Responses: []Response{{Status: 200, Body: latestMetricsResponse{}}}}), read, h.latest)
	s.handle(s.protected, base, op(Operation{Method: "GET", Path: base + "/nodes/{id}/metrics/history", ID: "listNodeMetricsHistory",
		Summary:   "one series over time, oldest first; times without samples have no point (nothing is filled with zeros) and the first point after a missing stretch has gapBefore true; resolutionSeconds says what each point covers; a window longer than the resolution keeps is VALIDATION_FAILED",
		List:      &metricsHistoryList,
		Responses: []Response{{Status: 200, Body: ListResponse[hostmetrics.Point]{}}}}), read, h.history)
}

type hostMetricsRoutes struct {
	store *hostmetrics.Store
	now   func() time.Time
}

func (h *hostMetricsRoutes) facts(c *gin.Context) {
	f, err := h.store.Facts(c.Request.Context(), c.Param("id"))
	if err != nil {
		fail(c, CodeInternal, "internal error")
		return
	}
	out := hostFactsResponse{}
	if f != nil {
		out = hostFactsResponse{Reported: true, CollectedAt: &f.CollectedAt, ReceivedAt: &f.ReceivedAt, Facts: &f.Facts}
	}
	c.JSON(http.StatusOK, out)
}

func (h *hostMetricsRoutes) latest(c *gin.Context) {
	l, err := h.store.Latest(c.Request.Context(), c.Param("id"))
	if err != nil {
		fail(c, CodeInternal, "internal error")
		return
	}
	out := latestMetricsResponse{}
	if l != nil {
		x := hostmetrics.Assess(&l.Sample)
		out = latestMetricsResponse{Reported: true, CollectedAt: &l.CollectedAt, ReceivedAt: &l.ReceivedAt, IntervalSeconds: &l.IntervalSeconds,
			Stale:          h.now().Sub(l.ReceivedAt) > 3*time.Duration(l.IntervalSeconds)*time.Second,
			DroppedSamples: l.DroppedSamples, Sample: &l.Sample, Exhaustion: &x}
	}
	c.JSON(http.StatusOK, out)
}

func (h *hostMetricsRoutes) history(c *gin.Context) {
	q, perr := parseListQuery(c, metricsHistoryList)
	if perr != nil {
		respond(c, *perr)
		return
	}
	one := func(name string) string {
		if v := q.Filters[name]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	hq := hostmetrics.HistoryQuery{NodeID: c.Param("id"), Series: one("series"), Key: one("key"), Resolution: one("resolution")}
	if hq.Resolution == "" {
		hq.Resolution = hostmetrics.ResolutionRaw
	}
	var fields []FieldError
	if hq.Series == "" {
		fields = append(fields, FieldError{Field: "series", Message: "required"})
	}
	parse := func(name string, def time.Time) time.Time {
		v := one(name)
		if v == "" {
			return def
		}
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			fields = append(fields, FieldError{Field: name, Message: "not an RFC 3339 time"})
		}
		return t
	}
	win := historyWindows[hq.Resolution]
	hq.To = parse("to", h.now())
	hq.From = parse("from", hq.To.Add(-win[0]))
	if len(fields) == 0 {
		switch {
		case !hq.From.Before(hq.To):
			fields = append(fields, FieldError{Field: "from", Message: "must be before to"})
		case hq.To.Sub(hq.From) > win[1]:
			fields = append(fields, FieldError{Field: "from", Message: "the window is longer than this resolution keeps (raw 48 hours, 5m 90 days)"})
		}
	}
	if len(fields) > 0 {
		e := newError(CodeValidationFailed, "the history request is not valid")
		e.Fields = fields
		respond(c, e)
		return
	}
	runList[hostmetrics.Point](c, metricsHistoryList, historySource{store: h.store, q: hq})
}

type historySource struct {
	store *hostmetrics.Store
	q     hostmetrics.HistoryQuery
}

func (s historySource) Count(ctx context.Context, _ ListQuery) (int, error) {
	return s.store.CountHistory(ctx, s.q)
}

func (s historySource) Fetch(ctx context.Context, q ListQuery, n int) ([]hostmetrics.Point, error) {
	return s.store.History(ctx, s.q, q.paging(n))
}

func (s historySource) Key(row hostmetrics.Point, sort []SortKey) []string {
	return sortValues(sort, func(string) string { return row.SortValue() })
}
