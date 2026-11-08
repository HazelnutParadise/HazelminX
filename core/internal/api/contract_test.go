package api

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/confirm"
	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

// contractHarness is the whole management API as main wires it: IAM, jobs,
// background list queries and confirmations on one real store.
type contractHarness struct {
	*harness
	owner   *client
	engine  *jobs.Engine
	bh      *blockingHandler
	lq      *ListQueries
	confirm *confirm.Service
	st      *store.Store
}

func newContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	hs := &harness{t: t, clock: time.Now().UTC().Truncate(time.Second)}
	svc, err := iam.New(st.DB, iam.Config{PasswordParams: iam.TestPasswordParams, Now: func() time.Time { return hs.clock }})
	if err != nil {
		t.Fatal(err)
	}
	bh := &blockingHandler{release: make(chan struct{})}
	e := jobs.New(jobs.NewSQLStore(st.DB), jobs.Options{PollInterval: 10 * time.Millisecond})
	lq := NewListQueries(st.DB, svc, e)
	for _, h := range []jobs.Handler{bh, lq} {
		if err := e.Register(h); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	e.Start(ctx)
	cf := confirm.New(st.DB, nil)
	s := New(st, nil)
	s.MountIAM(svc, IAMOptions{})
	s.RegisterJobs(e)
	s.MountListQueries(lq)
	s.MountConfirmations(cf)
	s.RegisterAgentConnections(fakeAgentConnections{})
	hs.h, hs.svc, hs.srv = s.Handler(), svc, s
	t.Cleanup(func() {
		select {
		case <-bh.release:
		default:
			close(bh.release)
		}
		cancel()
		e.Stop()
		st.Close()
	})
	return &contractHarness{harness: hs, owner: hs.bootstrap(), engine: e, bh: bh, lq: lq, confirm: cf, st: st}
}

func (ch *contractHarness) waitJob(id string, want jobs.State) *jobs.Job {
	ch.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j, err := ch.engine.Get(context.Background(), id)
		if err == nil && j.State == want {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	j, _ := ch.engine.Get(context.Background(), id)
	ch.t.Fatalf("job %s did not reach %s: %+v", id, want, j)
	return nil
}

// ---- the operation registry, the router and the error table agree ----------

func TestRoutesMatchOperations(t *testing.T) {
	ch := newContractHarness(t)
	documented := map[string]bool{}
	for _, op := range ch.srv.ops {
		k := op.Method + " " + ginPath(op.Path)
		if documented[k] {
			t.Errorf("operation registered twice: %s", k)
		}
		documented[k] = true
	}
	routed := map[string]bool{}
	for _, r := range ch.srv.engine.Routes() {
		p := r.Path
		if p != "/management/v1/" { // trailing-slash alias of the overview
			routed[r.Method+" "+p] = true
		}
	}
	for k := range routed {
		if !documented[k] {
			t.Errorf("route without an Operation: %s", k)
		}
	}
	for k := range documented {
		if !routed[k] {
			t.Errorf("Operation without a route: %s", k)
		}
	}
	// infra-manager keeps /api/ and /use/ (AGENTS.md): nothing of ours lives there.
	for k := range routed {
		p := strings.SplitN(k, " ", 2)[1]
		if !strings.HasPrefix(p, "/management/v1") && p != "/healthz" {
			t.Errorf("route outside /management/v1: %s", k)
		}
	}
	for _, p := range []string{"/api/v1/use", "/use/x", "/api/admin/users"} {
		r := ch.client("10.0.0.9").do(http.MethodGet, p, nil, nil)
		if r.Code == http.StatusUnauthorized || r.Body["code"] == CodeUnauthenticated {
			t.Errorf("%s must not reach the management API: %+v", p, r)
		}
	}
}

// TestErrorCodesComeFromTheTable checks the source: every literal code given
// to fail, newError or a StepError is a row of the table.
func TestErrorCodesComeFromTheTable(t *testing.T) {
	fset := token.NewFileSet()
	files, _ := filepath.Glob("*.go")
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var arg ast.Expr
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				if fn.Name == "newError" && len(call.Args) > 0 {
					arg = call.Args[0]
				}
				if fn.Name == "fail" && len(call.Args) > 1 {
					arg = call.Args[1]
				}
			}
			if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				code, _ := strconv.Unquote(lit.Value)
				checked++
				if _, ok := errorIndex[code]; !ok {
					t.Errorf("%s: code %q is not in the error table", fset.Position(lit.Pos()), code)
				}
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatal("found no literal codes; the scan is broken")
	}
	for _, c := range blockingOrder {
		if _, ok := errorIndex[c]; !ok {
			t.Errorf("precedence lists unknown code %s", c)
		}
	}
}

