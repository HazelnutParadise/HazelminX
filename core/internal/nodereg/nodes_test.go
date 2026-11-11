package nodereg

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
)

func identity(fp, serial string) nodes.NodeIdentity {
	return nodes.NodeIdentity{Fingerprint: fp, Serial: serial}
}

func TestAuthorizeNode(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h := newHost("a", "")
	n := e.enroll(h, CreateInput{}, ApproveInput{})
	if err := e.reg.AuthorizeNode(ctx, identity(h.fp, n.Certificate.Serial)); err != nil {
		t.Fatalf("enrolled node: %v", err)
	}
	err := e.reg.AuthorizeNode(ctx, identity(h.fp, "abc"))
	if err == nil || errors.Is(err, nodes.ErrNodeRevoked) {
		t.Fatalf("replaced certificate: %v", err)
	}
	if err := e.reg.AuthorizeNode(ctx, identity(newHost("b", "").fp, "1")); err == nil || errors.Is(err, nodes.ErrNodeRevoked) {
		t.Fatalf("unknown node: %v", err)
	}
	e.reg.OnConflict(h.fp)
	if err := e.reg.AuthorizeNode(ctx, identity(h.fp, n.Certificate.Serial)); !errors.Is(err, nodes.ErrIdentityConflict) {
		t.Fatalf("conflict: %v", err)
	}
	got, _ := e.reg.GetNode(ctx, n.ID)
	if got.State != StateIdentityConflict || got.ConflictAt == nil {
		t.Fatalf("conflict state: %+v", got)
	}
}

// T-017: a clone (same key) is resolved by re-enrolling one copy with a new
// key in place of the node; the other copy stays refused.
func TestConflictIsResolvedByReEnrolling(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h := newHost("vm", "m1")
	n := e.enroll(h, CreateInput{}, ApproveInput{})
	e.reg.OnConflict(h.fp)
	// The cloned key cannot be used to enroll again.
	_, code := e.newCode(CreateInput{})
	if st, _ := e.claim(h, code); st != 403 {
		t.Fatalf("conflicted key enrolling: %d", st)
	}
	fresh := newHost("vm", "m1")
	en, code2 := e.newCode(CreateInput{})
	e.claim(fresh, code2)
	fixed, err := e.reg.Approve(ctx, en.ID, ApproveInput{Actor: "usr_owner", ReplaceNodeID: n.ID})
	if err != nil || fixed.State != StateActive || fixed.Fingerprint != fresh.fp || fixed.ConflictAt != nil {
		t.Fatalf("resolved: %+v %v", fixed, err)
	}
	if err := e.reg.AuthorizeNode(ctx, identity(h.fp, n.Certificate.Serial)); !errors.Is(err, nodes.ErrNodeRevoked) {
		t.Fatalf("clone after resolution: %v", err)
	}
}

// SEC-003, T-019: revoking an online node closes its session at once and
// records when; later steps are refused before they are sent.
func TestRevokeOnlineNode(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h := newHost("a", "")
	n := e.enroll(h, CreateInput{}, ApproveInput{})
	e.ch.connect(h.fp, e.now(), n.Certificate.Serial)
	e.advance(time.Second)
	got, err := e.reg.RevokeNode(ctx, n.ID, "lost laptop", "usr_owner")
	if err != nil {
		t.Fatal(err)
	}
	r := got.Revocation
	if got.State != StateRevoked || r == nil || r.SessionClosedAt == nil || r.AgentNotifiedAt == nil || r.Pending || r.PropagationSeconds == nil || *r.PropagationSeconds != 0 {
		t.Fatalf("revocation %+v", r)
	}
	if !slices.Contains(e.ch.disconnected, h.fp) {
		t.Fatal("session not closed")
	}
	if err := e.reg.AuthorizeNode(ctx, identity(h.fp, n.Certificate.Serial)); !errors.Is(err, nodes.ErrNodeRevoked) {
		t.Fatalf("reconnect: %v", err)
	}
	se := e.reg.GateJob(ctx, &jobs.Job{Node: h.fp, CreatedAt: e.now()})
	if se == nil || se.Code != CodeNodeRevoked || se.Retryable || !se.NothingChanged {
		t.Fatalf("gate: %+v", se)
	}
	if _, err := e.reg.RevokeNode(ctx, n.ID, "", "usr_owner"); !errors.Is(err, ErrState) {
		t.Fatalf("revoke twice: %v", err)
	}
}

