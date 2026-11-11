package nodereg

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// Node is a managed host as administrators see it.
type Node struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Tags        []string      `json:"tags"`
	Group       string        `json:"group"`
	Fingerprint string        `json:"fingerprint"`
	Hostname    string        `json:"hostname"`
	MachineID   string        `json:"machineId"`
	OS          string        `json:"os"`
	Arch        string        `json:"arch"`
	State       string        `json:"state"`
	Connection  Connection    `json:"connection"`
	Certificate Certificate   `json:"certificate"`
	Maintenance Maintenance   `json:"maintenance"`
	Revocation  *Revocation   `json:"revocation"`
	Removal     *Removal      `json:"removal"`
	ConflictAt  *time.Time    `json:"conflictAt"`
	PendingSync []string      `json:"pendingSync"`
	Components  []Component   `json:"components"`
	CreatedAt   time.Time     `json:"createdAt"`
	UpdatedAt   time.Time     `json:"updatedAt"`
	Version     int64         `json:"version"`
	agent       agentSnapshot // what the latest session declared
	publicKey   ed25519.PublicKey
	nextPEM     string
	revokedAt   *time.Time
}

type agentSnapshot struct {
	version              string
	protocolVersion      int
	protocolMin, protoMx int
	caps                 []protocol.Capability
}

// Connection is the channel status. State offline means only that the
// channel is down: the host and its applications may well be running.
type Connection struct {
	State string `json:"state"`
	// LastContactAt is the last message Core received from the agent.
	LastContactAt *time.Time `json:"lastContactAt"`
	ConnectedAt   *time.Time `json:"connectedAt"`
	SessionID     string     `json:"sessionId,omitempty"`
	RemoteAddr    string     `json:"remoteAddr,omitempty"`
	Transport     string     `json:"transport,omitempty"`
	// ClockSkewSeconds is the agent clock minus the Core clock.
	ClockSkewSeconds *int `json:"clockSkewSeconds"`
	ClockSkewWarning bool `json:"clockSkewWarning"`
}

// Certificate is the node certificate and any rotation in progress.
type Certificate struct {
	Serial        string    `json:"serial"`
	NotAfter      time.Time `json:"notAfter"`
	State         string    `json:"state"`
	ExpiresInDays int       `json:"expiresInDays"`
	Rotation      *Rotation `json:"rotation"`
}

// Rotation is a new certificate the node has not used yet. Both are
// accepted until it does.
type Rotation struct {
	NewSerial   string     `json:"newSerial"`
	NewNotAfter time.Time  `json:"newNotAfter"`
	RequestedAt time.Time  `json:"requestedAt"`
	DeliveredAt *time.Time `json:"deliveredAt"`
	Error       string     `json:"error,omitempty"`
}

// Maintenance mode: new jobs are refused; CheckedAt is the first session
// after maintenance ended (capabilities and health re-read).
type Maintenance struct {
	Enabled            bool       `json:"enabled"`
	Reason             string     `json:"reason"`
	Since              *time.Time `json:"since"`
	EndedAt            *time.Time `json:"endedAt"`
	RecheckRequestedAt *time.Time `json:"recheckRequestedAt"`
	CheckedAt          *time.Time `json:"checkedAt"`
}

// Revocation records when it was asked for and when it reached the agent.
type Revocation struct {
	RequestedAt     time.Time  `json:"requestedAt"`
	Reason          string     `json:"reason"`
	By              string     `json:"by"`
	SessionClosedAt *time.Time `json:"sessionClosedAt"`
	AgentNotifiedAt *time.Time `json:"agentNotifiedAt"`
	// Pending: the agent has not been told yet (it was offline).
	Pending bool `json:"pending"`
	// PropagationSeconds is AgentNotifiedAt - RequestedAt.
	PropagationSeconds *int `json:"propagationSeconds"`
}

// Removal is how the node stopped being managed.
type Removal struct {
	Mode string    `json:"mode"`
	At   time.Time `json:"at"`
	By   string    `json:"by"`
}

// Component is one entry of the node page's component list.
type Component struct {
	Kind    string         `json:"kind"`
	Name    string         `json:"name"`
	Version string         `json:"version"`
	State   string         `json:"state"`
	Source  string         `json:"source"`
	Detail  map[string]any `json:"detail,omitempty"`
}

