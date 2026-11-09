package changelog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeLegacyEncryptedLog uses the retired v2 wire encoding independently of
// the current writer. Existing ciphertext must remain readable during upgrade.
func writeLegacyEncryptedLog(t *testing.T, path string, key []byte, count int) []Record {
	t.Helper()
	legacy := &Log{format: formatV2}
	copy(legacy.header[:], headerMagicV2)
	binary.BigEndian.PutUint32(legacy.header[8:12], formatV2)
	binary.BigEndian.PutUint32(legacy.header[12:16], flagEncrypted)
	copy(legacy.noncePrefix[:], "legacy-log-salt!!")
	copy(legacy.header[16:32], legacy.noncePrefix[:])
	tag := deriveHeaderTag(key, legacy.noncePrefix, legacy.header[:32])
	copy(legacy.header[32:], tag[:])
	if err := legacy.configureEncryption(key); err != nil {
		t.Fatal(err)
	}
	data := bytes.Clone(legacy.header[:])
	records := make([]Record, count)
	for i := range count {
		intent, err := PrepareIntent(int64(i), Entry{Op: OpPutCell, Key: fmt.Appendf(nil, "key/%04d", i), Encoded: []byte("legacy protected value")}, 8192)
		if err != nil {
			t.Fatal(err)
		}
		records[i] = intentRecord(intent, uint64(i+1))
		inner, err := encodeRecordInner(records[i])
		if err != nil {
			t.Fatal(err)
		}
		nonce := legacy.frameNonce(records[i].Seq)
		bodyLen := 8 + len(inner) + legacy.aead.Overhead()
		aad := legacy.frameAAD(records[i].Seq, uint32(bodyLen)) //nolint:gosec // tiny fixed-size test fixture
		body := make([]byte, 8, bodyLen)
		binary.BigEndian.PutUint64(body, records[i].Seq)
		body = legacy.aead.Seal(body, nonce[:], inner, aad[:])
		data = binary.BigEndian.AppendUint32(data, uint32(len(body))) //nolint:gosec // tiny fixed-size test fixture
		data = append(data, body...)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return records
}

func TestEncryptedV2UpgradePreservesHistoryAndAllowsAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy")
	key := bytes.Repeat([]byte{0x13}, 32)
	want := writeLegacyEncryptedLog(t, path, key, 600)
	log, err := OpenEncrypted(path, true, key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()
	if log.format != formatV3 {
		t.Fatalf("format = %d, want v3", log.format)
	}
	got, err := log.ReadSince(0, len(want)+1)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("upgraded history differs: count=%d err=%v", len(got), err)
	}
	var digest = emptyLogicalDigest
	for _, record := range want {
		inner, err := encodeRecordInner(record)
		if err != nil {
			t.Fatal(err)
		}
		digest = extendLogicalDigest(digest, inner)
		if record.Seq == 257 {
			got, err := log.LogicalDigestAt(record.Seq)
			if err != nil || got != digest {
				t.Fatalf("checkpoint digest changed: %v", err)
			}
		}
	}
	if log.logicalDigest != digest {
		t.Fatal("head digest changed")
	}
	if err := log.AppendIntents([]Intent{{WallUnixNs: 601, Op: OpDeleteCell, Key: []byte("after-upgrade")}}); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	log, err = OpenEncrypted(path, true, key)
	if err != nil {
		t.Fatal(err)
	}
	if log.MaxSeq() != 601 {
		t.Fatalf("reopened sequence = %d", log.MaxSeq())
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".hexxladb-changelog-upgrade-*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("temporary candidates remain: %v, %v", files, err)
	}
}

func TestEncryptedV2UpgradeFailureStates(t *testing.T) {
	t.Run("wrong key preserves source", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "legacy")
		key := bytes.Repeat([]byte{0x25}, 32)
		writeLegacyEncryptedLog(t, path, key, 2)
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := OpenEncrypted(path, true, bytes.Repeat([]byte{0x26}, 32)); !errors.Is(err, ErrEncryptionKeyMismatch) {
			t.Fatalf("wrong key error = %v", err)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("failed upgrade modified source: %v", err)
		}
	})
	t.Run("directory sync failure leaves complete replacement", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "legacy")
		key := bytes.Repeat([]byte{0x27}, 32)
		want := writeLegacyEncryptedLog(t, path, key, 2)
		fault := errors.New("directory sync fault")
		if _, err := openWithSync(path, false, key, false, func(...string) error { return fault }); !errors.Is(err, fault) {
			t.Fatalf("directory sync error = %v", err)
		}
		log, err := OpenEncrypted(path, true, key)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = log.Close() }()
		got, err := log.ReadSince(0, 3)
		if err != nil || !reflect.DeepEqual(got, want) || log.format != formatV3 {
			t.Fatalf("replacement not recoverable: %#v, %v", got, err)
		}
	})
}