// T-020: an offline node shows the revocation as waiting to reach it; its
// next connection attempt is refused and completes the revocation.
func TestRevokeOfflineNodeIsPendingUntilItCalls(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h := newHost("a", "")
	n := e.enroll(h, CreateInput{}, ApproveInput{})
	got, _ := e.reg.RevokeNode(ctx, n.ID, "", "usr_owner")
	if got.Revocation.SessionClosedAt != nil || !got.Revocation.Pending || !slices.Contains(got.PendingSync, SyncRevocation) {
		t.Fatalf("offline revocation %+v %v", got.Revocation, got.PendingSync)
	}
	e.advance(90 * time.Second)
	if err := e.reg.AuthorizeNode(ctx, identity(h.fp, n.Certificate.Serial)); !errors.Is(err, nodes.ErrNodeRevoked) {
		t.Fatal(err)
	}
	got, _ = e.reg.GetNode(ctx, n.ID)
	if got.Revocation.Pending || got.Revocation.PropagationSeconds == nil || *got.Revocation.PropagationSeconds != 90 || slices.Contains(got.PendingSync, SyncRevocation) {
		t.Fatalf("after refused reconnect %+v %v", got.Revocation, got.PendingSync)
	}
}

// NOD-007: last contact, online/stale/offline, never "powered off".
func TestConnectionState(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h := newHost("a", "")
	n := e.enroll(h, CreateInput{}, ApproveInput{})
	if got, _ := e.reg.GetNode(ctx, n.ID); got.Connection.State != ConnNeverConnected || got.Connection.LastContactAt != nil {
		t.Fatalf("never connected: %+v", got.Connection)
	}
	e.ch.connect(h.fp, e.now(), n.Certificate.Serial)
	if got, _ := e.reg.GetNode(ctx, n.ID); got.Connection.State != ConnOnline || got.Connection.LastContactAt == nil {
		t.Fatalf("online: %+v", got.Connection)
	}
	e.advance(31 * time.Second) // no message for longer than StaleAfter
	if got, _ := e.reg.GetNode(ctx, n.ID); got.Connection.State != ConnStale {
		t.Fatalf("stale: %+v", got.Connection)
	}
	seen := e.now().Add(-31 * time.Second)
	e.ch.Disconnect(h.fp, "")
	e.reg.recordContact(ctx, h.fp, seen, "")
	got, _ := e.reg.GetNode(ctx, n.ID)
	if got.Connection.State != ConnOffline || got.Connection.LastContactAt == nil || !got.Connection.LastContactAt.Equal(seen) {
		t.Fatalf("offline: %+v", got.Connection)
	}
}

func TestClockSkewIsShown(t *testing.T) {
	e := newEnv(t)
	h := newHost("a", "")
	n := e.enroll(h, CreateInput{}, ApproveInput{})
	e.ch.connect(h.fp, e.now(), n.Certificate.Serial)
	skew := 95
	e.ch.mu.Lock()
	c := e.ch.conns[h.fp]
	c.ClockSkewSeconds = &skew
	e.ch.conns[h.fp] = c
	e.ch.mu.Unlock()
	got, _ := e.reg.GetNode(context.Background(), n.ID)
	if got.Connection.ClockSkewSeconds == nil || *got.Connection.ClockSkewSeconds != 95 || !got.Connection.ClockSkewWarning {
		t.Fatalf("skew %+v", got.Connection)
	}
}

// NOD-008: maintenance refuses new jobs, lets older ones finish, lists the
// work affected, and re-checks the node when it ends.
func TestMaintenance(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h := newHost("a", "")
	n := e.enroll(h, CreateInput{}, ApproveInput{})
	before := e.now()
	e.advance(time.Second)
	got, affected, err := e.reg.SetMaintenance(ctx, n.ID, true, "disk swap", "usr_owner")
	if err != nil || !got.Maintenance.Enabled || got.Maintenance.Reason != "disk swap" || len(affected) != 1 || affected[0].ID != "job_running" {
		t.Fatalf("enter: %+v %+v %v", got.Maintenance, affected, err)
	}
	if se := e.reg.GateJob(ctx, &jobs.Job{Node: h.fp, CreatedAt: before}); se != nil {
		t.Fatalf("older job refused: %+v", se)
	}
	e.advance(time.Second)
	if se := e.reg.GateJob(ctx, &jobs.Job{Node: h.fp, CreatedAt: e.now()}); se == nil || se.Code != CodeNodeInMaintenance {
		t.Fatalf("new job allowed: %+v", se)
	}
	e.ch.connect(h.fp, e.now(), n.Certificate.Serial)
	got, _, err = e.reg.SetMaintenance(ctx, n.ID, false, "", "usr_owner")
	if err != nil || got.Maintenance.Enabled || got.Maintenance.RecheckRequestedAt == nil || !slices.Contains(e.ch.disconnected, h.fp) || !slices.Contains(got.PendingSync, SyncRecheck) {
		t.Fatalf("leave: %+v %v %v", got.Maintenance, got.PendingSync, err)
	}
	if se := e.reg.GateJob(ctx, &jobs.Job{Node: h.fp, CreatedAt: e.now()}); se != nil {
		t.Fatalf("job after maintenance: %+v", se)
	}
	// The reconnect after maintenance is the re-check.
	e.advance(2 * time.Second)
	e.reg.onSession(ctx, sessionFacts{fp: h.fp, serial: n.Certificate.Serial, at: e.now(), agentVersion: "0.1.1"})
	got, _ = e.reg.GetNode(ctx, n.ID)
	if got.Maintenance.CheckedAt == nil || slices.Contains(got.PendingSync, SyncRecheck) {
		t.Fatalf("re-check not recorded: %+v %v", got.Maintenance, got.PendingSync)
	}
}

