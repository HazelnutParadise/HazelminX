package api

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
)

// wgFailOnPanic runs as a deferred call in each subtest and turns a panic
// (the TODO stubs) into a failure of that one case.
func wgFailOnPanic(t *testing.T) {
	t.Helper()
	if r := recover(); r != nil {
		t.Fatalf("panic: %v", r)
	}
}

// wgSorted returns a sorted copy; nil stays nil so empty sets compare equal.
func wgSorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// wgStringDetails turns Details into plain strings, so a Controller value and
// a string with the same text compare equal.
func wgStringDetails(m map[string]any) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = fmt.Sprint(v)
	}
	return out
}

// wgReadOnly is the Details expected on RESOURCE_READ_ONLY.
func wgReadOnly(c Controller, t Takeover) map[string]map[string]string {
	return map[string]map[string]string{
		CodeResourceReadOnly: {"controller": string(c), "takeover": string(t)},
	}
}

type wbCase struct {
	name        string
	state       ResourceState
	wantCodes   []string // nil means writable
	wantDetails map[string]map[string]string
}

const wbRID = "res-1"

func wbCases() []wbCase {
	cases := []wbCase{
		// Writable combinations.
		{name: "zero value is writable", state: ResourceState{ResourceID: wbRID}},
		{name: "hazelminx not-applicable is writable", state: ResourceState{ResourceID: wbRID, Controller: ControllerHazelminX, Takeover: TakeoverNotApplicable}},
		{name: "os not-applicable is writable", state: ResourceState{ResourceID: wbRID, Controller: ControllerOS, Takeover: TakeoverNotApplicable}},
		{name: "hazelminx taken-over is writable", state: ResourceState{ResourceID: wbRID, Controller: ControllerHazelminX, Takeover: TakeoverTakenOver}},

		// Observing from the table.
		{name: "external observing is read-only", state: ResourceState{ResourceID: wbRID, Controller: ControllerExternal, Takeover: TakeoverObserving},
			wantCodes: []string{CodeResourceReadOnly}, wantDetails: wgReadOnly(ControllerExternal, TakeoverObserving)},
		{name: "cluster observing is read-only", state: ResourceState{ResourceID: wbRID, Controller: ControllerCluster, Takeover: TakeoverObserving},
			wantCodes: []string{CodeResourceReadOnly}, wantDetails: wgReadOnly(ControllerCluster, TakeoverObserving)},
		{name: "unknown observing is read-only", state: ResourceState{ResourceID: wbRID, Controller: ControllerUnknown, Takeover: TakeoverObserving},
			wantCodes: []string{CodeResourceReadOnly}, wantDetails: wgReadOnly(ControllerUnknown, TakeoverObserving)},

		// Not listed in the table, so they fall under "any other combination".
		{name: "hazelminx observing falls to read-only", state: ResourceState{ResourceID: wbRID, Controller: ControllerHazelminX, Takeover: TakeoverObserving},
			wantCodes: []string{CodeResourceReadOnly}, wantDetails: wgReadOnly(ControllerHazelminX, TakeoverObserving)},
		{name: "os observing falls to read-only", state: ResourceState{ResourceID: wbRID, Controller: ControllerOS, Takeover: TakeoverObserving},
			wantCodes: []string{CodeResourceReadOnly}, wantDetails: wgReadOnly(ControllerOS, TakeoverObserving)},
		{name: "external not-applicable falls to read-only", state: ResourceState{ResourceID: wbRID, Controller: ControllerExternal, Takeover: TakeoverNotApplicable},
			wantCodes: []string{CodeResourceReadOnly}, wantDetails: wgReadOnly(ControllerExternal, TakeoverNotApplicable)},
		{name: "cluster taken-over falls to read-only", state: ResourceState{ResourceID: wbRID, Controller: ControllerCluster, Takeover: TakeoverTakenOver},
			wantCodes: []string{CodeResourceReadOnly}, wantDetails: wgReadOnly(ControllerCluster, TakeoverTakenOver)},
		{name: "unknown controller string fails closed", state: ResourceState{ResourceID: wbRID, Controller: "martian", Takeover: TakeoverNotApplicable},
			wantCodes: []string{CodeResourceReadOnly}, wantDetails: wgReadOnly("martian", TakeoverNotApplicable)},
		{name: "unknown takeover string fails closed", state: ResourceState{ResourceID: wbRID, Controller: ControllerHazelminX, Takeover: "bogus"},
			wantCodes: []string{CodeResourceReadOnly}, wantDetails: wgReadOnly(ControllerHazelminX, "bogus")},

		// Empty fields default to hazelminx / not-applicable in the details too.
		{name: "empty controller with observing defaults to hazelminx", state: ResourceState{ResourceID: wbRID, Takeover: TakeoverObserving},
			wantCodes: []string{CodeResourceReadOnly}, wantDetails: wgReadOnly(ControllerHazelminX, TakeoverObserving)},
		{name: "empty takeover with external defaults to not-applicable", state: ResourceState{ResourceID: wbRID, Controller: ControllerExternal},
			wantCodes: []string{CodeResourceReadOnly}, wantDetails: wgReadOnly(ControllerExternal, TakeoverNotApplicable)},

		// Conflict and degradation.
		{name: "controller conflict on a hazelminx resource yields only conflict", state: ResourceState{ResourceID: wbRID, Controller: ControllerHazelminX, Takeover: TakeoverNotApplicable, ControllerConflict: true},
			wantCodes: []string{CodeControllerConflict}},
		{name: "controller conflict on an observing external resource yields both", state: ResourceState{ResourceID: wbRID, Controller: ControllerExternal, Takeover: TakeoverObserving, ControllerConflict: true},
			wantCodes: []string{CodeControllerConflict, CodeResourceReadOnly}, wantDetails: wgReadOnly(ControllerExternal, TakeoverObserving)},
		{name: "scope degraded node-offline", state: ResourceState{ResourceID: wbRID, ScopeDegraded: "node-offline"},
			wantCodes: []string{CodeScopeDegraded}, wantDetails: map[string]map[string]string{CodeScopeDegraded: {"mode": "node-offline"}}},
		{name: "pending confirmation", state: ResourceState{ResourceID: wbRID, PendingConfirmation: "c1"},
			wantCodes: []string{CodeConfirmRequired}, wantDetails: map[string]map[string]string{CodeConfirmRequired: {"confirmationId": "c1"}}},
		{name: "scope degraded and pending confirmation on a writable resource yield both", state: ResourceState{ResourceID: wbRID, ScopeDegraded: "engine-unresponsive", PendingConfirmation: "c2"},
			wantCodes: []string{CodeScopeDegraded, CodeConfirmRequired}, wantDetails: map[string]map[string]string{
				CodeScopeDegraded:   {"mode": "engine-unresponsive"},
				CodeConfirmRequired: {"confirmationId": "c2"},
			}},
		{name: "every blocker at once", state: ResourceState{ResourceID: wbRID, Controller: ControllerExternal, Takeover: TakeoverObserving,
			ControllerConflict: true, ScopeDegraded: "node-clock-skew", PendingConfirmation: "c3"},
			wantCodes: []string{CodeControllerConflict, CodeResourceReadOnly, CodeScopeDegraded, CodeConfirmRequired},
			wantDetails: map[string]map[string]string{
				CodeResourceReadOnly: {"controller": "external", "takeover": "observing"},
				CodeScopeDegraded:    {"mode": "node-clock-skew"},
				CodeConfirmRequired:  {"confirmationId": "c3"},
			}},
	}

	// Every controller under kept-read-only is read-only.
	for _, c := range []Controller{ControllerHazelminX, ControllerOS, ControllerExternal, ControllerCluster, ControllerUnknown} {
		cases = append(cases, wbCase{
			name:        fmt.Sprintf("%s kept-read-only is read-only", c),
			state:       ResourceState{ResourceID: wbRID, Controller: c, Takeover: TakeoverKeptReadOnly},
			wantCodes:   []string{CodeResourceReadOnly},
			wantDetails: wgReadOnly(c, TakeoverKeptReadOnly),
		})
	}

	// Every scope degradation mode from the field comment.
	for _, mode := range []string{"node-offline", "node-clock-skew", "engine-unresponsive"} {
		cases = append(cases, wbCase{
			name:        "scope degraded " + mode,
			state:       ResourceState{ResourceID: wbRID, ScopeDegraded: mode},
			wantCodes:   []string{CodeScopeDegraded},
			wantDetails: map[string]map[string]string{CodeScopeDegraded: {"mode": mode}},
		})
	}
	return cases
}