// TestErrorResponsesFollowTheTable sends requests that fail in different
// ways and checks each answer's status is the table status of its code.
func TestErrorResponsesFollowTheTable(t *testing.T) {
	ch := newContractHarness(t)
	anon := ch.client("10.0.0.8")
	cases := []struct {
		c                  *client
		method, path, body string
		code               string
	}{
		{anon, "GET", "/management/v1/jobs", "", CodeUnauthenticated},
		{ch.owner, "GET", "/management/v1/jobs/job_nope", "", CodeNotFound},
		{ch.owner, "GET", "/management/v1/users/nope", "", CodeNotFound},
		{ch.owner, "POST", "/management/v1/jobs", `{"requestId":"x","kind":"nope"}`, CodeValidationFailed},
		{ch.owner, "POST", "/management/v1/jobs", `{"requestId":"x","kind":"demo.install","typo":1}`, CodeValidationFailed},
		{ch.owner, "POST", "/management/v1/jobs", `{"requestId":"x","kind":"demo.install","params":"` + strings.Repeat("a", maxJSONBody) + `"}`, CodePayloadTooLarge},
		{ch.owner, "GET", "/management/v1/jobs?bogus=1", "", CodeValidationFailed},
		{ch.owner, "POST", "/management/v1/users?dryRun=true", `{"username":"zed","password":"zed-password-123"}`, CodeDryRunUnsupported},
		{ch.owner, "POST", "/management/v1/users", `{"username":"owner","password":"owner-password-123"}`, CodeAlreadyExists},
		{ch.owner, "GET", "/management/v1/confirmations/cfm_nope", "", CodeNotFound},
		{ch.owner, "GET", "/management/v1/nothing-here", "", CodeNotFound},
	}
	for _, tc := range cases {
		var body any
		if tc.body != "" {
			body = json.RawMessage(tc.body)
		}
		r := tc.c.do(tc.method, tc.path, body, nil)
		if r.Body["code"] != tc.code || r.Code != lookupCode(tc.code).Status {
			t.Errorf("%s %s: want %s/%d, got %d %s", tc.method, tc.path, tc.code, lookupCode(tc.code).Status, r.Code, r.Raw)
		}
		if r.Body["traceId"] == nil || r.Body["message"] == "" {
			t.Errorf("%s %s: error without traceId or message: %s", tc.method, tc.path, r.Raw)
		}
		if r.Body["retryable"] != lookupCode(tc.code).Retryable {
			t.Errorf("%s %s: retryable must default from the table: %s", tc.method, tc.path, r.Raw)
		}
	}
}

func TestOpenAPIDocumentMatchesServer(t *testing.T) {
	ch := newContractHarness(t)
	r := ch.client("10.0.0.7").do(http.MethodGet, "/management/v1/openapi.json", nil, nil)
	if r.Code != http.StatusOK {
		t.Fatalf("openapi: %+v", r)
	}
	doc := r.Body
	if doc["openapi"] != "3.1.0" {
		t.Fatalf("version: %v", doc["openapi"])
	}
	paths := doc["paths"].(map[string]any)
	for _, op := range ch.srv.ops {
		entry, ok := paths[op.Path].(map[string]any)[strings.ToLower(op.Method)].(map[string]any)
		if !ok {
			t.Errorf("%s %s missing from OpenAPI", op.Method, op.Path)
			continue
		}
		if entry["operationId"] != op.ID {
			t.Errorf("%s %s operationId %v", op.Method, op.Path, entry["operationId"])
		}
		if op.DryRun != (entry["x-hazelminx-dry-run"] == true) {
			t.Errorf("%s %s dryRun marking", op.Method, op.Path)
		}
		if op.Versioned != (entry["x-hazelminx-versioned"] == true) {
			t.Errorf("%s %s versioned marking", op.Method, op.Path)
		}
		if (op.List != nil) != (entry["x-hazelminx-list"] != nil) {
			t.Errorf("%s %s list marking", op.Method, op.Path)
		}
		responses := entry["responses"].(map[string]any)
		for _, code := range entry["x-hazelminx-error-codes"].([]any) {
			st := strconv.Itoa(lookupCode(code.(string)).Status)
			if !strings.Contains(responses[st].(map[string]any)["description"].(string), code.(string)) {
				t.Errorf("%s %s: %s not documented under %s", op.Method, op.Path, code, st)
			}
		}
	}
	// Error.code enumerates exactly the table.
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	var errSchema map[string]any
	for name, sch := range schemas {
		if strings.Contains(strings.ToLower(name), "apierror") {
			errSchema = sch.(map[string]any)
		}
	}
	if errSchema == nil {
		t.Fatalf("no error schema in %v", slices.Collect(func(yield func(string) bool) {
			for k := range schemas {
				if !yield(k) {
					return
				}
			}
		}))
	}
	enum := errSchema["properties"].(map[string]any)["code"].(map[string]any)["enum"].([]any)
	if len(enum) != len(errorTable) {
		t.Fatalf("enum has %d codes, table %d", len(enum), len(errorTable))
	}
	// ROB-003 checklist: the documented dry-run endpoints.
	submit := paths["/management/v1/jobs"].(map[string]any)["post"].(map[string]any)
	if submit["x-hazelminx-dry-run"] != true {
		t.Fatal("POST /jobs must be marked as dry-run capable")
	}
	// The overview links every operation to its OpenAPI entry.
	ov := ch.client("10.0.0.7").do(http.MethodGet, "/management/v1", nil, nil)
	if ov.Body["openapi"] != openAPIPath {
		t.Fatalf("overview openapi link: %v", ov.Body["openapi"])
	}
	for _, o := range ov.Body["operations"].([]any) {
		if !strings.HasPrefix(o.(map[string]any)["docs"].(string), openAPIPath+"#/paths/") {
			t.Fatalf("operation without docs link: %v", o)
		}
	}
}

