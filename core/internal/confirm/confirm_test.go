package confirm

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

var idPattern = regexp.MustCompile(`^cfm_[0-9a-f]{24}$`)

var nodeNetN1 = []jobs.ResourceRef{{Kind: "node-network", ID: "n1"}}

const missingID = "cfm_000000000000000000000000"

// fakeClock is a controllable clock. step, when non-zero, moves the time on
// every read so concurrent callers can be told apart.
type fakeClock struct {
	mu   sync.Mutex
	now  time.Time
	step time.Duration
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.now
	c.now = c.now.Add(c.step)
	return t
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// guard turns a panic from a method that is still a TODO into a test failure
// instead of a crash of the whole test binary.
func guard(t *testing.T) {
	t.Helper()
	if r := recover(); r != nil {
		t.Fatalf("panic: %v", r)
	}
}

type fixture struct {
	t     *testing.T
	db    *sql.DB
	svc   *Service
	clock *fakeClock
	base  time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	base := time.Now().UTC().Truncate(time.Second)
	clock := &fakeClock{now: base}
	f := &fixture{t: t, db: st.DB, svc: New(st.DB, clock.Now), clock: clock, base: base}
	f.addUser("usr_owner", "owner")
	f.addUser("usr_other", "other")
	return f
}

func (f *fixture) addUser(id, username string) {
	f.t.Helper()
	ts := f.base.Format(time.RFC3339)
	_, err := f.db.Exec(
		`INSERT INTO users(id,username,display_name,password_hash,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		id, username, username, "test-hash-not-a-password", "active", ts, ts)
	if err != nil {
		f.t.Fatalf("insert user %s: %v", id, err)
	}
}

// input is a valid CreateInput for usr_owner, applied at base.
func (f *fixture) input(window time.Duration) CreateInput {
	return CreateInput{
		UserID:    "usr_owner",
		Summary:   "change listen port",
		Resources: nodeNetN1,
		JobID:     "job_test_1",
		AppliedAt: f.base,
		Window:    window,
	}
}

// okAttempt is a request from the owner on a connection opened after the change.
func (f *fixture) okAttempt() Attempt {
	return Attempt{UserID: "usr_owner", ConnOpenedAt: f.base.Add(time.Second)}
}

func (f *fixture) create(in CreateInput) *Confirmation {
	f.t.Helper()
	c, err := f.svc.Create(context.Background(), in)
	if err != nil {
		f.t.Fatalf("Create: %v", err)
	}
	return c
}

type rowSnapshot struct {
	State       string
	AppliedAt   string
	ExpiresAt   string
	UpdatedAt   string
	ConfirmedAt sql.NullString
}

// snapshot reads the stored row without going through the Service.
func (f *fixture) snapshot(id string) rowSnapshot {
	f.t.Helper()
	var s rowSnapshot
	err := f.db.QueryRow(
		`SELECT state, applied_at, expires_at, updated_at, confirmed_at FROM confirmations WHERE id = ?`, id,
	).Scan(&s.State, &s.AppliedAt, &s.ExpiresAt, &s.UpdatedAt, &s.ConfirmedAt)
	if err != nil {
		f.t.Fatalf("read confirmations row %s: %v", id, err)
	}
	return s
}

func (f *fixture) rowCount() int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM confirmations`).Scan(&n); err != nil {
		f.t.Fatalf("count confirmations: %v", err)
	}
	return n
}

// stored parses a timestamp as written in the database (RFC 3339).
func (f *fixture) stored(raw string) time.Time {
	f.t.Helper()
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		f.t.Fatalf("stored time %q is not RFC 3339: %v", raw, err)
	}
	return ts
}

