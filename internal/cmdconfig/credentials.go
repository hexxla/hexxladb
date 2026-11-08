// Package cmdconfig shares configuration handling between operator commands.
package cmdconfig

import (
	"encoding/base64"
	"fmt"
	"os"

	"github.com/hexxla/hexxladb"
)

// CredentialOptions reads exactly one optional credential from the named environment variables.
func CredentialOptions(passphraseName, keyName string) (*hexxladb.Options, error) {
	passphrase, passphrasePresent := os.LookupEnv(passphraseName)
	encodedKey, keyPresent := os.LookupEnv(keyName)
	if passphrasePresent && keyPresent {
		return nil, fmt.Errorf("%q and %q are both set; choose one credential source", passphraseName, keyName)
	}
	if !passphrasePresent && !keyPresent {
		return nil, nil
	}
	if passphrasePresent {
		if passphrase == "" {
			return nil, fmt.Errorf("environment variable %q is set but empty", passphraseName)
		}
		return &hexxladb.Options{Passphrase: passphrase}, nil
	}
	if encodedKey == "" {
		return nil, fmt.Errorf("environment variable %q is set but empty", keyName)
	}
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, fmt.Errorf("environment variable %q is not standard base64: %w", keyName, err)
	}
	return &hexxladb.Options{EncryptionKey: key}, nil
}

// ClearCredentials clears owned key bytes and drops the passphrase reference.
func ClearCredentials(opts *hexxladb.Options) {
	if opts == nil {
		return
	}
	clear(opts.EncryptionKey)
	opts.EncryptionKey = nil
	opts.Passphrase = ""
}

// OpenExisting opens an operator-selected database with an optional environment credential.
func OpenExisting(path string) (*hexxladb.DB, error) {
	opts, err := CredentialOptions("HEXXLA_PASSPHRASE", "HEXXLA_ENCRYPTION_KEY")
	if err != nil {
		return nil, err
	}
	defer ClearCredentials(opts)
	return hexxladb.OpenExisting(path, opts)
}