// ---- lists --------------------------------------------------------------------

func TestJobListPagesWithCursor(t *testing.T) {
	ch := newContractHarness(t)
	for i := 0; i < 7; i++ {
		if r := ch.owner.do("POST", "/management/v1/jobs", map[string]any{"requestId": fmt.Sprint("p", i), "kind": "demo.install",
			"resources": []map[string]string{{"kind": "database", "id": fmt.Sprint("db", i)}}}, nil); r.Code != http.StatusAccepted {
			t.Fatalf("submit: %+v", r)
		}
	}
	var seen []string
	path := "/management/v1/jobs?limit=3"
	for pages := 0; ; pages++ {
		r := ch.owner.do("GET", path, nil, nil)
		if r.Code != http.StatusOK {
			t.Fatalf("list: %+v", r)
		}
		page := r.Body["page"].(map[string]any)
		if page["limit"].(float64) != 3 || page["total"].(float64) != 7 || page["sort"] != "-createdAt,-id" || page["timedOut"] != false || page["capped"] != false {
			t.Fatalf("page meta: %v", page)
		}
		for _, it := range r.Body["items"].([]any) {
			seen = append(seen, it.(map[string]any)["id"].(string))
		}
		next, ok := page["nextCursor"].(string)
		if !ok {
			break
		}
		if pages > 5 {
			t.Fatal("cursor never ends")
		}
		path = "/management/v1/jobs?limit=3&cursor=" + next
	}
	if len(seen) != 7 {
		t.Fatalf("walked %d jobs: %v", len(seen), seen)
	}
	r := ch.owner.do("GET", "/management/v1/jobs?state=queued,running&kind=demo.install", nil, nil)
	if r.Code != http.StatusOK {
		t.Fatalf("filters: %+v", r)
	}
	if r := ch.owner.do("GET", "/management/v1/jobs?state=bogus", nil, nil); r.Code != http.StatusBadRequest || r.Body["fields"] == nil {
		t.Fatalf("unknown state must name the field: %+v", r)
	}
}

func TestBackgroundListQuery(t *testing.T) {
	ch := newContractHarness(t)
	for i := 0; i < 5; i++ {
		ch.owner.do("POST", "/management/v1/jobs", map[string]any{"requestId": fmt.Sprint("b", i), "kind": "demo.install",
			"resources": []map[string]string{{"kind": "database", "id": fmt.Sprint("db", i)}}}, nil)
	}
	r := ch.owner.do("POST", "/management/v1/list-queries", map[string]any{"requestId": "lq-1", "list": "jobs", "sort": "-createdAt",
		"filters": map[string][]string{"kind": {"demo.install"}}}, nil)
	if r.Code != http.StatusAccepted || r.Hdr.Get("Location") == "" {
		t.Fatalf("submit query: %+v", r)
	}
	id := r.Body["jobId"].(string)
	ch.waitJob(id, jobs.StateSucceeded)
	st := ch.owner.do("GET", "/management/v1/list-queries/"+id, nil, nil)
	if st.Code != http.StatusOK || st.Body["total"].(float64) != 5 || st.Body["stored"].(float64) != 5 || st.Body["truncated"] != false {
		t.Fatalf("status: %+v", st)
	}
	items := ch.owner.do("GET", "/management/v1/list-queries/"+id+"/items?limit=2", nil, nil)
	if items.Code != http.StatusOK || len(items.Body["items"].([]any)) != 2 || items.Body["page"].(map[string]any)["nextCursor"] == nil {
		t.Fatalf("items: %+v", items)
	}
	if items.Body["items"].([]any)[0].(map[string]any)["kind"] != "demo.install" {
		t.Fatalf("items are list rows: %v", items.Body["items"])
	}
	// Only the requester sees it.
	other := ch.createUser(ch.owner, "olga", iam.RoleOwner, iam.Global, "10.0.0.5")
	if r := other.do("GET", "/management/v1/list-queries/"+id+"/items", nil, nil); r.Code != http.StatusNotFound {
		t.Fatalf("another user must get 404: %+v", r)
	}
	// Unknown list and an auditor without platform:read.
	if r := ch.owner.do("POST", "/management/v1/list-queries", map[string]any{"requestId": "lq-2", "list": "nope"}, nil); r.Code != http.StatusBadRequest {
		t.Fatalf("unknown list: %+v", r)
	}
	ops := ch.createUser(ch.owner, "hank", iam.RoleHostOperator, iam.Scope{Kind: iam.ScopeNode, ID: "n1"}, "10.0.0.7")
	if r := ops.do("POST", "/management/v1/list-queries", map[string]any{"requestId": "lq-3", "list": "users"}, nil); r.Code != http.StatusForbidden {
		t.Fatalf("a host operator cannot read users: %+v", r)
	}
	aud := ch.createUser(ch.owner, "audrey", iam.RoleAuditor, iam.Global, "10.0.0.6")
	if r := aud.do("POST", "/management/v1/list-queries", map[string]any{"requestId": "lq-4", "list": "audit-events"}, nil); r.Code != http.StatusAccepted {
		t.Fatalf("auditor may query the ledger without platform:write: %+v", r)
	}
	// Retention: after 7 days the rows are gone and the answer says so.
	ch.lq.now = func() time.Time { return time.Now().Add(ListQueryRetention + time.Hour) }
	if r := ch.owner.do("GET", "/management/v1/list-queries/"+id+"/items", nil, nil); r.Code != http.StatusGone || r.Body["code"] != CodeListQueryExpired {
		t.Fatalf("expired: %+v", r)
	}
	var left int
	ch.st.DB.QueryRow(`SELECT count(*) FROM list_query_items WHERE job_id=?`, id).Scan(&left)
	if left != 0 {
		t.Fatalf("%d rows left after purge", left)
	}
}

