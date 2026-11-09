package changelog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// AppendIntents projects durable commits while preserving the original times.
func (l *Log) AppendIntents(intents []Intent) error {
	if l == nil || l.f == nil {
		return errors.New("changelog: closed")
	}
	return l.appendBatch(len(intents), func(i int, seq uint64) (Record, error) {
		return intentRecord(intents[i], seq), nil
	})
}

func intentRecord(intent Intent, seq uint64) Record {
	return Record{
		Seq: seq, WallUnixNs: intent.WallUnixNs, Op: intent.Op, Key: intent.Key,
		Hash: intent.Hash, HashValid: intent.HashValid, Inline: intent.Inline, EncodedLen: intent.EncodedLen,
	}
}

func (l *Log) appendRecords(records []Record) error {
	return l.appendBatch(len(records), func(i int, seq uint64) (Record, error) {
		record := records[i]
		if record.Seq != seq {
			return Record{}, fmt.Errorf("%w: non-contiguous copy sequence", ErrCorrupt)
		}
		return record, nil
	})
}

func (l *Log) appendBatch(count int, get func(index int, seq uint64) (Record, error)) error {
	if count <= 0 {
		return nil
	}
	recordCount := uint64(count)
	if recordCount > maxSequence-l.maxSeq {
		return errors.New("changelog: sequence exhausted")
	}
	startSeq := l.maxSeq
	var buf bytes.Buffer
	var pendingCheckpoints []checkpoint
	pendingDigest := l.logicalDigest
	for i := range count {
		record, err := get(i, startSeq+uint64(i+1))
		if err != nil {
			return err
		}
		inner, err := encodeRecordInner(record)
		if err != nil {
			return err
		}
		body, err := l.encodeFrame(record.Seq, inner)
		if err != nil {
			return err
		}
		if len(body) > maxBodyBytes {
			return errors.New("changelog: record too large")
		}
		var lenBuf [4]byte
		// #nosec G115 -- encodeFrame rejects bodies larger than maxBodyBytes.
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(body))) //nolint:gosec // body is bounded by encodeFrame
		if _, err := buf.Write(lenBuf[:]); err != nil {
			return err
		}
		if _, err := buf.Write(body); err != nil {
			return err
		}
		pendingDigest = extendLogicalDigest(pendingDigest, inner)
		if record.Seq%checkpointStride == 0 {
			pendingCheckpoints = append(pendingCheckpoints, checkpoint{seq: record.Seq, offset: int64(buf.Len()), digest: pendingDigest})
		}
	}
	startOffset, err := l.f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := l.f.Write(buf.Bytes()); err != nil {
		return err
	}
	l.logicalDigest = pendingDigest
	l.maxSeq = startSeq + recordCount
	for i := range pendingCheckpoints {
		pendingCheckpoints[i].offset += startOffset
		l.checkpoints = append(l.checkpoints, pendingCheckpoints[i])
	}
	if l.sync {
		return l.f.Sync()
	}
	return nil
}
