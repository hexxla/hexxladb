# At-rest encryption

**Audience:** Callers configuring [`Options`](../../options.go) on [`Open`](../../db.go).

## What is encrypted

- **Data pages** (page id **≥ 1**) use **AES-256-XTS** with the **page id** as the tweak (sector index). Length is unchanged (equals the database's page size), so the on-disk B+ tree layout is unchanged.
- **Page 0** (file header) is **not** encrypted so magic, format version, allocator fields, and encryption metadata remain readable without a key.
- When [`Options.ChangelogEnabled`](../../options.go) is set, an encrypted database creates an authenticated encrypted **changelog format v2**. Its logical keys, operations, timestamps, hashes, and inline payloads use **XChaCha20-Poly1305**. The outer frame sequence and length remain visible, so an observer can estimate record count, order, and size.

## Header metadata

When encryption is enabled on a **new** database, the engine sets header **`Features`** bit **`FeatureEncryptedDataPages`** and stores:

- a **16-byte** **`EncryptionSalt`** (random), and
- an **`encryption_key_check`** verifier for deterministic wrong-key detection.

See [`internal/engine/ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md) for offsets.

## Key material

- **Raw key:** [`Options.EncryptionKey`](../../options.go) — arbitrary secret; stretched to a **64-byte** XTS key with **HKDF-SHA256**. The historical info label `hexxladb-m9-aes-xts-v1` is part of the encrypted-file compatibility contract.
- **Passphrase:** [`Options.Passphrase`](../../options.go) — **[Argon2id](https://pkg.go.dev/golang.org/x/crypto/argon2)** derives **32 bytes** from the passphrase and **`EncryptionSalt`**; then the same HKDF produces the XTS key. Use [`DeriveKeyFromPassphrase`](../../encryption.go) only if you need the same KDF outside `Open`.
- **Changelog:** a database- and purpose-specific master key is derived from the XTS key and database salt. Format v2 derives separate header-MAC and frame-AEAD subkeys from that master and a random per-log nonce prefix; it never reuses the page or WAL key directly.

Do **not** use [`EncryptionKey`](../../options.go) and [`Passphrase`](../../options.go) together; [`Open`](../../db.go) returns [`ErrEncryptionOptions`](../../errors.go).

## WAL policy

Redo WAL records store the **same bytes** written to the primary file **after** `BeforeWrite` — i.e. **ciphertext** when encryption is enabled. Replay applies those full page images to the primary without a second transform, matching the engine’s normal write path.

For encrypted databases, WAL records also carry a keyed **HMAC-SHA256** authenticator (`seq || page_id || payload`) so tampering is rejected during replay.

## Changelog policy

- Plaintext databases continue to use changelog format v1.
- Encrypted databases create changelog format v2 and reject a format-v1 sibling with [`ErrChangelogPlaintext`](../../errors.go). They never append encrypted frames to a plaintext file or plaintext frames to an encrypted file.
- To handle a legacy plaintext changelog beside an already encrypted database, close the database, preserve the old log according to the application's audit/retention policy, reconcile or rebuild consumers from authoritative database state, move the plaintext file away, and reopen to create a new encrypted log.
- Offline [`RotateEncryption`](../../rotation.go) re-encrypts and preserves the changelog when both current and new options enable it with the same effective path. Rotation from a plaintext database converts format v1 to encrypted format v2. Rotation rejects changing `ChangelogEnabled` or `ChangelogPath` in the same operation.

## Threat model and authenticated-page decision

The WAL has a keyed MAC and changelog v2 uses an AEAD, so modification of either is rejected. **AES-XTS data pages remain confidential but unauthenticated**: modified primary-file ciphertext can decrypt to arbitrary bytes. B+ tree validation may detect some resulting structural damage, but it is not a cryptographic integrity guarantee.

Authenticated primary pages are deferred to a future on-disk format rather than added as an unsafe patch. Pages are rewritten in place, so an AEAD design needs a unique persisted nonce/generation and authentication tag per rewrite, crash-consistent WAL integration, revised page capacity, migration and downgrade rules, and representative performance evidence. The current fixed-size XTS format cannot add those properties compatibly.

Until that format exists, use trusted storage and access controls plus independently authenticated, offline backups or a storage layer that explicitly provides authenticated integrity. Ordinary full-disk encryption may also use an unauthenticated mode and must not be assumed to detect tampering.

**Runtime:** key material and decrypted pages in memory are **not** hardened against a local attacker with memory access; that is outside the at-rest threat model.

## Wrong key

Opening with a **wrong** key/passphrase now fails at **`Open`** with **[`ErrEncryptionKeyMismatch`](../../errors.go)** when the database has an `encryption_key_check` verifier (new encrypted DBs and legacy encrypted DBs after first successful open with a key).

Legacy encrypted files without a verifier are upgraded in-place on successful keyed open (header update only), enabling deterministic mismatch detection on subsequent opens.

## Related errors

[`ErrEncryptionKeyRequired`](../../errors.go) — file is encrypted, open attempted without key or passphrase.

[`ErrDatabaseNotEncrypted`](../../errors.go) — encryption options supplied but the existing file is plaintext.

[`ErrEncryptionOptions`](../../errors.go) — encryption combined with custom page hooks or conflicting key options.

[`ErrEncryptionKeyMismatch`](../../errors.go) — provided key/passphrase does not match the database verifier.

[`ErrChangelogPlaintext`](../../errors.go) — an encrypted database encountered a legacy plaintext changelog; it is rejected without modification.

[`ErrChangelogEncryptionKeyRequired`](../../errors.go) — a plaintext database/open configuration encountered an encrypted changelog.

[`ErrChangelogEncryptionKeyMismatch`](../../errors.go) — the changelog header does not authenticate with the database-specific changelog key.

## Security invariants

- Wrong key/passphrase fails deterministically at `Open` for encrypted files with an `encryption_key_check`.
- Corrupt/truncated WAL remains rejected on replay (`ErrCorruptWAL` -> public `ErrCorruptDatabase` path).
- Changelog format v2 rejects wrong keys and modified headers or frames before returning logical data.
- Offline key rotation preserves logical key/value and changelog contents and invalidates old credentials when changelog configuration is preserved.
- Primary data-page tamper detection remains an explicitly accepted residual risk pending an authenticated on-disk format.
