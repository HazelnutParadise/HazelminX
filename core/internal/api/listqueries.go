package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
)

// Background list queries (ROB-008 "改成背景工作", 51.6 item decided in
// ticket 010): when a list is capped or times out, the same query runs as a
// job without the page timeout. At most ListQueryResultCap rows are kept,
// in their own table, for ListQueryRetention; the job record stays.
const (
	ListQueryKind      = "list.query"
	ListQueryResultCap = 100000
	ListQueryRetention = 7 * 24 * time.Hour
	// ListQueryKeepLimit is how many background queries one account may
	// keep (queued, running or with stored rows) at a time.
	ListQueryKeepLimit = 10
	listQueryBatch     = 500
)

// backgroundList is a list that can run as a background query.
type backgroundList struct {
	spec ListSpec
	// allowed reports whether p may read the list at all.
	allowed func(p *iam.Principal) bool
	// scopeKey names what p may see of the list. Stored rows were filtered
	// with the key of the run; a reader whose key differs (permissions
	// narrowed or widened since) does not get them.
	scopeKey func(p *iam.Principal) string
	// collect runs q for p and calls emit for each row in order, at most
	// limit rows; it returns how many rows matched in total.
	collect func(ctx context.Context, p *iam.Principal, q ListQuery, limit int, emit func(any) error) (int, error)
}

// ListQueries runs background list queries. It is a job handler and owns
// the list-queries routes.
type ListQueries struct {
	db    *sql.DB
	iam   *iam.Service
	lists map[string]backgroundList
	now   func() time.Time
}

// NewListQueries builds the handler for the lists Core serves. Register it
// with the job engine before Recover, then mount it with MountListQueries.
func NewListQueries(db *sql.DB, svc *iam.Service, engine *jobs.Engine) *ListQueries {
	lq := &ListQueries{db: db, iam: svc, lists: map[string]backgroundList{}, now: time.Now}
	global := func(*iam.Principal) string { return "global" }
	lq.lists[jobsList.Name] = backgroundList{spec: jobsList, scopeKey: global,
		allowed: func(p *iam.Principal) bool { return iam.Authorize(p, iam.ActionPlatformRead, iam.Global) },
		collect: func(ctx context.Context, _ *iam.Principal, q ListQuery, limit int, emit func(any) error) (int, error) {
			return collectAll[*jobs.Job](ctx, jobsSource{engine: engine, opts: jobListOptions(q)}, q, limit, emit)
		}}
	lq.lists[usersList.Name] = backgroundList{spec: usersList, scopeKey: global,
		allowed: func(p *iam.Principal) bool { return iam.Authorize(p, iam.ActionUsersRead, iam.Global) },
		collect: func(ctx context.Context, p *iam.Principal, q ListQuery, limit int, emit func(any) error) (int, error) {
			return collectAll[iam.UserDetail](ctx, usersSource{svc: svc, p: p}, q, limit, emit)
		}}
	lq.lists[auditList.Name] = backgroundList{spec: auditList, scopeKey: auditScopeKey,
		allowed: func(p *iam.Principal) bool { g, sc := iam.ScopesFor(p, iam.ActionAuditRead); return g || len(sc) > 0 },
		collect: func(ctx context.Context, p *iam.Principal, q ListQuery, limit int, emit func(any) error) (int, error) {
			f, ferr := auditFilter(q)
			if ferr != nil {
				return 0, errors.New(ferr.Message)
			}
			return collectAll[iam.AuditEvent](ctx, auditSource{svc: svc, p: p, f: f}, q, limit, emit)
		}}
	return lq
}

