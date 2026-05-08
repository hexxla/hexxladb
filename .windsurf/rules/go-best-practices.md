# Go Best Practices

- Always run `gofmt -s` and `goimports` (or `golangci-lint fmt ./...` for both)
- Wrap errors with `%w`; use `errors.Is`/`errors.As` for comparisons
- Use integer range loops (`for i := range n`) in Go 1.22+
- New benchmarks: use `testing.B.Loop` (Go 1.24+)
- Use `log/slog` in `cmd/` and adapters
- Keep tests fast and focused; integration tests get `//go:build integration`
- See https://go.dev/doc/devel/release for Go release notes
