package plans

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// digest is the identity of a plan's content: kind, params, targets with
// their versions, the diff, the affected services, each check's id, status
// and target, the risk and the approval it needs. It leaves out what may
// differ between two computations of the same content: check titles and
// details (free space, timings), the deadline and the plan id. Secret
// values enter as an HMAC under the plan's salt, so the digest changes when
// a secret changes but cannot be used to guess it.
func digest(kind string, params json.RawMessage, d *Draft, appr Approval, risk Risk, salt []byte) (string, error) {
	var canonParams any
	if len(params) > 0 {
		if err := json.Unmarshal(params, &canonParams); err != nil {
			return "", fmt.Errorf("%w: params must be JSON: %v", ErrInvalid, err)
		}
	}
	type change struct {
		Target string `json:"target"`
		Path   string `json:"path"`
		Op     string `json:"op"`
		Before any    `json:"before"`
		After  any    `json:"after"`
		Secret string `json:"secret,omitempty"`
	}
	type check struct {
		ID     string      `json:"id"`
		Status CheckStatus `json:"status"`
		Target string      `json:"target"`
	}
	changes := make([]change, 0, len(d.Changes))
	for _, c := range d.Changes {
		out := change{Target: c.Target, Path: c.Path, Op: c.Op, Before: c.Before, After: c.After}
		if c.Secret {
			v, err := json.Marshal([]any{c.Before, c.After})
			if err != nil {
				return "", err
			}
			m := hmac.New(sha256.New, salt)
			m.Write(v)
			out.Before, out.After, out.Secret = nil, nil, hex.EncodeToString(m.Sum(nil))
		}
		changes = append(changes, out)
	}
	checks := make([]check, 0, len(d.Checks))
	for _, c := range d.Checks {
		checks = append(checks, check{c.ID, c.Status, c.Target})
	}
	canon, err := json.Marshal(struct {
		Kind          string     `json:"kind"`
		Params        any        `json:"params"`
		Summary       string     `json:"summary"`
		Targets       []Target   `json:"targets"`
		Changes       []change   `json:"changes"`
		Affected      []Affected `json:"affected"`
		Checks        []check    `json:"checks"`
		Risk          Risk       `json:"risk"`
		Required      bool       `json:"approvalRequired"`
		OwnerRequired bool       `json:"ownerRequired"`
		Action        string     `json:"action"`
	}{kind, canonParams, d.Summary, d.Targets, changes, d.Affected, checks, risk, appr.Required, appr.OwnerRequired, string(appr.Action)})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canon)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
