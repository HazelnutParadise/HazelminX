package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/HazelnutParadise/hazelminx/core/internal/paging"
)

func TestPreviewDoesNotRecordOrLock(t *testing.T) {
	release := make(chan struct{})
	h := &fakeHandler{kind: "k", steps: []StepSpec{{Name: "a"}}, execute: func(ctx context.Context, _ *Job, _ *Step, _ Reporter) error {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	}}
	env := newEnv(t, Options{}, h)
	defer close(release)
	held := env.submit(SubmitRequest{RequestID: "held", Kind: "k", Resources: []ResourceRef{{Kind: "database", ID: "d"}}})
	env.waitState(held.ID, StateRunning)

	req := SubmitRequest{RequestID: "new", Kind: "k", Resources: []ResourceRef{{Kind: "database", ID: "d"}}}
	p, err := env.engine.Preview(env.ctx, req)
	if err != nil || p.Replay != nil || len(p.QueuedBehind) != 1 || p.QueuedBehind[0] != held.ID {
		t.Fatalf("preview: %+v %v", p, err)
	}
	if n, _ := env.engine.Count(env.ctx, ListOptions{}); n != 1 {
		t.Fatalf("preview created a job: %d", n)
	}
	// Previewing twice still reserves nothing, and the request id is free.
	if _, err := env.engine.Preview(env.ctx, req); err != nil {
		t.Fatal(err)
	}
	j, dedup, err := env.engine.Submit(env.ctx, req)
	if err != nil || dedup {
		t.Fatalf("submit after preview: %v %v", dedup, err)
	}
	if p, _ := env.engine.Preview(env.ctx, req); p.Replay == nil || p.Replay.ID != j.ID {
		t.Fatalf("replay preview: %+v", p)
	}
	other := req
	other.Params = []byte(`{"x":1}`)
	if _, err := env.engine.Preview(env.ctx, other); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed content: %v", err)
	}
	if _, err := env.engine.Preview(env.ctx, SubmitRequest{RequestID: "u", Kind: "nope"}); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("unknown kind: %v", err)
	}
	if _, err := env.engine.Preview(env.ctx, SubmitRequest{Kind: "k"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing request id: %v", err)
	}
	free, _ := env.engine.Preview(env.ctx, SubmitRequest{RequestID: "f", Kind: "k", Resources: []ResourceRef{{Kind: "database", ID: "other"}}})
	if len(free.QueuedBehind) != 0 {
		t.Fatalf("unrelated resource: %+v", free)
	}
}

func TestResourcesOverlapUsesLockIdentity(t *testing.T) {
	disk := ResourceRef{Kind: "disk", Device: "sda", Node: "n1"}
	path := ResourceRef{Kind: "path", Device: "sda", Mount: "/data", Path: "/data/x", Node: "n1"}
	if !ResourcesOverlap([]ResourceRef{disk}, []ResourceRef{path}) {
		t.Fatal("a disk operation overlaps a path on that disk")
	}
	if ResourcesOverlap([]ResourceRef{{Kind: "database", ID: "a", Node: "n1"}}, []ResourceRef{{Kind: "database", ID: "a", Node: "n2"}}) {
		t.Fatal("resources on different nodes never overlap")
	}
	if ResourcesOverlap(nil, []ResourceRef{disk}) {
		t.Fatal("nothing overlaps nothing")
	}
}

func TestListKeysetAndCount(t *testing.T) {
	env := newEnv(t, Options{}, &fakeHandler{kind: "k", steps: []StepSpec{{Name: "a"}}})
	for i := 0; i < 5; i++ {
		env.submit(SubmitRequest{RequestID: string(rune('a' + i)), Kind: "k", Node: map[bool]string{true: "n1", false: "n2"}[i%2 == 0]})
	}
	if n, _ := env.engine.Count(env.ctx, ListOptions{Node: "n1"}); n != 3 {
		t.Fatalf("count by node: %d", n)
	}
	q := ListOptions{Page: pageOf(2, nil)}
	var ids []string
	for {
		page, err := env.engine.List(env.ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		for _, j := range page {
			ids = append(ids, j.ID)
		}
		if len(page) < 2 {
			break
		}
		last := page[len(page)-1]
		q.Page = pageOf(2, []string{last.SortValue("createdAt"), last.SortValue("id")})
	}
	all, _ := env.engine.List(env.ctx, ListOptions{})
	if len(ids) != 5 || len(all) != 5 {
		t.Fatalf("walked %v, all %d", ids, len(all))
	}
	for i := range all {
		if all[i].ID != ids[i] {
			t.Fatalf("keyset order differs from the default order at %d", i)
		}
	}
}

func pageOf(n int, after []string) paging.Query {
	return paging.Query{Limit: n, After: after, Sort: []paging.SortKey{{Field: "createdAt", Desc: true}, {Field: "id", Desc: true}}}
}
