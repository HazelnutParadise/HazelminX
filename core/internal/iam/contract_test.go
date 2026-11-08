package iam

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/HazelnutParadise/hazelminx/core/internal/paging"
)

func TestUserVersionAdvancesOnEveryChange(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	owner := setupOwner(t, svc)
	ownerP, err := svc.PrincipalByID(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	src := Source{IP: "10.0.0.1"}
	u, err := svc.CreateUser(ctx, ownerP, CreateUserInput{Username: "vic", DisplayName: "Vic", Password: "vic-password-123"}, src)
	if err != nil || u.Version != 1 {
		t.Fatalf("create: %+v %v", u, err)
	}
	name := "Victor"
	d, err := svc.UpdateUser(ctx, ownerP, u.ID, 1, UpdateUserInput{DisplayName: &name}, src)
	if err != nil || d.Version != 2 || d.DisplayName != "Victor" {
		t.Fatalf("update: %+v %v", d, err)
	}
	other := "Stale"
	_, err = svc.UpdateUser(ctx, ownerP, u.ID, 1, UpdateUserInput{DisplayName: &other}, src)
	var vm *VersionMismatchError
	if !errors.Is(err, ErrVersionMismatch) || !errors.As(err, &vm) || vm.Current != 2 {
		t.Fatalf("stale update: %v", err)
	}
	if got, _ := svc.GetUser(ctx, ownerP, u.ID); got.DisplayName != "Victor" {
		t.Fatal("a stale update changed the account")
	}
	as, ver, err := svc.AssignRoleIf(ctx, ownerP, u.ID, RoleAuditor, Global, 2, src)
	if err != nil || ver != 3 {
		t.Fatalf("assign: version %d, %v", ver, err)
	}
	if got, _ := svc.GetUser(ctx, ownerP, u.ID); got.Version != 3 {
		t.Fatalf("assign must advance the version: %d", got.Version)
	}
	if _, err := svc.RevokeRoleIf(ctx, ownerP, as.ID, u.ID, 2, src); !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("stale revoke: %v", err)
	}
	if _, err := svc.RevokeRoleIf(ctx, ownerP, as.ID, "someone-else", 0, src); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke through another user's path: %v", err)
	}
	if ver, err := svc.RevokeRoleIf(ctx, ownerP, as.ID, u.ID, 3, src); err != nil || ver != 4 {
		t.Fatalf("revoke: version %d, %v", ver, err)
	}
	if got, _ := svc.GetUser(ctx, ownerP, u.ID); got.Version != 4 || len(got.Assignments) != 0 {
		t.Fatalf("revoke: %+v", got)
	}
	empty := UpdateUserInput{}
	if _, err := svc.UpdateUser(ctx, ownerP, u.ID, 4, empty, src); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty update: %v", err)
	}
	if _, err := svc.UpdateUser(ctx, principalWith(RoleAuditor, Global), u.ID, 4, UpdateUserInput{DisplayName: &name}, src); !errors.Is(err, ErrForbidden) {
		t.Fatalf("auditor update: %v", err)
	}
	// The audit ledger has the update with before and after.
	evs, _ := svc.ListAudit(ctx, ownerP, AuditFilter{ResourceID: u.ID})
	found := false
	for _, ev := range evs {
		if ev.Action == "user.update" && ev.Before["displayName"] == "Vic" && ev.After["displayName"] == "Victor" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no user.update audit event: %+v", evs)
	}
}

func TestUserAndAuditKeysetPages(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	owner := setupOwner(t, svc)
	ownerP, _ := svc.PrincipalByID(ctx, owner.ID)
	for i := 0; i < 6; i++ {
		if _, err := svc.CreateUser(ctx, ownerP, CreateUserInput{Username: fmt.Sprintf("user%d", i), Password: "user-password-123"}, Source{}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := svc.CountUsers(ctx, ownerP); err != nil || n != 7 {
		t.Fatalf("count: %d %v", n, err)
	}
	q := paging.Query{Sort: []paging.SortKey{{Field: "username", Desc: true}, {Field: "id", Desc: true}}, Limit: 3}
	var names []string
	for {
		page, err := svc.ListUsersPage(ctx, ownerP, q)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range page {
			names = append(names, u.Username)
		}
		if len(page) < q.Limit {
			break
		}
		last := page[len(page)-1]
		q.After = []string{last.SortValue("username"), last.SortValue("id")}
	}
	want := []string{"user5", "user4", "user3", "user2", "user1", "user0", "owner"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("pages: %v", names)
	}
	if _, err := svc.CountUsers(ctx, principalWith(RoleHostOperator, Scope{Kind: ScopeNode, ID: "n1"})); !errors.Is(err, ErrForbidden) {
		t.Fatalf("count without users:read: %v", err)
	}
	all, _ := svc.ListAudit(ctx, ownerP, AuditFilter{})
	n, err := svc.CountAudit(ctx, ownerP, AuditFilter{})
	if err != nil || n != len(all) || n < 7 {
		t.Fatalf("audit count %d vs %d: %v", n, len(all), err)
	}
	newest, err := svc.ListAudit(ctx, ownerP, AuditFilter{Page: paging.Query{Sort: []paging.SortKey{{Field: "seq", Desc: true}}, Limit: 2}})
	if err != nil || len(newest) != 2 || newest[0].Seq <= newest[1].Seq || newest[0].Seq != all[len(all)-1].Seq {
		t.Fatalf("newest first: %+v %v", newest, err)
	}
	older, _ := svc.ListAudit(ctx, ownerP, AuditFilter{Page: paging.Query{Sort: []paging.SortKey{{Field: "seq", Desc: true}}, Limit: 2, After: []string{newest[1].SortValue("seq")}}})
	if len(older) != 2 || older[0].Seq >= newest[1].Seq {
		t.Fatalf("keyset continues: %+v", older)
	}
	// afterSeq still filters a keyset page.
	tail, _ := svc.ListAudit(ctx, ownerP, AuditFilter{AfterSeq: all[len(all)-2].Seq, Page: paging.Query{Sort: []paging.SortKey{{Field: "seq"}}, Limit: 50}})
	if len(tail) != 1 {
		t.Fatalf("afterSeq filter: %+v", tail)
	}
}

func TestPrincipalByIDDropsAssignmentsOfDisabledAccounts(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	owner := setupOwner(t, svc)
	p, err := svc.PrincipalByID(ctx, owner.ID)
	if err != nil || len(p.Assignments) == 0 {
		t.Fatalf("owner: %+v %v", p, err)
	}
	if _, err := svc.db.Exec(`UPDATE users SET status='disabled' WHERE id=?`, owner.ID); err != nil {
		t.Fatal(err)
	}
	if p, _ := svc.PrincipalByID(ctx, owner.ID); len(p.Assignments) != 0 {
		t.Fatalf("disabled account keeps permissions: %+v", p)
	}
	if _, err := svc.PrincipalByID(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}
