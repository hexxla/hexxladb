// Package app holds application orchestration and use cases ([Service.PutCell], etc.).
// See docs/architecture/HEXAGONAL_ARCHITECTURE.md
// for layering (embedding vs consumer services). HexxlaDB ships outbound adapters under
// internal/adapters/out; inbound transports are implemented by services that import this module,
// not inside hexxladb.
package app
