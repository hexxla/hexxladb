// Package storagecontract provides reusable contract tests for [domain.Storage]
// implementations. Any adapter can import this package and call [RunAll] from
// its own *testing.T to validate behavioral contracts.
//
// The contracts verify interface semantics (round-trips, ordering, idempotency)
// without depending on any specific adapter.
package storagecontract