// setState moves a row to a state directly, for tests that do not exercise
// the transition itself.
func (f *fixture) setState(id, state string, updatedAt time.Time, confirmedAt *time.Time) {
	f.t.Helper()
	var conf any
	if confirmedAt != nil {
		conf = confirmedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := f.db.Exec(
		`UPDATE confirmations SET state = ?, updated_at = ?, confirmed_at = ? WHERE id = ?`,
		state, updatedAt.UTC().Format(time.RFC3339Nano), conf, id)
	if err != nil {
		f.t.Fatalf("set state %s: %v", state, err)
	}
}

func TestCreate(t *testing.T) {
	t.Run("returns pending record with generated id, window expiry and now() as created time", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		f.clock.Set(f.base.Add(5 * time.Second))
		c := f.create(f.input(10 * time.Minute))

		if !idPattern.MatchString(c.ID) {
			t.Fatalf("ID = %q, want cfm_ followed by 24 lowercase hex characters", c.ID)
		}
		c2 := f.create(f.input(10 * time.Minute))
		if c2.ID == c.ID {
			t.Fatalf("two Create calls returned the same ID %q", c.ID)
		}
		if c.State != StatePending {
			t.Fatalf("State = %q, want %q", c.State, StatePending)
		}
		if want := f.base.Add(10 * time.Minute); !c.ExpiresAt.Equal(want) {
			t.Fatalf("ExpiresAt = %v, want AppliedAt + Window = %v", c.ExpiresAt, want)
		}
		if want := f.base.Add(5 * time.Second); !c.CreatedAt.Equal(want) || !c.UpdatedAt.Equal(want) {
			t.Fatalf("CreatedAt = %v, UpdatedAt = %v, want both now() = %v", c.CreatedAt, c.UpdatedAt, want)
		}
		if c.ConfirmedAt != nil {
			t.Fatalf("ConfirmedAt = %v, want nil", c.ConfirmedAt)
		}
		if c.UserID != "usr_owner" || c.JobID != "job_test_1" {
			t.Fatalf("UserID = %q JobID = %q, want usr_owner and job_test_1", c.UserID, c.JobID)
		}
		if !reflect.DeepEqual(c.Resources, nodeNetN1) {
			t.Fatalf("Resources = %+v, want %+v", c.Resources, nodeNetN1)
		}
	})

	t.Run("stores times as UTC RFC 3339 and keeps nanoseconds", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		applied := f.base.Add(123456789 * time.Nanosecond)
		in := f.input(90 * time.Second)
		in.AppliedAt = applied.In(time.FixedZone("UTC+8", 8*60*60))
		c := f.create(in)

		wantExpires := applied.Add(90 * time.Second)
		if !c.AppliedAt.Equal(applied) || c.AppliedAt.Location() != time.UTC {
			t.Fatalf("AppliedAt = %v (%v), want %v in UTC", c.AppliedAt, c.AppliedAt.Location(), applied)
		}
		if !c.ExpiresAt.Equal(wantExpires) || c.ExpiresAt.Location() != time.UTC {
			t.Fatalf("ExpiresAt = %v (%v), want %v in UTC", c.ExpiresAt, c.ExpiresAt.Location(), wantExpires)
		}
		snap := f.snapshot(c.ID)
		if !strings.HasSuffix(snap.AppliedAt, "Z") || !f.stored(snap.AppliedAt).Equal(applied) {
			t.Fatalf("stored applied_at = %q, want RFC 3339 UTC keeping nanoseconds for %v", snap.AppliedAt, applied)
		}
		if !strings.HasSuffix(snap.ExpiresAt, "Z") || !f.stored(snap.ExpiresAt).Equal(wantExpires) {
			t.Fatalf("stored expires_at = %q, want RFC 3339 UTC for %v", snap.ExpiresAt, wantExpires)
		}
	})

	t.Run("rejects incomplete input with ErrInvalid and stores nothing", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*CreateInput)
			field  string
		}{
			{"empty UserID", func(in *CreateInput) { in.UserID = "" }, "user"},
			{"nil Resources", func(in *CreateInput) { in.Resources = nil }, "resource"},
			{"empty Resources", func(in *CreateInput) { in.Resources = []jobs.ResourceRef{} }, "resource"},
			{"zero AppliedAt", func(in *CreateInput) { in.AppliedAt = time.Time{} }, "applied"},
			{"zero Window", func(in *CreateInput) { in.Window = 0 }, "window"},
			{"negative Window", func(in *CreateInput) { in.Window = -time.Second }, "window"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				defer guard(t)
				f := newFixture(t)
				in := f.input(10 * time.Minute)
				tc.mutate(&in)
				_, err := f.svc.Create(context.Background(), in)
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("err = %v, want ErrInvalid", err)
				}
				if !strings.Contains(strings.ToLower(err.Error()), tc.field) {
					t.Fatalf("err = %q, want the message to name the field (%q)", err, tc.field)
				}
				if n := f.rowCount(); n != 0 {
					t.Fatalf("%d rows stored, want 0", n)
				}
			})
		}
	})

	t.Run("unknown UserID fails on the foreign key and stores nothing", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		in := f.input(10 * time.Minute)
		in.UserID = "usr_missing"
		_, err := f.svc.Create(context.Background(), in)
		if err == nil {
			t.Fatal("Create with an unknown UserID succeeded")
		}
		if errors.Is(err, ErrInvalid) {
			t.Fatalf("err = %v, want the foreign key error, not ErrInvalid", err)
		}
		if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
			t.Fatalf("err = %q, want the foreign key error", err)
		}
		if n := f.rowCount(); n != 0 {
			t.Fatalf("%d rows stored, want 0", n)
		}
	})
}