// SEC-002, T-022: the new certificate is delivered, both are accepted until
// the node proves the new one, then the old one stops working.
func TestCertificateRotation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h := newHost("a", "")
	n := e.enroll(h, CreateInput{}, ApproveInput{})
	old := n.Certificate.Serial
	e.ch.connect(h.fp, e.now(), old)
	got, err := e.reg.RotateCertificate(ctx, n.ID, "usr_owner")
	if err != nil || got.Certificate.Rotation == nil || got.Certificate.Rotation.DeliveredAt == nil {
		t.Fatalf("rotate: %+v %v", got.Certificate, err)
	}
	next := got.Certificate.Rotation.NewSerial
	id, err := e.pki.VerifyNodeCertificate(e.ch.renewed[h.fp], e.now())
	if err != nil || id.Serial != next || id.Fingerprint != h.fp {
		t.Fatalf("delivered certificate: %+v %v", id, err)
	}
	for _, s := range []string{old, next} {
		if err := e.reg.AuthorizeNode(ctx, identity(h.fp, s)); err != nil {
			t.Fatalf("serial %s during rotation: %v", s, err)
		}
	}
	e.reg.onSession(ctx, sessionFacts{fp: h.fp, serial: next, at: e.now()})
	got, _ = e.reg.GetNode(ctx, n.ID)
	if got.Certificate.Serial != next || got.Certificate.Rotation != nil {
		t.Fatalf("after switching: %+v", got.Certificate)
	}
	if err := e.reg.AuthorizeNode(ctx, identity(h.fp, old)); err == nil {
		t.Fatal("old certificate still accepted")
	}
}

// T-022: when the agent cannot take the new certificate, the old one keeps
// the channel working and the failure is shown.
func TestRotationFailureKeepsTheOldCertificate(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h := newHost("a", "")
	n := e.enroll(h, CreateInput{}, ApproveInput{})
	e.ch.connect(h.fp, e.now(), n.Certificate.Serial)
	e.ch.renewErr = &nodes.CertificateRefusedError{Code: "WRITE_FAILED", Message: "disk full"}
	got, err := e.reg.RotateCertificate(ctx, n.ID, "usr_owner")
	if err != nil || got.Certificate.Rotation == nil || !strings.Contains(got.Certificate.Rotation.Error, "disk full") || got.Certificate.Rotation.DeliveredAt != nil {
		t.Fatalf("failed rotation: %+v %v", got.Certificate, err)
	}
	if err := e.reg.AuthorizeNode(ctx, identity(h.fp, n.Certificate.Serial)); err != nil {
		t.Fatalf("old certificate after a failed rotation: %v", err)
	}
}

// An offline node gets its new certificate when it connects; a node whose
// certificate is close to expiry is rotated automatically, with a warning.
func TestRotationOnConnectAndExpiryWarning(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h := newHost("a", "")
	n := e.enroll(h, CreateInput{}, ApproveInput{})
	e.advance(61 * 24 * time.Hour)
	got, _ := e.reg.GetNode(ctx, n.ID)
	if got.Certificate.State != CertExpiring || got.Certificate.ExpiresInDays > 30 {
		t.Fatalf("expiry warning: %+v", got.Certificate)
	}
	e.ch.connect(h.fp, e.now(), n.Certificate.Serial)
	e.reg.onSession(ctx, sessionFacts{fp: h.fp, serial: n.Certificate.Serial, at: e.now()})
	got, _ = e.reg.GetNode(ctx, n.ID)
	if got.Certificate.Rotation == nil || got.Certificate.Rotation.DeliveredAt == nil || e.ch.renewed[h.fp] == nil {
		t.Fatalf("automatic rotation: %+v", got.Certificate)
	}
}

