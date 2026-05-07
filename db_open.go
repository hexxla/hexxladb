package hexxladb

import (
	"crypto/rand"
	"os"
	"time"

	"github.com/hexxla/hexxladb/internal/engine"
)

const hkdfInfoXTS = "hexxladb-m9-aes-xts-v1"

func engineOptsWithMVCC(base *engine.Options, opts *Options) *engine.Options {
	if opts == nil || !opts.EnableMVCC {
		return base
	}
	if base == nil {
		return &engine.Options{UseFormatV2: true}
	}
	base.UseFormatV2 = true
	return base
}

func buildEngineOptions(path string, opts *Options) (*engine.Options, error) {
	if opts == nil {
		hdr, err := engine.ReadHeaderFile(path)
		if err == nil && hdr.Features&engine.FeatureEncryptedDataPages != 0 {
			return nil, ErrEncryptionKeyRequired
		}
		return nil, nil
	}
	customHooks := opts.BeforeWritePage != nil || opts.AfterReadPage != nil
	hasEnc := len(opts.EncryptionKey) > 0 || opts.Passphrase != ""
	if customHooks && hasEnc {
		return nil, ErrEncryptionOptions
	}
	if opts.Passphrase != "" && len(opts.EncryptionKey) > 0 {
		return nil, ErrEncryptionOptions
	}
	if customHooks {
		return engineOptsWithMVCC(&engine.Options{
			Hooks: &engine.PageHooks{
				BeforeWrite: opts.BeforeWritePage,
				AfterRead:   opts.AfterReadPage,
			},
		}, opts), nil
	}
	if !hasEnc {
		return engineOptsWithMVCC(nil, opts), nil
	}

	if len(opts.EncryptionKey) > 0 {
		xtsKey, err := deriveXTSKeyMaterial(opts.EncryptionKey, nil, []byte(hkdfInfoXTS))
		if err != nil {
			return nil, err
		}
		return buildEncryptedEngineOpts(path, opts, xtsKey)
	}
	return buildPassphraseEngineOpts(path, opts)
}

// buildPassphraseEngineOpts handles the passphrase-based encryption path.
// For new databases, it generates a salt; for existing ones, it reads the salt from the header.
func buildPassphraseEngineOpts(path string, opts *Options) (*engine.Options, error) {
	isNew, err := isDatabaseNew(path)
	if err != nil {
		return nil, err
	}
	if isNew {
		var salt [16]byte
		if _, err := rand.Read(salt[:]); err != nil {
			return nil, err
		}
		xtsKey, err := derivePassphraseXTSKey(opts.Passphrase, salt[:])
		if err != nil {
			return nil, err
		}
		hooks, err := buildEncryptionHooks(xtsKey)
		if err != nil {
			return nil, err
		}
		return engineOptsWithMVCC(&engine.Options{
			Hooks:                    hooks,
			NewEncryptedDB:           true,
			EncryptionSalt:           salt,
			EncryptionKeyCheck:       deriveEncryptionKeyCheck(xtsKey, salt),
			ExpectEncryptionKeyCheck: true,
			WALMACKey:                deriveWALMACKey(xtsKey),
			EnableWALMAC:             true,
		}, opts), nil
	}
	hdr, err := engine.ReadHeaderFile(path)
	if err != nil {
		return nil, err
	}
	if hdr.Features&engine.FeatureEncryptedDataPages == 0 {
		return nil, ErrDatabaseNotEncrypted
	}
	xtsKey, err := derivePassphraseXTSKey(opts.Passphrase, hdr.EncryptionSalt[:])
	if err != nil {
		return nil, err
	}
	hooks, err := buildEncryptionHooks(xtsKey)
	if err != nil {
		return nil, err
	}
	return engineOptsWithMVCC(&engine.Options{
		Hooks:                    hooks,
		EncryptionKeyCheck:       deriveEncryptionKeyCheck(xtsKey, hdr.EncryptionSalt),
		ExpectEncryptionKeyCheck: true,
		WALMACKey:                deriveWALMACKey(xtsKey),
		EnableWALMAC:             true,
	}, opts), nil
}

