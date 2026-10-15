#!/usr/bin/env bash
# Checks the local toolchain and installed dependencies for development.
#
#   scripts/doctor.sh            full check (toolchain + installed deps)
#   scripts/doctor.sh --prereqs  toolchain only (used by `make setup`)
#
# Exits 1 when a required check fails; warnings never fail the run.
set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PREREQS_ONLY=0
[ "${1:-}" = "--prereqs" ] && PREREQS_ONLY=1

if [ -t 1 ]; then
	GREEN=$'\033[32m' YELLOW=$'\033[33m' RED=$'\033[31m' BOLD=$'\033[1m' RESET=$'\033[0m'
else
	GREEN='' YELLOW='' RED='' BOLD='' RESET=''
fi

failures=0
warnings=0
ok() { printf '  %s✓%s %s\n' "$GREEN" "$RESET" "$1"; }
warn() { printf '  %s!%s %s\n' "$YELLOW" "$RESET" "$1"; warnings=$((warnings + 1)); }
fail() { printf '  %s✗%s %s\n' "$RED" "$RESET" "$1"; failures=$((failures + 1)); }
section() { printf '\n%s%s%s\n' "$BOLD" "$1" "$RESET"; }

# version_ge A B: true when dotted version A >= B.
version_ge() {
	local IFS=.
	local -a a=($1) b=($2)
	local i x y
	for i in 0 1 2; do
		x=${a[i]:-0} y=${b[i]:-0}
		x=${x%%[!0-9]*} y=${y%%[!0-9]*}
		x=${x:-0} y=${y:-0}
		if ((10#$x > 10#$y)); then return 0; fi
		if ((10#$x < 10#$y)); then return 1; fi
	done
	return 0
}

# require NAME MIN_VERSION ACTUAL_VERSION HINT
check_version() {
	local name=$1 min=$2 actual=$3 hint=$4
	if [ -z "$actual" ]; then
		fail "$name not found (need >= $min). $hint"
	elif version_ge "$actual" "$min"; then
		ok "$name $actual (>= $min)"
	else
		fail "$name $actual is too old (need >= $min). $hint"
	fi
}

GO_MIN="$(awk '/^go /{print $2; exit}' "$ROOT/backend/go.mod")"
NODE_MIN=24
NPM_MIN=10

section "Toolchain"
go_version=""
command -v go >/dev/null 2>&1 && go_version="$(go env GOVERSION 2>/dev/null | sed 's/^go//')"
check_version go "$GO_MIN" "$go_version" "Install from https://go.dev/dl/"

node_version=""
command -v node >/dev/null 2>&1 && node_version="$(node --version | sed 's/^v//')"
check_version node "$NODE_MIN" "$node_version" "Install from https://nodejs.org/"

npm_version=""
command -v npm >/dev/null 2>&1 && npm_version="$(npm --version 2>/dev/null)"
check_version npm "$NPM_MIN" "$npm_version" "Ships with Node.js"

if command -v git >/dev/null 2>&1; then
	ok "git $(git --version | awk '{print $3}')"
else
	fail "git not found. Required for worktrees and agent integration."
fi

section "Optional tools"
if command -v tmux >/dev/null 2>&1; then
	ok "tmux $(tmux -V | awk '{print $2}')"
else
	warn "tmux not found. Source terminal runs fall back to it on macOS/Linux (brew install tmux)."
fi
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
	ok "docker (daemon reachable)"
else
	warn "docker not reachable. Only needed for local CI (agent-ci) and the local cloud stack."
fi
agents=""
for agent in claude codex aider gemini opencode; do
	command -v "$agent" >/dev/null 2>&1 && agents="$agents $agent"
done
if [ -n "$agents" ]; then
	ok "agent CLI(s):$agents"
else
	warn "no agent CLI found (claude, codex, aider, ...). Needed to run real sessions."
fi

if [ "$PREREQS_ONLY" = 0 ]; then
	section "Dependencies"
	for dir in . frontend packages/cloud-client packages/product-ui; do
		label="${dir#./}"
		[ "$dir" = "." ] && label="root"
		if [ ! -d "$ROOT/$dir/node_modules" ]; then
			fail "$label: node_modules missing. Run: make setup"
		elif [ "$ROOT/$dir/package-lock.json" -nt "$ROOT/$dir/node_modules/.package-lock.json" ]; then
			warn "$label: package-lock.json is newer than node_modules. Run: make setup"
		else
			ok "$label: node_modules installed"
		fi
	done
	if [ -n "$go_version" ]; then
		if (cd "$ROOT/backend" && GOPROXY=off GOFLAGS=-mod=mod go mod download >/dev/null 2>&1); then
			ok "backend: Go modules resolvable"
		else
			fail "backend: Go modules not downloaded. Run: make setup"
		fi
	fi

	section "App state"
	data_dir="${AO_DATA_DIR:-$HOME/.ao}"
	if [ -d "$data_dir" ]; then
		ok "data dir $data_dir exists"
	else
		ok "data dir $data_dir will be created on first daemon start"
	fi
fi

printf '\n'
if [ "$failures" -gt 0 ]; then
	printf '%s%d problem(s), %d warning(s).%s\n' "$RED" "$failures" "$warnings" "$RESET"
	exit 1
fi
printf '%sAll required checks passed%s (%d warning(s)).\n' "$GREEN" "$RESET" "$warnings"