// auditScopeKey lists the scopes where p holds audit:read.
func auditScopeKey(p *iam.Principal) string {
	g, scopes := iam.ScopesFor(p, iam.ActionAuditRead)
	if g {
		return "global"
	}
	keys := make([]string, len(scopes))
	for i, sc := range scopes {
		keys[i] = string(sc.Kind) + ":" + sc.ID
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// RunPurger deletes expired results every interval until ctx ends, so rows
// go away after the retention period even when nobody calls the API.
func (lq *ListQueries) RunPurger(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		lq.purgeExpired(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// collectAll pages src from the start of q, emitting at most limit rows.
func collectAll[T any](ctx context.Context, src ListSource[T], q ListQuery, limit int, emit func(any) error) (int, error) {
	total, err := src.Count(ctx, q)
	if err != nil {
		return 0, err
	}
	q.After, q.Pos = nil, 0
	for sent := 0; sent < limit; {
		n := min(listQueryBatch, limit-sent)
		rows, err := src.Fetch(ctx, q, n)
		if err != nil {
			return 0, err
		}
		for _, r := range rows {
			if err := emit(r); err != nil {
				return 0, err
			}
		}
		sent += len(rows)
		if len(rows) < n {
			break
		}
		q.After = src.Key(rows[len(rows)-1], q.Sort)
	}
	return total, nil
}

// ListQueryRequest is the body of POST /management/v1/list-queries.
type ListQueryRequest struct {
	RequestID string              `json:"requestId"`
	List      string              `json:"list"`
	Sort      string              `json:"sort,omitempty"`
	Filters   map[string][]string `json:"filters,omitempty"`
}

// ListQueryStatus is the state of a background query.
type ListQueryStatus struct {
	JobID     string    `json:"jobId"`
	List      string    `json:"list"`
	Query     string    `json:"query"`
	Total     *int      `json:"total"`
	Stored    int       `json:"stored"`
	Truncated bool      `json:"truncated"`
	ExpiresAt time.Time `json:"expiresAt"`
	Purged    bool      `json:"purged"`
	Job       *jobs.Job `json:"job"`
	Items     string    `json:"items"`
}

// MountListQueries adds the list-queries routes.
func (s *Server) MountListQueries(lq *ListQueries) {
	if s.protected == nil || s.jobs == nil {
		panic("api: MountListQueries needs MountIAM and RegisterJobs first")
	}
	const base = "/management/v1"
	s.handle(s.protected, base, Operation{Method: "POST", Path: base + "/list-queries", ID: "submitListQuery", Tag: "lists", Auth: true,
		Summary: "run a capped or timed-out list as a background job (the page's backgroundQuery plus a requestId); " +
			"keeps at most 100000 rows for 7 days",
		Permission: "the read permission of that list", Request: ListQueryRequest{}, LongRunning: true,
		Responses: []Response{{Status: 202, Body: JobAccepted{}, Headers: []string{"Location"}}},
		Errors:    []string{CodeRequestIDConflict, CodePlatformDegraded}}, func(c *gin.Context) { lq.submit(c, s) })
	s.handle(s.protected, base, Operation{Method: "GET", Path: base + "/list-queries/{id}", ID: "getListQuery", Tag: "lists", Auth: true,
		Summary: "state of a background query; only its requester sees it", Permission: "requester, with the list's read permission",
		Responses: []Response{{Status: 200, Body: ListQueryStatus{}}}}, func(c *gin.Context) { lq.status(c, s) })
	s.handle(s.protected, base, Operation{Method: "GET", Path: base + "/list-queries/{id}/items", ID: "listListQueryItems", Tag: "lists", Auth: true,
		Summary: "the stored rows of a finished background query, in query order", Permission: "requester, with the list's read permission",
		List:      &listQueryItemsList,
		Responses: []Response{{Status: 200, Body: ListResponse[json.RawMessage]{}}},
		Errors:    []string{CodeListQueryNotReady, CodeListQueryExpired}}, func(c *gin.Context) { lq.items(c, s) })
}

var listQueryItemsList = ListSpec{Name: "list-query-items", MaxLimit: 1000, QueryCap: ListQueryResultCap, Sorts: []string{"pos"},
	DefaultSort: []SortKey{{Field: "pos"}}, TieBreaker: "pos"}

// parseStoredQuery validates a background query against its list spec with
// the same rules as the list endpoint.
func parseStoredQuery(spec ListSpec, sortParam string, filters map[string][]string) (ListQuery, *apiError) {
	v := url.Values{}
	if sortParam != "" {
		v.Set("sort", sortParam)
	}
	for k, vals := range filters {
		v.Set(k, strings.Join(vals, ","))
	}
	req, _ := http.NewRequest(http.MethodGet, "/?"+v.Encode(), nil)
	return parseListQuery(&gin.Context{Request: req}, spec)
}

func (lq *ListQueries) submit(c *gin.Context, s *Server) {
	var in ListQueryRequest
	if !bindJSON(c, &in, "body must be JSON with requestId, list, sort and filters") {
		return
	}
	bl, ok := lq.lists[in.List]
	if !ok {
		e := newError(CodeValidationFailed, "no background query for list "+strconv.Quote(in.List))
		e.Fields = []FieldError{{Field: "list", Message: "one of jobs, users, audit-events"}}
		respond(c, e)
		return
	}
	p := Principal(c)
	if !bl.allowed(p) {
		fail(c, CodeForbidden, "this account is not allowed to read this list")
		return
	}
	q, perr := parseStoredQuery(bl.spec, in.Sort, in.Filters)
	if perr != nil {
		respond(c, *perr)
		return
	}
	lq.purgeExpired(c.Request.Context())
	params, _ := json.Marshal(listQueryParams{List: in.List, Sort: renderSort(q.Sort), Filters: q.Filters})
	req := jobs.SubmitRequest{RequestID: in.RequestID, Kind: ListQueryKind, Params: params, Requester: p.User.ID}
	ctx := c.Request.Context()
	pv, err := s.jobs.Preview(ctx, req)
	if err != nil {
		respond(c, jobError(err, in.RequestID))
		return
	}
	if pv.Replay != nil && pv.Replay.Requester != p.User.ID {
		respond(c, requestIDTaken(in.RequestID))
		return
	}
	if pv.Replay == nil {
		var kept int
		if err := lq.db.QueryRowContext(ctx, `SELECT count(*) FROM list_queries WHERE requester_id=? AND purged_at IS NULL`, p.User.ID).Scan(&kept); err != nil {
			respond(c, newError(CodeInternal, "internal error"))
			return
		}
		if kept >= ListQueryKeepLimit {
			e := newError(CodeListQueryLimit, fmt.Sprintf("this account already keeps %d background queries; their results are deleted 7 days after they were submitted", ListQueryKeepLimit))
			e.Details = map[string]any{"limit": ListQueryKeepLimit}
			respond(c, e)
			return
		}
		blockers, err := s.writeBlockers(ctx, ListQueryKind, ResourceState{}, nil)
		if err != nil {
			respond(c, newError(CodeInternal, "internal error"))
			return
		}
		if top := pickBlocking(blockers); top != nil {
			respond(c, *top)
			return
		}
	}
	job, dedup, err := s.jobs.Submit(ctx, req)
	if err != nil {
		respond(c, jobError(err, in.RequestID))
		return
	}
	if dedup && job.Requester != p.User.ID {
		respond(c, requestIDTaken(in.RequestID))
		return
	}
	if err := lq.record(ctx, lq.db, job.ID, job.Requester, job.Params); err != nil {
		respond(c, newError(CodeInternal, "internal error"))
		return
	}
	acceptJob(c, job, dedup)
}

// record creates the query row once; submit and Execute both call it, so
// the row exists whichever runs first.
func (lq *ListQueries) record(ctx context.Context, ex interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, jobID, requester string, params []byte) error {
	var p listQueryParams
	_ = json.Unmarshal(params, &p)
	now := lq.now().UTC()
	_, err := ex.ExecContext(ctx, `INSERT OR IGNORE INTO list_queries(job_id,requester_id,list,query,created_at,updated_at,expires_at) VALUES(?,?,?,?,?,?,?)`,
		jobID, requester, p.List, string(params), ts(now), ts(now), ts(now.Add(ListQueryRetention)))
	return err
}

type listQueryParams struct {
	List    string              `json:"list"`
	Sort    string              `json:"sort"`
	Filters map[string][]string `json:"filters,omitempty"`
}

type listQueryRow struct {
	jobID, requester, list, query string
	scopeKey                      string
	total                         *int
	stored                        int
	truncated                     bool
	expiresAt                     time.Time
	purged                        bool
}

// load returns the query row visible to p (requester only, and only while
// p may still read the list); sql.ErrNoRows otherwise.
func (lq *ListQueries) load(ctx context.Context, id string, p *iam.Principal) (*listQueryRow, error) {
	var r listQueryRow
	var total sql.NullInt64
	var exp string
	var purged sql.NullString
	err := lq.db.QueryRowContext(ctx, `SELECT job_id,requester_id,list,query,scope_key,total,stored,truncated,expires_at,purged_at FROM list_queries WHERE job_id=?`, id).
		Scan(&r.jobID, &r.requester, &r.list, &r.query, &r.scopeKey, &total, &r.stored, &r.truncated, &exp, &purged)
	if err != nil {
		return nil, err
	}
	if r.requester != p.User.ID {
		return nil, sql.ErrNoRows
	}
	bl, ok := lq.lists[r.list]
	if !ok || !bl.allowed(p) || (r.scopeKey != "" && r.scopeKey != bl.scopeKey(p)) {
		return nil, sql.ErrNoRows
	}
	if total.Valid {
		n := int(total.Int64)
		r.total = &n
	}
	r.expiresAt, _ = time.Parse(time.RFC3339Nano, exp)
	r.purged = purged.Valid
	return &r, nil
}

func (lq *ListQueries) status(c *gin.Context, s *Server) {
	ctx := c.Request.Context()
	lq.purgeExpired(ctx)
	r, err := lq.load(ctx, c.Param("id"), Principal(c))
	if err != nil {
		notFound(c)
		return
	}
	job, err := s.jobs.Get(ctx, r.jobID)
	if err != nil {
		respond(c, jobError(err, ""))
		return
	}
	c.JSON(http.StatusOK, ListQueryStatus{JobID: r.jobID, List: r.list, Query: r.query, Total: r.total, Stored: r.stored,
		Truncated: r.truncated, ExpiresAt: r.expiresAt, Purged: r.purged, Job: job,
		Items: "/management/v1/list-queries/" + r.jobID + "/items"})
}

// itemsSource pages the stored rows of one query.
type itemsSource struct {
	db     *sql.DB
	jobID  string
	stored int
}

func (i itemsSource) Count(context.Context, ListQuery) (int, error) { return i.stored, nil }
func (i itemsSource) Fetch(ctx context.Context, q ListQuery, n int) ([]storedItem, error) {
	after := int64(-1)
	if q.After != nil {
		after, _ = strconv.ParseInt(q.After[0], 10, 64)
	}
	order := "ASC"
	op := ">"
	if q.Sort[0].Desc {
		order, op = "DESC", "<"
		if q.After == nil {
			after = int64(i.stored) + 1
		}
	}
	rows, err := i.db.QueryContext(ctx, `SELECT pos,item FROM list_query_items WHERE job_id=? AND pos `+op+` ? ORDER BY pos `+order+` LIMIT ?`, i.jobID, after, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storedItem
	for rows.Next() {
		var it storedItem
		var raw string
		if err := rows.Scan(&it.pos, &raw); err != nil {
			return out, err
		}
		it.item = json.RawMessage(raw)
		out = append(out, it)
	}
	return out, rows.Err()
}
func (i itemsSource) Key(row storedItem, sort []SortKey) []string {
	out := make([]string, len(sort))
	for k := range sort {
		out[k] = strconv.Itoa(row.pos)
	}
	return out
}

type storedItem struct {
	pos  int
	item json.RawMessage
}

func (s storedItem) MarshalJSON() ([]byte, error) { return s.item, nil }

func (lq *ListQueries) items(c *gin.Context, s *Server) {
	ctx := c.Request.Context()
	lq.purgeExpired(ctx)
	r, err := lq.load(ctx, c.Param("id"), Principal(c))
	if err != nil {
		notFound(c)
		return
	}
	if r.purged {
		e := newError(CodeListQueryExpired, "the results were kept for 7 days and have been deleted; submit the query again")
		e.JobID = r.jobID
		respond(c, e)
		return
	}
	if job, err := s.jobs.Get(ctx, r.jobID); err != nil || job.State != jobs.StateSucceeded {
		e := newError(CodeListQueryNotReady, "the query has not finished successfully; follow the job")
		e.JobID = r.jobID
		if err == nil {
			e.Details = map[string]any{"state": job.State}
			e.Retryable = !job.State.IsTerminal()
		}
		respond(c, e)
		return
	}
	runList[storedItem](c, listQueryItemsList, itemsSource{db: lq.db, jobID: r.jobID, stored: r.stored})
}

// purgeExpired deletes the rows of queries past their retention.
func (lq *ListQueries) purgeExpired(ctx context.Context) {
	now := ts(lq.now().UTC())
	tx, err := lq.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM list_query_items WHERE job_id IN (SELECT job_id FROM list_queries WHERE purged_at IS NULL AND expires_at < ?)`, now); err != nil {
		return
	}
	if _, err := tx.ExecContext(ctx, `UPDATE list_queries SET purged_at=?, updated_at=? WHERE purged_at IS NULL AND expires_at < ?`, now, now, now); err != nil {
		return
	}
	_ = tx.Commit()
}

// ts writes times with a fixed nine-digit fraction so that their text order
// is their time order (RFC3339Nano drops trailing zeros).
func ts(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z07:00") }

// ---- jobs.Handler -----------------------------------------------------------

// Kind implements jobs.Handler.
func (lq *ListQueries) Kind() string { return ListQueryKind }

// Plan implements jobs.Handler: one repeatable step.
func (lq *ListQueries) Plan(ctx context.Context, j *jobs.Job) ([]jobs.StepSpec, error) {
	var p listQueryParams
	if err := json.Unmarshal(j.Params, &p); err != nil {
		return nil, &jobs.StepError{Code: CodeValidationFailed, Summary: "list query parameters are not valid JSON"}
	}
	if _, ok := lq.lists[p.List]; !ok {
		return nil, &jobs.StepError{Code: CodeValidationFailed, Summary: "no background query for list " + strconv.Quote(p.List)}
	}
	return []jobs.StepSpec{{Name: "run-query", RollbackMethod: "delete-results", Cancellable: true, Retryable: true}}, nil
}

// Recheck implements jobs.Handler.
func (lq *ListQueries) Recheck(context.Context, *jobs.Job, *jobs.Step) error { return nil }

// Execute implements jobs.Handler: re-resolve the requester's permissions,
// run the query to the end and store at most ListQueryResultCap rows.
func (lq *ListQueries) Execute(ctx context.Context, j *jobs.Job, _ *jobs.Step, rep jobs.Reporter) error {
	var p listQueryParams
	if err := json.Unmarshal(j.Params, &p); err != nil {
		return &jobs.StepError{Code: CodeValidationFailed, Summary: "list query parameters are not valid JSON"}
	}
	bl := lq.lists[p.List]
	who, err := lq.iam.PrincipalByID(ctx, j.Requester)
	if err != nil || !bl.allowed(who) {
		return &jobs.StepError{Code: CodeForbidden, Summary: "the requester may no longer read this list"}
	}
	q, perr := parseStoredQuery(bl.spec, p.Sort, p.Filters)
	if perr != nil {
		return &jobs.StepError{Code: perr.Code, Summary: perr.Message}
	}
	// The store has a single connection, so reads of the list and writes
	// of the result never overlap inside one transaction: rows are read a
	// batch at a time and each batch is written in its own short one.
	if err := lq.record(ctx, lq.db, j.ID, j.Requester, j.Params); err != nil {
		return err
	}
	if _, err := lq.db.ExecContext(ctx, `DELETE FROM list_query_items WHERE job_id=?`, j.ID); err != nil {
		return err
	}
	pos := 0
	var batch []string
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		tx, err := lq.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for i, item := range batch {
			if _, err := tx.ExecContext(ctx, `INSERT INTO list_query_items(job_id,pos,item) VALUES(?,?,?)`, j.ID, pos-len(batch)+i, item); err != nil {
				return err
			}
		}
		batch = batch[:0]
		if err := tx.Commit(); err != nil {
			return err
		}
		// Progress and heartbeat write to the store too, so they wait until
		// the batch's transaction has released the connection.
		rep.Progress(min(99, pos*100/ListQueryResultCap))
		rep.Heartbeat()
		return nil
	}
	total, err := bl.collect(ctx, who, q, ListQueryResultCap, func(row any) error {
		b, err := json.Marshal(row)
		if err != nil {
			return err
		}
		batch = append(batch, string(b))
		pos++
		if len(batch) == listQueryBatch {
			return flush()
		}
		return nil
	})
	if err == nil {
		err = flush()
	}
	if err != nil {
		return err
	}
	now := ts(lq.now().UTC())
	if _, err := lq.db.ExecContext(ctx, `UPDATE list_queries SET total=?, stored=?, truncated=?, scope_key=?, updated_at=? WHERE job_id=?`,
		total, pos, total > pos, bl.scopeKey(who), now, j.ID); err != nil {
		return err
	}
	rep.Log(fmt.Sprintf("stored %d of %d rows", pos, total))
	return nil
}

// Rollback implements jobs.Handler: drop the stored rows.
func (lq *ListQueries) Rollback(ctx context.Context, j *jobs.Job, _ *jobs.Step, _ jobs.Reporter) error {
	_, err := lq.db.ExecContext(ctx, `DELETE FROM list_query_items WHERE job_id=?`, j.ID)
	return err
}

// Reconcile implements jobs.Handler: the step runs in Core and is safe to
// repeat, so an interrupted run is reported as started and not completed.
func (lq *ListQueries) Reconcile(_ context.Context, _ *jobs.Job, s *jobs.Step) (jobs.StepReport, error) {
	return jobs.StepReport{Index: s.Index, Started: true}, nil
}

// Verify implements jobs.Handler: the stored row count matches the record.
func (lq *ListQueries) Verify(ctx context.Context, j *jobs.Job) (jobs.CompletionReport, error) {
	var stored, rows int
	err := lq.db.QueryRowContext(ctx, `SELECT stored, (SELECT count(*) FROM list_query_items WHERE job_id=?) FROM list_queries WHERE job_id=?`, j.ID, j.ID).Scan(&stored, &rows)
	if err != nil {
		return jobs.CompletionReport{}, err
	}
	return jobs.CompletionReport{Checks: []jobs.Check{{Name: "rows-stored", Required: true, Passed: stored == rows,
		Detail: fmt.Sprintf("%d rows recorded, %d rows stored", stored, rows)}}}, nil
}

// renderSort writes sort keys in the sort parameter syntax.
func renderSort(sort []SortKey) string {
	parts := make([]string, len(sort))
	for i, k := range sort {
		if k.Desc {
			parts[i] = "-" + k.Field
		} else {
			parts[i] = k.Field
		}
	}
	return strings.Join(parts, ",")
}
