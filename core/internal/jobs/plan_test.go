package jobs

import (
	"errors"
	"slices"
	"testing"
)

// A job created from a change plan carries who approved it and which plan
// and digest it runs (spec 33.3 批准者、計畫版本與摘要), and the plan is part
// of the request identity.
func TestSubmitCarriesChangePlan(t *testing.T) {
	env := newEnv(t, Options{}, &fakeHandler{kind: "k", steps: []StepSpec{{Name: "a"}}})
	req := SubmitRequest{RequestID: "r1", Kind: "k", Requester: "usr_a", Approver: "usr_b",
		ChangePlanID: "plan_1", ChangePlanDigest: "sha256:aa", RequiredPermissions: []string{"projects:write"}}
	j, _, err := env.engine.Submit(env.ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := env.engine.Get(env.ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Approver != "usr_b" || got.ChangePlanID != "plan_1" || got.ChangePlanDigest != "sha256:aa" || !slices.Equal(got.RequiredPermissions, []string{"projects:write"}) {
		t.Fatalf("job %+v", got)
	}
	other := req
	other.ChangePlanDigest = "sha256:bb"
	if _, _, err := env.engine.Submit(env.ctx, other); !errors.Is(err, ErrConflict) {
		t.Fatalf("same request id, another plan digest: %v", err)
	}
	if _, dedup, err := env.engine.Submit(env.ctx, req); err != nil || !dedup {
		t.Fatalf("replay: %v %v", dedup, err)
	}
}

// Requests without a plan keep the request hash they had before plans
// existed, so stored jobs still replay.
func TestRequestHashWithoutPlanIsUnchanged(t *testing.T) {
	h, err := requestHash(SubmitRequest{Kind: "k", Params: []byte(`{"a":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	// sha256 of {"kind":"k","params":{"a":1},"resources":null,"node":"","requiredBytes":0},
	// the hash before change plans were added.
	const before = "44a81f8ef3e59df121245cd636af300098b2133e262fef6d278fddaf88f28d1a"
	if h != before {
		t.Fatalf("hash of a request without a plan changed: %s", h)
	}
}