func TestGet(t *testing.T) {
	t.Run("owner sees the record; other user and unknown id get ErrNotFound", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		c := f.create(f.input(10 * time.Minute))

		got, err := f.svc.Get(context.Background(), c.ID, "usr_owner")
		if err != nil {
			t.Fatalf("owner Get: %v", err)
		}
		if got.ID != c.ID || got.State != StatePending || !got.ExpiresAt.Equal(c.ExpiresAt) {
			t.Fatalf("owner Get = %+v, want the created pending record", got)
		}
		if _, err := f.svc.Get(context.Background(), c.ID, "usr_other"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("other user Get err = %v, want ErrNotFound", err)
		}
		if _, err := f.svc.Get(context.Background(), missingID, "usr_owner"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown id Get err = %v, want ErrNotFound", err)
		}
	})

	t.Run("pending past ExpiresAt returns expired and persists it", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		c := f.create(f.input(10 * time.Minute))
		now := c.ExpiresAt.Add(time.Second)
		f.clock.Set(now)

		got, err := f.svc.Get(context.Background(), c.ID, "usr_owner")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.State != StateExpired {
			t.Fatalf("State = %q, want %q", got.State, StateExpired)
		}
		snap := f.snapshot(c.ID)
		if snap.State != string(StateExpired) {
			t.Fatalf("stored state = %q, want expired", snap.State)
		}
		if !f.stored(snap.UpdatedAt).Equal(now) {
			t.Fatalf("stored updated_at = %q, want now() = %v", snap.UpdatedAt, now)
		}
	})

	t.Run("pending exactly at ExpiresAt stays pending", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		c := f.create(f.input(10 * time.Minute))
		f.clock.Set(c.ExpiresAt)

		got, err := f.svc.Get(context.Background(), c.ID, "usr_owner")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.State != StatePending {
			t.Fatalf("State = %q, want pending at the exact expiry instant", got.State)
		}
		if snap := f.snapshot(c.ID); snap.State != string(StatePending) {
			t.Fatalf("stored state = %q, want pending", snap.State)
		}
	})

	t.Run("stored confirmed and rolled_back are returned as stored, even past ExpiresAt", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		confirmed := f.create(f.input(10 * time.Minute))
		rolled := f.create(f.input(10 * time.Minute))
		ct := f.base.Add(time.Minute)
		f.setState(confirmed.ID, "confirmed", ct, &ct)
		f.setState(rolled.ID, "rolled_back", ct, nil)
		f.clock.Set(f.base.Add(time.Hour))

		got, err := f.svc.Get(context.Background(), confirmed.ID, "usr_owner")
		if err != nil {
			t.Fatalf("Get confirmed: %v", err)
		}
		if got.State != StateConfirmed || got.ConfirmedAt == nil || !got.ConfirmedAt.Equal(ct) {
			t.Fatalf("Get confirmed = state %q ConfirmedAt %v, want confirmed at %v", got.State, got.ConfirmedAt, ct)
		}
		got, err = f.svc.Get(context.Background(), rolled.ID, "usr_owner")
		if err != nil {
			t.Fatalf("Get rolled_back: %v", err)
		}
		if got.State != StateRolledBack {
			t.Fatalf("State = %q, want rolled_back", got.State)
		}
		if snap := f.snapshot(rolled.ID); snap.State != "rolled_back" {
			t.Fatalf("stored state = %q, want rolled_back kept", snap.State)
		}
	})
}

