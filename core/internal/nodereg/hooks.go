package nodereg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// AuthorizeNode implements nodes.Authorizer. A node may connect when it is
// enrolled and active and presents its current certificate or, during a
// rotation, the new one. A refused revoked or removed node records that it
// was told (the revocation has then propagated).
func (r *Registry) AuthorizeNode(ctx context.Context, id nodes.NodeIdentity) error {
	// A retired key stays refused whatever else happens to it.
	var nodeID, reason string
	switch e := r.db.QueryRowContext(ctx, `SELECT node_id, reason FROM node_retired_keys WHERE fingerprint = ?`, id.Fingerprint).Scan(&nodeID, &reason); {
	case e == nil:
		return fmt.Errorf("%w: this key of node %s is retired (%s)", nodes.ErrNodeRevoked, nodeID, reason)
	case !errors.Is(e, sql.ErrNoRows):
		return e
	}
	n, err := r.loadNode(ctx, r.db, "fingerprint = ?", id.Fingerprint)
	if errors.Is(err, ErrNotFound) {
		return errors.New("node is not enrolled with this core")
	}
	if err != nil {
		return err
	}
	switch n.State {
	case StateRevoked, StateRemoved:
		now := stamp(r.now())
		_, _ = r.db.ExecContext(ctx, `UPDATE nodes SET revocation_notified_at = COALESCE(revocation_notified_at, ?) WHERE id = ?`, now, n.ID)
		return fmt.Errorf("%w: node %s is %s", nodes.ErrNodeRevoked, n.ID, n.State)
	case StateIdentityConflict:
		return fmt.Errorf("%w: node %s key was seen live in two places; enroll the host again with a new key", nodes.ErrIdentityConflict, n.ID)
	}
	if id.Serial != n.Certificate.Serial && (n.Certificate.Rotation == nil || id.Serial != n.Certificate.Rotation.NewSerial) {
		return fmt.Errorf("certificate %s of node %s was replaced; the node must use its current certificate", id.Serial, n.ID)
	}
	return nil
}

// sessionFacts is what a session start tells the registry.
type sessionFacts struct {
	fp, serial, remote string
	at                 time.Time
	agentVersion       string
	protocolVersion    int
	agentMin, agentMax int
	caps               []protocol.Capability
}

// OnReady is the hub's OnReady for the registry (runs on its own goroutine).
func (r *Registry) OnReady(s *nodes.Session) {
	ci := s.Info()
	r.onSession(context.Background(), sessionFacts{fp: ci.NodeID, serial: ci.CertificateSerial, remote: ci.RemoteAddr, at: ci.ConnectedAt,
		agentVersion: ci.AgentVersion, protocolVersion: ci.ProtocolVersion, agentMin: ci.AgentRange.Min, agentMax: ci.AgentRange.Max, caps: ci.Capabilities})
}

