package nodereg

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// NOD-001: a short-lived single-use code; the host waits for an
// administrator to check it; retries return the same enrollment; the
// certificate is for the host's own key.
func TestEnrollmentWaitsForApprovalThenIssuesTheCertificate(t *testing.T) {
	e := newEnv(t)
	en, code := e.newCode(CreateInput{Name: "lab-pi-02", Tags: []string{"lab", "arm64"}, Group: "lab"})
	if en.State != EnrollmentWaiting || !en.ExpiresAt.Equal(e.now().Add(10*time.Minute)) {
		t.Fatalf("new code: %+v", en)
	}
	c, _ := protocol.ParseEnrollmentCode(code)
	d, err := e.reg.Discovery(context.Background(), c.ID(), "203.0.113.61")
	if err != nil || !protocol.VerifyDiscoveryMAC(c.Key(), []byte(d.CACertificate), d.MAC) || d.CACertificate != string(e.pki.CACertificatePEM()) {
		t.Fatalf("discovery %+v %v", d, err)
	}
	h := newHost("lab-pi-02", "5c1e0000000000000000000000a09f")
	st, r := e.claim(h, code)
	if st != 202 || r.Status != protocol.EnrollPending || r.EnrollmentID != en.ID || r.RetryAfterSeconds <= 0 {
		t.Fatalf("claim: %d %+v", st, r)
	}
	// The administrator sees what to check.
	got, err := e.reg.GetEnrollment(context.Background(), en.ID)
	if err != nil || got.State != EnrollmentPendingApproval || got.Host == nil || got.Host.Fingerprint != h.fp ||
		got.Host.Hostname != "lab-pi-02" || got.Host.MachineID != h.machineID || got.Host.SourceAddr != "203.0.113.61" {
		t.Fatalf("pending enrollment: %+v %v", got, err)
	}
	// Retrying before approval is the same enrollment.
	if st, r2 := e.claim(h, code); st != 202 || r2.EnrollmentID != en.ID {
		t.Fatalf("retry: %d %+v", st, r2)
	}
	n, err := e.reg.Approve(context.Background(), en.ID, ApproveInput{Actor: "usr_owner"})
	if err != nil {
		t.Fatal(err)
	}
	if n.Name != "lab-pi-02" || n.Group != "lab" || strings.Join(n.Tags, ",") != "arm64,lab" || n.Fingerprint != h.fp || !strings.HasPrefix(n.ID, "nod_") {
		t.Fatalf("node %+v", n)
	}
	st, r = e.claim(h, code)
	if st != 200 || r.Status != protocol.EnrollApproved || r.NodeID != n.ID || r.CACertificate != d.CACertificate || r.PrivilegedIssuerKey != "ISSUER-PUB" {
		t.Fatalf("approved: %d %+v", st, r)
	}
	id, err := e.pki.VerifyNodeCertificate([]byte(r.NodeCertificate), e.now())
	if err != nil || id.Fingerprint != h.fp || id.Serial != n.Certificate.Serial {
		t.Fatalf("certificate: %+v %v", id, err)
	}
	// Asking again gives the same certificate, never a second node.
	_, r3 := e.claim(h, code)
	if r3.NodeCertificate != r.NodeCertificate {
		t.Fatal("second answer issued another certificate")
	}
	list, _ := e.reg.ListNodes(context.Background(), NodeFilter{})
	if len(list) != 1 {
		t.Fatalf("%d nodes", len(list))
	}
}

// SEC-001, T-016: used, expired, revoked and wrong-group codes cannot be
// used again, no node is created and the validity is not extended.
func TestCodesThatCannotBeUsed(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h, other := newHost("a", ""), newHost("b", "")

	_, used := e.newCode(CreateInput{})
	e.claim(h, used)
	if st, r := e.claim(other, used); st != 403 || r.Code != protocol.EnrollCodeUsed {
		t.Fatalf("used: %d %+v", st, r)
	}

	exp, expired := e.newCode(CreateInput{TTL: time.Minute})
	e.advance(61 * time.Second)
	if st, r := e.claim(h, expired); st != 403 || r.Code != protocol.EnrollCodeExpired {
		t.Fatalf("expired: %d %+v", st, r)
	}
	if again, _ := e.reg.GetEnrollment(ctx, exp.ID); !again.ExpiresAt.Equal(exp.ExpiresAt) || again.State != EnrollmentExpired {
		t.Fatalf("expired code changed: %+v", again)
	}
	c, _ := protocol.ParseEnrollmentCode(expired)
	if _, err := e.reg.Discovery(ctx, c.ID(), "203.0.113.61"); !errors.Is(err, ErrCodeNotFound) {
		t.Fatalf("discovery of an expired code: %v", err)
	}

	rev, revoked := e.newCode(CreateInput{})
	if _, err := e.reg.RevokeEnrollment(ctx, rev.ID, "usr_owner"); err != nil {
		t.Fatal(err)
	}
	if st, r := e.claim(h, revoked); st != 403 || r.Code != protocol.EnrollCodeRevoked {
		t.Fatalf("revoked: %d %+v", st, r)
	}

	grp, grouped := e.newCode(CreateInput{Group: "prod"})
	req := h.request(grouped)
	req.Group = "lab"
	if st, r := e.reg.Claim(ctx, req, "203.0.113.61"); st != 403 || r.Code != protocol.EnrollGroupMismatch {
		t.Fatalf("group: %d %+v", st, r)
	}
	if g, _ := e.reg.GetEnrollment(ctx, grp.ID); g.State != EnrollmentWaiting {
		t.Fatalf("a refused group consumed the code: %+v", g)
	}

	// Right ID, wrong secret.
	cc, _ := protocol.ParseEnrollmentCode(grouped)
	wrong := "HZX-" + cc.ID() + "-0000-0000-0000"
	if st, r := e.claim(h, wrong); st != 403 || r.Code != protocol.EnrollCodeInvalid {
		t.Fatalf("wrong secret: %d %+v", st, r)
	}
	if st, r := e.claim(h, "garbage"); st != 400 || r.Code != protocol.EnrollRequestInvalid {
		t.Fatalf("garbage: %d %+v", st, r)
	}
	if list, _ := e.reg.ListNodes(ctx, NodeFilter{}); len(list) != 0 {
		t.Fatalf("refused codes created %d nodes", len(list))
	}
}

