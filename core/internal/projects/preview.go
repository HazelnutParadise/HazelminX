package projects

import (
	"context"
	"slices"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/engines"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// EngineViews reads what Core knows about a node's engines (ticket 039.1).
type EngineViews interface {
	Get(ctx context.Context, nodeID string) (*engines.View, error)
}

// Target states of a preview.
const (
	TargetNotSet      = "not-set"
	TargetNotProbed   = "not-probed"  // Core has no engine report of the node yet
	TargetNotFound    = "not-found"   // the node's report has no such environment
	TargetUnsupported = "unsupported" // not Docker or Podman
	TargetNotRunning  = "not-running" // the engine does not answer
	TargetNoCompose   = "no-compose"  // no Docker Compose CLI plugin on the host
	TargetReady       = "ready"
)

// TargetNotVisible: the caller may not see the target node.
const TargetNotVisible = "not-visible"

// TargetCheck is what the preview knows about the target engine.
type TargetCheck struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
	// NodeID and EngineEnvironmentID echo the unit's target.
	NodeID              string     `json:"nodeId,omitempty"`
	EngineEnvironmentID string     `json:"engineEnvironmentId,omitempty"`
	Kind                string     `json:"kind,omitempty"`
	Mode                string     `json:"mode,omitempty"`
	EngineVersion       *string    `json:"engineVersion,omitempty"`
	ComposeVersion      *string    `json:"composeVersion,omitempty"`
	ReportCollectedAt   *time.Time `json:"reportCollectedAt,omitempty"`
}

// Preview is what deploying the unit would do, as far as Core can tell
// without asking the host (044.2 adds the host checks: images, disk, ports
// in use and bind mount sources).
type Preview struct {
	Analysis Analysis    `json:"analysis"`
	Target   TargetCheck `json:"target"`
	// Deployable: valid files and a ready target.
	Deployable bool `json:"deployable"`
	// NeedsApproval: a high-risk setting makes the deployment go through a
	// change plan.
	NeedsApproval bool `json:"needsApproval"`
	// Blockers are the error findings and the target problem, in order.
	Blockers []Finding `json:"blockers"`
	// HostChecksPending lists what is checked on the host before anything
	// changes, in ticket 044.2.
	HostChecksPending []string `json:"hostChecksPending"`
}

var hostChecks = []string{"images-reachable", "disk-space", "ports-free", "bind-mount-sources"}

// PreviewUnit computes the preview of a stored unit.
// canSeeNode says whether the caller may see the target node; when not,
// nothing about the node is shown.
func (s *Service) PreviewUnit(ctx context.Context, u *Unit, views EngineViews, canSeeNode bool) (Preview, error) {
	a, err := s.AnalyzeUnit(ctx, u)
	if err != nil {
		return Preview{}, err
	}
	var t TargetCheck
	if u.Target != nil && !canSeeNode {
		t = TargetCheck{State: TargetNotVisible, Reason: "this account cannot see the target node"}
	} else if t, err = checkTarget(ctx, u.Target, views); err != nil {
		return Preview{}, err
	}
	p := Preview{Analysis: a, Target: t, Blockers: []Finding{}, HostChecksPending: hostChecks}
	for _, f := range a.Findings {
		switch f.Severity {
		case SeverityError:
			p.Blockers = append(p.Blockers, f)
		case SeverityHighRisk:
			p.NeedsApproval = true
		}
	}
	if t.State != TargetReady {
		p.Blockers = append(p.Blockers, Finding{Severity: SeverityError, Code: "target-" + t.State, Message: t.Reason})
	}
	p.Deployable = len(p.Blockers) == 0
	return p, nil
}

func checkTarget(ctx context.Context, t *Target, views EngineViews) (TargetCheck, error) {
	if t == nil {
		return TargetCheck{State: TargetNotSet, Reason: "choose the node and engine environment to deploy to"}, nil
	}
	c := TargetCheck{NodeID: t.NodeID, EngineEnvironmentID: t.EngineEnvironmentID}
	v, err := views.Get(ctx, t.NodeID)
	if err != nil {
		return c, err
	}
	if v == nil || v.Report == nil {
		c.State, c.Reason = TargetNotProbed, "Core has no engine report of this node yet; it asks when the node is connected"
		return c, nil
	}
	c.ReportCollectedAt = v.CollectedAt
	i := slices.IndexFunc(v.Report.Environments, func(e protocol.Environment) bool { return e.ID == t.EngineEnvironmentID })
	if i < 0 {
		c.State, c.Reason = TargetNotFound, "the node's latest engine report has no such environment; installing an engine is not available yet (ticket 039.2)"
		return c, nil
	}
	env := v.Report.Environments[i]
	c.Kind, c.Mode, c.EngineVersion = env.Kind, env.Mode, env.Version
	switch {
	case env.Kind != protocol.EngineDocker && env.Kind != protocol.EnginePodman:
		c.State, c.Reason = TargetUnsupported, "Compose units deploy to Docker or Podman; this environment is "+env.Kind
		return c, nil
	case env.State != protocol.EngineRunning:
		c.State, c.Reason = TargetNotRunning, "the engine is "+env.State+"; it must be running"
		if env.Error != nil {
			c.Reason += ": " + *env.Error
		}
		return c, nil
	}
	// Both engines are driven with the Docker Compose CLI (52.1), a plugin
	// of the docker CLI.
	for _, e := range v.Report.Engines {
		if e.Kind != protocol.EngineDocker {
			continue
		}
		for _, p := range e.Plugins {
			if p.Name == "compose" {
				c.ComposeVersion = p.Version
				c.State = TargetReady
				return c, nil
			}
		}
	}
	c.State, c.Reason = TargetNoCompose, "the host has no Docker Compose CLI plugin (docker compose); installing it is not available yet (ticket 039.2)"
	return c, nil
}