// NOD-005: names, tags and groups; search; identity is the node ID and key,
// so changing the host name or IP changes nothing about who the node is.
func TestNamesTagsGroupsAndSearch(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.enroll(newHost("web-1", ""), CreateInput{Name: "web-1", Tags: []string{"prod"}, Group: "tokyo"}, ApproveInput{})
	e.enroll(newHost("db-1", ""), CreateInput{Name: "db-1", Tags: []string{"prod", "db"}, Group: "osaka"}, ApproveInput{})
	name, tags := "web-tokyo-1", []string{"prod", "edge"}
	got, err := e.reg.UpdateNode(ctx, a.ID, a.Version, NodePatch{Name: &name, Tags: &tags}, "usr_owner")
	if err != nil || got.Name != name || got.Version != a.Version+1 || got.Fingerprint != a.Fingerprint {
		t.Fatalf("update: %+v %v", got, err)
	}
	if _, err := e.reg.UpdateNode(ctx, a.ID, a.Version, NodePatch{Name: &name}, "usr_owner"); !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("stale version: %v", err)
	}
	for _, c := range []struct {
		f    NodeFilter
		want int
	}{
		{NodeFilter{Query: "tokyo"}, 1},
		{NodeFilter{Tag: "prod"}, 2},
		{NodeFilter{Tag: "db"}, 1},
		{NodeFilter{Group: "osaka"}, 1},
		{NodeFilter{Query: a.Fingerprint[:10]}, 1},
	} {
		if list, _ := e.reg.ListNodes(ctx, c.f); len(list) != c.want {
			t.Errorf("%+v: %d nodes", c.f, len(list))
		}
	}
	bad := "has\nnewline"
	if _, err := e.reg.UpdateNode(ctx, a.ID, got.Version, NodePatch{Name: &bad}, "usr_owner"); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad name: %v", err)
	}
}

// NOD-009: the four removal meanings are separate; "stop managing" leaves
// the host and its applications alone, the others are refused until the
// platform can do them.
func TestRemoveModes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h := newHost("a", "")
	n := e.enroll(h, CreateInput{}, ApproveInput{})
	for _, m := range []string{RemoveAgent, RemoveClusterNode, RemoveWorkloads} {
		if _, err := e.reg.Remove(ctx, n.ID, m, "usr_owner"); !errors.Is(err, ErrRemovalUnavailable) {
			t.Errorf("%s: %v", m, err)
		}
	}
	if _, err := e.reg.Remove(ctx, n.ID, "delete-everything", "usr_owner"); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown mode: %v", err)
	}
	e.ch.connect(h.fp, e.now(), n.Certificate.Serial)
	got, err := e.reg.Remove(ctx, n.ID, Unmanage, "usr_owner")
	if err != nil || got.State != StateRemoved || got.Removal == nil || got.Removal.Mode != Unmanage || !slices.Contains(e.ch.disconnected, h.fp) {
		t.Fatalf("unmanage: %+v %v", got, err)
	}
	if list, _ := e.reg.ListNodes(ctx, NodeFilter{}); len(list) != 0 {
		t.Fatal("removed node still listed by default")
	}
	if list, _ := e.reg.ListNodes(ctx, NodeFilter{State: StateRemoved}); len(list) != 1 {
		t.Fatal("removed node not listed on request")
	}
	if err := e.reg.AuthorizeNode(ctx, identity(h.fp, n.Certificate.Serial)); !errors.Is(err, nodes.ErrNodeRevoked) {
		t.Fatalf("removed node connecting: %v", err)
	}
	// Enrolling the same key again brings the same node back.
	back := e.enroll(h, CreateInput{}, ApproveInput{})
	if back.ID != n.ID || back.State != StateActive || back.Removal != nil {
		t.Fatalf("re-enrolled after removal: %+v", back)
	}
}

func TestComponentsListAgentAndCertificate(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h := newHost("a", "")
	n := e.enroll(h, CreateInput{}, ApproveInput{})
	e.ch.connect(h.fp, e.now(), n.Certificate.Serial)
	e.reg.onSession(ctx, sessionFacts{fp: h.fp, serial: n.Certificate.Serial, at: e.now(), agentVersion: "0.1.0", protocolVersion: 1, agentMin: 1, agentMax: 1})
	got, _ := e.reg.GetNode(ctx, n.ID)
	kinds := map[string]Component{}
	for _, c := range got.Components {
		kinds[c.Kind] = c
	}
	if kinds[ComponentAgent].Version != "0.1.0" || kinds[ComponentAgent].State != ConnOnline || kinds[ComponentCertificate].Version != n.Certificate.Serial {
		t.Fatalf("components %+v", got.Components)
	}
}
