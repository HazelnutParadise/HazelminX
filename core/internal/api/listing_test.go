package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// Table-driven tests for the list contract in listing.go. Every Expected
// results bullet of parseListQuery, encodeCursor and runList has at least one
// case here. The implementations are still panic("TODO"), so these tests fail
// until they are filled in; ltGuard turns that panic into a test failure.

// ltRow is the row type of the in-memory list source.
type ltRow struct {
	ID        string `json:"id"`
	CreatedAt string `json:"createdAt"`
	Kind      string `json:"kind"`
}

// ltSafety bounds how long the in-memory source blocks when the context never
// ends. If an implementation ignores ListSpec.Timeout, the source returns a
// plain (non-timeout) error after this long, so the test fails instead of
// hanging.
const ltSafety = 3 * time.Second

// ltSource is an in-memory ListSource[ltRow]. Its Count and Fetch honour only
// the kind filter, because ltRow has no state or sequence field.
type ltSource struct {
	rows []ltRow

	countSleep bool  // Count blocks until ctx ends, then returns ctx.Err()
	countErr   error // Count returns this error (not a timeout)

	fetchReadN int   // with fetchSleep: rows returned before blocking
	fetchSleep bool  // Fetch blocks after fetchReadN rows until ctx ends
	fetchErr   error // Fetch returns this error (not a timeout)

	countCalls int
	fetchCalls int
	fetchNs    []int // n passed to each Fetch call, in order
}

func (s *ltSource) Count(ctx context.Context, q ListQuery) (int, error) {
	s.countCalls++
	if s.countSleep {
		return 0, ltWait(ctx)
	}
	if s.countErr != nil {
		return 0, s.countErr
	}
	n := 0
	for _, r := range s.rows {
		if ltMatch(r, q) {
			n++
		}
	}
	return n, nil
}

func (s *ltSource) Fetch(ctx context.Context, q ListQuery, n int) ([]ltRow, error) {
	s.fetchCalls++
	s.fetchNs = append(s.fetchNs, n)
	if s.fetchErr != nil {
		return nil, s.fetchErr
	}
	rows := slices.Clone(s.rows)
	sort.SliceStable(rows, func(i, j int) bool {
		return ltCmpKey(rows[i], ltRowKey(rows[j], q.Sort), q.Sort) < 0
	})
	out := []ltRow{}
	for _, r := range rows {
		if !ltMatch(r, q) {
			continue
		}
		if q.After != nil && ltCmpKey(r, q.After, q.Sort) <= 0 {
			continue
		}
		if len(out) == n {
			break
		}
		if s.fetchSleep && len(out) == s.fetchReadN {
			return out, ltWait(ctx)
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *ltSource) Key(r ltRow, keys []SortKey) []string {
	return ltRowKey(r, keys)
}

// ltWait blocks until ctx ends and returns ctx.Err(), or after ltSafety with a
// non-timeout error.
func ltWait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(ltSafety):
		return errors.New("test source: slept past ltSafety without ctx ending; ListSpec.Timeout not enforced")
	}
}

func ltMatch(r ltRow, q ListQuery) bool {
	if kinds, ok := q.Filters["kind"]; ok && len(kinds) > 0 {
		return slices.Contains(kinds, r.Kind)
	}
	return true
}

func ltKey(r ltRow, field string) string {
	switch field {
	case "createdAt":
		return r.CreatedAt
	case "kind":
		return r.Kind
	case "id":
		return r.ID
	}
	return ""
}

func ltRowKey(r ltRow, keys []SortKey) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = ltKey(r, k.Field)
	}
	return out
}

// ltCmpKey compares row r with the key after under keys. It returns a value
// below zero, zero, or above zero when r sorts before, equal to, or after.
func ltCmpKey(r ltRow, after []string, keys []SortKey) int {
	for i, k := range keys {
		c := strings.Compare(ltKey(r, k.Field), after[i])
		if c != 0 {
			if k.Desc {
				return -c
			}
			return c
		}
	}
	return 0
}

// ltRows returns n rows. Rows come in pairs that share a CreatedAt, so the
// tie breaker id is exercised. Kinds cycle build, test, deploy.
func ltRows(n int) []ltRow {
	kinds := []string{"build", "test", "deploy"}
	out := make([]ltRow, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, ltRow{
			ID:        fmt.Sprintf("job-%02d", i),
			CreatedAt: fmt.Sprintf("2026-03-%02dT09:00:00Z", i/2+1),
			Kind:      kinds[i%3],
		})
	}
	return out
}

// ltExpect returns the IDs of rows in keys order.
func ltExpect(rows []ltRow, keys []SortKey) []string {
	cp := slices.Clone(rows)
	sort.SliceStable(cp, func(i, j int) bool {
		return ltCmpKey(cp[i], ltRowKey(cp[j], keys), keys) < 0
	})
	return ltIDs(cp)
}

func ltIDs(items []ltRow) []string {
	out := make([]string, 0, len(items))
	for _, r := range items {
		out = append(out, r.ID)
	}
	return out
}

func ltFind(rows []ltRow, id string) ltRow {
	for _, r := range rows {
		if r.ID == id {
			return r
		}
	}
	return ltRow{}
}

// ltDefaultSort is DefaultSort (-createdAt) plus the tie breaker, both desc.
func ltDefaultSort() []SortKey {
	return []SortKey{{Field: "createdAt", Desc: true}, {Field: "id", Desc: true}}
}