// ---- versioned writes ----------------------------------------------------------

func TestUserUpdateNeedsCurrentVersion(t *testing.T) {
	ch := newContractHarness(t)
	r := ch.owner.do("POST", "/management/v1/users", map[string]string{"username": "vera", "displayName": "Vera", "password": "vera-password-123"}, nil)
	id := r.Body["user"].(map[string]any)["id"].(string)
	got := ch.owner.do("GET", "/management/v1/users/"+id, nil, nil)
	tag := got.Hdr.Get("ETag")
	if tag != `"v1"` {
		t.Fatalf("ETag: %q", tag)
	}
	body := map[string]string{"displayName": "Vera L."}
	if r := ch.owner.do("PATCH", "/management/v1/users/"+id, body, nil); r.Code != http.StatusPreconditionRequired || r.Body["code"] != CodeVersionRequired {
		t.Fatalf("missing If-Match: %+v", r)
	}
	if r := ch.owner.do("PATCH", "/management/v1/users/"+id, body, map[string]string{"If-Match": "*"}); r.Code != http.StatusPreconditionRequired {
		t.Fatalf("If-Match * is no condition: %+v", r)
	}
	if r := ch.owner.do("PATCH", "/management/v1/users/"+id, body, map[string]string{"If-Match": "v1"}); r.Code != http.StatusBadRequest {
		t.Fatalf("malformed If-Match: %+v", r)
	}
	ok := ch.owner.do("PATCH", "/management/v1/users/"+id, body, map[string]string{"If-Match": tag})
	if ok.Code != http.StatusOK || ok.Hdr.Get("ETag") != `"v2"` || ok.Body["user"].(map[string]any)["displayName"] != "Vera L." {
		t.Fatalf("update: %+v", ok)
	}
	// A second tab still holding v1 must not overwrite (34.2).
	stale := ch.owner.do("PATCH", "/management/v1/users/"+id, map[string]string{"displayName": "Other"}, map[string]string{"If-Match": tag})
	if stale.Code != http.StatusPreconditionFailed || stale.Body["code"] != CodeVersionMismatch ||
		stale.Body["details"].(map[string]any)["currentVersion"].(float64) != 2 || stale.Body["resourceId"] != id {
		t.Fatalf("stale write: %+v", stale)
	}
	if now := ch.owner.do("GET", "/management/v1/users/"+id, nil, nil); now.Body["user"].(map[string]any)["displayName"] != "Vera L." {
		t.Fatal("the stale write changed the account")
	}
	// Role changes are part of the representation: they advance the version,
	// and a stale optional If-Match is refused.
	as := ch.owner.do("POST", "/management/v1/users/"+id+"/roles", map[string]any{"role": "auditor", "scope": map[string]string{"kind": "global", "id": "*"}}, nil)
	if as.Code != http.StatusCreated || as.Hdr.Get("ETag") != `"v3"` {
		t.Fatalf("assign: %+v", as)
	}
	if r := ch.owner.do("POST", "/management/v1/users/"+id+"/roles", map[string]any{"role": "host_operator", "scope": map[string]string{"kind": "node", "id": "n1"}},
		map[string]string{"If-Match": `"v2"`}); r.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale assign: %+v", r)
	}
	// Existence and permission come before the version (51.6).
	aud := ch.createUser(ch.owner, "aldo", iam.RoleAuditor, iam.Global, "10.0.0.4")
	if r := aud.do("PATCH", "/management/v1/users/"+id, body, nil); r.Code != http.StatusNotFound && r.Code != http.StatusForbidden {
		t.Fatalf("no users:write: %+v", r)
	}
	if r := ch.owner.do("PATCH", "/management/v1/users/nope", body, nil); r.Code != http.StatusNotFound {
		t.Fatalf("unknown user before version: %+v", r)
	}
}

