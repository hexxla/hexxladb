# Ports and adapters example

This reference application shows how to build use cases over HexxlaDB. The application calls the storage interface in `internal/domain`. The outbound adapter in `internal/adapters/out/hexxladb` implements that interface using the public database API. The example uses public HexxlaDB value types to illustrate the storage contract.

Run `go test ./examples/ports_adapters/...` from the module root for the application and adapter contract tests.