// ltSpec is the example spec from the test brief: "jobs".
func ltSpec() ListSpec {
	return ListSpec{
		Name:        "jobs",
		MaxLimit:    200,
		Sorts:       []string{"createdAt", "kind"},
		DefaultSort: []SortKey{{Field: "createdAt", Desc: true}},
		TieBreaker:  "id",
		Filters: []FilterSpec{
			{Name: "state", Summary: "job state", Multi: true, Enum: []string{"queued", "running", "succeeded"}},
			{Name: "kind", Summary: "job kind"},
			{Name: "afterSeq", Summary: "sequence number", Int: true},
		},
	}
}

// ltCtx builds a gin test context whose request has the raw query string.
func ltCtx(raw string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/x?"+raw, nil)
	return c, rec
}

func ltParse(spec ListSpec, raw string) (ListQuery, *apiError) {
	c, _ := ltCtx(raw)
	return parseListQuery(c, spec)
}

func ltRun(spec ListSpec, src ListSource[ltRow], raw string) *httptest.ResponseRecorder {
	c, rec := ltCtx(raw)
	runList(c, spec, src)
	return rec
}

// ltWalk requests n successive pages. Every page sends limit (a cursor does
// not carry the limit), and each page after the first adds the cursor from the
// page before. It fails the test when an earlier page has no nextCursor.
func ltWalk(t *testing.T, spec ListSpec, src ListSource[ltRow], limit string, n int) []*httptest.ResponseRecorder {
	t.Helper()
	recs := make([]*httptest.ResponseRecorder, 0, n)
	raw := limit
	for i := 0; i < n; i++ {
		rec := ltRun(spec, src, raw)
		if rec.Code != http.StatusOK {
			t.Fatalf("page %d: status %d, want 200; body %s", i+1, rec.Code, rec.Body.String())
		}
		recs = append(recs, rec)
		next := ltDecodeList(t, rec).Page.NextCursor
		if i < n-1 {
			if next == "" {
				t.Fatalf("page %d: no nextCursor, but page %d was requested", i+1, i+2)
			}
			raw = limit + "&cursor=" + url.QueryEscape(next)
		}
	}
	return recs
}

func ltDecodeList(t *testing.T, rec *httptest.ResponseRecorder) ListResponse[ltRow] {
	t.Helper()
	var v ListResponse[ltRow]
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode list body: %v; body %s", err, rec.Body.String())
	}
	return v
}

