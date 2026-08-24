package hexxladb

import (
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func (tx *Tx) getVisibleVersionRaw(
	from, to []byte,
	parseCommitSeq func([]byte) (uint64, bool),
) (raw []byte, commitSeq uint64, ok bool, err error) {
	err = tx.descendRange(from, to, func(k, v []byte) bool {
		seq, valid := parseCommitSeq(k)
		if !valid {
			return true
		}
		raw = append([]byte(nil), v...)
		commitSeq = seq
		ok = true
		return false
	})
	return raw, commitSeq, ok, err
}

func (tx *Tx) getCellVisibleRaw(key lattice.PackedCoord) (raw []byte, commitSeq uint64, ok bool, err error) {
	if tx == nil || tx.db == nil {
		return nil, 0, false, ErrClosed
	}
	if tx.db.activeEng() == nil {
		return nil, 0, false, ErrDatabaseClosed
	}
	if !tx.db.useMVCC {
		raw, ok, err = tx.Get(index.CellKey(key))
		if err != nil || !ok {
			return nil, 0, ok, err
		}
		return raw, 0, true, nil
	}
	// Same-tx delete takes priority over overlay and btree.
	if tx.cellDeleted[key] {
		return nil, 0, false, nil
	}
	if tx.cellOverlay != nil {
		if rec, o := tx.cellOverlay[key]; o {
			b, err := record.EncodeCell(rec)
			if err != nil {
				return nil, 0, false, err
			}
			return b, tx.writeSeq, true, nil
		}
	}
	from := index.CellKeyWithVersion(key, 0)
	to := index.CellKeyWithVersion(key, tx.readSeq)
	val, seq, ok, err := tx.getVisibleVersionRaw(from, to, index.ParseCommitSeqFromCellKey)
	if err != nil {
		return nil, 0, false, err
	}
	if !ok {
		return nil, 0, false, nil
	}
	// Zero-length value is an MVCC tombstone (cell deleted at this commit_seq).
	if len(val) == 0 {
		return nil, 0, false, nil
	}
	return val, seq, true, nil
}

func (tx *Tx) visibleCellAndSeq(key lattice.PackedCoord) (rec record.CellRecord, commitSeq uint64, ok bool, err error) {
	raw, seq, ok, err := tx.getCellVisibleRaw(key)
	if err != nil || !ok {
		return record.CellRecord{}, 0, ok, err
	}
	rec, err = record.DecodeCell(raw)
	if err != nil {
		return record.CellRecord{}, 0, false, err
	}
	return rec, seq, true, nil
}

func (tx *Tx) getFacetVisibleRaw(p lattice.PackedCoord, facetID byte) (raw []byte, ok bool, err error) {
	if tx == nil || tx.db == nil {
		return nil, false, ErrClosed
	}
	if tx.db.activeEng() == nil {
		return nil, false, ErrDatabaseClosed
	}
	if !tx.db.useMVCC {
		k, err := index.FacetKey(p, facetID)
		if err != nil {
			return nil, false, err
		}
		return tx.Get(k)
	}
	from, err := index.FacetKeyWithVersion(p, facetID, 0)
	if err != nil {
		return nil, false, err
	}
	to, err := index.FacetKeyWithVersion(p, facetID, tx.readSeq)
	if err != nil {
		return nil, false, err
	}
	val, _, ok, err := tx.getVisibleVersionRaw(from, to, index.ParseFacetCommitSeq)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	// Zero-length value is an MVCC tombstone (facet deleted).
	if len(val) == 0 {
		return nil, false, nil
	}
	return val, true, nil
}

func (tx *Tx) getSeamVisibleRaw(id string) (raw []byte, commitSeq uint64, ok bool, err error) {
	if tx == nil || tx.db == nil {
		return nil, 0, false, ErrClosed
	}
	if tx.db.activeEng() == nil {
		return nil, 0, false, ErrDatabaseClosed
	}
	if !tx.db.useMVCC {
		k, err := index.SeamKey(id)
		if err != nil {
			return nil, 0, false, err
		}
		raw, ok, err = tx.Get(k)
		if err != nil || !ok {
			return nil, 0, ok, err
		}
		return raw, 0, true, nil
	}
	from, err := index.SeamKeyWithVersion(id, 0)
	if err != nil {
		return nil, 0, false, err
	}
	to, err := index.SeamKeyWithVersion(id, tx.readSeq)
	if err != nil {
		return nil, 0, false, err
	}
	val, seq, ok, err := tx.getVisibleVersionRaw(from, to, index.ParseSeamCommitSeq)
	if err != nil {
		return nil, 0, false, err
	}
	if !ok {
		return nil, 0, false, nil
	}
	return val, seq, true, nil
}

func (tx *Tx) visibleSeamAndSeq(id string) (rec record.SeamRecord, commitSeq uint64, ok bool, err error) {
	raw, seq, ok, err := tx.getSeamVisibleRaw(id)
	if err != nil || !ok {
		return record.SeamRecord{}, 0, ok, err
	}
	rec, err = record.DecodeSeam(raw)
	if err != nil {
		return record.SeamRecord{}, 0, false, err
	}
	return rec, seq, true, nil
}
