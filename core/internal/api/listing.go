package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HazelnutParadise/hazelminx/core/internal/paging"
)

// Lists (spec 36.1, addendum 03 機制六 / ROB-008). Every list endpoint
// answers
//
//	{"items": [...], "page": {limit, queryCap, timeoutSeconds, nextCursor,
//	 total, capped, timedOut, sort, backgroundQuery}}
//
// and pages with an opaque keyset cursor: the sort values of the last row
// returned, so rows inserted in front of the reader never shift a page.

// List defaults (机制六).
const (
	DefaultPageLimit = 50
	DefaultMaxLimit  = 200
	DefaultQueryCap  = 10000
	DefaultListTime  = 15 * time.Second
)

// SortKey is one sort field (shared with the stores).
type SortKey = paging.SortKey

// FilterSpec declares one filter query parameter.
type FilterSpec struct {
	Name    string
	Summary string
	// Multi accepts a comma-separated list ("state=queued,running").
	Multi bool
	// Enum, when set, is the only accepted values.
	Enum []string
	// Int requires a non-negative integer value.
	Int bool
}

// ListSpec declares a list endpoint. Zero numeric fields take the defaults
// above.
type ListSpec struct {
	Name         string // "jobs", "users", "audit-events"; also the backgroundQuery list name
	DefaultLimit int
	MaxLimit     int
	QueryCap     int
	Timeout      time.Duration
	// Sorts are the fields a client may sort by.
	Sorts []string
	// DefaultSort applies when the request has no sort.
	DefaultSort []SortKey
	// TieBreaker is a unique field appended to every sort (same direction as
	// the last key) so the keyset is total. It need not be in Sorts.
	TieBreaker string
	Filters    []FilterSpec
}

// ListQuery is a parsed, validated list request.
type ListQuery struct {
	// Limit is the page size the source must honour (already clamped).
	Limit int
	// Sort always ends with the tie breaker.
	Sort []SortKey
	// Filters holds only declared filters; Multi filters may have several
	// values, others exactly one.
	Filters map[string][]string
	// After is nil on the first page; otherwise the Key of the last row the
	// client received, aligned with Sort.
	After []string
	// Pos is how many rows the client already received before this page.
	Pos int
	// Total is the count taken on the first page, carried by the cursor;
	// nil when not counted yet or when the count timed out.
	Total *int
}

// ListSource reads one list for one principal. Implementations translate
// Sort, Filters and After into their query (for SQL: ORDER BY the keys and
// a row-value comparison against After).
type ListSource[T any] interface {
	// Count returns the number of rows matching q.Filters (After ignored).
	Count(ctx context.Context, q ListQuery) (int, error)
	// Fetch returns up to n rows strictly after q.After in q.Sort order.
	// When ctx ends while reading it returns the rows read so far together
	// with ctx.Err().
	Fetch(ctx context.Context, q ListQuery, n int) ([]T, error)
	// Key returns the values of the fields of sort for row, as strings.
	Key(row T, sort []SortKey) []string
}