// NodeMatch is an existing node an enrolling host resembles.
type NodeMatch struct {
	NodeID string `json:"nodeId"`
	Name   string `json:"name"`
	State  string `json:"state"`
	// Reason is MatchSameKey or MatchMachineID.
	Reason string `json:"reason"`
}

const (
	MatchSameKey   = "same_key"
	MatchMachineID = "same_machine_id"
)

// NodeFilter narrows ListNodes. Without State, removed nodes are left out.
type NodeFilter struct {
	Query string // name, host name, ID or fingerprint prefix, case-insensitive
	Tag   string
	Group string
	State string
}

// NodePatch changes what administrators name a node; nil fields stay.
type NodePatch struct {
	Name  *string   `json:"name"`
	Tags  *[]string `json:"tags"`
	Group *string   `json:"group"`
}

const nodeColumns = `id, name, tags, node_group, fingerprint, public_key, hostname, machine_id, os, arch,
	agent_version, agent_protocol_min, agent_protocol_max, protocol_version, capabilities, state,
	cert_serial, cert_not_after, next_cert_serial, next_cert_not_after, next_cert_pem, rotation_requested_at, rotation_delivered_at, rotation_error,
	maintenance, maintenance_reason, maintenance_since, maintenance_ended_at, recheck_requested_at,
	revoked_at, revoke_reason, revoked_by, revocation_closed_at, revocation_notified_at, conflict_at,
	removed_at, removed_by, removal_mode, last_contact_at, last_connected_at, last_remote_addr, created_at, updated_at, version`

type scanner interface{ Scan(dest ...any) error }

func scanNode(sc scanner) (Node, error) {
	var n Node
	var tags, caps, notAfter, created, updated string
	var nextSerial, nextNotAfter, nextPEM, rotReq, rotDel sql.NullString
	var mSince, mEnded, recheck, revAt, revClosed, revNotified, conflict, remAt, lastContact, lastConn sql.NullString
	var maint int
	var revReason, revBy, remBy, remMode, rotErr, lastRemote string
	var pub []byte
	err := sc.Scan(&n.ID, &n.Name, &tags, &n.Group, &n.Fingerprint, &pub, &n.Hostname, &n.MachineID, &n.OS, &n.Arch,
		&n.agent.version, &n.agent.protocolMin, &n.agent.protoMx, &n.agent.protocolVersion, &caps, &n.State,
		&n.Certificate.Serial, &notAfter, &nextSerial, &nextNotAfter, &nextPEM, &rotReq, &rotDel, &rotErr,
		&maint, &n.Maintenance.Reason, &mSince, &mEnded, &recheck,
		&revAt, &revReason, &revBy, &revClosed, &revNotified, &conflict,
		&remAt, &remBy, &remMode, &lastContact, &lastConn, &lastRemote, &created, &updated, &n.Version)
	if err != nil {
		return n, err
	}
	n.Tags = decodeTags(tags)
	_ = json.Unmarshal([]byte(caps), &n.agent.caps)
	n.publicKey = pub
	n.Certificate.NotAfter = parseStamp(notAfter)
	if nextSerial.Valid {
		n.Certificate.Rotation = &Rotation{NewSerial: nextSerial.String, NewNotAfter: parseStamp(nextNotAfter.String),
			DeliveredAt: parseNull(rotDel), Error: rotErr}
		if t := parseNull(rotReq); t != nil {
			n.Certificate.Rotation.RequestedAt = *t
		}
		n.nextPEM = nextPEM.String
	}
	n.Maintenance.Enabled = maint == 1
	n.Maintenance.Since, n.Maintenance.EndedAt, n.Maintenance.RecheckRequestedAt = parseNull(mSince), parseNull(mEnded), parseNull(recheck)
	n.revokedAt = parseNull(revAt)
	if n.State == StateRevoked && n.revokedAt != nil {
		n.Revocation = &Revocation{RequestedAt: *n.revokedAt, Reason: revReason, By: revBy, SessionClosedAt: parseNull(revClosed), AgentNotifiedAt: parseNull(revNotified)}
	}
	if n.State == StateRemoved {
		if t := parseNull(remAt); t != nil {
			n.Removal = &Removal{Mode: remMode, At: *t, By: remBy}
		}
		// A removal reaches the agent the way a revocation does: its session
		// is closed, or its next connection is refused.
		if !revNotified.Valid {
			n.PendingSync = append(n.PendingSync, SyncRemoval)
		}
	}
	n.ConflictAt = parseNull(conflict)
	n.Connection.LastContactAt = parseNull(lastContact)
	n.Connection.ConnectedAt = parseNull(lastConn)
	n.Connection.RemoteAddr = lastRemote
	n.CreatedAt, n.UpdatedAt = parseStamp(created), parseStamp(updated)
	return n, nil
}

