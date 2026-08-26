# Remote-access owner service

This standard-library-only example shows the supported boundary for remote
HexxlaDB use: one application process owns the database handle and exposes a
small authenticated API. Remote clients never open, copy, or modify the
primary, WAL, or changelog files.

The example is boundary-validation code, not a general-purpose database
server. It intentionally provides only authenticated health, cell put, and
cell get operations. Product-specific authorization, tenancy, schemas, and
workflows belong in the owning application.

## Threat model and defaults

- The host, owner process, and local TLS proxy are trusted. A compromised host
  or stolen bearer token is outside this example's protection boundary.
- The listener accepts only an explicit numeric loopback address. A trusted local
  reverse proxy must terminate TLS before traffic reaches it; do not expose the
  listener directly or forward plaintext traffic across a network.
- Every endpoint, including health, requires one bearer token of at least 32
  bytes. Token comparison is constant-time. The example has one deployment
  principal, not per-user authorization or tenant isolation.
- The encrypted database requires a standard-base64 32-byte key. Keys, tokens,
  cell content, and request bodies are not logged.
- Request bodies are limited to 64 KiB and decoded strictly. A global
  120-request/minute fixed window and 16-request admission bound limit one
  instance. These are conservative demonstration defaults, not workload-tuned
  production policy.
- Browser cross-origin access is not enabled. HTTP timeouts, header limits, and
  graceful shutdown are configured.

## Run

Generate independent credentials, keep them in a secrets manager for real
deployments, and start the sole database owner:

```bash
export HEXXLA_DB_PATH=/absolute/path/to/remote.db
export HEXXLA_REMOTE_TOKEN="$(openssl rand -hex 32)"
export HEXXLA_ENCRYPTION_KEY="$(openssl rand -base64 32)"
task demo-remote
```

`HEXXLA_REMOTE_ADDR` defaults to `127.0.0.1:8080` and rejects non-loopback
addresses. From the same host, after retaining the token value:

```bash
curl --fail-with-body \
  -H "Authorization: Bearer $HEXXLA_REMOTE_TOKEN" \
  -H 'Content-Type: application/json' \
  -X PUT http://127.0.0.1:8080/v1/cells \
  --data '{"q":2,"r":-1,"content":"bounded remote cell","tags":["remote"],"confidence":0.8}'

curl --fail-with-body \
  -H "Authorization: Bearer $HEXXLA_REMOTE_TOKEN" \
  'http://127.0.0.1:8080/v1/cells?q=2&r=-1'
```

In a remote deployment, clients use the proxy's HTTPS address, never the
loopback URL.

## Operational cost and limits

The owner process is the availability and scaling boundary. Writes retain
HexxlaDB's serialized commit semantics, while the proxy adds deployment,
certificate, monitoring, and request-routing responsibilities. Token or key
rotation requires coordinated secret replacement and an owner restart. Use the
normal encrypted backup, restore, recovery-drill, and capacity runbooks.

The fixed-window limiter is process-local and global, has no durable state, and
is not fair across principals. Replace the example boundary with
application-specific identity, authorization, audit, admission, and observability
before production use. Multiple independent file owners, distributed
replication, consensus, automatic failover, and tenant isolation are not
provided by this pattern.