// PageMeta is the "page" object.
type PageMeta struct {
	Limit          int    `json:"limit"`
	QueryCap       int    `json:"queryCap"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	NextCursor     string `json:"nextCursor,omitempty"`
	// Total is null when counting timed out ("總數未知").
	Total *int `json:"total"`
	// Capped: more rows match than queryCap; only the first queryCap can be
	// browsed ("可瀏覽前 N 筆／總數 M").
	Capped   bool   `json:"capped"`
	TimedOut bool   `json:"timedOut"`
	Sort     string `json:"sort"`
	// BackgroundQuery is present when Capped or TimedOut: the body to POST
	// to /management/v1/list-queries to run the whole query as a job.
	BackgroundQuery *BackgroundQuery `json:"backgroundQuery,omitempty"`
}

// BackgroundQuery is the request body of POST /management/v1/list-queries
// (minus requestId, which the client supplies).
type BackgroundQuery struct {
	List    string              `json:"list"`
	Sort    string              `json:"sort,omitempty"`
	Filters map[string][]string `json:"filters,omitempty"`
}

// ListResponse is the body of every list endpoint.
type ListResponse[T any] struct {
	Items []T      `json:"items"`
	Page  PageMeta `json:"page"`
}

// parseListQuery validates the query string of a list request against spec.
//
// Accepted parameters: limit, cursor, sort and the declared filters.
// Expected results:
//   - no parameters → Limit = DefaultLimit, Sort = DefaultSort + tie
//     breaker, Filters empty map, After nil, Pos 0, Total nil.
//   - limit: integer 1..MaxLimit; "0", "-1", "abc", "201" (when MaxLimit
//     200) → VALIDATION_FAILED with Fields[0].Field "limit".
//   - sort: comma-separated fields, "-" prefix = descending
//     ("-createdAt,kind"). A field not in Sorts, an empty element or a
//     repeated field → VALIDATION_FAILED field "sort". The tie breaker is
//     appended with the direction of the last key unless already the last
//     key; the tie breaker given explicitly in the middle is an error.
//   - filters: unknown values for an Enum filter, a non-integer for an Int
//     filter, several values for a non-Multi filter (comma or repeated
//     parameter) → VALIDATION_FAILED with Fields naming the filter. Multi
//     values are split on commas, trimmed, empty elements dropped, order
//     kept, duplicates removed.
//   - an undeclared parameter (e.g. "foo=1", or "afterSeq" on a list that
//     does not declare it) → VALIDATION_FAILED, one FieldError per unknown
//     name, sorted. Nothing is silently ignored. "dryRun" is undeclared too.
//   - several problems at once → one VALIDATION_FAILED listing all fields.
//   - cursor: produced by encodeCursor for the same spec.Name. It carries
//     After, Pos, Total, Sort and Filters, so cursor alone continues the
//     query. If sort or filters are also given they must equal the
//     cursor's (after normalisation) or VALIDATION_FAILED field "cursor".
//     A cursor that is not valid base64url JSON, was made for another list,
//     or whose After length differs from its Sort → VALIDATION_FAILED field
//     "cursor". limit may differ from the first page.
//   - the error's Message is plain language; Code is VALIDATION_FAILED.
func parseListQuery(c *gin.Context, spec ListSpec) (ListQuery, *apiError) {
	spec = withListDefaults(spec)
	vals := c.Request.URL.Query()
	q := ListQuery{Limit: spec.DefaultLimit, Filters: map[string][]string{}}
	var fields []FieldError
	bad := func(field, msg string) { fields = append(fields, FieldError{Field: field, Message: msg}) }

	declared := map[string]FilterSpec{}
	for _, f := range spec.Filters {
		declared[f.Name] = f
	}
	var unknown []string
	for k := range vals {
		if k == "limit" || k == "cursor" || k == "sort" {
			continue
		}
		if _, ok := declared[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	for _, k := range unknown {
		bad(k, "not a parameter of this list")
	}

	if raw, ok := vals["limit"]; ok {
		n, err := strconv.Atoi(raw[len(raw)-1])
		if err != nil || n < 1 || n > spec.MaxLimit || len(raw) > 1 {
			bad("limit", fmt.Sprintf("an integer from 1 to %d", spec.MaxLimit))
		} else {
			q.Limit = n
		}
	}

	var sortGiven bool
	if raw, ok := vals["sort"]; ok {
		sortGiven = true
		keys, msg := parseSort(spec, strings.Join(raw, ","))
		if msg != "" {
			bad("sort", msg)
		} else {
			q.Sort = keys
		}
	} else {
		q.Sort = withTieBreaker(spec, spec.DefaultSort)
	}

	filtersGiven := false
	for _, f := range spec.Filters {
		raw, ok := vals[f.Name]
		if !ok {
			continue
		}
		filtersGiven = true
		var values []string
		for _, r := range raw {
			for _, v := range strings.Split(r, ",") {
				if v = strings.TrimSpace(v); v != "" && !slices.Contains(values, v) {
					values = append(values, v)
				}
			}
		}
		switch {
		case len(values) == 0:
			bad(f.Name, "empty value")
			continue
		case !f.Multi && (len(values) > 1 || len(raw) > 1):
			bad(f.Name, "takes one value")
			continue
		}
		ok = true
		for _, v := range values {
			if len(f.Enum) > 0 && !slices.Contains(f.Enum, v) {
				bad(f.Name, "one of "+strings.Join(f.Enum, ", "))
				ok = false
				break
			}
			if f.Int {
				if n, err := strconv.ParseInt(v, 10, 64); err != nil || n < 0 {
					bad(f.Name, "a non-negative integer")
					ok = false
					break
				}
			}
		}
		if ok {
			q.Filters[f.Name] = values
		}
	}

	if raw, ok := vals["cursor"]; ok && len(fields) == 0 {
		cur, err := decodeCursor(spec, raw[len(raw)-1])
		switch {
		case err != nil:
			bad("cursor", "not a cursor of this list; start again without it")
		case sortGiven && renderSort(cur.Sort) != renderSort(q.Sort):
			bad("cursor", "the cursor belongs to another sort; send the same sort or none")
		case filtersGiven && !sameFilters(cur.Filters, q.Filters):
			bad("cursor", "the cursor belongs to other filters; send the same filters or none")
		default:
			q.Sort, q.Filters, q.After, q.Pos, q.Total = cur.Sort, cur.Filters, cur.After, cur.Pos, cur.Total
			if q.Filters == nil {
				q.Filters = map[string][]string{}
			}
		}
	}

	if len(fields) > 0 {
		names := make([]string, len(fields))
		for i, f := range fields {
			names[i] = f.Field
		}
		e := newError(CodeValidationFailed, "invalid list parameters: "+strings.Join(names, ", "))
		e.Fields = fields
		return ListQuery{}, &e
	}
	return q, nil
}

// parseSort reads "-a,b" against spec; the message is empty when valid.
func parseSort(spec ListSpec, raw string) ([]SortKey, string) {
	var keys []SortKey
	seen := map[string]bool{}
	parts := strings.Split(raw, ",")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		k := SortKey{Field: strings.TrimPrefix(p, "-"), Desc: strings.HasPrefix(p, "-")}
		switch {
		case k.Field == "":
			return nil, "empty sort field"
		case seen[k.Field]:
			return nil, "field " + k.Field + " is repeated"
		case k.Field == spec.TieBreaker && i != len(parts)-1:
			return nil, spec.TieBreaker + " can only be the last sort field"
		case k.Field != spec.TieBreaker && !slices.Contains(spec.Sorts, k.Field):
			return nil, "sortable fields: " + strings.Join(spec.Sorts, ", ")
		}
		seen[k.Field] = true
		keys = append(keys, k)
	}
	return withTieBreaker(spec, keys), ""
}

// withTieBreaker appends the tie breaker with the direction of the last key
// unless it already ends the sort.
func withTieBreaker(spec ListSpec, keys []SortKey) []SortKey {
	out := append([]SortKey(nil), keys...)
	if len(out) > 0 && out[len(out)-1].Field == spec.TieBreaker {
		return out
	}
	desc := len(out) > 0 && out[len(out)-1].Desc
	return append(out, SortKey{Field: spec.TieBreaker, Desc: desc})
}

func sameFilters(a, b map[string][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !slices.Equal(v, b[k]) {
			return false
		}
	}
	return true
}

// cursorState is what a cursor carries.
type cursorState struct {
	List    string              `json:"l"`
	Sort    []SortKey           `json:"s"`
	Filters map[string][]string `json:"f,omitempty"`
	After   []string            `json:"a"`
	Pos     int                 `json:"p"`
	Total   *int                `json:"t,omitempty"`
}

// cursorKey signs cursors so a client cannot move its position past the
// query cap or forge the total. It changes on every start of Core, which
// only makes old cursors start over.
var cursorKey = func() []byte {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		panic(err)
	}
	return k
}()

const cursorMACLen = 16

func cursorMAC(payload []byte) []byte {
	m := hmac.New(sha256.New, cursorKey)
	m.Write(payload)
	return m.Sum(nil)[:cursorMACLen]
}

func decodeCursor(spec ListSpec, raw string) (cursorState, error) {
	var cur cursorState
	signed, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return cur, err
	}
	if len(signed) <= cursorMACLen {
		return cur, errors.New("cursor too short")
	}
	b, mac := signed[:len(signed)-cursorMACLen], signed[len(signed)-cursorMACLen:]
	if !hmac.Equal(mac, cursorMAC(b)) {
		return cur, errors.New("cursor signature does not match")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cur); err != nil {
		return cur, err
	}
	if cur.List != spec.Name || len(cur.Sort) == 0 || len(cur.After) != len(cur.Sort) || cur.Pos < 0 {
		return cur, errors.New("cursor does not fit this list")
	}
	for _, k := range cur.Sort {
		if k.Field != spec.TieBreaker && !slices.Contains(spec.Sorts, k.Field) {
			return cur, errors.New("cursor sorts by an unknown field")
		}
	}
	return cur, nil
}

// encodeCursor makes the opaque cursor for continuing q after the row whose
// key is after, having returned pos rows in total.
//
// Expected results:
//   - base64url without padding; decodes back (through parseListQuery) to
//     the same Sort, Filters, After, Pos and Total.
//   - a cursor for spec A is rejected by spec B.
func encodeCursor(spec ListSpec, q ListQuery, after []string, pos int) string {
	var f map[string][]string
	if len(q.Filters) > 0 {
		f = q.Filters
	}
	b, _ := json.Marshal(cursorState{List: spec.Name, Sort: q.Sort, Filters: f, After: after, Pos: pos, Total: q.Total})
	return base64.RawURLEncoding.EncodeToString(append(b, cursorMAC(b)...))
}

// runList answers a list request: parse, count, fetch, build PageMeta.
//
// Expected results:
//   - on a parse error it responds with that error and returns.
//   - the whole request (count + fetch) runs under spec.Timeout.
//   - the page is fetched first, then counted, so a slow count still
//     returns the rows already read.
//   - first page (Total nil in the query): Count once after the fetch. A
//     count that ends because of the timeout leaves Total nil and sets
//     TimedOut; any other count error → 500 INTERNAL. A fetch that timed
//     out leaves no budget, so Count is skipped and Total stays nil. Later
//     pages reuse the cursor's Total and do not call Count; a cursor whose
//     Total is nil (the first count timed out) counts again.
//   - Pos >= QueryCap → 400 QUERY_LIMIT_EXCEEDED, SuggestedActions
//     ["run-as-background-query"], Details {"queryCap": cap,
//     "backgroundQuery": {...}}; Fetch is not called.
//   - fetch n = min(Limit, QueryCap-Pos) + 1 rows to know whether more
//     exist; return at most n-1. A fetch error that is the timeout returns
//     the partial rows with TimedOut true (and a NextCursor after the last
//     returned row if any rows came back and Pos+returned < QueryCap); any
//     other error → 500 INTERNAL.
//   - NextCursor is set when more rows exist and Pos+returned < QueryCap.
//   - Capped is true when Total > QueryCap, or when Total is unknown and
//     more rows exist after reaching QueryCap.
//   - BackgroundQuery is set iff Capped or TimedOut, with List =
//     spec.Name, Sort = PageMeta.Sort, Filters = q.Filters (nil if empty).
//   - PageMeta.Sort renders the effective q.Sort, tie breaker included, in
//     the same syntax the sort parameter accepts ("-createdAt,-id").
//   - Limit, QueryCap and TimeoutSeconds echo the effective values.
//   - Items is [] (not null) when empty. Status 200.
func runList[T any](c *gin.Context, spec ListSpec, src ListSource[T]) {
	spec = withListDefaults(spec)
	q, perr := parseListQuery(c, spec)
	if perr != nil {
		respond(c, *perr)
		return
	}
	var filters map[string][]string
	if len(q.Filters) > 0 {
		filters = q.Filters
	}
	bq := &BackgroundQuery{List: spec.Name, Sort: renderSort(q.Sort), Filters: filters}
	if q.Pos >= spec.QueryCap {
		e := newError(CodeQueryLimitExceeded, fmt.Sprintf("only the first %d rows of this list can be browsed; run it as a background query for the rest", spec.QueryCap))
		e.SuggestedActions = []string{"run-as-background-query"}
		e.Details = map[string]any{"queryCap": spec.QueryCap, "backgroundQuery": bq}
		respond(c, e)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), spec.Timeout)
	defer cancel()
	meta := PageMeta{Limit: q.Limit, QueryCap: spec.QueryCap, TimeoutSeconds: int(spec.Timeout.Seconds()), Sort: renderSort(q.Sort), Total: q.Total}
	if meta.TimeoutSeconds == 0 {
		meta.TimeoutSeconds = 1
	}
	// The page comes first so a slow count still leaves the rows already
	// read ("總數未知" keeps the part obtained).
	room := min(q.Limit, spec.QueryCap-q.Pos)
	rows, err := src.Fetch(ctx, q, room+1)
	fetchTimedOut := false
	if errors.Is(err, paging.ErrBadQuery) {
		e := newError(CodeValidationFailed, "the cursor does not fit this list; start again without it")
		e.Fields = []FieldError{{Field: "cursor", Message: "not a cursor of this list"}}
		respond(c, e)
		return
	}
	if err != nil {
		if ctx.Err() == nil {
			respond(c, newError(CodeInternal, "internal error"))
			return
		}
		fetchTimedOut, meta.TimedOut = true, true
	}
	if q.Total == nil && !fetchTimedOut {
		n, err := src.Count(ctx, q)
		switch {
		case err == nil:
			meta.Total, q.Total = &n, &n
		case ctx.Err() != nil:
			meta.TimedOut = true
		default:
			respond(c, newError(CodeInternal, "internal error"))
			return
		}
	}
	more := len(rows) > room
	if more {
		rows = rows[:room]
	}
	end := q.Pos + len(rows)
	if len(rows) > 0 && end < spec.QueryCap && (more || fetchTimedOut) {
		meta.NextCursor = encodeCursor(spec, q, src.Key(rows[len(rows)-1], q.Sort), end)
	}
	if meta.Total != nil {
		meta.Capped = *meta.Total > spec.QueryCap
	} else {
		meta.Capped = more && end >= spec.QueryCap
	}
	if meta.Capped || meta.TimedOut {
		meta.BackgroundQuery = bq
	}
	if rows == nil {
		rows = []T{}
	}
	c.JSON(http.StatusOK, ListResponse[T]{Items: rows, Page: meta})
}

// paging converts the query to a store page of n rows.
func (q ListQuery) paging(n int) paging.Query {
	return paging.Query{Limit: n, Sort: q.Sort, After: q.After}
}

// sortValues renders the sort fields of a row with its SortValue method.
func sortValues(sort []SortKey, value func(field string) string) []string {
	out := make([]string, len(sort))
	for i, k := range sort {
		out[i] = value(k.Field)
	}
	return out
}

// withListDefaults fills the zero numeric fields of spec.
func withListDefaults(spec ListSpec) ListSpec {
	if spec.DefaultLimit <= 0 {
		spec.DefaultLimit = DefaultPageLimit
	}
	if spec.MaxLimit <= 0 {
		spec.MaxLimit = DefaultMaxLimit
	}
	if spec.QueryCap <= 0 {
		spec.QueryCap = DefaultQueryCap
	}
	if spec.Timeout <= 0 {
		spec.Timeout = DefaultListTime
	}
	return spec
}
