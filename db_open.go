package hexxladb

import (
	"crypto/rand"
	"errors"
	"io"
	"os"

	"github.com/hexxla/hexxladb/internal/engine"
)

func buildEngineOptions(path string, opts *Options) (*engine.Options, []byte, error) {
	var rawKey []byte
	var passphrase string
	if opts != nil {
		rawKey, passphrase = opts.EncryptionKey, opts.Passphrase
	}
	if len(rawKey) > 0 && passphrase != "" {
		return nil, nil, ErrEncryptionOptions
	}

	// Derive credentials from immutable identity before the engine repairs a
	// torn mutable header from a complete, identity-bound WAL transaction.
	header, err := engine.ReadHeaderIdentityFile(path)
	if len(rawKey) == 0 && passphrase == "" {
		if err == nil && header.FormatVersion == engine.AuthenticatedFormatVersion {
			return nil, nil, ErrEncryptionKeyRequired
		}
		return nil, nil, nil
	}
	newDatabase := errors.Is(err, os.ErrNotExist)
	if !newDatabase && err != nil {
		return nil, nil, err
	}
	if !newDatabase && header.FormatVersion != engine.AuthenticatedFormatVersion {
		return nil, nil, ErrDatabaseNotEncrypted
	}

	salt := header.EncryptionSalt
	if newDatabase {
		if _, err := io.ReadFull(rand.Reader, salt[:]); err != nil {
			return nil, nil, err
		}
	}
	var master []byte
	if passphrase != "" {
		master, err = derivePassphraseAuthenticatedMaster(passphrase, salt[:])
	} else {
		master, err = deriveAuthenticatedMaster(rawKey)
	}
	if err != nil {
		return nil, nil, err
	}
	hooks, err := buildAuthenticatedEncryptionHooks(master, salt)
	if err != nil {
		clear(master)
		return nil, nil, err
	}
	return &engine.Options{
		Hooks:              hooks,
		NewAuthenticatedDB: newDatabase,
		EncryptionSalt:     salt,
		EncryptionKeyCheck: deriveEncryptionKeyCheck(master, salt),
		WALMACKey:          deriveWALMACKey(master),
		EnableWALMAC:       true,
		HeaderMACKey:       deriveHeaderMACKey(master, salt),
		EnableHeaderMAC:    true,
	}, master, nil
}

func derivePassphraseAuthenticatedMaster(passphrase string, salt []byte) ([]byte, error) {
	raw, err := DeriveKeyFromPassphrase(passphrase, salt)
	if err != nil {
		return nil, err
	}
	defer clear(raw)
	return deriveAuthenticatedMaster(raw)
}

func mergeEnginePageSize(eo *engine.Options, o *Options) *engine.Options {
	if o == nil || o.PageSize == 0 {
		return eo
	}
	if eo == nil {
		return &engine.Options{PageSize: o.PageSize}
	}
	eo.PageSize = o.PageSize
	return eo
}

// mergeEnginePrimaryFdatasync sets [engine.Options.UsePrimaryFdatasync] from public [Options].
func mergeEnginePrimaryFdatasync(eo *engine.Options, o *Options) *engine.Options {
	if o == nil || !o.UsePrimaryFdatasync {
		return eo
	}
	if eo == nil {
		return &engine.Options{UsePrimaryFdatasync: true}
	}
	eo.UsePrimaryFdatasync = true
	return eo
}

func mergeEngineMaxValueBytes(eo *engine.Options, o *Options) *engine.Options {
	if o == nil || o.MaxValueBytes == 0 {
		return eo
	}
	if eo == nil {
		return &engine.Options{MaxValueBytes: o.MaxValueBytes}
	}
	eo.MaxValueBytes = o.MaxValueBytes
	return eo
}

func mergeEngineEmbedding(eo *engine.Options, o *Options) *engine.Options {
	if o == nil || o.EmbeddingDimension == 0 {
		return eo
	}
	if eo == nil {
		return &engine.Options{
			EmbeddingDim:    o.EmbeddingDimension,
			EmbeddingMetric: engine.DistanceMetric(o.DistanceMetric),
		}
	}
	eo.EmbeddingDim = o.EmbeddingDimension
	eo.EmbeddingMetric = engine.DistanceMetric(o.DistanceMetric)
	return eo
}

const defaultPageCacheSize = 4 << 20 // 4 MiB

// mergeEnginePageCache resolves the public PageCacheSize convention:
//
//   - 0 (default) → 4 MiB cache budget
//   - -1           → cache disabled (engine receives 0)
//   - positive     → use as-is
func mergeEnginePageCache(eo *engine.Options, o *Options) *engine.Options {
	var budget int64
	switch {
	case o == nil || o.PageCacheSize == 0:
		budget = defaultPageCacheSize
	case o.PageCacheSize < 0:
		budget = 0 // disabled
	default:
		budget = o.PageCacheSize
	}
	if eo == nil {
		return &engine.Options{PageCacheSize: budget}
	}
	eo.PageCacheSize = budget
	return eo
}
