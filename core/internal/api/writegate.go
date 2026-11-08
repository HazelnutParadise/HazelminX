package api

import (
	"slices"
	"sort"
)

// Controller is who controls a managed resource (addendum 03 機制一, the
// ARC-005 owner column).
type Controller string

const (
	ControllerHazelminX Controller = "hazelminx"
	ControllerOS        Controller = "os"
	ControllerExternal  Controller = "external" // 1Panel, Portainer, certbot, Helm…
	ControllerCluster   Controller = "cluster"
	ControllerUnknown   Controller = "unknown"
)

// Takeover is the takeover state of a managed resource.
type Takeover string

const (
	TakeoverNotApplicable Takeover = "not-applicable"
	TakeoverObserving     Takeover = "observing"      // 唯讀觀察中
	TakeoverTakenOver     Takeover = "taken-over"     // 已接管
	TakeoverKeptReadOnly  Takeover = "kept-read-only" // 已保留唯讀
)

// ResourceState is what a write route knows about its target before acting.
// Resource tickets fill it from their records; the zero value is a resource
// the platform created and owns.
type ResourceState struct {
	ResourceID string
	// Controller and Takeover default to hazelminx / not-applicable when
	// empty.
	Controller Controller
	Takeover   Takeover
	// ControllerConflict: two controllers were detected and no one chose yet.
	ControllerConflict bool
	// ScopeDegraded names the degradation of the node or engine environment
	// the resource lives on ("node-offline", "node-clock-skew",
	// "engine-unresponsive"); empty when healthy.
	ScopeDegraded string
	// PendingConfirmation is the id of a ROB-010 change on this resource that
	// still waits for confirmation; empty when none.
	PendingConfirmation string
}

// WriteBlockers returns every refusal that applies to writing the resource,
// in no particular order; empty means writable. It implements the
// "控制者與能不能寫入" table of addendum 03 機制一 and must be the only place
// that table is encoded, so the interface's disabled buttons and the API
// refusals cannot disagree (51.1).
//
// Expected results (C = Controller, T = Takeover):
//   - C hazelminx, T not-applicable → writable.
//   - C os, T not-applicable → writable.
//   - C hazelminx, T taken-over → writable (a resource taken over from an
//     external tool or an unknown controller).
//   - C external, T observing → RESOURCE_READ_ONLY.
//   - C cluster, T observing → RESOURCE_READ_ONLY.
//   - C unknown, T observing → RESOURCE_READ_ONLY.
//   - any C, T kept-read-only → RESOURCE_READ_ONLY.
//   - any other combination (e.g. external + not-applicable, os +
//     observing, cluster + taken-over, an unknown string) →
//     RESOURCE_READ_ONLY: an unrecognised state fails closed.
//   - empty C and empty T count as hazelminx / not-applicable.
//   - ControllerConflict → CONTROLLER_CONFLICT, in addition to whatever the
//     table says (so a conflicting hazelminx resource yields only
//     CONTROLLER_CONFLICT; a conflicting observing one yields both).
//   - ScopeDegraded "x" → SCOPE_DEGRADED with Details {"mode": "x"}.
//   - PendingConfirmation "c1" → CONFIRMATION_REQUIRED with Details
//     {"confirmationId": "c1"}.
//   - Every returned error has ResourceID set, Message non-empty, and
//     Retryable equal to the table default of its code (use newError).
//   - RESOURCE_READ_ONLY carries Details {"controller": C, "takeover": T}
//     with the defaulted values.
func WriteBlockers(s ResourceState) []apiError {
	c, t := s.Controller, s.Takeover
	if c == "" {
		c = ControllerHazelminX
	}
	if t == "" {
		t = TakeoverNotApplicable
	}
	writable := (c == ControllerHazelminX && (t == TakeoverNotApplicable || t == TakeoverTakenOver)) ||
		(c == ControllerOS && t == TakeoverNotApplicable)
	var out []apiError
	add := func(e apiError) {
		e.ResourceID = s.ResourceID
		out = append(out, e)
	}
	if s.ControllerConflict {
		add(newError(CodeControllerConflict, "two controllers manage this resource; choose one before changing it"))
	}
	if !writable {
		e := newError(CodeResourceReadOnly, "the platform may not change this resource: controller "+string(c)+", takeover "+string(t))
		e.Details = map[string]any{"controller": c, "takeover": t}
		add(e)
	}
	if s.ScopeDegraded != "" {
		e := newError(CodeScopeDegraded, "the node or engine environment of this resource is degraded ("+s.ScopeDegraded+")")
		e.Details = map[string]any{"mode": s.ScopeDegraded}
		add(e)
	}
	if s.PendingConfirmation != "" {
		e := newError(CodeConfirmRequired, "a change on this resource is waiting for confirmation")
		e.Details = map[string]any{"confirmationId": s.PendingConfirmation}
		add(e)
	}
	return out
}

// pickBlocking chooses the error to send when several apply (51.6): the one
// earliest in blockingOrder; codes not in blockingOrder rank after all listed
// ones, keeping their input order. The chosen error gets AlsoBlockedBy = the
// other codes in precedence order, without duplicates and without its own
// code.
//
// Expected results:
//   - nil or empty input → nil.
//   - one error → that error, AlsoBlockedBy empty (nil).
//   - [VERSION_MISMATCH, RESOURCE_READ_ONLY] → RESOURCE_READ_ONLY with
//     AlsoBlockedBy [VERSION_MISMATCH].
//   - [PRECHECK_FAILED, PLATFORM_DEGRADED, CONFIRMATION_REQUIRED] →
//     PLATFORM_DEGRADED with AlsoBlockedBy [CONFIRMATION_REQUIRED,
//     PRECHECK_FAILED].
//   - [CONFLICT, PRECHECK_FAILED] → PRECHECK_FAILED with AlsoBlockedBy
//     [CONFLICT] (unlisted CONFLICT ranks last).
//   - two errors with the same code → the first one, AlsoBlockedBy empty.
//   - the input slice is not modified.
func pickBlocking(errs []apiError) *apiError {
	if len(errs) == 0 {
		return nil
	}
	rank := func(code string) int {
		if i := slices.Index(blockingOrder, code); i >= 0 {
			return i
		}
		return len(blockingOrder)
	}
	order := make([]int, len(errs))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return rank(errs[order[a]].Code) < rank(errs[order[b]].Code) })
	top := errs[order[0]]
	top.AlsoBlockedBy = nil
	seen := map[string]bool{top.Code: true}
	for _, i := range order[1:] {
		if c := errs[i].Code; !seen[c] {
			seen[c] = true
			top.AlsoBlockedBy = append(top.AlsoBlockedBy, c)
		}
	}
	return &top
}