// ---- dryRun, degradation, confirmations and the 202 answer ------------------------

func TestJobSubmitDryRunChangesNothing(t *testing.T) {
	ch := newContractHarness(t)
	first := ch.owner.do("POST", "/management/v1/jobs", map[string]any{"requestId": "hold", "kind": "demo.install",
		"resources": []map[string]string{{"kind": "database", "id": "d1"}}}, nil)
	ch.waitJob(first.Body["jobId"].(string), jobs.StateRunning)
	req := map[string]any{"requestId": "dry-1", "kind": "demo.install", "resources": []map[string]string{{"kind": "database", "id": "d1"}}}
	pv := ch.owner.do("POST", "/management/v1/jobs?dryRun=true", req, nil)
	if pv.Code != http.StatusOK || pv.Body["dryRun"] != true || pv.Body["ok"] != true {
		t.Fatalf("preview: %+v", pv)
	}
	if q := pv.Body["preview"].(map[string]any)["queuedBehind"].([]any); len(q) != 1 || q[0] != first.Body["jobId"] {
		t.Fatalf("preview must say which job it would wait for: %+v", pv.Body["preview"])
	}
	if n, _ := ch.engine.Count(context.Background(), jobs.ListOptions{}); n != 1 {
		t.Fatalf("dryRun created a job: %d jobs", n)
	}
	// The requestId was not consumed.
	real := ch.owner.do("POST", "/management/v1/jobs", req, nil)
	if real.Code != http.StatusAccepted || real.Body["deduplicated"] != false {
		t.Fatalf("real submit after preview: %+v", real)
	}
	// A preview of a replay says so and is still not a new job.
	rp := ch.owner.do("POST", "/management/v1/jobs?dryRun=true", req, nil)
	if rp.Body["preview"].(map[string]any)["replay"] == nil {
		t.Fatalf("replay preview: %+v", rp)
	}
	if r := ch.owner.do("POST", "/management/v1/jobs?dryRun=yes", req, nil); r.Code != http.StatusBadRequest {
		t.Fatalf("dryRun must be true or false: %+v", r)
	}
	// The preview needs the same permission as the write.
	aud := ch.createUser(ch.owner, "ada", iam.RoleAuditor, iam.Global, "10.0.0.3")
	if r := aud.do("POST", "/management/v1/jobs?dryRun=true", req, nil); r.Code != http.StatusForbidden {
		t.Fatalf("read-only account preview: %+v", r)
	}
}

func TestPlatformDegradedRefusesNewJobs(t *testing.T) {
	ch := newContractHarness(t)
	ok := ch.owner.do("POST", "/management/v1/jobs", map[string]any{"requestId": "before", "kind": "demo.install"}, nil)
	ch.srv.SetPlatformState(func() *PlatformDegradation { return &PlatformDegradation{Mode: "database-read-only"} })
	r := ch.owner.do("POST", "/management/v1/jobs", map[string]any{"requestId": "during", "kind": "demo.install"}, nil)
	if r.Code != http.StatusServiceUnavailable || r.Body["code"] != CodePlatformDegraded || r.Body["details"].(map[string]any)["mode"] != "database-read-only" || r.Body["retryable"] != true {
		t.Fatalf("degraded: %+v", r)
	}
	// An accepted request still replays its job (ENG-002 before degradation).
	if rp := ch.owner.do("POST", "/management/v1/jobs", map[string]any{"requestId": "before", "kind": "demo.install"}, nil); rp.Code != http.StatusAccepted || rp.Body["jobId"] != ok.Body["jobId"] {
		t.Fatalf("replay while degraded: %+v", rp)
	}
	pv := ch.owner.do("POST", "/management/v1/jobs?dryRun=true", map[string]any{"requestId": "during", "kind": "demo.install"}, nil)
	if pv.Code != http.StatusOK || pv.Body["ok"] != false || pv.Body["blockers"].([]any)[0].(map[string]any)["code"] != CodePlatformDegraded {
		t.Fatalf("preview while degraded: %+v", pv)
	}
	if r := ch.owner.do("GET", "/management/v1/jobs", nil, nil); r.Code != http.StatusOK {
		t.Fatalf("reads continue: %+v", r)
	}
	// A kind allowed in the mode goes through.
	ch.srv.SetPlatformState(func() *PlatformDegradation {
		return &PlatformDegradation{Mode: "data-disk-low", AllowKinds: []string{"demo.install"}}
	})
	if r := ch.owner.do("POST", "/management/v1/jobs", map[string]any{"requestId": "cleanup", "kind": "demo.install"}, nil); r.Code != http.StatusAccepted {
		t.Fatalf("allowed kind: %+v", r)
	}
}

