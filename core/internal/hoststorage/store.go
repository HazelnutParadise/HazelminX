// Package hoststorage keeps what agents report about their storage
// (ticket 030.1): the latest inventory (disks, partitions, mounts and how
// they are configured), the latest per-category usage measurement, and the
// capacity alert rules. It combines them with the latest metrics sample
// (capacity and inodes) into the node's storage view.
package hoststorage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

const (
	timeLayout = "2006-01-02T15:04:05Z"
	// maxClockAhead matches hostmetrics: later reports come from a wrong
	// agent clock.
	maxClockAhead = 5 * time.Minute
	// Bounds of one report, so a broken agent cannot fill the database.
	maxDevices = 4096
	maxMounts  = 4096
	maxConfigs = 4096
)

var (
	ErrValidation = errors.New("hoststorage: validation failed")
	ErrNotFound   = errors.New("hoststorage: not found")
)

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(db *sql.DB, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{db: db, now: now}
}

func ts(t time.Time) string { return t.UTC().Truncate(time.Second).Format(timeLayout) }

func parseTS(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// RecordInventory replaces a node's inventory. One collected before the
// stored one is ignored (a late resend); one from a clock too far ahead,
// or too large, is ErrValidation.
func (s *Store) RecordInventory(ctx context.Context, nodeID string, m *protocol.HostStorage) error {
	if m.CollectedAt.After(s.now().Add(maxClockAhead)) {
		return fmt.Errorf("%w: inventory collected at %s, ahead of Core's clock", ErrValidation, ts(m.CollectedAt))
	}
	inv := m.Storage
	if len(inv.Devices) > maxDevices || len(inv.Mounts) > maxMounts || len(inv.Configs) > maxConfigs {
		return fmt.Errorf("%w: inventory too large", ErrValidation)
	}
	for _, mt := range inv.Mounts {
		for _, i := range mt.Configs {
			if int(i) >= len(inv.Configs) {
				return fmt.Errorf("%w: mount %q refers to configuration %d of %d", ErrValidation, mt.MountPoint, i, len(inv.Configs))
			}
		}
	}
	for _, p := range inv.Problems {
		for _, i := range p.Configs {
			if int(i) >= len(inv.Configs) {
				return fmt.Errorf("%w: problem refers to configuration %d of %d", ErrValidation, i, len(inv.Configs))
			}
		}
	}
	b, err := json.Marshal(inv)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO host_storage (node_id, inventory, collected_at, received_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (node_id) DO UPDATE SET inventory = excluded.inventory, collected_at = excluded.collected_at, received_at = excluded.received_at
		WHERE excluded.collected_at >= host_storage.collected_at`,
		nodeID, string(b), ts(m.CollectedAt), ts(s.now()))
	return err
}

// RecordUsage replaces a node's usage measurement, under the same rules.
func (s *Store) RecordUsage(ctx context.Context, nodeID string, m *protocol.StorageUsage) error {
	u := m.Usage
	if u.StartedAt.After(s.now().Add(maxClockAhead)) {
		return fmt.Errorf("%w: usage started at %s, ahead of Core's clock", ErrValidation, ts(u.StartedAt))
	}
	if len(u.Filesystems) > maxMounts {
		return fmt.Errorf("%w: usage too large", ErrValidation)
	}
	known := map[string]bool{protocol.CategoryAppData: true, protocol.CategoryDatabase: true, protocol.CategoryBackupStaging: true,
		protocol.CategoryContainerImages: true, protocol.CategoryOSAndLogs: true}
	for _, f := range u.Filesystems {
		seen := map[string]bool{}
		for _, c := range f.Categories {
			if !known[c.Category] || seen[c.Category] {
				return fmt.Errorf("%w: filesystem %q has category %q twice or unknown", ErrValidation, f.MountPoint, c.Category)
			}
			seen[c.Category] = true
		}
	}
	b, err := json.Marshal(u)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO host_storage_usage (node_id, usage, started_at, received_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (node_id) DO UPDATE SET usage = excluded.usage, started_at = excluded.started_at, received_at = excluded.received_at
		WHERE excluded.started_at >= host_storage_usage.started_at`,
		nodeID, string(b), ts(u.StartedAt), ts(s.now()))
	return err
}

// Inventory is a node's stored inventory.
type Inventory struct {
	CollectedAt time.Time
	ReceivedAt  time.Time
	Storage     protocol.StorageInventory
}

// Inventory returns nil when the node has reported none.
func (s *Store) Inventory(ctx context.Context, nodeID string) (*Inventory, error) {
	var raw, col, rec string
	err := s.db.QueryRowContext(ctx, `SELECT inventory, collected_at, received_at FROM host_storage WHERE node_id = ?`, nodeID).Scan(&raw, &col, &rec)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := &Inventory{CollectedAt: parseTS(col), ReceivedAt: parseTS(rec)}
	return out, json.Unmarshal([]byte(raw), &out.Storage)
}

// Usage is a node's stored usage measurement.
type Usage struct {
	ReceivedAt time.Time
	Usage      protocol.StorageUsageData
}

// Usage returns nil when the node has reported none.
func (s *Store) Usage(ctx context.Context, nodeID string) (*Usage, error) {
	var raw, rec string
	err := s.db.QueryRowContext(ctx, `SELECT usage, received_at FROM host_storage_usage WHERE node_id = ?`, nodeID).Scan(&raw, &rec)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := &Usage{ReceivedAt: parseTS(rec)}
	return out, json.Unmarshal([]byte(raw), &out.Usage)
}
