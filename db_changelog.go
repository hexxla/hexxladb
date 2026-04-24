package hexxladb

import (
	"bytes"

	"github.com/hexxla/hexxladb/internal/changelog"
)

// ChangelogRecord is one entry from the logical changefeed (see [docs/hexxladb/CHANGEFEED.md](docs/hexxladb/CHANGEFEED.md)).
type ChangelogRecord = changelog.Record

// Changelog operation codes (stable for consumers).
const (
	ChangelogOpPutCell = changelog.OpPutCell
	ChangelogOpPutSeam = changelog.OpPutSeam
	// ChangelogOpResolveSeam is emitted only by [Tx.ResolveSeam] (same btree layout as PutSeam).
	ChangelogOpResolveSeam = changelog.OpResolveSeam
	ChangelogOpPutFacet    = changelog.OpPutFacet
	ChangelogOpPutEdge     = changelog.OpPutEdge
)

// ReadChangelogSince returns up to limit records with Seq > afterSeq. Requires [Options.ChangelogEnabled].
// Delivery is at-least-once; see [docs/hexxladb/CHANGEFEED.md](docs/hexxladb/CHANGEFEED.md).
func (db *DB) ReadChangelogSince(afterSeq uint64, limit int) ([]ChangelogRecord, error) {
	if db == nil {
		return nil, ErrDatabaseClosed
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.changelog == nil {
		return nil, ErrChangelogDisabled
	}
	return db.changelog.ReadSince(afterSeq, limit)
}

// ChangelogFilter configures [DB.ReadChangelogFiltered].
type ChangelogFilter struct {
	// Ops limits results to these operation codes. Nil or empty means all ops.
	Ops []byte
	// KeyPrefix filters to records whose key starts with this prefix. Nil means all keys.
	KeyPrefix []byte
}

// ReadChangelogFiltered returns up to limit records with Seq > afterSeq that match filter.
// Requires [Options.ChangelogEnabled].
func (db *DB) ReadChangelogFiltered(afterSeq uint64, limit int, filter ChangelogFilter) ([]ChangelogRecord, error) {
	if db == nil {
		return nil, ErrDatabaseClosed
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.changelog == nil {
		return nil, ErrChangelogDisabled
	}
	// Over-read then filter; changelog is sequential so we read a generous batch.
	fetchLimit := max(limit*4, 256)
	all, err := db.changelog.ReadSince(afterSeq, fetchLimit)
	if err != nil {
		return nil, err
	}
	opSet := make(map[byte]struct{}, len(filter.Ops))
	for _, op := range filter.Ops {
		opSet[op] = struct{}{}
	}
	out := make([]ChangelogRecord, 0, min(limit, len(all)))
	for i := range all {
		if len(out) >= limit {
			break
		}
		if len(opSet) > 0 {
			if _, ok := opSet[all[i].Op]; !ok {
				continue
			}
		}
		if len(filter.KeyPrefix) > 0 && !bytes.HasPrefix(all[i].Key, filter.KeyPrefix) {
			continue
		}
		out = append(out, all[i])
	}
	return out, nil
}