func TestWriteBlockersExpectedResults(t *testing.T) {
	for _, tc := range wbCases() {
		t.Run(tc.name, func(t *testing.T) {
			defer wgFailOnPanic(t)
			got := WriteBlockers(tc.state)

			var codes []string
			for _, e := range got {
				codes = append(codes, e.Code)
			}
			if want := wgSorted(tc.wantCodes); !reflect.DeepEqual(wgSorted(codes), want) {
				t.Fatalf("codes = %v, want %v", wgSorted(codes), want)
			}
			for _, e := range got {
				if e.ResourceID != tc.state.ResourceID {
					t.Errorf("%s ResourceID = %q, want %q", e.Code, e.ResourceID, tc.state.ResourceID)
				}
				if e.Message == "" {
					t.Errorf("%s Message is empty", e.Code)
				}
				if want := lookupCode(e.Code).Retryable; e.Retryable != want {
					t.Errorf("%s Retryable = %v, want table default %v", e.Code, e.Retryable, want)
				}
				if gotD, wantD := wgStringDetails(e.Details), tc.wantDetails[e.Code]; !reflect.DeepEqual(gotD, wantD) {
					t.Errorf("%s Details = %v, want %v", e.Code, gotD, wantD)
				}
			}
		})
	}
}

type pbCase struct {
	name    string
	in      []apiError
	wantNil bool
	// wantFrom is the index in in of the error that must be chosen.
	wantFrom int
	// wantAlso is the exact AlsoBlockedBy expected, in order (nil for none).
	wantAlso []string
}