// ltPageMap returns the raw "page" object, to tell absent keys from null.
func ltPageMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var v struct {
		Page map[string]any `json:"page"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode page: %v; body %s", err, rec.Body.String())
	}
	return v.Page
}

func ltDecodeErr(t *testing.T, rec *httptest.ResponseRecorder) apiError {
	t.Helper()
	var e apiError
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("decode error body: %v; body %s", err, rec.Body.String())
	}
	return e
}

// ltWantParseErr fails unless e is a VALIDATION_FAILED error with a message
// and, when field is set, a FieldError naming it.
func ltWantParseErr(t *testing.T, e *apiError, field string) {
	t.Helper()
	if e == nil {
		t.Fatalf("want VALIDATION_FAILED error for field %q, got nil", field)
	}
	if e.Code != CodeValidationFailed {
		t.Errorf("code = %q, want %q", e.Code, CodeValidationFailed)
	}
	if e.Message == "" {
		t.Errorf("message is empty, want plain-language text")
	}
	if field == "" {
		return
	}
	for _, f := range e.Fields {
		if f.Field == field {
			return
		}
	}
	t.Errorf("fields = %+v, want one naming %q", e.Fields, field)
}

func ltFieldNames(e *apiError) []string {
	out := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		out = append(out, f.Field)
	}
	return out
}

func ltFiltersEq(a, b map[string][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || !slices.Equal(av, bv) {
			return false
		}
	}
	return true
}

// ltCursor makes a cursor with encodeCursor, for spec, keys and so on.
func ltCursor(spec ListSpec, keys []SortKey, filters map[string][]string, after []string, pos int, total *int) string {
	return encodeCursor(spec, ListQuery{Sort: keys, Filters: filters, Total: total}, after, pos)
}

// ltGuard turns the panic("TODO") in the stubs into a test failure. It must be
// deferred directly so that recover sees the panic.
func ltGuard(t *testing.T) {
	if r := recover(); r != nil {
		t.Fatalf("listing stub panicked (not implemented yet): %v", r)
	}
}

// ---------------------------------------------------------------- parseListQuery

func TestParseListQuery_Defaults(t *testing.T) {
	defer ltGuard(t)
	q, e := ltParse(ltSpec(), "")
	if e != nil {
		t.Fatalf("unexpected error: %+v", *e)
	}
	if q.Limit != DefaultPageLimit {
		t.Errorf("Limit = %d, want DefaultLimit %d", q.Limit, DefaultPageLimit)
	}
	if !slices.Equal(q.Sort, ltDefaultSort()) {
		t.Errorf("Sort = %+v, want DefaultSort plus tie breaker %+v", q.Sort, ltDefaultSort())
	}
	if q.Filters == nil || len(q.Filters) != 0 {
		t.Errorf("Filters = %#v, want an empty non-nil map", q.Filters)
	}
	if q.After != nil {
		t.Errorf("After = %v, want nil on the first page", q.After)
	}
	if q.Pos != 0 {
		t.Errorf("Pos = %d, want 0", q.Pos)
	}
	if q.Total != nil {
		t.Errorf("Total = %d, want nil", *q.Total)
	}
}

func TestParseListQuery_Limit(t *testing.T) {
	defer ltGuard(t)
	ok := []struct {
		raw  string
		want int
	}{
		{"limit=1", 1},
		{"limit=200", 200},
	}
	for _, tc := range ok {
		t.Run("accept "+tc.raw, func(t *testing.T) {
			defer ltGuard(t)
			q, e := ltParse(ltSpec(), tc.raw)
			if e != nil {
				t.Fatalf("unexpected error: %+v", *e)
			}
			if q.Limit != tc.want {
				t.Errorf("Limit = %d, want %d", q.Limit, tc.want)
			}
		})
	}
	for _, raw := range []string{"limit=0", "limit=-1", "limit=abc", "limit=201"} {
		t.Run("reject "+raw, func(t *testing.T) {
			defer ltGuard(t)
			_, e := ltParse(ltSpec(), raw)
			ltWantParseErr(t, e, "limit")
			if e != nil && e.Fields[0].Field != "limit" {
				t.Errorf("Fields[0].Field = %q, want limit", e.Fields[0].Field)
			}
		})
	}
}

func TestParseListQuery_Sort(t *testing.T) {
	defer ltGuard(t)
	ok := []struct {
		name string
		raw  string
		want []SortKey
	}{
		{"desc first key, tie breaker follows last key", "sort=-createdAt,kind",
			[]SortKey{{Field: "createdAt", Desc: true}, {Field: "kind"}, {Field: "id"}}},
		{"single asc key gets asc tie breaker", "sort=kind",
			[]SortKey{{Field: "kind"}, {Field: "id"}}},
		{"single desc key gets desc tie breaker", "sort=-createdAt",
			[]SortKey{{Field: "createdAt", Desc: true}, {Field: "id", Desc: true}}},
		{"explicit tie breaker as last key is not doubled", "sort=kind,id",
			[]SortKey{{Field: "kind"}, {Field: "id"}}},
		{"explicit desc tie breaker as last key", "sort=-kind,-id",
			[]SortKey{{Field: "kind", Desc: true}, {Field: "id", Desc: true}}},
	}
	for _, tc := range ok {
		t.Run(tc.name, func(t *testing.T) {
			defer ltGuard(t)
			q, e := ltParse(ltSpec(), tc.raw)
			if e != nil {
				t.Fatalf("unexpected error: %+v", *e)
			}
			if !slices.Equal(q.Sort, tc.want) {
				t.Errorf("Sort = %+v, want %+v", q.Sort, tc.want)
			}
		})
	}
	bad := []struct{ name, raw string }{
		{"field not in Sorts", "sort=foo"},
		{"tie breaker not last", "sort=id,kind"},
		{"empty element in the middle", "sort=createdAt,,kind"},
		{"empty trailing element", "sort=createdAt,"},
		{"repeated field", "sort=createdAt,-createdAt"},
		{"bare direction prefix", "sort=-"},
	}
	for _, tc := range bad {
		t.Run("reject "+tc.name, func(t *testing.T) {
			defer ltGuard(t)
			_, e := ltParse(ltSpec(), tc.raw)
			ltWantParseErr(t, e, "sort")
		})
	}
}

func TestParseListQuery_Filters(t *testing.T) {
	defer ltGuard(t)
	ok := []struct {
		name string
		raw  string
		want map[string][]string
	}{
		{"multi splits, trims, drops empties and duplicates, keeps order",
			"state=queued,%20running,,queued,succeeded",
			map[string][]string{"state": {"queued", "running", "succeeded"}}},
		{"single value for a single filter", "kind=build",
			map[string][]string{"kind": {"build"}}},
		{"int filter", "afterSeq=7",
			map[string][]string{"afterSeq": {"7"}}},
	}
	for _, tc := range ok {
		t.Run(tc.name, func(t *testing.T) {
			defer ltGuard(t)
			q, e := ltParse(ltSpec(), tc.raw)
			if e != nil {
				t.Fatalf("unexpected error: %+v", *e)
			}
			if !ltFiltersEq(q.Filters, tc.want) {
				t.Errorf("Filters = %#v, want %#v", q.Filters, tc.want)
			}
		})
	}
	bad := []struct{ name, raw, field string }{
		{"enum value unknown", "state=bogus", "state"},
		{"enum list with one unknown value", "state=queued,bogus", "state"},
		{"single filter given two values", "kind=build,test", "kind"},
		{"single filter repeated", "kind=build&kind=test", "kind"},
		{"int filter not an integer", "afterSeq=abc", "afterSeq"},
		{"int filter negative", "afterSeq=-1", "afterSeq"},
	}
	for _, tc := range bad {
		t.Run("reject "+tc.name, func(t *testing.T) {
			defer ltGuard(t)
			_, e := ltParse(ltSpec(), tc.raw)
			ltWantParseErr(t, e, tc.field)
		})
	}
}

func TestParseListQuery_UndeclaredParams(t *testing.T) {
	defer ltGuard(t)
	noSeq := ltSpec()
	noSeq.Filters = noSeq.Filters[:2] // state and kind only
	cases := []struct {
		name   string
		spec   ListSpec
		raw    string
		fields []string
	}{
		{"foo is not declared", ltSpec(), "foo=1", []string{"foo"}},
		{"dryRun is not declared", ltSpec(), "dryRun=true", []string{"dryRun"}},
		{"two unknown names are listed sorted", ltSpec(), "foo=1&bar=2", []string{"bar", "foo"}},
		{"afterSeq on a list that does not declare it", noSeq, "afterSeq=1", []string{"afterSeq"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer ltGuard(t)
			_, e := ltParse(tc.spec, tc.raw)
			if e == nil {
				t.Fatalf("want VALIDATION_FAILED, got nil")
			}
			if e.Code != CodeValidationFailed {
				t.Errorf("code = %q, want %q", e.Code, CodeValidationFailed)
			}
			if got := ltFieldNames(e); !slices.Equal(got, tc.fields) {
				t.Errorf("field names = %v, want %v", got, tc.fields)
			}
		})
	}
}

func TestParseListQuery_ReportsAllProblemsAtOnce(t *testing.T) {
	defer ltGuard(t)
	_, e := ltParse(ltSpec(), "limit=0&state=bogus&foo=1")
	if e == nil {
		t.Fatalf("want VALIDATION_FAILED, got nil")
	}
	if e.Code != CodeValidationFailed {
		t.Errorf("code = %q, want %q", e.Code, CodeValidationFailed)
	}
	got := ltFieldNames(e)
	slices.Sort(got)
	want := []string{"foo", "limit", "state"}
	if !slices.Equal(got, want) {
		t.Errorf("field names = %v, want all three problems %v in one error", got, want)
	}
}

func TestParseListQuery_CursorMustMatchSortAndFilters(t *testing.T) {
	defer ltGuard(t)
	spec := ltSpec()
	cur := ltCursor(spec, ltDefaultSort(), map[string][]string{"kind": {"build"}},
		[]string{"2026-03-05T09:00:00Z", "job-07"}, 12, nil)
	ok := []struct{ name, raw string }{
		{"cursor alone", "cursor=" + url.QueryEscape(cur)},
		{"same sort written without tie breaker", "cursor=" + url.QueryEscape(cur) + "&sort=-createdAt"},
		{"same sort written in full", "cursor=" + url.QueryEscape(cur) + "&sort=-createdAt,-id"},
		{"same filter", "cursor=" + url.QueryEscape(cur) + "&kind=build"},
	}
	for _, tc := range ok {
		t.Run("accept "+tc.name, func(t *testing.T) {
			defer ltGuard(t)
			q, e := ltParse(spec, tc.raw)
			if e != nil {
				t.Fatalf("unexpected error: %+v", *e)
			}
			if !slices.Equal(q.Sort, ltDefaultSort()) {
				t.Errorf("Sort = %+v, want cursor sort %+v", q.Sort, ltDefaultSort())
			}
		})
	}
	bad := []struct{ name, raw string }{
		{"different sort", "cursor=" + url.QueryEscape(cur) + "&sort=createdAt"},
		{"different sort field", "cursor=" + url.QueryEscape(cur) + "&sort=kind"},
		{"different filter value", "cursor=" + url.QueryEscape(cur) + "&kind=test"},
		{"filter the cursor did not have", "cursor=" + url.QueryEscape(cur) + "&state=queued"},
	}
	for _, tc := range bad {
		t.Run("reject "+tc.name, func(t *testing.T) {
			defer ltGuard(t)
			_, e := ltParse(spec, tc.raw)
			ltWantParseErr(t, e, "cursor")
		})
	}
}

func TestParseListQuery_InvalidCursor(t *testing.T) {
	defer ltGuard(t)
	spec := ltSpec()
	other := ltSpec()
	other.Name = "users"
	otherCur := ltCursor(other, ltDefaultSort(), nil, []string{"2026-03-01T09:00:00Z", "job-01"}, 1, nil)
	mismatch := ltCursor(spec, ltDefaultSort(), nil, []string{"only-one"}, 5, nil)
	cases := []struct{ name, raw string }{
		{"not base64url JSON", "cursor=" + url.QueryEscape("not-a-cursor!!")},
		{"valid base64url but not JSON", "cursor=" + base64.RawURLEncoding.EncodeToString([]byte("hello"))},
		{"cursor made for another list", "cursor=" + url.QueryEscape(otherCur)},
		{"after length differs from sort length", "cursor=" + url.QueryEscape(mismatch)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer ltGuard(t)
			_, e := ltParse(spec, tc.raw)
			ltWantParseErr(t, e, "cursor")
		})
	}
}

func TestParseListQuery_CursorAllowsOtherLimit(t *testing.T) {
	defer ltGuard(t)
	spec := ltSpec()
	after := []string{"2026-03-05T09:00:00Z", "job-07"}
	cur := ltCursor(spec, ltDefaultSort(), nil, after, 12, nil)
	q, e := ltParse(spec, "limit=5&cursor="+url.QueryEscape(cur))
	if e != nil {
		t.Fatalf("unexpected error: %+v", *e)
	}
	if q.Limit != 5 {
		t.Errorf("Limit = %d, want 5 (limit may differ from the first page)", q.Limit)
	}
	if !slices.Equal(q.After, after) {
		t.Errorf("After = %v, want %v from the cursor", q.After, after)
	}
}

// ---------------------------------------------------------------- encodeCursor

func TestEncodeCursor_RoundTripThroughParse(t *testing.T) {
	defer ltGuard(t)
	spec := ltSpec()
	total := 45
	cases := []struct {
		name  string
		total *int
	}{
		{"total counted", &total},
		{"total unknown", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer ltGuard(t)
			q := ListQuery{
				Sort:    ltDefaultSort(),
				Filters: map[string][]string{"kind": {"build"}, "state": {"queued", "running"}},
				Total:   tc.total,
			}
			after := []string{"2026-03-05T09:00:00Z", "job-07"}
			cur := encodeCursor(spec, q, after, 12)

			if cur == "" {
				t.Fatalf("empty cursor")
			}
			if strings.ContainsAny(cur, "+/=") {
				t.Errorf("cursor %q is not unpadded base64url", cur)
			}
			if _, err := base64.RawURLEncoding.DecodeString(cur); err != nil {
				t.Errorf("cursor is not base64url without padding: %v", err)
			}

			got, e := ltParse(spec, "cursor="+url.QueryEscape(cur))
			if e != nil {
				t.Fatalf("parse of own cursor failed: %+v", *e)
			}
			if !slices.Equal(got.Sort, q.Sort) {
				t.Errorf("Sort = %+v, want %+v", got.Sort, q.Sort)
			}
			if !ltFiltersEq(got.Filters, q.Filters) {
				t.Errorf("Filters = %#v, want %#v", got.Filters, q.Filters)
			}
			if !slices.Equal(got.After, after) {
				t.Errorf("After = %v, want %v", got.After, after)
			}
			if got.Pos != 12 {
				t.Errorf("Pos = %d, want 12", got.Pos)
			}
			switch {
			case tc.total == nil && got.Total != nil:
				t.Errorf("Total = %d, want nil", *got.Total)
			case tc.total != nil && (got.Total == nil || *got.Total != *tc.total):
				t.Errorf("Total = %v, want %d", got.Total, *tc.total)
			}
		})
	}
}

func TestEncodeCursor_RejectedBySpecWithOtherName(t *testing.T) {
	defer ltGuard(t)
	specA := ltSpec()
	specB := ltSpec()
	specB.Name = "users"
	cur := encodeCursor(specA, ListQuery{Sort: ltDefaultSort()}, []string{"2026-03-05T09:00:00Z", "job-07"}, 3)

	if _, e := ltParse(specA, "cursor="+url.QueryEscape(cur)); e != nil {
		t.Fatalf("spec A should accept its own cursor, got %+v", *e)
	}
	_, e := ltParse(specB, "cursor="+url.QueryEscape(cur))
	ltWantParseErr(t, e, "cursor")
}

// ---------------------------------------------------------------- runList

func TestRunList_WalksAllPagesWithCursor(t *testing.T) {
	defer ltGuard(t)
	spec := ltSpec()
	src := &ltSource{rows: ltRows(25)}
	want := ltExpect(src.rows, ltDefaultSort())

	recs := ltWalk(t, spec, src, "limit=10", 3)
	var got []string
	for i, rec := range recs {
		body := ltDecodeList(t, rec)
		page := body.Page
		got = append(got, ltIDs(body.Items)...)

		wantLen := 10
		if i == 2 {
			wantLen = 5
		}
		if len(body.Items) != wantLen {
			t.Errorf("page %d: %d items, want %d", i+1, len(body.Items), wantLen)
		}
		if page.Limit != 10 || page.QueryCap != DefaultQueryCap || page.TimeoutSeconds != 15 {
			t.Errorf("page %d: limit/queryCap/timeoutSeconds = %d/%d/%d, want 10/%d/15",
				i+1, page.Limit, page.QueryCap, page.TimeoutSeconds, DefaultQueryCap)
		}
		if page.Sort != "-createdAt,-id" {
			t.Errorf("page %d: sort = %q, want %q", i+1, page.Sort, "-createdAt,-id")
		}
		if page.Total == nil || *page.Total != 25 {
			t.Errorf("page %d: total = %v, want 25 on every page", i+1, page.Total)
		}
		if page.Capped || page.TimedOut || page.BackgroundQuery != nil {
			t.Errorf("page %d: capped/timedOut/backgroundQuery = %v/%v/%v, want false/false/absent",
				i+1, page.Capped, page.TimedOut, page.BackgroundQuery)
		}
		pm := ltPageMap(t, rec)
		if _, ok := pm["backgroundQuery"]; ok {
			t.Errorf("page %d: backgroundQuery key present on a normal page", i+1)
		}
		if i == 2 {
			if _, ok := pm["nextCursor"]; ok || page.NextCursor != "" {
				t.Errorf("last page: nextCursor present (%q), want none", page.NextCursor)
			}
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("walk IDs = %v\nwant %v", got, want)
	}
	if src.countCalls != 1 {
		t.Errorf("Count calls = %d, want 1 (later pages reuse the cursor's total)", src.countCalls)
	}
}

func TestRunList_QueryCapStopsWalkAndCaps(t *testing.T) {
	defer ltGuard(t)
	spec := ltSpec()
	spec.QueryCap = 30
	src := &ltSource{rows: ltRows(45)}
	want := ltExpect(src.rows, ltDefaultSort())

	recs := ltWalk(t, spec, src, "limit=8", 4)
	var got []string
	for i, rec := range recs {
		body := ltDecodeList(t, rec)
		page := body.Page
		got = append(got, ltIDs(body.Items)...)

		if !page.Capped {
			t.Errorf("page %d: capped = false, want true (total 45 > queryCap 30)", i+1)
		}
		if page.BackgroundQuery == nil {
			t.Fatalf("page %d: backgroundQuery absent, want it because capped", i+1)
		}
		bq := page.BackgroundQuery
		if bq.List != "jobs" || bq.Sort != "-createdAt,-id" || bq.Filters != nil {
			t.Errorf("page %d: backgroundQuery = %+v, want list jobs, sort -createdAt,-id, no filters", i+1, *bq)
		}
	}
	last := ltDecodeList(t, recs[3])
	if len(last.Items) != 6 {
		t.Errorf("last page: %d items, want 6 (30 is the cap, 24 already read)", len(last.Items))
	}
	if n := last.Page.NextCursor; n != "" {
		t.Errorf("page at the cap: nextCursor = %q, want none (Pos+returned == queryCap)", n)
	}
	if got := ltIDs(last.Items); !slices.Equal(got, want[24:30]) {
		t.Errorf("last page IDs = %v, want %v", got, want[24:30])
	}
	if !slices.Equal(ltIDs(ltDecodeList(t, recs[0]).Items), want[:8]) {
		t.Errorf("first page IDs are not the first 8 rows in order")
	}
	if !slices.Equal(src.fetchNs, []int{9, 9, 9, 7}) {
		t.Errorf("Fetch n values = %v, want [9 9 9 7] (min(limit, queryCap-Pos)+1)", src.fetchNs)
	}
	if len(got) != 30 {
		t.Errorf("walk returned %d rows, want 30 (the cap)", len(got))
	}
}

func TestRunList_CursorPastQueryCapIsRejected(t *testing.T) {
	defer ltGuard(t)
	spec := ltSpec()
	spec.QueryCap = 30
	src := &ltSource{rows: ltRows(45)}
	want := ltExpect(src.rows, ltDefaultSort())
	total := 45
	cur := ltCursor(spec, ltDefaultSort(), nil,
		ltRowKey(ltFind(src.rows, want[29]), ltDefaultSort()), 30, &total)

	rec := ltRun(spec, src, "limit=10&cursor="+url.QueryEscape(cur))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", rec.Code, rec.Body.String())
	}
	e := ltDecodeErr(t, rec)
	if e.Code != CodeQueryLimitExceeded {
		t.Errorf("code = %q, want %q", e.Code, CodeQueryLimitExceeded)
	}
	if !slices.Equal(e.SuggestedActions, []string{"run-as-background-query"}) {
		t.Errorf("suggestedActions = %v, want [run-as-background-query]", e.SuggestedActions)
	}
	if got, _ := e.Details["queryCap"].(float64); got != 30 {
		t.Errorf("details.queryCap = %v, want 30", e.Details["queryCap"])
	}
	bq, ok := e.Details["backgroundQuery"].(map[string]any)
	if !ok {
		t.Fatalf("details.backgroundQuery = %v, want an object", e.Details["backgroundQuery"])
	}
	if bq["list"] != "jobs" {
		t.Errorf("details.backgroundQuery.list = %v, want jobs", bq["list"])
	}
	if src.fetchCalls != 0 {
		t.Errorf("Fetch calls = %d, want 0 when Pos >= queryCap", src.fetchCalls)
	}
}

func TestRunList_CountTimeoutReturnsItemsWithoutTotal(t *testing.T) {
	defer ltGuard(t)
	spec := ltSpec()
	spec.Timeout = 50 * time.Millisecond
	src := &ltSource{rows: ltRows(25), countSleep: true}
	want := ltExpect(src.rows, ltDefaultSort())

	rec := ltRun(spec, src, "limit=10")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	body := ltDecodeList(t, rec)
	if got := ltIDs(body.Items); !slices.Equal(got, want[:10]) {
		t.Errorf("items = %v, want the first page %v", got, want[:10])
	}
	pm := ltPageMap(t, rec)
	if v, ok := pm["total"]; !ok || v != nil {
		t.Errorf("page.total = %v (present=%v), want null", v, ok)
	}
	if !body.Page.TimedOut {
		t.Errorf("timedOut = false, want true")
	}
	if body.Page.BackgroundQuery == nil {
		t.Errorf("backgroundQuery absent, want it because timedOut")
	}
	if body.Page.NextCursor == "" {
		t.Errorf("nextCursor absent, want it (25 rows, 10 returned)")
	}
	if body.Page.Capped {
		t.Errorf("capped = true, want false (not reached queryCap)")
	}
	if src.countCalls != 1 {
		t.Errorf("Count calls = %d, want 1", src.countCalls)
	}
}

func TestRunList_FetchTimeoutKeepsReadRows(t *testing.T) {
	defer ltGuard(t)
	cases := []struct {
		name       string
		readN      int
		wantCursor bool
	}{
		{"rows read before the timeout are returned", 3, true},
		{"timeout before any row gives an empty page and no cursor", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer ltGuard(t)
			spec := ltSpec()
			spec.Timeout = 50 * time.Millisecond
			src := &ltSource{rows: ltRows(25), fetchReadN: tc.readN, fetchSleep: true}
			want := ltExpect(src.rows, ltDefaultSort())

			rec := ltRun(spec, src, "limit=10")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
			}
			if tc.readN == 0 && !strings.Contains(rec.Body.String(), `"items":[]`) {
				t.Errorf("items is not an empty array: %s", rec.Body.String())
			}
			body := ltDecodeList(t, rec)
			if got := ltIDs(body.Items); !slices.Equal(got, want[:tc.readN]) {
				t.Errorf("items = %v, want %v", got, want[:tc.readN])
			}
			if !body.Page.TimedOut {
				t.Errorf("timedOut = false, want true")
			}
			// The page is read before the count; a fetch that ran out of time
			// leaves no budget to count, so the total is unknown.
			if body.Page.Total != nil {
				t.Errorf("total = %v, want null (the fetch used up the timeout before counting)", *body.Page.Total)
			}
			if tc.wantCursor {
				if body.Page.NextCursor == "" {
					t.Fatalf("nextCursor absent, want one after the last returned row")
				}
				q, e := ltParse(spec, "cursor="+url.QueryEscape(body.Page.NextCursor))
				if e != nil {
					t.Fatalf("parse of nextCursor failed: %+v", *e)
				}
				wantAfter := ltRowKey(ltFind(src.rows, want[tc.readN-1]), ltDefaultSort())
				if !slices.Equal(q.After, wantAfter) {
					t.Errorf("cursor After = %v, want key of the last returned row %v", q.After, wantAfter)
				}
				if q.Pos != tc.readN {
					t.Errorf("cursor Pos = %d, want %d", q.Pos, tc.readN)
				}
			} else if body.Page.NextCursor != "" {
				t.Errorf("nextCursor = %q, want none when no row came back", body.Page.NextCursor)
			}
		})
	}
}

func TestRunList_CappedWhenCountUnknown(t *testing.T) {
	defer ltGuard(t)
	spec := ltSpec()
	spec.QueryCap = 5
	spec.Timeout = 50 * time.Millisecond
	src := &ltSource{rows: ltRows(45), countSleep: true}
	want := ltExpect(src.rows, ltDefaultSort())

	rec := ltRun(spec, src, "limit=10")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	body := ltDecodeList(t, rec)
	if !body.Page.Capped {
		t.Errorf("capped = false, want true (total unknown, more rows after reaching queryCap 5)")
	}
	if body.Page.Total != nil {
		t.Errorf("total = %d, want null", *body.Page.Total)
	}
	if got := ltIDs(body.Items); !slices.Equal(got, want[:5]) {
		t.Errorf("items = %v, want %v", got, want[:5])
	}
	if body.Page.NextCursor != "" {
		t.Errorf("nextCursor = %q, want none at queryCap", body.Page.NextCursor)
	}
	if body.Page.BackgroundQuery == nil {
		t.Errorf("backgroundQuery absent, want it")
	}
	if !slices.Equal(src.fetchNs, []int{6}) {
		t.Errorf("Fetch n = %v, want [6] (min(10, 5)+1)", src.fetchNs)
	}
}

func TestRunList_BackgroundQueryEchoesSortAndFilters(t *testing.T) {
	defer ltGuard(t)
	spec := ltSpec()
	spec.QueryCap = 5
	src := &ltSource{rows: ltRows(45)}
	var builds []ltRow
	for _, r := range src.rows {
		if r.Kind == "build" {
			builds = append(builds, r)
		}
	}
	want := ltExpect(builds, ltDefaultSort())

	rec := ltRun(spec, src, "kind=build&limit=10")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	body := ltDecodeList(t, rec)
	if body.Page.Total == nil || *body.Page.Total != 15 {
		t.Errorf("total = %v, want 15 builds", body.Page.Total)
	}
	if !body.Page.Capped {
		t.Errorf("capped = false, want true (15 builds > queryCap 5)")
	}
	if got := ltIDs(body.Items); !slices.Equal(got, want[:5]) {
		t.Errorf("items = %v, want %v", got, want[:5])
	}
	bq := body.Page.BackgroundQuery
	if bq == nil {
		t.Fatalf("backgroundQuery absent")
	}
	if bq.List != "jobs" || bq.Sort != "-createdAt,-id" {
		t.Errorf("backgroundQuery list/sort = %q/%q, want jobs/-createdAt,-id", bq.List, bq.Sort)
	}
	if !ltFiltersEq(bq.Filters, map[string][]string{"kind": {"build"}}) {
		t.Errorf("backgroundQuery.filters = %#v, want kind=[build]", bq.Filters)
	}
}

func TestRunList_NotCappedAtExactQueryCap(t *testing.T) {
	defer ltGuard(t)
	spec := ltSpec()
	spec.QueryCap = 30
	src := &ltSource{rows: ltRows(30)}

	recs := ltWalk(t, spec, src, "limit=10", 3)
	last := ltDecodeList(t, recs[2])
	if last.Page.Total == nil || *last.Page.Total != 30 {
		t.Errorf("total = %v, want 30", last.Page.Total)
	}
	if last.Page.Capped {
		t.Errorf("capped = true, want false (total 30 is not > queryCap 30)")
	}
	if last.Page.BackgroundQuery != nil {
		t.Errorf("backgroundQuery present, want absent on a page that is neither capped nor timed out")
	}
	if last.Page.NextCursor != "" {
		t.Errorf("nextCursor = %q, want none", last.Page.NextCursor)
	}
}

func TestRunList_NextCursorOnlyWhenMoreRows(t *testing.T) {
	defer ltGuard(t)
	cases := []struct {
		name       string
		rows       int
		wantCursor bool
	}{
		{"exactly two full pages, no more rows", 20, false},
		{"one row more than two pages", 21, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer ltGuard(t)
			spec := ltSpec()
			src := &ltSource{rows: ltRows(tc.rows)}
			recs := ltWalk(t, spec, src, "limit=10", 2)
			second := ltDecodeList(t, recs[1])
			if len(second.Items) != 10 {
				t.Errorf("page 2: %d items, want 10", len(second.Items))
			}
			if got := second.Page.NextCursor != ""; got != tc.wantCursor {
				t.Errorf("page 2 has nextCursor = %v, want %v", got, tc.wantCursor)
			}
		})
	}
}

func TestRunList_ErrorsAreAnswered(t *testing.T) {
	defer ltGuard(t)
	other := ltSpec()
	other.Name = "users"
	otherCur := ltCursor(other, ltDefaultSort(), nil, []string{"2026-03-01T09:00:00Z", "job-01"}, 1, nil)
	cases := []struct {
		name      string
		src       *ltSource
		raw       string
		status    int
		code      string
		field     string
		wantCount int
		wantFetch int
	}{
		{"limit out of range is a parse error and stops before Count",
			&ltSource{rows: ltRows(25)}, "limit=0", http.StatusBadRequest, CodeValidationFailed, "limit", 0, 0},
		{"undeclared dryRun is a parse error",
			&ltSource{rows: ltRows(25)}, "dryRun=true", http.StatusBadRequest, CodeValidationFailed, "dryRun", 0, 0},
		{"cursor of another list is a parse error",
			&ltSource{rows: ltRows(25)}, "cursor=" + url.QueryEscape(otherCur), http.StatusBadRequest, CodeValidationFailed, "cursor", 0, 0},
		{"non-timeout Count error is INTERNAL",
			&ltSource{rows: ltRows(25), countErr: errors.New("db down")}, "limit=10", http.StatusInternalServerError, CodeInternal, "", 1, 1},
		{"non-timeout Fetch error is INTERNAL",
			&ltSource{rows: ltRows(25), fetchErr: errors.New("db down")}, "limit=10", http.StatusInternalServerError, CodeInternal, "", 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer ltGuard(t)
			rec := ltRun(ltSpec(), tc.src, tc.raw)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tc.status, rec.Body.String())
			}
			e := ltDecodeErr(t, rec)
			if e.Code != tc.code {
				t.Errorf("code = %q, want %q", e.Code, tc.code)
			}
			if tc.field != "" {
				ltWantParseErr(t, &e, tc.field)
			}
			if tc.src.countCalls != tc.wantCount {
				t.Errorf("Count calls = %d, want %d", tc.src.countCalls, tc.wantCount)
			}
			if tc.src.fetchCalls != tc.wantFetch {
				t.Errorf("Fetch calls = %d, want %d", tc.src.fetchCalls, tc.wantFetch)
			}
		})
	}
}

func TestRunList_EffectiveValuesAreEchoed(t *testing.T) {
	defer ltGuard(t)
	// Zero numeric fields take the defaults: DefaultLimit 50, QueryCap 10000,
	// Timeout 15s.
	spec := ListSpec{
		Name:        "jobs",
		Sorts:       []string{"createdAt"},
		DefaultSort: []SortKey{{Field: "createdAt", Desc: true}},
		TieBreaker:  "id",
	}
	src := &ltSource{rows: ltRows(3)}
	rec := ltRun(spec, src, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	body := ltDecodeList(t, rec)
	if body.Page.Limit != DefaultPageLimit || body.Page.QueryCap != DefaultQueryCap || body.Page.TimeoutSeconds != 15 {
		t.Errorf("limit/queryCap/timeoutSeconds = %d/%d/%d, want %d/%d/15",
			body.Page.Limit, body.Page.QueryCap, body.Page.TimeoutSeconds, DefaultPageLimit, DefaultQueryCap)
	}
	if body.Page.Sort != "-createdAt,-id" {
		t.Errorf("sort = %q, want -createdAt,-id", body.Page.Sort)
	}
	if len(body.Items) != 3 {
		t.Errorf("items = %d, want 3", len(body.Items))
	}
}

func TestRunList_SortRendersWithTieBreaker(t *testing.T) {
	defer ltGuard(t)
	cases := []struct {
		name     string
		raw      string
		wantSort string
		keys     []SortKey
	}{
		{"asc single key", "sort=kind", "kind,id",
			[]SortKey{{Field: "kind"}, {Field: "id"}}},
		{"desc then asc", "sort=-createdAt,kind", "-createdAt,kind,id",
			[]SortKey{{Field: "createdAt", Desc: true}, {Field: "kind"}, {Field: "id"}}},
		{"explicit tie breaker last", "sort=kind,id", "kind,id",
			[]SortKey{{Field: "kind"}, {Field: "id"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer ltGuard(t)
			src := &ltSource{rows: ltRows(25)}
			rec := ltRun(ltSpec(), src, tc.raw)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
			}
			body := ltDecodeList(t, rec)
			if body.Page.Sort != tc.wantSort {
				t.Errorf("sort = %q, want %q", body.Page.Sort, tc.wantSort)
			}
			if got, want := ltIDs(body.Items), ltExpect(src.rows, tc.keys); !slices.Equal(got, want) {
				t.Errorf("items = %v, want %v", got, want)
			}
		})
	}
}

func TestRunList_EmptyResultIsArrayAndOK(t *testing.T) {
	defer ltGuard(t)
	src := &ltSource{}
	rec := ltRun(ltSpec(), src, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("items is not []: %s", rec.Body.String())
	}
	body := ltDecodeList(t, rec)
	if body.Page.Total == nil || *body.Page.Total != 0 {
		t.Errorf("total = %v, want 0", body.Page.Total)
	}
	if body.Page.NextCursor != "" || body.Page.Capped || body.Page.TimedOut {
		t.Errorf("nextCursor/capped/timedOut = %q/%v/%v, want empty/false/false",
			body.Page.NextCursor, body.Page.Capped, body.Page.TimedOut)
	}
}