func TestConfirm(t *testing.T) {
	t.Run("confirms inside the window with ConfirmedAt equal to UpdatedAt", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		c := f.create(f.input(10 * time.Minute))
		now := f.base.Add(2 * time.Minute)
		f.clock.Set(now)

		got, err := f.svc.Confirm(context.Background(), c.ID, f.okAttempt())
		if err != nil {
			t.Fatalf("Confirm: %v", err)
		}
		if got.State != StateConfirmed {
			t.Fatalf("State = %q, want confirmed", got.State)
		}
		if got.ConfirmedAt == nil || !got.ConfirmedAt.Equal(now) || !got.UpdatedAt.Equal(now) {
			t.Fatalf("ConfirmedAt = %v, UpdatedAt = %v, want both %v", got.ConfirmedAt, got.UpdatedAt, now)
		}
		snap := f.snapshot(c.ID)
		if snap.State != string(StateConfirmed) || !snap.ConfirmedAt.Valid || !f.stored(snap.ConfirmedAt.String).Equal(now) {
			t.Fatalf("stored row = %+v, want confirmed at %v", snap, now)
		}
	})

	t.Run("other user or unknown id gets ErrNotFound and changes nothing", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		c := f.create(f.input(10 * time.Minute))
		before := f.snapshot(c.ID)
		f.clock.Set(f.base.Add(time.Minute))

		other := f.okAttempt()
		other.UserID = "usr_other"
		if _, err := f.svc.Confirm(context.Background(), c.ID, other); !errors.Is(err, ErrNotFound) {
			t.Fatalf("other user err = %v, want ErrNotFound", err)
		}
		if _, err := f.svc.Confirm(context.Background(), missingID, f.okAttempt()); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown id err = %v, want ErrNotFound", err)
		}
		if after := f.snapshot(c.ID); after != before {
			t.Fatalf("row changed: before %+v, after %+v", before, after)
		}
	})

	t.Run("confirmed record answers the same again and ignores later attempts", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		in := f.input(10 * time.Minute)
		in.RequireHost = "panel.example.com"
		c := f.create(in)
		f.clock.Set(f.base.Add(time.Minute))
		first, err := f.svc.Confirm(context.Background(), c.ID, Attempt{
			UserID: "usr_owner", ConnOpenedAt: f.base.Add(time.Second), Host: "panel.example.com",
		})
		if err != nil {
			t.Fatalf("first Confirm: %v", err)
		}
		if first.ConfirmedAt == nil {
			t.Fatal("first Confirm returned no ConfirmedAt")
		}
		before := f.snapshot(c.ID)

		f.clock.Set(f.base.Add(3 * time.Minute))
		again, err := f.svc.Confirm(context.Background(), c.ID, Attempt{
			UserID: "usr_owner", ConnOpenedAt: f.base.Add(2 * time.Minute), Host: "evil.example.net",
		})
		if err != nil {
			t.Fatalf("retried Confirm with a would-be rejected attempt: %v", err)
		}
		if again.State != StateConfirmed || again.ConfirmedAt == nil || !again.ConfirmedAt.Equal(*first.ConfirmedAt) {
			t.Fatalf("retry = state %q ConfirmedAt %v, want confirmed at %v", again.State, again.ConfirmedAt, *first.ConfirmedAt)
		}
		if after := f.snapshot(c.ID); after != before {
			t.Fatalf("row changed on retry: before %+v, after %+v", before, after)
		}
	})

	t.Run("pending past its window gives ErrExpired and is persisted as expired", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		c := f.create(f.input(10 * time.Minute))
		now := c.ExpiresAt.Add(time.Second)
		f.clock.Set(now)

		_, err := f.svc.Confirm(context.Background(), c.ID, f.okAttempt())
		if !errors.Is(err, ErrExpired) {
			t.Fatalf("err = %v, want ErrExpired", err)
		}
		snap := f.snapshot(c.ID)
		if snap.State != string(StateExpired) || !f.stored(snap.UpdatedAt).Equal(now) {
			t.Fatalf("stored row = %+v, want expired with updated_at %v", snap, now)
		}
	})

	t.Run("attempt exactly at ExpiresAt is accepted", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		c := f.create(f.input(10 * time.Minute))
		f.clock.Set(c.ExpiresAt)
		got, err := f.svc.Confirm(context.Background(), c.ID, f.okAttempt())
		if err != nil {
			t.Fatalf("Confirm at ExpiresAt: %v", err)
		}
		if got.State != StateConfirmed {
			t.Fatalf("State = %q, want confirmed", got.State)
		}
	})

	t.Run("attempt one nanosecond after ExpiresAt is expired", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		c := f.create(f.input(10 * time.Minute))
		f.clock.Set(c.ExpiresAt.Add(time.Nanosecond))
		if _, err := f.svc.Confirm(context.Background(), c.ID, f.okAttempt()); !errors.Is(err, ErrExpired) {
			t.Fatalf("err = %v, want ErrExpired", err)
		}
	})

	t.Run("rolled_back gives ErrExpired and changes nothing", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		c := f.create(f.input(10 * time.Minute))
		f.setState(c.ID, "rolled_back", f.base.Add(30*time.Second), nil)
		before := f.snapshot(c.ID)
		f.clock.Set(f.base.Add(time.Minute))

		if _, err := f.svc.Confirm(context.Background(), c.ID, f.okAttempt()); !errors.Is(err, ErrExpired) {
			t.Fatalf("err = %v, want ErrExpired", err)
		}
		if after := f.snapshot(c.ID); after != before {
			t.Fatalf("row changed: before %+v, after %+v", before, after)
		}
	})

	t.Run("connection opened at or before AppliedAt is rejected", func(t *testing.T) {
		cases := []struct {
			name   string
			offset time.Duration
		}{
			{"equal to AppliedAt", 0},
			{"one second before AppliedAt", -time.Second},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				defer guard(t)
				f := newFixture(t)
				c := f.create(f.input(10 * time.Minute))
				before := f.snapshot(c.ID)
				f.clock.Set(f.base.Add(time.Minute))

				_, err := f.svc.Confirm(context.Background(), c.ID, Attempt{
					UserID: "usr_owner", ConnOpenedAt: f.base.Add(tc.offset),
				})
				if !errors.Is(err, ErrNewConnectionRequired) {
					t.Fatalf("err = %v, want ErrNewConnectionRequired", err)
				}
				if !strings.Contains(err.Error(), "connection opened before the change") {
					t.Fatalf("err = %q, want the message to say the connection opened before the change", err)
				}
				if after := f.snapshot(c.ID); after != before {
					t.Fatalf("row changed: before %+v, after %+v", before, after)
				}
			})
		}
	})

	t.Run("connection one nanosecond after AppliedAt is accepted", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		c := f.create(f.input(10 * time.Minute))
		f.clock.Set(f.base.Add(time.Minute))
		_, err := f.svc.Confirm(context.Background(), c.ID, Attempt{
			UserID: "usr_owner", ConnOpenedAt: f.base.Add(time.Nanosecond),
		})
		if err != nil {
			t.Fatalf("Confirm: %v", err)
		}
	})

	t.Run("RequireHost is matched ignoring case and the :port suffix", func(t *testing.T) {
		cases := []struct {
			name    string
			require string
			host    string
			ok      bool
		}{
			{"different host", "panel.example.com", "other.example.com", false},
			{"host with a longer prefix", "panel.example.com", "x.panel.example.com", false},
			{"suffix trick", "panel.example.com", "panel.example.com.evil.net", false},
			{"different host with a port", "panel.example.com", "other.example.com:8443", false},
			{"empty host", "panel.example.com", "", false},
			{"case differs", "Panel.Example.com", "PANEL.example.COM", true},
			{"request carries a port", "panel.example.com", "panel.example.com:8443", true},
			{"required value carries a port", "panel.example.com:8443", "panel.example.com", true},
			{"port differs on both sides", "panel.example.com:8443", "PANEL.example.com:9443", true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				defer guard(t)
				f := newFixture(t)
				in := f.input(10 * time.Minute)
				in.RequireHost = tc.require
				c := f.create(in)
				before := f.snapshot(c.ID)
				f.clock.Set(f.base.Add(time.Minute))

				got, err := f.svc.Confirm(context.Background(), c.ID, Attempt{
					UserID: "usr_owner", ConnOpenedAt: f.base.Add(time.Second), Host: tc.host,
				})
				if !tc.ok {
					if !errors.Is(err, ErrNewConnectionRequired) {
						t.Fatalf("err = %v, want ErrNewConnectionRequired", err)
					}
					if !strings.Contains(err.Error(), "confirm through "+tc.require) {
						t.Fatalf("err = %q, want the message to name %q", err, tc.require)
					}
					if after := f.snapshot(c.ID); after != before {
						t.Fatalf("row changed: before %+v, after %+v", before, after)
					}
					return
				}
				if err != nil {
					t.Fatalf("Confirm: %v", err)
				}
				if got.State != StateConfirmed {
					t.Fatalf("State = %q, want confirmed", got.State)
				}
			})
		}
	})

	t.Run("RequireLocalAddr must match exactly", func(t *testing.T) {
		cases := []struct {
			name      string
			require   string
			localAddr string
			ok        bool
		}{
			{"other port", "10.0.0.5:8443", "10.0.0.5:8080", false},
			{"other address", "10.0.0.5:8443", "10.0.0.6:8443", false},
			{"empty local address", "10.0.0.5:8443", "", false},
			{"exact match", "10.0.0.5:8443", "10.0.0.5:8443", true},
			{"not required", "", "10.0.0.9:9", true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				defer guard(t)
				f := newFixture(t)
				in := f.input(10 * time.Minute)
				in.RequireLocalAddr = tc.require
				c := f.create(in)
				before := f.snapshot(c.ID)
				f.clock.Set(f.base.Add(time.Minute))

				_, err := f.svc.Confirm(context.Background(), c.ID, Attempt{
					UserID: "usr_owner", ConnOpenedAt: f.base.Add(time.Second), LocalAddr: tc.localAddr,
				})
				if !tc.ok {
					if !errors.Is(err, ErrNewConnectionRequired) {
						t.Fatalf("err = %v, want ErrNewConnectionRequired", err)
					}
					if !strings.Contains(err.Error(), "confirm through "+tc.require) {
						t.Fatalf("err = %q, want the message to name %q", err, tc.require)
					}
					if after := f.snapshot(c.ID); after != before {
						t.Fatalf("row changed: before %+v, after %+v", before, after)
					}
					return
				}
				if err != nil {
					t.Fatalf("Confirm: %v", err)
				}
			})
		}
	})

	t.Run("expiry is checked before the connection rule", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		c := f.create(f.input(10 * time.Minute))
		f.clock.Set(c.ExpiresAt.Add(time.Second))
		_, err := f.svc.Confirm(context.Background(), c.ID, Attempt{
			UserID: "usr_owner", ConnOpenedAt: f.base.Add(-time.Hour),
		})
		if !errors.Is(err, ErrExpired) {
			t.Fatalf("err = %v, want ErrExpired", err)
		}
	})

	t.Run("two concurrent attempts confirm once with the same ConfirmedAt", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		f.clock.step = time.Millisecond
		for round := 0; round < 20; round++ {
			c := f.create(f.input(10 * time.Minute))

			type result struct {
				c   *Confirmation
				err error
				p   any
			}
			start := make(chan struct{})
			out := make(chan result, 2)
			for i := 0; i < 2; i++ {
				go func() {
					var r result
					defer func() {
						r.p = recover()
						out <- r
					}()
					<-start
					r.c, r.err = f.svc.Confirm(context.Background(), c.ID, f.okAttempt())
				}()
			}
			close(start)
			a, b := <-out, <-out

			for _, r := range []result{a, b} {
				if r.p != nil {
					t.Fatalf("round %d: Confirm panicked: %v", round, r.p)
				}
				if r.err != nil {
					t.Fatalf("round %d: Confirm: %v", round, r.err)
				}
				if r.c == nil || r.c.State != StateConfirmed || r.c.ConfirmedAt == nil {
					t.Fatalf("round %d: result = %+v, want confirmed with ConfirmedAt", round, r.c)
				}
			}
			if !a.c.ConfirmedAt.Equal(*b.c.ConfirmedAt) {
				t.Fatalf("round %d: ConfirmedAt differs: %v and %v", round, *a.c.ConfirmedAt, *b.c.ConfirmedAt)
			}
			snap := f.snapshot(c.ID)
			if !snap.ConfirmedAt.Valid || !f.stored(snap.ConfirmedAt.String).Equal(*a.c.ConfirmedAt) {
				t.Fatalf("round %d: stored confirmed_at = %+v, want %v", round, snap.ConfirmedAt, *a.c.ConfirmedAt)
			}
		}
	})
}