func userID(t *testing.T, c *client) string {
	t.Helper()
	r := c.do("GET", "/management/v1/auth/session", nil, nil)
	return r.Body["user"].(map[string]any)["id"].(string)
}

func TestPendingConfirmationBlocksSameResource(t *testing.T) {
	ch := newContractHarness(t)
	cf, err := ch.confirm.Create(context.Background(), confirm.CreateInput{UserID: userID(t, ch.owner), Summary: "firewall",
		Resources: []jobs.ResourceRef{{Kind: "database", ID: "d1"}}, AppliedAt: time.Now(), Window: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	r := ch.owner.do("POST", "/management/v1/jobs", map[string]any{"requestId": "c1", "kind": "demo.install", "resources": []map[string]string{{"kind": "database", "id": "d1"}}}, nil)
	if r.Code != http.StatusConflict || r.Body["code"] != CodeConfirmRequired || strings.Contains(r.Raw, cf.ID) {
		t.Fatalf("pending confirmation: %+v", r)
	}
	if r := ch.owner.do("POST", "/management/v1/jobs", map[string]any{"requestId": "c2", "kind": "demo.install", "resources": []map[string]string{{"kind": "database", "id": "d2"}}}, nil); r.Code != http.StatusAccepted {
		t.Fatalf("other resource: %+v", r)
	}
}

func TestConfirmationNeedsANewConnection(t *testing.T) {
	ch := newContractHarness(t)
	srv := httptest.NewUnstartedServer(ch.h)
	srv.Config.ConnContext = ConnContext
	srv.Start()
	defer srv.Close()
	// The operator's browser has a connection open before the change.
	oldConn := &http.Client{Transport: &http.Transport{MaxConnsPerHost: 1}}
	send := func(cl *http.Client, c *client, method, path string) (int, map[string]any) {
		req, _ := http.NewRequest(method, srv.URL+path, nil)
		for _, ck := range c.cookies {
			req.AddCookie(ck)
		}
		req.Header.Set(CSRFHeader, c.csrf)
		res, err := cl.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var body map[string]any
		json.NewDecoder(res.Body).Decode(&body)
		return res.StatusCode, body
	}
	if code, _ := send(oldConn, ch.owner, "GET", "/management/v1/auth/session"); code != http.StatusOK {
		t.Fatalf("warm-up: %d", code)
	}
	time.Sleep(5 * time.Millisecond)
	cf, err := ch.confirm.Create(context.Background(), confirm.CreateInput{UserID: userID(t, ch.owner),
		Resources: []jobs.ResourceRef{{Kind: "host-firewall", ID: "core"}}, AppliedAt: time.Now(), Window: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	// T-188: the confirmation on the connection from before is refused.
	if code, body := send(oldConn, ch.owner, "POST", "/management/v1/confirmations/"+cf.ID); code != http.StatusConflict || body["code"] != CodeConfirmNewConn {
		t.Fatalf("old connection: %d %v", code, body)
	}
	// Another account cannot even see it.
	other := ch.createUser(ch.owner, "oscar", iam.RoleOwner, iam.Global, "10.0.0.2")
	if code, _ := send(&http.Client{Transport: &http.Transport{}}, other, "POST", "/management/v1/confirmations/"+cf.ID); code != http.StatusNotFound {
		t.Fatalf("other user: %d", code)
	}
	newConn := &http.Client{Transport: &http.Transport{}}
	code, body := send(newConn, ch.owner, "POST", "/management/v1/confirmations/"+cf.ID)
	if code != http.StatusOK || body["confirmation"].(map[string]any)["state"] != "confirmed" {
		t.Fatalf("new connection: %d %v", code, body)
	}
	if code, _ := send(newConn, ch.owner, "POST", "/management/v1/confirmations/"+cf.ID); code != http.StatusOK {
		t.Fatalf("confirming twice answers the same: %d", code)
	}
}

func TestLongOperationAnswer(t *testing.T) {
	ch := newContractHarness(t)
	r := ch.owner.do("POST", "/management/v1/jobs", map[string]any{"requestId": "lo-1", "kind": "demo.install"}, nil)
	id, _ := r.Body["jobId"].(string)
	links, _ := r.Body["links"].(map[string]any)
	if r.Code != http.StatusAccepted || id == "" || r.Hdr.Get("Location") != "/management/v1/jobs/"+id || links["events"] != "/management/v1/jobs/"+id+"/events" ||
		links["cancel"] != "/management/v1/jobs/"+id+"/cancel" || r.Body["job"].(map[string]any)["id"] != id {
		t.Fatalf("202: %+v", r)
	}
	c := ch.owner.do("POST", "/management/v1/jobs/"+id+"/cancel", nil, nil)
	if c.Code != http.StatusAccepted || c.Body["stopped"] == nil || c.Body["links"] == nil {
		t.Fatalf("cancel: %+v", c)
	}
}

// newStoppedHarness is the contract harness whose job engine has not
// started, so submitted jobs stay queued until start is called.
func newStoppedHarness(t *testing.T) (*contractHarness, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	hs := &harness{t: t, clock: time.Now().UTC().Truncate(time.Second)}
	svc, err := iam.New(st.DB, iam.Config{PasswordParams: iam.TestPasswordParams, Now: func() time.Time { return hs.clock }})
	if err != nil {
		t.Fatal(err)
	}
	bh := &blockingHandler{release: make(chan struct{})}
	e := jobs.New(jobs.NewSQLStore(st.DB), jobs.Options{PollInterval: 10 * time.Millisecond})
	lq := NewListQueries(st.DB, svc, e)
	for _, h := range []jobs.Handler{bh, lq} {
		if err := e.Register(h); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	s := New(st, nil)
	s.MountIAM(svc, IAMOptions{})
	s.RegisterJobs(e)
	s.MountListQueries(lq)
	s.MountConfirmations(confirm.New(st.DB, nil))
	hs.h, hs.svc, hs.srv = s.Handler(), svc, s
	started := false
	t.Cleanup(func() {
		close(bh.release)
		cancel()
		if started {
			e.Stop()
		}
		st.Close()
	})
	ch := &contractHarness{harness: hs, owner: hs.bootstrap(), engine: e, bh: bh, lq: lq, st: st}
	return ch, func() { started = true; e.Start(ctx) }
}

// A requestId another account already used never hands its job (or a
// background query's rows) to the second account.
func TestRequestIDOfAnotherAccountIsNotReplayed(t *testing.T) {
	ch, start := newStoppedHarness(t)
	scoped := ch.createUser(ch.owner, "pia", iam.RoleAuditor, iam.Scope{Kind: iam.ScopeProject, ID: "p1"}, "10.0.0.11")
	body := map[string]any{"requestId": "shared-1", "list": "audit-events", "sort": "-seq"}
	if r := ch.owner.do("POST", "/management/v1/list-queries", body, nil); r.Code != http.StatusAccepted {
		t.Fatalf("owner: %+v", r)
	}
	r := scoped.do("POST", "/management/v1/list-queries", body, nil)
	if r.Code != http.StatusConflict || r.Body["code"] != CodeRequestIDConflict || strings.Contains(r.Raw, `"job"`) {
		t.Fatalf("the second account must not get the owner's job: %+v", r)
	}
	start()
	var id string
	ch.st.DB.QueryRow(`SELECT id FROM jobs WHERE request_id='shared-1'`).Scan(&id)
	ch.waitJob(id, jobs.StateSucceeded)
	var requester string
	ch.st.DB.QueryRow(`SELECT requester_id FROM list_queries WHERE job_id=?`, id).Scan(&requester)
	if requester != userID(t, ch.owner) {
		t.Fatalf("the query belongs to %s", requester)
	}
	if r := scoped.do("GET", "/management/v1/list-queries/"+id+"/items", nil, nil); r.Code != http.StatusNotFound {
		t.Fatalf("the second account must not read the rows: %+v", r)
	}
	// The same through POST /jobs.
	ops := ch.createUser(ch.owner, "pat", iam.RolePlatformAdmin, iam.Global, "10.0.0.12")
	ch.owner.do("POST", "/management/v1/jobs", map[string]any{"requestId": "shared-2", "kind": "demo.install"}, nil)
	if r := ops.do("POST", "/management/v1/jobs", map[string]any{"requestId": "shared-2", "kind": "demo.install"}, nil); r.Code != http.StatusConflict {
		t.Fatalf("jobs replay across accounts: %+v", r)
	}
}

func TestJobRequesterIsTheSignedInAccount(t *testing.T) {
	ch := newContractHarness(t)
	ops := ch.createUser(ch.owner, "paul", iam.RolePlatformAdmin, iam.Global, "10.0.0.13")
	r := ops.do("POST", "/management/v1/jobs", map[string]any{"requestId": "spoof", "kind": "demo.install", "requester": userID(t, ch.owner)}, nil)
	if r.Code != http.StatusAccepted || r.Body["job"].(map[string]any)["requester"] != userID(t, ops) {
		t.Fatalf("requester must be the caller: %+v", r)
	}
}

func TestBackgroundQueryRowsFollowCurrentScopes(t *testing.T) {
	ch := newContractHarness(t)
	aud := ch.createUser(ch.owner, "ava", iam.RoleAuditor, iam.Global, "10.0.0.14")
	r := aud.do("POST", "/management/v1/list-queries", map[string]any{"requestId": "scope-1", "list": "audit-events"}, nil)
	id := r.Body["jobId"].(string)
	ch.waitJob(id, jobs.StateSucceeded)
	if r := aud.do("GET", "/management/v1/list-queries/"+id+"/items", nil, nil); r.Code != http.StatusOK {
		t.Fatalf("before: %+v", r)
	}
	// Narrow the auditor to one project: the global rows are no longer theirs.
	uid := userID(t, aud)
	detail := ch.owner.do("GET", "/management/v1/users/"+uid, nil, nil)
	for _, a := range detail.Body["user"].(map[string]any)["assignments"].([]any) {
		ch.owner.do("DELETE", "/management/v1/users/"+uid+"/roles/"+a.(map[string]any)["id"].(string), nil, nil)
	}
	ch.owner.do("POST", "/management/v1/users/"+uid+"/roles", map[string]any{"role": "auditor", "scope": map[string]string{"kind": "project", "id": "p1"}}, nil)
	if r := aud.do("GET", "/management/v1/list-queries/"+id+"/items", nil, nil); r.Code != http.StatusNotFound {
		t.Fatalf("after narrowing: %+v", r)
	}
}

func TestBackgroundQueriesPerAccountAreLimited(t *testing.T) {
	ch := newContractHarness(t)
	for i := 0; i < ListQueryKeepLimit; i++ {
		if r := ch.owner.do("POST", "/management/v1/list-queries", map[string]any{"requestId": fmt.Sprint("q", i), "list": "users"}, nil); r.Code != http.StatusAccepted {
			t.Fatalf("query %d: %+v", i, r)
		}
	}
	r := ch.owner.do("POST", "/management/v1/list-queries", map[string]any{"requestId": "one-more", "list": "users"}, nil)
	if r.Code != http.StatusConflict || r.Body["code"] != CodeListQueryLimit {
		t.Fatalf("over the limit: %+v", r)
	}
}

func TestForgedCursorIsRefused(t *testing.T) {
	ch := newContractHarness(t)
	for i := 0; i < 3; i++ {
		ch.owner.do("POST", "/management/v1/jobs", map[string]any{"requestId": fmt.Sprint("f", i), "kind": "demo.install",
			"resources": []map[string]string{{"kind": "database", "id": fmt.Sprint("d", i)}}}, nil)
	}
	page := ch.owner.do("GET", "/management/v1/audit-events?limit=1", nil, nil)
	cur := page.Body["page"].(map[string]any)["nextCursor"].(string)
	forged := []byte(cur)
	forged[len(forged)/2] ^= 1
	for _, c := range []string{string(forged), cur[:len(cur)-2]} {
		if r := ch.owner.do("GET", "/management/v1/audit-events?cursor="+c, nil, nil); r.Code != http.StatusBadRequest {
			t.Fatalf("forged cursor: %+v", r)
		}
	}
}

func TestDryRunRefusalComesBeforeLookups(t *testing.T) {
	ch := newContractHarness(t)
	ops := ch.createUser(ch.owner, "hugo", iam.RoleHostOperator, iam.Scope{Kind: iam.ScopeNode, ID: "n1"}, "10.0.0.15")
	r := ops.do("PATCH", "/management/v1/users/nope?dryRun=true", map[string]string{"displayName": "x"}, nil)
	if r.Code != http.StatusBadRequest || r.Body["code"] != CodeDryRunUnsupported {
		t.Fatalf("an unsupported dryRun is refused before the handler looks anything up: %+v", r)
	}
	if !slices.Contains(BlockingOrder()[:4], CodeDryRunUnsupported) {
		t.Fatalf("DRY_RUN_UNSUPPORTED must rank right after authentication: %v", BlockingOrder())
	}
}

func TestConcurrentRevokeAuditsOnce(t *testing.T) {
	ch := newContractHarness(t)
	r := ch.owner.do("POST", "/management/v1/users", map[string]string{"username": "rita", "password": "rita-password-123"}, nil)
	uid := r.Body["user"].(map[string]any)["id"].(string)
	as := ch.owner.do("POST", "/management/v1/users/"+uid+"/roles", map[string]any{"role": "auditor", "scope": map[string]string{"kind": "global", "id": "*"}}, nil)
	aid := as.Body["assignment"].(map[string]any)["id"].(string)
	ownerP, _ := ch.svc.PrincipalByID(context.Background(), userID(t, ch.owner))
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := ch.svc.RevokeRoleIf(context.Background(), ownerP, aid, uid, 0, iam.Source{})
			errs <- err
		}()
	}
	ok := 0
	for i := 0; i < 2; i++ {
		if <-errs == nil {
			ok++
		}
	}
	var n int
	ch.st.DB.QueryRow(`SELECT count(*) FROM audit_events WHERE action='role.revoke' AND resource_id=?`, uid).Scan(&n)
	if ok != 1 || n != 1 {
		t.Fatalf("revoked %d times, %d audit events", ok, n)
	}
}
