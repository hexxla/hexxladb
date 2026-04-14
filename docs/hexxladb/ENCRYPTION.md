# At-rest encryption (M9)

**Audience:** Callers configuring [`Options`](../../options.go) on [`Open`](../../db.go).

## What is encrypted

- **Data pages** (page id **≥ 1**) use **AES-256-XTS** with the **page id** as the tweak (sector index). Length is unchanged (**64 KiB** per page), so the on-disk B+ tree layout is unchanged.
- **Page 0** (file header) is **not** encrypted so magic, format version, allocator fields, and encryption metadata remain readable without a key.

## Header metadata

When encryption is enabled on a **new** database, the engine sets header **`Features`** bit **`FeatureEncryptedDataPages`** and stores a **16-byte** **`EncryptionSalt`** (random for passphrase mode; zeros when only a raw [`Options.EncryptionKey`](../../options.go) is used).

See [`internal/engine/ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md) for offsets.

## Key material

- **Raw key:** [`Options.EncryptionKey`](../../options.go) — arbitrary secret; stretched to a **64-byte** XTS key with **HKDF-SHA256** (info label `hexxladb-m9-aes-xts-v1`).
- **Passphrase:** [`Options.Passphrase`](../../options.go) — **[Argon2id](https://pkg.go.dev/golang.org/x/crypto/argon2)** derives **32 bytes** from the passphrase and **`EncryptionSalt`**; then the same HKDF produces the XTS key. Use [`DeriveKeyFromPassphrase`](../../encryption.go) only if you need the same KDF outside `Open`.

Do **not** use [`EncryptionKey`](../../options.go) and [`Passphrase`](../../options.go) together; [`Open`](../../db.go) returns [`ErrEncryptionOptions`](../../errors.go).

## WAL policy

Redo WAL records store the **same bytes** written to the primary file **after** `BeforeWrite` — i.e. **ciphertext** when encryption is enabled. Replay applies those full page images to the primary without a second transform, matching the engine’s normal write path.

**Threat model:** protects **ciphertext at rest** on disk and on the WAL file if both are copied together. It does **not** authenticate plaintext: **XTS does not provide integrity** comparable to an AEAD; a tampered ciphertext may decrypt to arbitrary bytes. Callers who need **tamper detection** should plan a future format that adds authentication (or use external full-disk encryption).

**Runtime:** key material and decrypted pages in memory are **not** hardened against a local attacker with memory access; that is out of scope for M9.

## Wrong key

Opening with a **wrong** key does not reliably fail at `Open` (no MAC). Operations may return corruption or parse errors. Callers should treat unexpected errors after `Open` as possible key mismatch when the file is marked encrypted.

## Related errors

[`ErrEncryptionKeyRequired`](../../errors.go) — file is encrypted, open attempted without key or passphrase.

[`ErrDatabaseNotEncrypted`](../../errors.go) — encryption options supplied but the existing file is plaintext.

[`ErrEncryptionOptions`](../../errors.go) — encryption combined with custom page hooks or conflicting key options.
