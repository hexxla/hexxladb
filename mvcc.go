package hexxladb

import (
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/mvcc"
	"github.com/hexxla/hexxladb/internal/record"
)

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
	if tx.cellOverlay != nil {
		if rec, o := tx.cellOverlay[key]; o {
			b, err := record.EncodeCell(rec)
			if err != nil {
				return nil, 0, false, err
			}
			return b, tx.writeSeq, true, nil
		}
	}
	from, to := index.CellVersionScanBounds(key)
	var versions []mvcc.VersionKV
	err = tx.db.btree.AscendRange(from, to, func(k, v []byte) bool {
		cs, ok := index.ParseCommitSeqFromCellKey(k)
		if !ok {
			return true
		}
		versions = append(versions, mvcc.VersionKV{CommitSeq: cs, Value: append([]byte(nil), v...)})
		return true
	})
	if err != nil {
		return nil, 0, false, err
	}
	val, seq, ok := mvcc.SelectVisible(versions, tx.readSeq)
	if !ok {
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
	from, to, err := index.FacetVersionScanBounds(p, facetID)
	if err != nil {
		return nil, false, err
	}
	var versions []mvcc.VersionKV
	err = tx.AscendRange(from, to, func(k, v []byte) bool {
		cs, ok := index.ParseFacetCommitSeq(k)
		if !ok {
			return true
		}
		versions = append(versions, mvcc.VersionKV{CommitSeq: cs, Value: append([]byte(nil), v...)})
		return true
	})
	if err != nil {
		return nil, false, err
	}
	val, _, ok := mvcc.SelectVisible(versions, tx.readSeq)
	if !ok {
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
	from, to, err := index.SeamVersionScanBounds(id)
	if err != nil {
		return nil, 0, false, err
	}
	var versions []mvcc.VersionKV
	err = tx.AscendRange(from, to, func(k, v []byte) bool {
		cs, ok := index.ParseSeamCommitSeq(k)
		if !ok {
			return true
		}
		versions = append(versions, mvcc.VersionKV{CommitSeq: cs, Value: append([]byte(nil), v...)})
		return true
	})
	if err != nil {
		return nil, 0, false, err
	}
	val, seq, ok := mvcc.SelectVisible(versions, tx.readSeq)
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