// derivePassphraseXTSKey derives XTS key material from a passphrase and salt.
func derivePassphraseXTSKey(passphrase string, salt []byte) ([]byte, error) {
	raw, err := DeriveKeyFromPassphrase(passphrase, salt)
	if err != nil {
		return nil, err
	}
	return deriveXTSKeyMaterial(raw, nil, []byte(hkdfInfoXTS))
}

// isDatabaseNew checks whether the database file at path exists.
// Returns true if the file does not exist, false if it does, or an error for other stat failures.
func isDatabaseNew(path string) (bool, error) {
	_, statErr := os.Stat(path)
	if statErr == nil {
		return false, nil
	}
	if os.IsNotExist(statErr) {
		return true, nil
	}
	return false, statErr
}

// buildEncryptedEngineOpts constructs engine.Options for an encrypted database
// using the derived XTS key material.
func buildEncryptedEngineOpts(path string, opts *Options, xtsKey []byte) (*engine.Options, error) {
	hooks, err := buildEncryptionHooks(xtsKey)
	if err != nil {
		return nil, err
	}
	isNew, err := isDatabaseNew(path)
	if err != nil {
		return nil, err
	}
	if isNew {
		return buildNewEncryptedDB(opts, xtsKey, hooks)
	}
	return buildExistingEncryptedDB(path, opts, xtsKey, hooks)
}

// buildNewEncryptedDB creates engine.Options for a brand-new encrypted database.
func buildNewEncryptedDB(opts *Options, xtsKey []byte, hooks *engine.PageHooks) (*engine.Options, error) {
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return nil, err
	}
	return engineOptsWithMVCC(&engine.Options{
		Hooks:                    hooks,
		NewEncryptedDB:           true,
		EncryptionSalt:           salt,
		EncryptionKeyCheck:       deriveEncryptionKeyCheck(xtsKey, salt),
		ExpectEncryptionKeyCheck: true,
		WALMACKey:                deriveWALMACKey(xtsKey),
		EnableWALMAC:             true,
	}, opts), nil
}

// buildExistingEncryptedDB creates engine.Options for reopening an existing encrypted database.
func buildExistingEncryptedDB(path string, opts *Options, xtsKey []byte, hooks *engine.PageHooks) (*engine.Options, error) {
	hdr, err := engine.ReadHeaderFile(path)
	if err != nil {
		return nil, err
	}
	if hdr.Features&engine.FeatureEncryptedDataPages == 0 {
		return nil, ErrDatabaseNotEncrypted
	}
	return engineOptsWithMVCC(&engine.Options{
		Hooks:                    hooks,
		EncryptionKeyCheck:       deriveEncryptionKeyCheck(xtsKey, hdr.EncryptionSalt),
		ExpectEncryptionKeyCheck: true,
		WALMACKey:                deriveWALMACKey(xtsKey),
		EnableWALMAC:             true,
	}, opts), nil
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

func mergeEngineGroupWAL(eo *engine.Options, o *Options) *engine.Options {
	mw := time.Duration(0)
	if o != nil {
		mw = max(time.Duration(0), o.GroupWALMaxBatchWait)
	}
	gw := engine.GroupWAL{Enabled: true, MaxBatchWait: mw}
	if eo == nil {
		return &engine.Options{GroupWAL: gw}
	}
	eo.GroupWAL = gw
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

func openValidateEncryption(opts *Options, hdr engine.Header) error {
	if opts == nil {
		opts = &Options{}
	}
	hasEnc := len(opts.EncryptionKey) > 0 || opts.Passphrase != ""
	customHooks := opts.BeforeWritePage != nil || opts.AfterReadPage != nil
	if hasEnc || customHooks {
		return nil
	}
	if hdr.Features&engine.FeatureEncryptedDataPages != 0 {
		return ErrEncryptionKeyRequired
	}
	return nil
}
