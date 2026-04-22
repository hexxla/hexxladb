// Package in is a reserved name for illustration only.
//
// HexxlaDB does not ship inbound transports (HTTP, gRPC, metrics endpoints) in this module.
// Production services that embed github.com/hexxla/hexxladb implement their own adapters in their
// repository and call package hexxladb or internal/app/domain from their composition root.
//
// Outbound persistence uses [domain.Storage]; the reference implementation is internal/adapters/out/hexxladb.
// See docs/hexxladb/HEXXLA_PRODUCT_WIRING.md and docs/context/HEXAGONAL_ARCHITECTURE.md for boundary guidance.
package in