// view adds what comes from the live channel and the clock.
func (r *Registry) view(n Node, live map[string]nodes.ConnectionInfo) Node {
	now := r.now()
	if c, ok := live[n.Fingerprint]; ok && (n.State == StateActive) {
		seen := c.LastSeenAt.UTC()
		conn := c.ConnectedAt.UTC()
		n.Connection.LastContactAt = &seen
		n.Connection.ConnectedAt = &conn
		n.Connection.SessionID, n.Connection.RemoteAddr, n.Connection.Transport = c.SessionID, c.RemoteAddr, c.Transport
		n.Connection.ClockSkewSeconds = c.ClockSkewSeconds
		if c.ClockSkewSeconds != nil {
			d := time.Duration(*c.ClockSkewSeconds) * time.Second
			n.Connection.ClockSkewWarning = d >= ClockSkewWarning || d <= -ClockSkewWarning
		}
		n.Connection.State = ConnOnline
		if now.Sub(seen) > r.opts.StaleAfter {
			n.Connection.State = ConnStale
		}
	} else if n.Connection.LastContactAt != nil {
		n.Connection.State = ConnOffline
	} else {
		n.Connection.State = ConnNeverConnected
	}

	left := n.Certificate.NotAfter.Sub(now)
	n.Certificate.ExpiresInDays = int(left.Hours() / 24)
	switch {
	case left <= 0:
		n.Certificate.State = CertExpired
	case left < r.opts.RenewBefore:
		n.Certificate.State = CertExpiring
	default:
		n.Certificate.State = CertValid
	}

	if rv := n.Revocation; rv != nil {
		rv.Pending = rv.AgentNotifiedAt == nil
		if rv.AgentNotifiedAt != nil {
			s := int(rv.AgentNotifiedAt.Sub(rv.RequestedAt).Round(time.Second) / time.Second)
			rv.PropagationSeconds = &s
		}
		if rv.Pending {
			n.PendingSync = append(n.PendingSync, SyncRevocation)
		}
	}
	if n.State == StateActive && n.Certificate.Rotation != nil {
		n.PendingSync = append(n.PendingSync, SyncRotation)
	}
	if rc := n.Maintenance.RecheckRequestedAt; rc != nil {
		if n.Connection.ConnectedAt != nil && !n.Connection.ConnectedAt.Before(*rc) {
			n.Maintenance.CheckedAt = n.Connection.ConnectedAt
		} else if n.State == StateActive {
			n.PendingSync = append(n.PendingSync, SyncRecheck)
		}
	}
	if n.PendingSync == nil {
		n.PendingSync = []string{}
	}

	agentState := n.Connection.State
	n.Components = []Component{
		{Kind: ComponentAgent, Name: "hazelminx-agent", Version: n.agent.version, State: agentState, Source: "handshake",
			Detail: map[string]any{"protocolVersion": n.agent.protocolVersion, "agentProtocol": protocol.Range{Min: n.agent.protocolMin, Max: n.agent.protoMx},
				"capabilities": nonNilCaps(n.agent.caps)}},
		{Kind: ComponentCertificate, Name: "node certificate", Version: n.Certificate.Serial, State: n.Certificate.State, Source: "registry",
			Detail: map[string]any{"fingerprint": n.Fingerprint, "notAfter": n.Certificate.NotAfter}},
	}
	return n
}

func nonNilCaps(c []protocol.Capability) []protocol.Capability {
	if c == nil {
		return []protocol.Capability{}
	}
	return c
}

