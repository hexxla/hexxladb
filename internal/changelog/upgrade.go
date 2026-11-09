package changelog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// upgradeEncryptedV2 replaces the validated legacy log with an equivalent v3
// log before returning a writable handle. The database owner holds its primary
// lock throughout Open, excluding every other supported writer. Until rename,
// the original remains recoverable; after rename, the complete synced v3 image
// is recoverable. A crash can leave an unreferenced temporary candidate.
func (l *Log) upgradeEncryptedV2(key []byte, syncParents func(...string) error) (_ *Log, retErr error) {
	candidateFile, err := os.CreateTemp(filepath.Dir(l.path), ".hexxladb-changelog-upgrade-*")
	if err != nil {
		return nil, fmt.Errorf("changelog: create encrypted upgrade: %w", err)
	}
	candidatePath := candidateFile.Name()
	candidate := &Log{path: candidatePath, f: candidateFile}
	defer func() {
		retErr = errors.Join(retErr, candidate.Close())
		if err := os.Remove(candidatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			retErr = errors.Join(retErr, err)
		}
	}()
	if err := candidate.writeHeader(key); err != nil {
		return nil, err
	}
	if err := l.CopyTo(candidate); err != nil {
		return nil, fmt.Errorf("changelog: copy encrypted upgrade: %w", err)
	}
	if err := candidate.Sync(); err != nil {
		return nil, err
	}
	// Validate the stored replacement, not just the state accumulated by writes.
	if err := candidate.readHeaderAndScan(key, false); err != nil {
		return nil, err
	}
	if candidate.maxSeq != l.maxSeq || candidate.logicalDigest != l.logicalDigest {
		return nil, fmt.Errorf("%w: encrypted upgrade changed logical history", ErrCorrupt)
	}
	if err := candidate.Close(); err != nil {
		return nil, err
	}
	if err := l.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(candidatePath, l.path); err != nil {
		return nil, fmt.Errorf("changelog: publish encrypted upgrade: %w", err)
	}
	if err := syncParents(l.path); err != nil {
		return nil, fmt.Errorf("changelog: sync encrypted upgrade: %w", err)
	}
	return openWithSync(l.path, l.sync, key, false, syncParents)
}
