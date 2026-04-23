package hexxladb

import (
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