func (r *Registry) liveMap() map[string]nodes.ConnectionInfo {
	out := map[string]nodes.ConnectionInfo{}
	if ch := r.channel(); ch != nil {
		conns, _ := ch.Connections()
		for _, c := range conns {
			out[c.NodeID] = c
		}
	}
	return out
}

func (r *Registry) loadNode(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, where string, arg any) (Node, error) {
	n, err := scanNode(q.QueryRowContext(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE `+where, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return n, ErrNotFound
	}
	return n, err
}

// GetNode returns one node with its live status.
func (r *Registry) GetNode(ctx context.Context, id string) (Node, error) {
	n, err := r.loadNode(ctx, r.db, "id = ?", id)
	if err != nil {
		return n, err
	}
	return r.view(n, r.liveMap()), nil
}

// ListNodes returns the nodes matching f, by name.
func (r *Registry) ListNodes(ctx context.Context, f NodeFilter) ([]Node, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+nodeColumns+` FROM nodes ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	var all []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	live := r.liveMap()
	q := strings.ToLower(strings.TrimSpace(f.Query))
	out := []Node{}
	for _, n := range all {
		if f.State == "" && n.State == StateRemoved || f.State != "" && n.State != f.State {
			continue
		}
		if f.Group != "" && n.Group != f.Group {
			continue
		}
		if f.Tag != "" && !slices.Contains(n.Tags, f.Tag) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(n.Name), q) && !strings.Contains(strings.ToLower(n.Hostname), q) &&
			!strings.Contains(strings.ToLower(n.Group), q) && !strings.HasPrefix(n.ID, q) && !strings.HasPrefix(n.Fingerprint, q) {
			continue
		}
		out = append(out, r.view(n, live))
	}
	return out, nil
}

// UpdateNode renames, re-tags or regroups a node at version.
func (r *Registry) UpdateNode(ctx context.Context, id string, version int64, p NodePatch, actor string) (Node, error) {
	n, err := r.loadNode(ctx, r.db, "id = ?", id)
	if err != nil {
		return n, err
	}
	if n.Version != version {
		return n, fmt.Errorf("%w: current version %d", ErrVersionMismatch, n.Version)
	}
	name, tags, group := n.Name, n.Tags, n.Group
	if p.Name != nil {
		name = strings.TrimSpace(*p.Name)
		if name == "" {
			return n, validationf("name must not be empty")
		}
		if err := checkText("name", name, maxName); err != nil {
			return n, err
		}
	}
	if p.Tags != nil {
		if tags, err = normalizeTags(*p.Tags); err != nil {
			return n, err
		}
	}
	if p.Group != nil {
		group = strings.TrimSpace(*p.Group)
		if err := checkText("group", group, maxGroup); err != nil {
			return n, err
		}
	}
	res, err := r.db.ExecContext(ctx, `UPDATE nodes SET name = ?, tags = ?, node_group = ?, updated_at = ?, version = version + 1 WHERE id = ? AND version = ?`,
		name, encodeTags(tags), group, stamp(r.now()), id, version)
	if err != nil {
		return n, err
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return n, fmt.Errorf("%w: changed meanwhile", ErrVersionMismatch)
	}
	return r.GetNode(ctx, id)
}

// RevokeNode stops trusting a node (SEC-003): new connections are refused,
// a live session is closed now, job steps are refused.
func (r *Registry) RevokeNode(ctx context.Context, id, reason, actor string) (Node, error) {
	if err := checkText("reason", reason, 500); err != nil {
		return Node{}, err
	}
	n, err := r.loadNode(ctx, r.db, "id = ?", id)
	if err != nil {
		return n, err
	}
	if n.State != StateActive && n.State != StateIdentityConflict {
		return n, statef("node is %s", n.State)
	}
	now := r.now()
	res, err := r.db.ExecContext(ctx, `UPDATE nodes SET state = 'revoked', revoked_at = ?, revoke_reason = ?, revoked_by = ?,
		revocation_closed_at = NULL, revocation_notified_at = NULL,
		next_cert_serial = NULL, next_cert_not_after = NULL, next_cert_pem = NULL, rotation_requested_at = NULL, rotation_delivered_at = NULL, rotation_error = '',
		updated_at = ?, version = version + 1 WHERE id = ? AND state = ?`, stamp(now), reason, actor, stamp(now), id, n.State)
	if err != nil {
		return n, err
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return n, statef("node changed meanwhile")
	}
	r.closeAndRecord(ctx, n.Fingerprint, "node revoked")
	return r.GetNode(ctx, id)
}

// closeAndRecord closes a live session of fp and records that the agent was
// told.
func (r *Registry) closeAndRecord(ctx context.Context, fp, reason string) {
	ch := r.channel()
	if ch == nil || !ch.Disconnect(fp, reason) {
		return
	}
	now := stamp(r.now())
	_, _ = r.db.ExecContext(ctx, `UPDATE nodes SET revocation_closed_at = ?, revocation_notified_at = COALESCE(revocation_notified_at, ?) WHERE fingerprint = ?`, now, now, fp)
}

// SetMaintenance turns maintenance on (new jobs refused; the node's active
// jobs are returned so the caller can show what is affected) or off (the
// session is dropped so the agent reconnects and declares its capabilities
// and health again).
func (r *Registry) SetMaintenance(ctx context.Context, id string, on bool, reason, actor string) (Node, []*jobs.Job, error) {
	if err := checkText("reason", reason, 500); err != nil {
		return Node{}, nil, err
	}
	n, err := r.loadNode(ctx, r.db, "id = ?", id)
	if err != nil {
		return n, nil, err
	}
	if n.State != StateActive {
		return n, nil, statef("node is %s", n.State)
	}
	if n.Maintenance.Enabled == on {
		return n, nil, statef("maintenance is already %s", map[bool]string{true: "on", false: "off"}[on])
	}
	now := stamp(r.now())
	if on {
		_, err = r.db.ExecContext(ctx, `UPDATE nodes SET maintenance = 1, maintenance_reason = ?, maintenance_since = ?, maintenance_ended_at = NULL,
			recheck_requested_at = NULL, updated_at = ?, version = version + 1 WHERE id = ?`, reason, now, now, id)
	} else {
		_, err = r.db.ExecContext(ctx, `UPDATE nodes SET maintenance = 0, maintenance_ended_at = ?, recheck_requested_at = ?, updated_at = ?, version = version + 1 WHERE id = ?`,
			now, now, now, id)
	}
	if err != nil {
		return n, nil, err
	}
	var affected []*jobs.Job
	if on && r.opts.Jobs != nil {
		affected, err = r.opts.Jobs.List(ctx, jobs.ListOptions{Node: n.Fingerprint,
			States: []jobs.State{jobs.StatePrechecking, jobs.StateQueued, jobs.StateRunning, jobs.StateVerifying, jobs.StateRollingBack, jobs.StateCancelRequested, jobs.StateUnknown}})
		if err != nil {
			return n, nil, err
		}
	}
	if !on {
		if ch := r.channel(); ch != nil {
			ch.Disconnect(n.Fingerprint, "maintenance ended; reconnect to re-check capabilities and health")
		}
	}
	got, err := r.GetNode(ctx, id)
	return got, affected, err
}

// Remove stops managing a node in one of the four distinct ways (NOD-009).
// Only Unmanage is available: it forgets the node's trust and leaves the
// host, the agent and every application on it untouched.
func (r *Registry) Remove(ctx context.Context, id, mode, actor string) (Node, error) {
	switch mode {
	case Unmanage:
	case RemoveAgent:
		return Node{}, fmt.Errorf("%w: removing the agent from the host needs the uninstall action (ticket 037); use %q to stop managing the node and remove the agent on the host with the hazelminx CLI", ErrRemovalUnavailable, Unmanage)
	case RemoveClusterNode:
		return Node{}, fmt.Errorf("%w: this node is not a member of a cluster managed by this platform (Swarm and K3s are tickets 043 and 087)", ErrRemovalUnavailable)
	case RemoveWorkloads:
		return Node{}, fmt.Errorf("%w: deleting workloads is done per application, never as part of removing a node; this platform manages no workloads on the node yet", ErrRemovalUnavailable)
	default:
		return Node{}, validationf("mode must be one of %s, %s, %s, %s", Unmanage, RemoveAgent, RemoveClusterNode, RemoveWorkloads)
	}
	n, err := r.loadNode(ctx, r.db, "id = ?", id)
	if err != nil {
		return n, err
	}
	if n.State == StateRemoved {
		return n, statef("node is already removed")
	}
	now := stamp(r.now())
	if _, err := r.db.ExecContext(ctx, `UPDATE nodes SET state = 'removed', removed_at = ?, removed_by = ?, removal_mode = ?, maintenance = 0,
		revocation_closed_at = NULL, revocation_notified_at = NULL,
		next_cert_serial = NULL, next_cert_not_after = NULL, next_cert_pem = NULL, rotation_requested_at = NULL, rotation_delivered_at = NULL, rotation_error = '',
		updated_at = ?, version = version + 1 WHERE id = ?`, now, actor, mode, now, id); err != nil {
		return n, err
	}
	r.closeAndRecord(ctx, n.Fingerprint, "node removed from management")
	return r.GetNode(ctx, id)
}

// RotateCertificate issues a new certificate for the node's key and hands
// it over if the node is connected; otherwise at its next connection.
func (r *Registry) RotateCertificate(ctx context.Context, id, actor string) (Node, error) {
	n, err := r.loadNode(ctx, r.db, "id = ?", id)
	if err != nil {
		return n, err
	}
	if n.State != StateActive {
		return n, statef("node is %s", n.State)
	}
	if err := r.issueNext(ctx, n); err != nil {
		return n, err
	}
	r.deliver(ctx, n.Fingerprint)
	return r.GetNode(ctx, id)
}

func (r *Registry) issueNext(ctx context.Context, n Node) error {
	certPEM, err := r.opts.PKI.IssueNodeCertificate(n.publicKey, r.opts.CertValidity)
	if err != nil {
		return err
	}
	id, err := r.opts.PKI.VerifyNodeCertificate(certPEM, r.now())
	if err != nil {
		return err
	}
	now := stamp(r.now())
	_, err = r.db.ExecContext(ctx, `UPDATE nodes SET next_cert_serial = ?, next_cert_not_after = ?, next_cert_pem = ?, rotation_requested_at = ?,
		rotation_delivered_at = NULL, rotation_error = '', updated_at = ?, version = version + 1 WHERE id = ?`,
		id.Serial, stamp(id.NotAfter), string(certPEM), now, now, n.ID)
	return err
}

// deliver hands a pending certificate to a connected node; at most one
// delivery per node runs at a time.
func (r *Registry) deliver(ctx context.Context, fp string) {
	ch := r.channel()
	if ch == nil {
		return
	}
	r.rotMu.Lock()
	if r.rotating[fp] {
		r.rotMu.Unlock()
		return
	}
	r.rotating[fp] = true
	r.rotMu.Unlock()
	defer func() { r.rotMu.Lock(); delete(r.rotating, fp); r.rotMu.Unlock() }()

	n, err := r.loadNode(ctx, r.db, "fingerprint = ?", fp)
	if err != nil || n.State != StateActive || n.Certificate.Rotation == nil || n.nextPEM == "" {
		return
	}
	dctx, cancel := context.WithTimeout(ctx, deliverTimeout)
	defer cancel()
	err = ch.RenewCertificate(dctx, fp, []byte(n.nextPEM))
	switch {
	case err == nil:
		_, _ = r.db.ExecContext(ctx, `UPDATE nodes SET rotation_delivered_at = ?, rotation_error = '' WHERE id = ? AND next_cert_serial = ?`,
			stamp(r.now()), n.ID, n.Certificate.Rotation.NewSerial)
	case errors.Is(err, nodes.ErrNotConnected):
		// Delivered at the next connection.
	default:
		r.opts.Logger.Warn("node certificate rotation not delivered", "node", n.ID, "error", err)
		_, _ = r.db.ExecContext(ctx, `UPDATE nodes SET rotation_error = ? WHERE id = ? AND next_cert_serial = ?`,
			err.Error(), n.ID, n.Certificate.Rotation.NewSerial)
	}
}

// recordContact stores the last time the agent was heard from.
func (r *Registry) recordContact(ctx context.Context, fp string, at time.Time, remote string) {
	_, _ = r.db.ExecContext(ctx, `UPDATE nodes SET last_contact_at = ?, last_remote_addr = CASE WHEN ? = '' THEN last_remote_addr ELSE ? END WHERE fingerprint = ?`,
		stamp(at), remote, remote, fp)
}
