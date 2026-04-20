package hexxladb

import (
	"crypto/rand"
	"os"

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
		hooks, err := buildEncryptionHooks(xtsKey)
		if err != nil {
			return nil, err
		}
		_, statErr := os.Stat(path)
		isNew := statErr != nil && os.IsNotExist(statErr)
		if statErr != nil && !os.IsNotExist(statErr) {
			return nil, statErr
		}
		if isNew {
			var salt [16]byte
			if _, err := rand.Read(salt[:]); err != nil {
				return nil, err
			}
			check := deriveEncryptionKeyCheck(xtsKey, salt)
			return engineOptsWithMVCC(&engine.Options{
				Hooks:                    hooks,
				NewEncryptedDB:           true,
				EncryptionSalt:           salt,
				EncryptionKeyCheck:       check,
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
		return engineOptsWithMVCC(&engine.Options{
			Hooks:                    hooks,
			EncryptionKeyCheck:       deriveEncryptionKeyCheck(xtsKey, hdr.EncryptionSalt),
			ExpectEncryptionKeyCheck: true,
			WALMACKey:                deriveWALMACKey(xtsKey),
			EnableWALMAC:             true,
		}, opts), nil
	}

	// Passphrase
	_, statErr := os.Stat(path)
	isNew := statErr != nil && os.IsNotExist(statErr)
	if statErr != nil && !os.IsNotExist(statErr) {
		return nil, statErr
	}
	if isNew {
		var salt [16]byte
		if _, err := rand.Read(salt[:]); err != nil {
			return nil, err
		}
		raw, err := DeriveKeyFromPassphrase(opts.Passphrase, salt[:])
		if err != nil {
			return nil, err
		}
		xtsKey, err := deriveXTSKeyMaterial(raw, nil, []byte(hkdfInfoXTS))
		if err != nil {
			return nil, err
		}
		hooks, err := buildEncryptionHooks(xtsKey)
		if err != nil {
			return nil, err
		}
		check := deriveEncryptionKeyCheck(xtsKey, salt)
		return engineOptsWithMVCC(&engine.Options{
			Hooks:                    hooks,
			NewEncryptedDB:           true,
			EncryptionSalt:           salt,
			EncryptionKeyCheck:       check,
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
	raw, err := DeriveKeyFromPassphrase(opts.Passphrase, hdr.EncryptionSalt[:])
	if err != nil {
		return nil, err
	}
	xtsKey, err := deriveXTSKeyMaterial(raw, nil, []byte(hkdfInfoXTS))
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
