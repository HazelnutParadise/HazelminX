package nodereg

import (
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// Enrollment states, derived from the row and the clock.
const (
	EnrollmentWaiting         = "waiting"          // code not used yet, still valid
	EnrollmentExpired         = "expired"          // code not used before it expired
	EnrollmentRevoked         = "revoked"          // code withdrawn before use
	EnrollmentPendingApproval = "pending_approval" // a host used the code; an administrator must check it
	EnrollmentApproved        = "approved"
	EnrollmentRejected        = "rejected"
	EnrollmentApprovalExpired = "approval_expired" // nobody checked the host in time
)

// Enrollment is one code and what happened with it. The code itself is
// only returned by CreateEnrollment.
type Enrollment struct {
	ID        string     `json:"id"`
	CodeID    string     `json:"codeId"`
	Name      string     `json:"name"`
	Tags      []string   `json:"tags"`
	Group     string     `json:"group"`
	State     string     `json:"state"`
	CreatedBy string     `json:"createdBy"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt time.Time  `json:"expiresAt"`
	RevokedAt *time.Time `json:"revokedAt"`
	RevokedBy string     `json:"revokedBy,omitempty"`
	// Host is what the host that used the code reported; nil before use.
	Host      *ClaimedHost `json:"host"`
	DecidedAt *time.Time   `json:"decidedAt"`
	DecidedBy string       `json:"decidedBy,omitempty"`
	NodeID    string       `json:"nodeId,omitempty"`
	// MatchingNodes are existing nodes with the same key or machine-id.
	MatchingNodes []NodeMatch `json:"matchingNodes"`
	Version       int64       `json:"version"`

	key         []byte
	publicKey   []byte
	certificate string
	decision    string
}

// ClaimedHost is the host that used a code.
type ClaimedHost struct {
	ClaimedAt      time.Time `json:"claimedAt"`
	ClaimExpiresAt time.Time `json:"claimExpiresAt"`
	Fingerprint    string    `json:"fingerprint"`
	Hostname       string    `json:"hostname"`
	MachineID      string    `json:"machineId"`
	OS             string    `json:"os"`
	Arch           string    `json:"arch"`
	AgentVersion   string    `json:"agentVersion"`
	SourceAddr     string    `json:"sourceAddr"`
}

// CreateInput is a new code: what the node will be called once approved,
// the group the host must ask for (if it names one), and how long the code
// is valid (default 10 minutes, 1 to 60).
type CreateInput struct {
	Name  string
	Tags  []string
	Group string
	TTL   time.Duration
	Actor string
}

// ApproveInput decides a pending enrollment. ReplaceNodeID gives the
// node's identity to the new key; AsNewNode creates a node although one
// with the same machine-id exists.
type ApproveInput struct {
	Actor         string
	ReplaceNodeID string
	AsNewNode     bool
}

const enrollmentColumns = `id, code_id, code_key, name, tags, node_group, created_by, created_at, expires_at, revoked_at, revoked_by,
	claimed_at, claim_expires_at, fingerprint, public_key, hostname, machine_id, os, arch, agent_version, source_addr,
	decision, decided_at, decided_by, node_id, node_certificate, version`

func (r *Registry) scanEnrollment(sc scanner) (Enrollment, error) {
	var e Enrollment
	var tags, created, expires string
	var revoked, claimed, claimExp, fp, decision, decided, nodeID, cert sql.NullString
	var host ClaimedHost
	err := sc.Scan(&e.ID, &e.CodeID, &e.key, &e.Name, &tags, &e.Group, &e.CreatedBy, &created, &expires, &revoked, &e.RevokedBy,
		&claimed, &claimExp, &fp, &e.publicKey, &host.Hostname, &host.MachineID, &host.OS, &host.Arch, &host.AgentVersion, &host.SourceAddr,
		&decision, &decided, &e.DecidedBy, &nodeID, &cert, &e.Version)
	if err != nil {
		return e, err
	}
	e.Tags = decodeTags(tags)
	e.CreatedAt, e.ExpiresAt = parseStamp(created), parseStamp(expires)
	e.RevokedAt = parseNull(revoked)
	if t := parseNull(claimed); t != nil {
		host.ClaimedAt = *t
		host.ClaimExpiresAt = parseStamp(claimExp.String)
		host.Fingerprint = fp.String
		e.Host = &host
	}
	e.decision = decision.String
	e.DecidedAt = parseNull(decided)
	e.NodeID = nodeID.String
	e.certificate = cert.String
	now := r.now()
	switch {
	case e.decision == "approved":
		e.State = EnrollmentApproved
	case e.decision == "rejected":
		e.State = EnrollmentRejected
	case e.Host != nil && !now.Before(e.Host.ClaimExpiresAt):
		e.State = EnrollmentApprovalExpired
	case e.Host != nil:
		e.State = EnrollmentPendingApproval
	case e.RevokedAt != nil:
		e.State = EnrollmentRevoked
	case !now.Before(e.ExpiresAt):
		e.State = EnrollmentExpired
	default:
		e.State = EnrollmentWaiting
	}
	e.MatchingNodes = []NodeMatch{}
	return e, nil
}

func (r *Registry) withMatches(ctx context.Context, e Enrollment) (Enrollment, error) {
	if e.Host == nil || e.State != EnrollmentPendingApproval {
		return e, nil
	}
	m, err := r.matches(ctx, e.Host.Fingerprint, e.Host.MachineID)
	e.MatchingNodes = m
	return e, err
}

// matches lists nodes an enrolling host resembles: same key first.
func (r *Registry) matches(ctx context.Context, fp, machineID string) ([]NodeMatch, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, state, fingerprint FROM nodes WHERE fingerprint = ? OR (machine_id <> '' AND machine_id = ? AND state <> 'removed') ORDER BY fingerprint = ? DESC, name`,
		fp, machineID, fp)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NodeMatch{}
	for rows.Next() {
		var m NodeMatch
		var f string
		if err := rows.Scan(&m.NodeID, &m.Name, &m.State, &f); err != nil {
			return nil, err
		}
		m.Reason = MatchMachineID
		if f == fp {
			m.Reason = MatchSameKey
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CreateEnrollment makes a single-use code. The code is returned once and
// never stored.
func (r *Registry) CreateEnrollment(ctx context.Context, in CreateInput) (Enrollment, protocol.EnrollmentCode, error) {
	var zero protocol.EnrollmentCode
	ttl := in.TTL
	if ttl == 0 {
		ttl = r.opts.CodeTTL
	}
	if ttl < MinCodeTTL || ttl > MaxCodeTTL {
		return Enrollment{}, zero, validationf("ttl must be between %s and %s", MinCodeTTL, MaxCodeTTL)
	}
	name, group := strings.TrimSpace(in.Name), strings.TrimSpace(in.Group)
	if err := checkText("name", name, maxName); err != nil {
		return Enrollment{}, zero, err
	}
	if err := checkText("group", group, maxGroup); err != nil {
		return Enrollment{}, zero, err
	}
	tags, err := normalizeTags(in.Tags)
	if err != nil {
		return Enrollment{}, zero, err
	}
	if in.Actor == "" {
		return Enrollment{}, zero, validationf("actor is required")
	}
	now := r.now()
	for range 20 {
		code, err := protocol.NewEnrollmentCode()
		if err != nil {
			return Enrollment{}, zero, err
		}
		// The code ID must point at one code while any code with that ID
		// can still be used or polled.
		var busy int
		if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_enrollments WHERE code_id = ? AND (expires_at > ? OR (claim_expires_at IS NOT NULL AND claim_expires_at > ?))`,
			code.ID(), stamp(now), stamp(now)).Scan(&busy); err != nil {
			return Enrollment{}, zero, err
		}
		if busy > 0 {
			continue
		}
		id := newID("enr_")
		if _, err := r.db.ExecContext(ctx, `INSERT INTO node_enrollments (id, code_id, code_key, name, tags, node_group, created_by, created_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, code.ID(), code.Key(), name, encodeTags(tags), group, in.Actor, stamp(now), stamp(now.Add(ttl))); err != nil {
			return Enrollment{}, zero, err
		}
		e, err := r.GetEnrollment(ctx, id)
		return e, code, err
	}
	return Enrollment{}, zero, errors.New("nodereg: could not draw an unused code id")
}

// GetEnrollment returns one enrollment, with matching nodes while it waits
// for approval.
func (r *Registry) GetEnrollment(ctx context.Context, id string) (Enrollment, error) {
	e, err := r.scanEnrollment(r.db.QueryRowContext(ctx, `SELECT `+enrollmentColumns+` FROM node_enrollments WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	if err != nil {
		return e, err
	}
	return r.withMatches(ctx, e)
}

// ListEnrollments returns enrollments, newest first; state filters by the
// derived state when not empty.
func (r *Registry) ListEnrollments(ctx context.Context, state string) ([]Enrollment, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+enrollmentColumns+` FROM node_enrollments ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, err
	}
	var all []Enrollment
	for rows.Next() {
		e, err := r.scanEnrollment(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		if state == "" || e.State == state {
			all = append(all, e)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Enrollment, 0, len(all))
	for _, e := range all {
		e, err := r.withMatches(ctx, e)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// RevokeEnrollment withdraws a code nobody has used yet.
func (r *Registry) RevokeEnrollment(ctx context.Context, id, actor string) (Enrollment, error) {
	e, err := r.GetEnrollment(ctx, id)
	if err != nil {
		return e, err
	}
	if e.State != EnrollmentWaiting {
		return e, statef("code is %s; only an unused, valid code can be withdrawn (reject a host that used it instead)", e.State)
	}
	res, err := r.db.ExecContext(ctx, `UPDATE node_enrollments SET revoked_at = ?, revoked_by = ?, version = version + 1 WHERE id = ? AND claimed_at IS NULL AND revoked_at IS NULL`,
		stamp(r.now()), actor, id)
	if err != nil {
		return e, err
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return e, statef("code was used meanwhile")
	}
	return r.GetEnrollment(ctx, id)
}

// Reject refuses the host that used a code; the code is spent.
func (r *Registry) Reject(ctx context.Context, id, actor string) (Enrollment, error) {
	e, err := r.GetEnrollment(ctx, id)
	if err != nil {
		return e, err
	}
	if e.State != EnrollmentPendingApproval {
		return e, statef("enrollment is %s", e.State)
	}
	res, err := r.db.ExecContext(ctx, `UPDATE node_enrollments SET decision = 'rejected', decided_at = ?, decided_by = ?, version = version + 1 WHERE id = ? AND decision IS NULL`,
		stamp(r.now()), actor, id)
	if err != nil {
		return e, err
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return e, statef("enrollment was decided meanwhile")
	}
	return r.GetEnrollment(ctx, id)
}

// Approve accepts the host that used a code and issues its certificate.
// The same key keeps its node (also a removed one); a new key with the
// machine-id of an existing node needs ReplaceNodeID or AsNewNode.
func (r *Registry) Approve(ctx context.Context, id string, in ApproveInput) (Node, error) {
	e, err := r.GetEnrollment(ctx, id)
	if err != nil {
		return Node{}, err
	}
	if e.State != EnrollmentPendingApproval {
		return Node{}, statef("enrollment is %s", e.State)
	}
	h := e.Host
	existing, err := r.loadNode(ctx, r.db, "fingerprint = ?", h.Fingerprint)
	sameKey := err == nil
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Node{}, err
	}
	if sameKey && (existing.State == StateRevoked || existing.State == StateIdentityConflict) {
		return Node{}, statef("this key belongs to node %s, which is %s; the host must enroll with a new key", existing.ID, existing.State)
	}
	var target *Node
	switch {
	case sameKey:
		if in.ReplaceNodeID != "" && in.ReplaceNodeID != existing.ID {
			return Node{}, statef("this key already belongs to node %s", existing.ID)
		}
		target = &existing
	case in.ReplaceNodeID != "":
		old, err := r.loadNode(ctx, r.db, "id = ?", in.ReplaceNodeID)
		if errors.Is(err, ErrNotFound) {
			return Node{}, validationf("replaceNodeId %s does not exist", in.ReplaceNodeID)
		}
		if err != nil {
			return Node{}, err
		}
		target = &old
	case !in.AsNewNode:
		m, err := r.matches(ctx, h.Fingerprint, h.MachineID)
		if err != nil {
			return Node{}, err
		}
		if len(m) > 0 {
			return Node{}, &ChoiceRequiredError{Matches: m}
		}
	}

	certPEM, err := r.opts.PKI.IssueNodeCertificate(ed25519.PublicKey(e.publicKey), r.opts.CertValidity)
	if err != nil {
		return Node{}, err
	}
	cid, err := r.opts.PKI.VerifyNodeCertificate(certPEM, r.now())
	if err != nil {
		return Node{}, err
	}
	now := stamp(r.now())
	nodeID := newID("nod_")
	if target != nil {
		nodeID = target.ID
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Node{}, err
	}
	defer tx.Rollback()
	// Checked again inside the transaction: the key may have been retired,
	// revoked or seen twice while the claim waited.
	if blocked, err := keyBlockedIn(ctx, tx, h.Fingerprint); err != nil {
		return Node{}, err
	} else if blocked {
		return Node{}, statef("this key was revoked, replaced or seen on two hosts after the host used the code; the host must enroll with a new key")
	}
	// Facts the host reported replace the old ones; names stay unless the
	// code carried new ones.
	const reset = `state = 'active', cert_serial = ?, cert_not_after = ?,
		next_cert_serial = NULL, next_cert_not_after = NULL, next_cert_pem = NULL, rotation_requested_at = NULL, rotation_delivered_at = NULL, rotation_error = '',
		revoked_at = NULL, revoke_reason = '', revoked_by = '', revocation_closed_at = NULL, revocation_notified_at = NULL, conflict_at = NULL,
		removed_at = NULL, removed_by = '', removal_mode = '',
		hostname = ?, machine_id = ?, os = ?, arch = ?, agent_version = ?, updated_at = ?, version = version + 1`
	resetArgs := []any{cid.Serial, stamp(cid.NotAfter), h.Hostname, h.MachineID, h.OS, h.Arch, h.AgentVersion, now}
	switch {
	case target == nil:
		name := e.Name
		if name == "" {
			name = h.Hostname
		}
		if name == "" {
			name = "node-" + h.Fingerprint[:8]
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO nodes (id, name, tags, node_group, fingerprint, public_key, hostname, machine_id, os, arch, agent_version,
			state, cert_serial, cert_not_after, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, ?)`,
			nodeID, name, encodeTags(e.Tags), e.Group, h.Fingerprint, e.publicKey, h.Hostname, h.MachineID, h.OS, h.Arch, h.AgentVersion,
			cid.Serial, stamp(cid.NotAfter), now, now)
	case sameKey:
		var res sql.Result
		res, err = tx.ExecContext(ctx, `UPDATE nodes SET `+reset+` WHERE id = ? AND fingerprint = ? AND state IN ('active', 'removed')`,
			append(resetArgs, nodeID, h.Fingerprint)...)
		if err == nil {
			if k, _ := res.RowsAffected(); k == 0 {
				return Node{}, statef("node %s changed meanwhile", nodeID)
			}
		}
	default:
		// Replace: the old key is retired for good, the node keeps its ID.
		if _, err = tx.ExecContext(ctx, `INSERT INTO node_retired_keys (fingerprint, node_id, retired_at, reason) VALUES (?, ?, ?, 'replaced at re-enrollment')`,
			target.Fingerprint, nodeID, now); err != nil {
			return Node{}, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE nodes SET fingerprint = ?, public_key = ?, `+reset+` WHERE id = ?`,
			append(append([]any{h.Fingerprint, e.publicKey}, resetArgs...), nodeID)...)
	}
	if err != nil {
		return Node{}, err
	}
	if e.Name != "" || len(e.Tags) > 0 || e.Group != "" {
		if target != nil {
			name, group, tags := target.Name, target.Group, target.Tags
			if e.Name != "" {
				name = e.Name
			}
			if e.Group != "" {
				group = e.Group
			}
			if len(e.Tags) > 0 {
				tags = e.Tags
			}
			if _, err := tx.ExecContext(ctx, `UPDATE nodes SET name = ?, node_group = ?, tags = ? WHERE id = ?`, name, group, encodeTags(tags), nodeID); err != nil {
				return Node{}, err
			}
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE node_enrollments SET decision = 'approved', decided_at = ?, decided_by = ?, node_id = ?, node_certificate = ?, version = version + 1
		WHERE id = ? AND decision IS NULL`, now, in.Actor, nodeID, string(certPEM), id)
	if err != nil {
		return Node{}, err
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return Node{}, statef("enrollment was decided meanwhile")
	}
	if err := tx.Commit(); err != nil {
		return Node{}, err
	}
	// The old certificate (same key) or the old key (replace) must not keep
	// a session open.
	if ch := r.channel(); ch != nil && target != nil {
		ch.Disconnect(target.Fingerprint, "node enrolled again")
	}
	return r.GetNode(ctx, nodeID)
}

// ---- agent listener -----------------------------------------------------------

// Discovery answers GET /agent/v1/enroll/discovery for a code that can still
// be used or polled.
// Unknown code IDs count against source like wrong codes do, so code IDs
// cannot be enumerated freely.
func (r *Registry) Discovery(ctx context.Context, codeID, source string) (protocol.EnrollDiscovery, error) {
	if !r.limiter.allow(source, r.now()) {
		return protocol.EnrollDiscovery{}, ErrTooManyAttempts
	}
	e, ok, err := r.liveEnrollment(ctx, codeID)
	if err != nil {
		return protocol.EnrollDiscovery{}, err
	}
	if !ok {
		r.limiter.fail(source, r.now())
		return protocol.EnrollDiscovery{}, ErrCodeNotFound
	}
	ca := r.opts.PKI.CACertificatePEM()
	return protocol.EnrollDiscovery{CACertificate: string(ca), MAC: protocol.DiscoveryMAC(e.key, ca)}, nil
}

// liveEnrollment is the enrollment with codeID that a host can still use or
// poll (waiting, pending approval, or approved within the claim window).
func (r *Registry) liveEnrollment(ctx context.Context, codeID string) (Enrollment, bool, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+enrollmentColumns+` FROM node_enrollments WHERE code_id = ? ORDER BY created_at DESC`, codeID)
	if err != nil {
		return Enrollment{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		e, err := r.scanEnrollment(rows)
		if err != nil {
			return e, false, err
		}
		if e.State == EnrollmentWaiting || e.State == EnrollmentPendingApproval ||
			e.State == EnrollmentApproved && e.Host != nil && r.now().Before(e.Host.ClaimExpiresAt) {
			return e, true, nil
		}
	}
	return Enrollment{}, false, rows.Err()
}

func refused(status int, code, msg string) (int, protocol.EnrollResponse) {
	return status, protocol.EnrollResponse{Status: protocol.EnrollRefused, Code: code, Message: msg}
}

// Claim handles POST /agent/v1/enroll. source is the client address for
// throttling and for the administrator to check.
func (r *Registry) Claim(ctx context.Context, req protocol.EnrollRequest, source string) (int, protocol.EnrollResponse) {
	return r.claim(ctx, req, source, true)
}

func (r *Registry) claim(ctx context.Context, req protocol.EnrollRequest, source string, mayRetry bool) (int, protocol.EnrollResponse) {
	if !r.limiter.allow(source, r.now()) {
		return refused(http.StatusTooManyRequests, protocol.EnrollTooManyTries, "too many wrong codes from this address; wait 10 minutes")
	}
	code, err := protocol.ParseEnrollmentCode(req.Code)
	if err != nil {
		r.limiter.fail(source, r.now())
		return refused(http.StatusBadRequest, protocol.EnrollRequestInvalid, err.Error())
	}
	pub, err := base64.RawURLEncoding.DecodeString(req.NodePublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return refused(http.StatusBadRequest, protocol.EnrollRequestInvalid, "nodePublicKey must be 32 bytes, base64url without padding")
	}
	for field, v := range map[string]string{"hostname": req.Hostname, "machineId": req.MachineID, "os": req.OS, "arch": req.Arch, "agentVersion": req.AgentVersion, "group": req.Group} {
		if err := checkText(field, v, 256); err != nil {
			return refused(http.StatusBadRequest, protocol.EnrollRequestInvalid, err.Error())
		}
	}
	fp := protocol.Fingerprint(pub)

	e, err := r.enrollmentForCode(ctx, code)
	if errors.Is(err, ErrCodeNotFound) {
		r.limiter.fail(source, r.now())
		return refused(http.StatusForbidden, protocol.EnrollCodeInvalid, "no such enrollment code")
	}
	if err != nil {
		return refused(http.StatusInternalServerError, "INTERNAL", "internal error")
	}
	switch e.State {
	case EnrollmentRevoked:
		return refused(http.StatusForbidden, protocol.EnrollCodeRevoked, "the code was withdrawn")
	case EnrollmentRejected:
		if e.Host != nil && e.Host.Fingerprint == fp {
			return refused(http.StatusForbidden, protocol.EnrollRejected, "an administrator rejected this host")
		}
		return refused(http.StatusForbidden, protocol.EnrollCodeUsed, "the code was already used")
	case EnrollmentExpired:
		return refused(http.StatusForbidden, protocol.EnrollCodeExpired, fmt.Sprintf("the code expired at %s", e.ExpiresAt.Format(time.RFC3339)))
	}
	if e.Host != nil {
		if e.Host.Fingerprint != fp {
			return refused(http.StatusForbidden, protocol.EnrollCodeUsed, "the code was already used by another host")
		}
		switch e.State {
		case EnrollmentApprovalExpired:
			return refused(http.StatusForbidden, protocol.EnrollExpired, "nobody approved this host within 24 hours; create a new code")
		case EnrollmentApproved:
			if !r.now().Before(e.Host.ClaimExpiresAt) {
				return refused(http.StatusForbidden, protocol.EnrollExpired, "the approval can no longer be collected; create a new code")
			}
			return http.StatusOK, protocol.EnrollResponse{Status: protocol.EnrollApproved, EnrollmentID: e.ID, NodeID: e.NodeID,
				NodeCertificate: e.certificate, CACertificate: string(r.opts.PKI.CACertificatePEM()), PrivilegedIssuerKey: string(r.opts.IssuerKeyPEM)}
		}
		return http.StatusAccepted, protocol.EnrollResponse{Status: protocol.EnrollPending, EnrollmentID: e.ID, RetryAfterSeconds: 5}
	}
	// First use.
	if e.Group != "" && req.Group != "" && req.Group != e.Group || e.Group == "" && req.Group != "" {
		return refused(http.StatusForbidden, protocol.EnrollGroupMismatch, "the code was issued for another group")
	}
	if blocked, err := r.keyBlocked(ctx, fp); err != nil {
		return refused(http.StatusInternalServerError, "INTERNAL", "internal error")
	} else if blocked {
		return refused(http.StatusForbidden, protocol.EnrollNodeKeyRevoked, "this node key was revoked, replaced or seen on two hosts; enroll with a new key (hazelminx-agent enroll --new-identity)")
	}
	now := r.now()
	res, err := r.db.ExecContext(ctx, `UPDATE node_enrollments SET claimed_at = ?, claim_expires_at = ?, fingerprint = ?, public_key = ?, hostname = ?, machine_id = ?,
		os = ?, arch = ?, agent_version = ?, source_addr = ?, version = version + 1 WHERE id = ? AND claimed_at IS NULL AND revoked_at IS NULL AND expires_at > ?`,
		stamp(now), stamp(now.Add(r.opts.ApprovalTTL)), fp, pub, req.Hostname, req.MachineID, req.OS, req.Arch, req.AgentVersion, source, e.ID, stamp(now))
	if err != nil {
		return refused(http.StatusInternalServerError, "INTERNAL", "internal error")
	}
	if k, _ := res.RowsAffected(); k == 0 {
		// Another request used it first (or it just expired); answer as for
		// a retry, once.
		if mayRetry {
			return r.claim(ctx, req, source, false)
		}
		return refused(http.StatusForbidden, protocol.EnrollCodeUsed, "the code was used or expired meanwhile")
	}
	r.opts.Logger.Info("enrollment code used; waiting for an administrator", "enrollment", e.ID, "fingerprint", fp, "hostname", req.Hostname, "source", source)
	return http.StatusAccepted, protocol.EnrollResponse{Status: protocol.EnrollPending, EnrollmentID: e.ID, RetryAfterSeconds: 5}
}

// enrollmentForCode finds the enrollment whose key matches the code,
// newest first.
func (r *Registry) enrollmentForCode(ctx context.Context, code protocol.EnrollmentCode) (Enrollment, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+enrollmentColumns+` FROM node_enrollments WHERE code_id = ? ORDER BY created_at DESC`, code.ID())
	if err != nil {
		return Enrollment{}, err
	}
	defer rows.Close()
	key := code.Key()
	for rows.Next() {
		e, err := r.scanEnrollment(rows)
		if err != nil {
			return e, err
		}
		if subtle.ConstantTimeCompare(e.key, key) == 1 {
			return e, nil
		}
	}
	if err := rows.Err(); err != nil {
		return Enrollment{}, err
	}
	return Enrollment{}, ErrCodeNotFound
}

// keyBlocked: the key was retired, or belongs to a revoked or conflicted node.
func (r *Registry) keyBlocked(ctx context.Context, fp string) (bool, error) {
	return keyBlockedIn(ctx, r.db, fp)
}

func keyBlockedIn(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, fp string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM node_retired_keys WHERE fingerprint = ?) +
		(SELECT COUNT(*) FROM nodes WHERE fingerprint = ? AND state IN ('revoked', 'identity_conflict'))`, fp, fp).Scan(&n)
	return n > 0, err
}

// failureLimiter counts wrong codes per source in a fixed window.
type failureLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	seen   map[string]*failures
}

type failures struct {
	start time.Time
	n     int
}

func newFailureLimiter(max int, window time.Duration) *failureLimiter {
	return &failureLimiter{max: max, window: window, seen: map[string]*failures{}}
}

func (l *failureLimiter) allow(src string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, ok := l.seen[src]
	if !ok || now.Sub(f.start) >= l.window {
		return true
	}
	return f.n < l.max
}

// maxLimiterSources bounds the limiter's memory. When it is full, expired
// entries go and then arbitrary ones until a tenth is free, so the cost of
// one scan is spread over the next thousand new sources.
const maxLimiterSources = 10000

func (l *failureLimiter) fail(src string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.seen[src]; !ok && len(l.seen) >= maxLimiterSources {
		for k, f := range l.seen {
			if now.Sub(f.start) >= l.window {
				delete(l.seen, k)
			}
		}
		for k := range l.seen {
			if len(l.seen) < maxLimiterSources*9/10 {
				break
			}
			delete(l.seen, k)
		}
	}
	f, ok := l.seen[src]
	if !ok || now.Sub(f.start) >= l.window {
		l.seen[src] = &failures{start: now, n: 1}
		return
	}
	f.n++
}