func TestRejectedHostCannotRetry(t *testing.T) {
	e := newEnv(t)
	en, code := e.newCode(CreateInput{})
	h := newHost("x", "")
	e.claim(h, code)
	if _, err := e.reg.Reject(context.Background(), en.ID, "usr_owner"); err != nil {
		t.Fatal(err)
	}
	if st, r := e.claim(h, code); st != 403 || r.Code != protocol.EnrollRejected {
		t.Fatalf("after reject: %d %+v", st, r)
	}
	if _, err := e.reg.Approve(context.Background(), en.ID, ApproveInput{Actor: "usr_owner"}); !errors.Is(err, ErrState) {
		t.Fatalf("approve after reject: %v", err)
	}
}

func TestPendingApprovalExpires(t *testing.T) {
	e := newEnv(t)
	en, code := e.newCode(CreateInput{})
	h := newHost("x", "")
	e.claim(h, code)
	e.advance(24*time.Hour + time.Second)
	if st, r := e.claim(h, code); st != 403 || r.Code != protocol.EnrollExpired {
		t.Fatalf("stale pending: %d %+v", st, r)
	}
	if _, err := e.reg.Approve(context.Background(), en.ID, ApproveInput{Actor: "usr_owner"}); !errors.Is(err, ErrState) {
		t.Fatalf("approve after expiry: %v", err)
	}
}

func TestWrongCodesFromOneSourceAreLimited(t *testing.T) {
	e := newEnv(t)
	h := newHost("x", "")
	var last int
	for range 21 {
		last, _ = e.claim(h, "HZX-AAAA-0000-0000-0000")
	}
	if last != 429 {
		t.Fatalf("21st wrong code answered %d", last)
	}
}

// Same key enrolling again (NOD-006 re-register) keeps the node; a new key
// on the same machine-id must be told apart by the administrator.
func TestReEnrollment(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h := newHost("web-1", "aaaa1111")
	n := e.enroll(h, CreateInput{Name: "web-1"}, ApproveInput{})
	again := e.enroll(h, CreateInput{}, ApproveInput{})
	if again.ID != n.ID || again.Certificate.Serial == n.Certificate.Serial || again.Name != "web-1" {
		t.Fatalf("same key: %+v vs %+v", again, n)
	}

	rekeyed := newHost("web-1", "aaaa1111")
	en, code := e.newCode(CreateInput{})
	e.claim(rekeyed, code)
	pending, _ := e.reg.GetEnrollment(ctx, en.ID)
	if len(pending.MatchingNodes) != 1 || pending.MatchingNodes[0].NodeID != n.ID || pending.MatchingNodes[0].Reason != MatchMachineID {
		t.Fatalf("matches: %+v", pending.MatchingNodes)
	}
	_, err := e.reg.Approve(ctx, en.ID, ApproveInput{Actor: "usr_owner"})
	var choice *ChoiceRequiredError
	if !errors.As(err, &choice) {
		t.Fatalf("approve without a choice: %v", err)
	}
	replaced, err := e.reg.Approve(ctx, en.ID, ApproveInput{Actor: "usr_owner", ReplaceNodeID: n.ID})
	if err != nil || replaced.ID != n.ID || replaced.Fingerprint != rekeyed.fp {
		t.Fatalf("replace: %+v %v", replaced, err)
	}
	if err := e.reg.AuthorizeNode(ctx, identity(h.fp, again.Certificate.Serial)); !errors.Is(err, nodes.ErrNodeRevoked) {
		t.Fatalf("old key after replacement: %v", err)
	}
	// The old key cannot enroll again either.
	_, code2 := e.newCode(CreateInput{})
	if st, r := e.claim(h, code2); st != 403 || r.Code != protocol.EnrollNodeKeyRevoked {
		t.Fatalf("retired key enrolling: %d %+v", st, r)
	}
	if list, _ := e.reg.ListNodes(ctx, NodeFilter{}); len(list) != 1 {
		t.Fatalf("%d nodes after re-enrollment", len(list))
	}

	// "Create a new node" is the other answer.
	clone := newHost("web-1", "aaaa1111")
	en3, code3 := e.newCode(CreateInput{})
	e.claim(clone, code3)
	second, err := e.reg.Approve(ctx, en3.ID, ApproveInput{Actor: "usr_owner", AsNewNode: true})
	if err != nil || second.ID == n.ID {
		t.Fatalf("as new node: %+v %v", second, err)
	}
}

func TestCreateEnrollmentValidates(t *testing.T) {
	e := newEnv(t)
	for _, in := range []CreateInput{
		{Actor: "u", TTL: 30 * time.Second},
		{Actor: "u", TTL: 61 * time.Minute},
		{Actor: "u", Name: strings.Repeat("x", 101)},
		{Actor: "u", Tags: []string{"has space"}},
		{Actor: "u", Group: strings.Repeat("g", 65)},
	} {
		if _, _, err := e.reg.CreateEnrollment(context.Background(), in); !errors.Is(err, ErrValidation) {
			t.Errorf("%+v accepted: %v", in, err)
		}
	}
}
