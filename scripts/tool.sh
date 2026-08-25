#!/usr/bin/env bash
# Run repository tooling at reviewed versions without requiring PATH installation.
set -euo pipefail

if (($# == 0)); then
	echo "usage: $0 <golangci-lint|gocyclo|gocognit|gosec|govulncheck> [args...]" >&2
	exit 2
fi

tool="$1"
shift

# Build and run analyzers with the module's minimum toolchain. A newer host Go can
# otherwise expose standard-library export data the pinned analyzers do not understand.
export GOTOOLCHAIN=go1.27.0

case "$tool" in
	golangci-lint)
		exec go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.1 "$@"
		;;
	gocyclo)
		exec go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 "$@"
		;;
	gocognit)
		exec go run github.com/uudashr/gocognit/cmd/gocognit@v1.2.1 "$@"
		;;
	gosec)
		# Keep package loading deterministic on hosts without a working C toolchain.
		export CGO_ENABLED=0
		exec go run github.com/securego/gosec/v2/cmd/gosec@v2.28.0 "$@"
		;;
	govulncheck)
		exec go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 "$@"
		;;
	*)
		echo "unknown repository tool: $tool" >&2
		exit 2
		;;
esac
