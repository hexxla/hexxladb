// Package hexxladbout implements the contradiction port with HexxlaDB transactions.
package hexxladbout

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	hxdb "github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/examples/ports_adapters/internal/domain"
)

// Storage adapts the public HexxlaDB API to the application port.
type Storage struct {
	db *hxdb.DB
}

var _ domain.ConflictStore = (*Storage)(nil)

// New binds the adapter to an open database.
func New(db *hxdb.DB) *Storage {
	return &Storage{db: db}
}

// RecordConflict writes both observations and their seam in one transaction.
func (s *Storage) RecordConflict(ctx context.Context, left, right domain.Observation, reason string) (string, error) {
	a, err := hxdb.Pack(hxdb.Coord{Q: left.At.Q, R: left.At.R})
	if err != nil {
		return "", fmt.Errorf("pack first observation: %w", err)
	}
	b, err := hxdb.Pack(hxdb.Coord{Q: right.At.Q, R: right.At.R})
	if err != nil {
		return "", fmt.Errorf("pack second observation: %w", err)
	}
	lo, hi := a, b
	if b.Less(a) {
		lo, hi = b, a
	}
	id := ulid.Make().String()
	err = s.db.Update(func(tx *hxdb.Tx) error {
		if err := tx.PutCell(ctx, hxdb.CellRecord{Key: a, RawContent: left.Text}); err != nil {
			return err
		}
		if err := tx.PutCell(ctx, hxdb.CellRecord{Key: b, RawContent: right.Text}); err != nil {
			return err
		}
		return tx.PutSeam(ctx, hxdb.SeamRecord{
			ID: id, CellA: lo, CellB: hi, SeamType: hxdb.SeamTypeConflict,
			Reason: reason, DetectedAt: time.Now().UnixNano(),
		})
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// ResolveConflict stores the resolution note on the specified seam.
func (s *Storage) ResolveConflict(ctx context.Context, id, note string) error {
	err := s.db.Update(func(tx *hxdb.Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return tx.ResolveSeam(id, "resolved", note)
	})
	if errors.Is(err, hxdb.ErrSeamNotFound) {
		return fmt.Errorf("%w: %s", domain.ErrConflictNotFound, id)
	}
	return err
}