func pbE(code, msg string) apiError {
	return apiError{Code: code, Message: msg}
}

func pbCases() []pbCase {
	return []pbCase{
		{name: "nil input returns nil", in: nil, wantNil: true},
		{name: "empty input returns nil", in: []apiError{}, wantNil: true},
		{name: "one error is returned with no also-blocked", in: []apiError{pbE(CodePrecheckFailed, "p")}, wantFrom: 0},
		{name: "one unlisted code is returned with no also-blocked", in: []apiError{pbE("UNLISTED_ONE", "u")}, wantFrom: 0},
		{name: "read-only beats version mismatch", wantFrom: 1, wantAlso: []string{CodeVersionMismatch},
			in: []apiError{pbE(CodeVersionMismatch, "v"), pbE(CodeResourceReadOnly, "r")}},
		{name: "platform degraded beats confirmation and precheck", wantFrom: 1, wantAlso: []string{CodeConfirmRequired, CodePrecheckFailed},
			in: []apiError{pbE(CodePrecheckFailed, "p"), pbE(CodePlatformDegraded, "d"), pbE(CodeConfirmRequired, "c")}},
		{name: "unlisted code ranks after listed ones", wantFrom: 1, wantAlso: []string{"CONFLICT"},
			in: []apiError{pbE("CONFLICT", "x"), pbE(CodePrecheckFailed, "p")}},
		{name: "unlisted codes keep input order after listed ones", wantFrom: 2, wantAlso: []string{"ZZZ_UNLISTED", "AAA_UNLISTED"},
			in: []apiError{pbE("ZZZ_UNLISTED", "z"), pbE("AAA_UNLISTED", "a"), pbE(CodeControllerConflict, "c")}},
		{name: "two errors with the same code return the first", wantFrom: 0, wantAlso: nil,
			in: []apiError{pbE(CodePrecheckFailed, "first"), pbE(CodePrecheckFailed, "second")}},
		{name: "duplicate codes are listed once and own code is excluded", wantFrom: 1, wantAlso: []string{CodeVersionMismatch},
			in: []apiError{pbE(CodeVersionMismatch, "v1"), pbE(CodeResourceReadOnly, "r"), pbE(CodeVersionMismatch, "v2")}},
		{name: "repeated chosen code is not listed in also-blocked", wantFrom: 0, wantAlso: []string{CodeVersionMismatch},
			in: []apiError{pbE(CodeResourceReadOnly, "r1"), pbE(CodeResourceReadOnly, "r2"), pbE(CodeVersionMismatch, "v")}},
		{name: "identity code beats validation regardless of input order", wantFrom: 2, wantAlso: []string{CodeNotFound, CodeValidationFailed},
			in: []apiError{pbE(CodeValidationFailed, "val"), pbE(CodeNotFound, "nf"), pbE(CodeUnauthenticated, "un")}},
	}
}

// pbClone copies the input so the test can detect any change pickBlocking makes.
func pbClone(in []apiError) []apiError {
	if in == nil {
		return nil
	}
	out := make([]apiError, len(in))
	copy(out, in)
	for i := range out {
		if in[i].AlsoBlockedBy != nil {
			out[i].AlsoBlockedBy = append([]string{}, in[i].AlsoBlockedBy...)
		}
	}
	return out
}

func TestPickBlockingExpectedResults(t *testing.T) {
	for _, tc := range pbCases() {
		t.Run(tc.name, func(t *testing.T) {
			defer wgFailOnPanic(t)
			before := pbClone(tc.in)
			got := pickBlocking(tc.in)
			if !reflect.DeepEqual(tc.in, before) {
				t.Fatalf("input slice was modified: before %+v, after %+v", before, tc.in)
			}
			if tc.wantNil {
				if got != nil {
					t.Fatalf("pickBlocking = %+v, want nil", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("pickBlocking = nil, want %s", tc.in[tc.wantFrom].Code)
			}
			want := tc.in[tc.wantFrom]
			want.AlsoBlockedBy = tc.wantAlso
			if !reflect.DeepEqual(*got, want) {
				t.Fatalf("pickBlocking = %+v\nwant %+v", *got, want)
			}
		})
	}
}