func TestMarkRolledBack(t *testing.T) {
	t.Run("pending becomes rolled_back with UpdatedAt now", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		c := f.create(f.input(10 * time.Minute))
		now := f.base.Add(time.Minute)
		f.clock.Set(now)

		if err := f.svc.MarkRolledBack(context.Background(), c.ID); err != nil {
			t.Fatalf("MarkRolledBack: %v", err)
		}
		snap := f.snapshot(c.ID)
		if snap.State != "rolled_back" || !f.stored(snap.UpdatedAt).Equal(now) {
			t.Fatalf("stored row = %+v, want rolled_back at %v", snap, now)
		}
	})

	t.Run("expired becomes rolled_back", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		c := f.create(f.input(10 * time.Minute))
		f.setState(c.ID, "expired", f.base.Add(11*time.Minute), nil)
		now := f.base.Add(12 * time.Minute)
		f.clock.Set(now)

		if err := f.svc.MarkRolledBack(context.Background(), c.ID); err != nil {
			t.Fatalf("MarkRolledBack: %v", err)
		}
		snap := f.snapshot(c.ID)
		if snap.State != "rolled_back" || !f.stored(snap.UpdatedAt).Equal(now) {
			t.Fatalf("stored row = %+v, want rolled_back at %v", snap, now)
		}
	})

	t.Run("confirmed is kept and returns ErrInvalid", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		c := f.create(f.input(10 * time.Minute))
		ct := f.base.Add(time.Minute)
		f.setState(c.ID, "confirmed", ct, &ct)
		before := f.snapshot(c.ID)
		f.clock.Set(f.base.Add(2 * time.Minute))

		if err := f.svc.MarkRolledBack(context.Background(), c.ID); !errors.Is(err, ErrInvalid) {
			t.Fatalf("err = %v, want ErrInvalid", err)
		}
		if after := f.snapshot(c.ID); after != before {
			t.Fatalf("row changed: before %+v, after %+v", before, after)
		}
	})

	t.Run("rolled_back again returns nil and keeps the state", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		c := f.create(f.input(10 * time.Minute))
		f.setState(c.ID, "rolled_back", f.base.Add(30*time.Second), nil)

		if err := f.svc.MarkRolledBack(context.Background(), c.ID); err != nil {
			t.Fatalf("MarkRolledBack: %v", err)
		}
		if snap := f.snapshot(c.ID); snap.State != "rolled_back" {
			t.Fatalf("stored state = %q, want rolled_back", snap.State)
		}
	})

	t.Run("unknown id gives ErrNotFound", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		if err := f.svc.MarkRolledBack(context.Background(), missingID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestPendingFor(t *testing.T) {
	t.Run("no pending confirmation gives nil, even when a confirmed one matches", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		c := f.create(f.input(10 * time.Minute))
		ct := f.base.Add(time.Minute)
		f.setState(c.ID, "confirmed", ct, &ct)

		got, err := f.svc.PendingFor(context.Background(), nodeNetN1)
		if err != nil {
			t.Fatalf("PendingFor: %v", err)
		}
		if got != nil {
			t.Fatalf("PendingFor = %+v, want nil", got)
		}
	})

	t.Run("pending confirmation on the same resource is returned", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		c := f.create(f.input(10 * time.Minute))

		got, err := f.svc.PendingFor(context.Background(), nodeNetN1)
		if err != nil {
			t.Fatalf("PendingFor: %v", err)
		}
		if got == nil || got.ID != c.ID || got.State != StatePending {
			t.Fatalf("PendingFor = %+v, want pending %s", got, c.ID)
		}
	})

	t.Run("same id on another node or another id gives nil", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		in := f.input(10 * time.Minute)
		in.Resources = []jobs.ResourceRef{{Kind: "node-network", ID: "n1", Node: "node-a"}}
		c := f.create(in)

		queries := []struct {
			name string
			res  []jobs.ResourceRef
			want bool
		}{
			{"same node and id", []jobs.ResourceRef{{Kind: "node-network", ID: "n1", Node: "node-a"}}, true},
			{"other node, same id", []jobs.ResourceRef{{Kind: "node-network", ID: "n1", Node: "node-b"}}, false},
			{"same node, other id", []jobs.ResourceRef{{Kind: "node-network", ID: "n2", Node: "node-a"}}, false},
			{"no node, same id", []jobs.ResourceRef{{Kind: "node-network", ID: "n1"}}, false},
		}
		for _, q := range queries {
			got, err := f.svc.PendingFor(context.Background(), q.res)
			if err != nil {
				t.Fatalf("%s: PendingFor: %v", q.name, err)
			}
			if q.want && (got == nil || got.ID != c.ID) {
				t.Fatalf("%s: PendingFor = %+v, want %s", q.name, got, c.ID)
			}
			if !q.want && got != nil {
				t.Fatalf("%s: PendingFor = %+v, want nil", q.name, got)
			}
		}
	})

	t.Run("pending past ExpiresAt does not count and is persisted as expired", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		c := f.create(f.input(10 * time.Minute))
		f.clock.Set(c.ExpiresAt.Add(time.Second))

		got, err := f.svc.PendingFor(context.Background(), nodeNetN1)
		if err != nil {
			t.Fatalf("PendingFor: %v", err)
		}
		if got != nil {
			t.Fatalf("PendingFor = %+v, want nil", got)
		}
		if snap := f.snapshot(c.ID); snap.State != string(StateExpired) {
			t.Fatalf("stored state = %q, want expired", snap.State)
		}
	})

	t.Run("several matches return the one with the earliest ExpiresAt", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		f.create(f.input(10 * time.Minute))
		short := f.create(f.input(5 * time.Minute))
		other := f.input(2 * time.Minute)
		other.Resources = []jobs.ResourceRef{{Kind: "node-network", ID: "n2"}}
		f.create(other)

		got, err := f.svc.PendingFor(context.Background(), nodeNetN1)
		if err != nil {
			t.Fatalf("PendingFor: %v", err)
		}
		if got == nil || got.ID != short.ID {
			t.Fatalf("PendingFor = %+v, want the earliest-expiring %s", got, short.ID)
		}
	})

	t.Run("path resources overlap by parent and child, not by string prefix", func(t *testing.T) {
		defer guard(t)
		f := newFixture(t)
		in := f.input(10 * time.Minute)
		in.Resources = []jobs.ResourceRef{{Kind: "path", Path: "/data"}}
		c := f.create(in)

		got, err := f.svc.PendingFor(context.Background(), []jobs.ResourceRef{{Kind: "path", Path: "/data/mysql"}})
		if err != nil {
			t.Fatalf("PendingFor child path: %v", err)
		}
		if got == nil || got.ID != c.ID {
			t.Fatalf("PendingFor child path = %+v, want %s", got, c.ID)
		}
		got, err = f.svc.PendingFor(context.Background(), []jobs.ResourceRef{{Kind: "path", Path: "/database"}})
		if err != nil {
			t.Fatalf("PendingFor sibling path: %v", err)
		}
		if got != nil {
			t.Fatalf("PendingFor sibling path = %+v, want nil", got)
		}
	})
}

func TestConfirmWildcardLocalAddrMatchesPort(t *testing.T) {
	for _, tc := range []struct {
		want, got string
		ok        bool
	}{
		{"0.0.0.0:9800", "192.0.2.7:9800", true},
		{"[::]:9800", "[2001:db8::1]:9800", true},
		{":9800", "127.0.0.1:9800", true},
		{"0.0.0.0:9800", "192.0.2.7:9801", false},
		{"192.0.2.7:9800", "192.0.2.8:9800", false},
		{"192.0.2.7:9800", "192.0.2.7:9800", true},
	} {
		if got := sameLocalAddr(tc.want, tc.got); got != tc.ok {
			t.Errorf("sameLocalAddr(%q, %q) = %v, want %v", tc.want, tc.got, got, tc.ok)
		}
	}
}
