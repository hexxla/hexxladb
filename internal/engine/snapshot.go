package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

// CopySnapshotTo copies the current primary and WAL file images through the
// engine's already-open descriptors. The caller must exclude engine writes for
// the full call so the two images describe one recovery state.
func (e *Engine) CopySnapshotTo(ctx context.Context, primaryDest, walDest io.Writer, buffer []byte) error {
	if e == nil || e.db == nil || e.wal == nil {
		return errors.New("engine: closed")
	}
	if err := copyOpenFileSnapshot(ctx, e.db, primaryDest, buffer); err != nil {
		return fmt.Errorf("primary: %w", err)
	}
	if err := copyOpenFileSnapshot(ctx, e.wal, walDest, buffer); err != nil {
		return fmt.Errorf("WAL: %w", err)
	}
	return nil
}

func copyOpenFileSnapshot(ctx context.Context, source *os.File, dest io.Writer, buffer []byte) error {
	if len(buffer) == 0 {
		return errors.New("engine: snapshot buffer is empty")
	}
	info, err := source.Stat()
	if err != nil {
		return err
	}
	for offset := int64(0); offset < info.Size(); {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunkSize := min(int64(len(buffer)), info.Size()-offset)
		n, readErr := source.ReadAt(buffer[:chunkSize], offset)
		if n > 0 {
			written, writeErr := dest.Write(buffer[:n])
			if writeErr != nil {
				return writeErr
			}
			if written != n {
				return io.ErrShortWrite
			}
			offset += int64(n)
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		if n == 0 {
			return io.ErrUnexpectedEOF
		}
	}
	return nil
}