// onSession records the session, finishes a rotation the node just proved,
// and hands over a pending or due certificate.
func (r *Registry) onSession(ctx context.Context, f sessionFacts) {
	n, err := r.loadNode(ctx, r.db, "fingerprint = ?", f.fp)
	if err != nil {
		return
	}
	if n.State != StateActive {
		// Revoked, removed or conflicted while this session's handshake ran:
		// close it now, as the revocation would have.
		r.closeAndRecord(ctx, f.fp, "node is "+n.State)
		return
	}
	at := stamp(f.at)
	if _, err := r.db.ExecContext(ctx, `UPDATE nodes SET last_connected_at = ?, last_contact_at = ?, last_remote_addr = ?, agent_version = ?,
		protocol_version = ?, agent_protocol_min = ?, agent_protocol_max = ?, capabilities = ? WHERE id = ?`,
		at, at, f.remote, f.agentVersion, f.protocolVersion, f.agentMin, f.agentMax, capsJSON(f.caps), n.ID); err != nil {
		r.opts.Logger.Warn("record node session", "node", n.ID, "error", err)
		return
	}
	// A re-check asked for after maintenance (or while the node was
	// offline) starts on this session; startRecheck submits each request once.
	if rc := n.Maintenance.Recheck; rc != nil && rc.requestID == "" {
		r.startRecheck(ctx, f.fp)
	}
	rot := n.Certificate.Rotation
	switch {
	case rot != nil && f.serial == rot.NewSerial:
		// The node proved the new certificate: stop accepting the old one.
		now := stamp(r.now())
		_, _ = r.db.ExecContext(ctx, `UPDATE nodes SET cert_serial = next_cert_serial, cert_not_after = next_cert_not_after,
			next_cert_serial = NULL, next_cert_not_after = NULL, next_cert_pem = NULL, rotation_requested_at = NULL,
			rotation_delivered_at = NULL, rotation_error = '', updated_at = ?, version = version + 1 WHERE id = ? AND next_cert_serial = ?`, now, n.ID, rot.NewSerial)
		return
	case rot != nil && rot.DeliveredAt != nil && f.at.After(*rot.DeliveredAt):
		// The agent stored the new certificate but connected with the old
		// one: it could not use it. Keep both accepted, say so, and hand it
		// over again once RedeliverAfter has passed.
		_, _ = r.db.ExecContext(ctx, `UPDATE nodes SET rotation_error = ? WHERE id = ? AND next_cert_serial = ?`,
			"the agent reconnected with its previous certificate after receiving the new one; it will be offered again", n.ID, rot.NewSerial)
		if r.now().Sub(*rot.DeliveredAt) >= RedeliverAfter {
			r.deliver(ctx, f.fp)
		}
		return
	case rot != nil:
		r.deliver(ctx, f.fp)
	case n.Certificate.NotAfter.Sub(r.now()) < r.opts.RenewBefore:
		if err := r.issueNext(ctx, n); err == nil {
			r.deliver(ctx, f.fp)
		}
	}
}

// OnClosed is the hub's OnClosed: the last time the agent was heard from.
func (r *Registry) OnClosed(s *nodes.Session) {
	ci := s.Info()
	r.recordContact(context.Background(), ci.NodeID, ci.LastSeenAt, ci.RemoteAddr)
}

// OnConflict is the hub's OnConflict: the node's key is live in two places.
// Every connection with it is refused until the node enrolls again.
func (r *Registry) OnConflict(fp string) {
	now := stamp(r.now())
	_, err := r.db.Exec(`UPDATE nodes SET state = 'identity_conflict', conflict_at = ?, updated_at = ?, version = version + 1 WHERE fingerprint = ? AND state = 'active'`,
		now, now, fp)
	if err != nil {
		r.opts.Logger.Warn("record node identity conflict", "fingerprint", fp, "error", err)
	}
	r.opts.Logger.Warn("node key is live in two places; the node is refused until it enrolls again", "fingerprint", fp)
}

// GateJob is the step runner's gate: nothing is sent to revoked, removed,
// conflicted or unknown nodes, nor for jobs created after maintenance began.
func (r *Registry) GateJob(ctx context.Context, job *jobs.Job) *jobs.StepError {
	n, err := r.loadNode(ctx, r.db, "fingerprint = ?", job.Node)
	refuse := func(code, summary string, actions ...string) *jobs.StepError {
		return &jobs.StepError{Code: code, Summary: summary, NothingChanged: true, SuggestedActions: actions}
	}
	if errors.Is(err, ErrNotFound) {
		return refuse(CodeNodeNotEnrolled, "the job's node is not enrolled; nothing was sent")
	}
	if err != nil {
		return &jobs.StepError{Code: "INTERNAL", Summary: "node registry unavailable: " + err.Error(), Retryable: true, NothingChanged: true}
	}
	switch n.State {
	case StateRevoked, StateRemoved:
		return refuse(CodeNodeRevoked, fmt.Sprintf("node %s is %s; nothing was sent", n.Name, n.State))
	case StateIdentityConflict:
		return refuse(CodeNodeConflict, fmt.Sprintf("node %s has an identity conflict (its key is live in two places); nothing was sent", n.Name), "re-enroll-node")
	}
	if n.Maintenance.Enabled && n.Maintenance.Since != nil && !job.CreatedAt.Before(*n.Maintenance.Since) {
		return refuse(CodeNodeInMaintenance, fmt.Sprintf("node %s is in maintenance since %s; jobs created during maintenance are not run", n.Name, n.Maintenance.Since.Format(time.RFC3339)), "end-maintenance")
	}
	return nil
}
