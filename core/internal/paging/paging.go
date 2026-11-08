// Package paging is the keyset half of the management list contract
// (ticket 010): stores turn a Query into ORDER BY and a "rows after this
// key" condition, so pages never shift when rows are inserted in front.
package paging

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// SortKey is one sort field.
type SortKey struct {
	Field string
	Desc  bool
}

// Query is one page request. Sort ends with a unique tie-breaker field;
// After is nil for the first page, otherwise the sort values of the last row
// already returned, aligned with Sort.
type Query struct {
	Limit int
	Sort  []SortKey
	After []string
}

// Column maps a sort field to its SQL expression.
type Column struct {
	Expr string
	// Int compares After values as integers (seq); otherwise as text.
	Int bool
}

// ErrBadQuery reports a Sort field without a column or an After that does
// not fit Sort. The API validates queries first, so this is a programming
// error or a forged cursor.
var ErrBadQuery = errors.New("paging: bad query")

// SQL returns the ORDER BY clause (without the keyword) and, when After is
// set, the keyset condition with its arguments. For keys k1..kn the
// condition is (k1 > v1) OR (k1 = v1 AND k2 > v2) OR …, with < for
// descending keys.
func (q Query) SQL(cols map[string]Column) (orderBy, where string, args []any, err error) {
	if len(q.Sort) == 0 {
		return "", "", nil, fmt.Errorf("%w: empty sort", ErrBadQuery)
	}
	if q.After != nil && len(q.After) != len(q.Sort) {
		return "", "", nil, fmt.Errorf("%w: cursor has %d values for %d sort keys", ErrBadQuery, len(q.After), len(q.Sort))
	}
	var order []string
	vals := make([]any, len(q.Sort))
	for i, k := range q.Sort {
		col, ok := cols[k.Field]
		if !ok {
			return "", "", nil, fmt.Errorf("%w: unknown sort field %q", ErrBadQuery, k.Field)
		}
		dir := "ASC"
		if k.Desc {
			dir = "DESC"
		}
		order = append(order, col.Expr+" "+dir)
		if q.After != nil {
			if col.Int {
				n, err := strconv.ParseInt(q.After[i], 10, 64)
				if err != nil {
					return "", "", nil, fmt.Errorf("%w: %s is not an integer", ErrBadQuery, k.Field)
				}
				vals[i] = n
			} else {
				vals[i] = q.After[i]
			}
		}
	}
	if q.After == nil {
		return strings.Join(order, ", "), "", nil, nil
	}
	var ors []string
	for i, k := range q.Sort {
		var ands []string
		for j := 0; j < i; j++ {
			ands = append(ands, cols[q.Sort[j].Field].Expr+" = ?")
			args = append(args, vals[j])
		}
		op := ">"
		if k.Desc {
			op = "<"
		}
		ands = append(ands, cols[k.Field].Expr+" "+op+" ?")
		args = append(args, vals[i])
		ors = append(ors, "("+strings.Join(ands, " AND ")+")")
	}
	return strings.Join(order, ", "), "(" + strings.Join(ors, " OR ") + ")", args, nil
}
