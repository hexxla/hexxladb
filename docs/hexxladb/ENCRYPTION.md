# At-rest encryption

**Audience:** callers configuring [`Options`](../../options.go) on [`Open`](../../db.go), and operators migrating legacy encrypted files.

## Current formats

New databases created with `EncryptionKey` or `Passphrase` use engine **format v3**:

- data pages use **XChaCha20-Poly1305** authenticated encryption;
- page 0 remains readable but carries a keyed HMAC-SHA256 authenticator;
- WAL records contain the same authenticated physical page images plus a keyed MAC;
- the final authenticated header is recorded as the v3 WAL commit marker; and
- an enabled changelog uses its independent authenticated encrypted format v2.

Plaintext creation uses engine v2. Both profiles use MVCC.

## Authenticated page layout

Each v3 logical page has 48 physical bytes of overhead:

```text
rewrite generation (8) | random nonce (24) | ciphertext (logical page size) | tag (16)
```

The associated data binds the database salt/identity, format version, logical page size, page id, and rewrite generation. A modified generation, nonce, ciphertext, tag, or a page image moved to a different page id fails before plaintext is returned. The authenticated header pins the current B+ tree root generation and the first external freelist generation; each freelist metadata page pins the next. This protects allocator publication and reuse metadata, but does not create a trusted generation catalog for every ordinary non-root tree page.

XChaCha20-Poly1305 is the established `golang.org/x/crypto/chacha20poly1305.NewX` construction. Its 24-byte nonce is suitable for random generation and avoids making database correctness depend on reconstructing a global nonce counter after a crash. See the [official Go package documentation](https://pkg.go.dev/golang.org/x/crypto/chacha20poly1305) and the [CFRG XChaCha construction](https://datatracker.ietf.org/doc/html/draft-irtf-cfrg-xchacha).

At a 4 KiB logical page size, the primary-file overhead is 48/4096 = **1.171875%** per allocated data page. `StorageStats.PageSize` reports the logical size and `StorageStats.PhysicalPageSize` reports the physical stride.

## Header and visible metadata

Page 0 is not encrypted. Magic, format, page size, allocation fields, feature bits, salt, commit sequence, and embedding configuration remain visible. In v3, HMAC-SHA256 authenticates the complete fixed 512-byte header prefix, including the current root and its generation.

Traffic analysis remains possible. An observer can estimate allocated page count, page size, WAL activity, changelog record count/order, and frame sizes. Encryption does not hide file paths, access timing, or file growth.

## Key material

- **Raw key:** [`Options.EncryptionKey`](../../options.go) accepts an arbitrary secret. Use at least 128 bits of entropy. New v3 databases derive a 32-byte authenticated master with HKDF-SHA256, then domain-separated page, header, WAL, verifier, and changelog keys.
- **Passphrase:** [`Options.Passphrase`](../../options.go) uses [Argon2id](https://pkg.go.dev/golang.org/x/crypto/argon2) with the random 16-byte database salt, then the same v3 key hierarchy.

Do not set `EncryptionKey` and `Passphrase` together. `Open` returns [`ErrEncryptionOptions`](../../errors.go). Key material and decrypted pages in process memory are not protected from an attacker with process-memory access.

## WAL and recovery

The WAL stores physical page images after encryption. Its HMAC-SHA256 covers `sequence || page_id || payload`, so modification or page-id substitution is rejected.

For v3 transactions, the WAL ends with an authenticated header commit marker. Recovery validates the WAL and marker, writes the committed page generation set, syncs the primary, publishes the matching authenticated header, syncs again, and only then reuses the WAL extent. A crash at a named write barrier therefore reopens to the old state or the complete marked state; it does not publish a new root without its pages.

## Threat model and residual limits

The in-scope attacker can read and modify the primary, WAL, or changelog while the database is closed and does not possess the encryption secret. V3 detects:

- header-field modification;
- data-page generation, nonce, ciphertext, or tag modification;
- moving a valid page image to a different page id;
- replay of an older current-root image while the authenticated header remains current;
- WAL payload or page-id modification; and
- truncation encountered while reading reachable authenticated pages.

Two rollback classes remain outside the v3 local-file guarantee:

1. **Same-slot non-root replay.** An older, valid image for the same non-root page id has valid associated data. Detecting it requires a trusted expected generation for every reachable page, such as a Merkle/generation catalog whose root is independently pinned. HexxlaDB does not yet add that second metadata tree.
2. **Coordinated recovery-set rollback.** Replacing the primary, header, WAL, and related state with one older internally consistent set cannot be distinguished by self-contained files. Prevention requires an external monotonic trust anchor or independently versioned authenticated backup policy.

Use least-privilege file ownership and independently authenticated, offline backups. The AEAD supplies integrity, but it does not make hostile storage available or prevent deletion.

## Changelog policy

- Plaintext databases use changelog format v1.
- Encrypted databases use authenticated encrypted changelog format v2 and reject a plaintext sibling with [`ErrChangelogPlaintext`](../../errors.go).
- Frame sequences and lengths remain visible; logical keys, operations, timestamps, hashes, and inline payloads are encrypted.
- Existing plaintext changelog history is never silently mixed with encrypted frames. Archive/reconcile it according to application retention policy, then create a new encrypted log.
- [`RotateEncryption`](../../rotation.go) re-encrypts changelog history when both option sets enable the same effective changelog path. Its recovery marker makes interrupted filesystem swaps explicit through `ErrRotationIncomplete` and `RecoverInterruptedRotation`.

## Rotation and offline conversion

[`RotateEncryption`](../../rotation.go) rewrites a plaintext v2 or encrypted
v3 database into an encrypted v3 candidate with the replacement credential.
It preserves the commit head, retained versions, snapshot retention floor,
and embedding configuration. It reopens the candidate and checks the head
and floor before replacing the source. Retain a verified recovery set until
the replacement reopens successfully.

Engine v1 and AES-XTS files require the [offline conversion procedure](./OPERATIONS.md#offline-conversion-of-retired-files).

## Related errors and invariants

- [`ErrEncryptionKeyRequired`](../../errors.go): encrypted file opened without a credential.
- [`ErrEncryptionKeyMismatch`](../../errors.go): key/passphrase does not match the database verifier.
- [`ErrCorruptDatabase`](../../db.go): authenticated header/page/WAL validation or reachable-page truncation failed.
- [`ErrMigrationIncomplete`](../../errors.go): an offline conversion candidate still has a checkpoint.
- [`ErrCompactionIncomplete`](../../errors.go): an interrupted copy candidate is unpublished.
- [`ErrChangelogCorrupt`](../../errors.go): authenticated changelog validation failed.

Wrong credentials fail before page data is returned. New encrypted files are v3 from their first durable header. Compaction preserves the source profile. Backup preserves encryption byte-for-byte. Rotation writes through the destination format from its first page.
