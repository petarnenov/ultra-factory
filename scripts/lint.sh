#!/usr/bin/env bash
# Runs the go.yml lint job locally with the Go toolchain pinned to go.mod.
# Without arguments it also runs the backend suite, as `npm run lint` did.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT/backend"

GO_VERSION="$(awk '/^go[[:space:]]/{print $2; exit}' go.mod)"
export GOTOOLCHAIN="go${GO_VERSION}"

# CI runs without AO_* variables; clear them so session state cannot leak in.
while IFS='=' read -r name _; do
	case "$name" in AO_*) unset "$name" ;; esac
done < <(env)

if [ "${1:-}" != "--lint-only" ]; then
	go test -p "${GO_TEST_PARALLELISM:-2}" ./...
fi
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run --path-mode=abs
