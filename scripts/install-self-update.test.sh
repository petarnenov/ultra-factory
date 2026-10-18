#!/usr/bin/env bash
# Tests for install-self-update.sh: layout and service detection.
# Run: scripts/install-self-update.test.sh all
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/install-self-update.sh"
tmp="$(cd "$(mktemp -d)" && pwd -P)"
trap 'rm -rf "$tmp"' EXIT

ok() { printf 'ok   %s\n' "$*"; }
fail() {
	printf 'FAIL %s\n' "$*" >&2
	exit 1
}
assert_contains() {
	case "$2" in
		*"$1"*) ;;
		*) fail "$3: '$1' not found in: $2" ;;
	esac
}

# Discovery must not depend on the machine running the tests.
shim="$tmp/bin"
mkdir -p "$shim"
for tool in python3 curl tar uname basename dirname grep head id; do
	ln -sf "$(command -v "$tool")" "$shim/$tool"
done

# The installer must not see the ao on this machine, so it runs with a PATH
# that only carries the tools it needs.
run_installer() {
	env PATH="$shim" /bin/bash "$script" "$@"
}

fake_daemon() {
	mkdir -p "$(dirname "$1")"
	printf '%s\n' '#!/bin/sh' 'echo 9.9.9 commit abc123456 built 2026-01-01T00:00:00Z' >"$1"
	chmod 0755 "$1"
}

write_service() {
	local bin="$1" name="$2" wrapper="${3:-}"
	case "$(uname -s)" in
		Linux)
			mkdir -p "$HOME/.config/systemd/user"
			{
				printf '%s\n' '[Unit]' 'Description=test'
				printf 'ExecStart=%s daemon\n' "${bin//$HOME/%h}"
			} >"$HOME/.config/systemd/user/$name.service"
			;;
		Darwin)
			mkdir -p "$HOME/Library/LaunchAgents"
			program="$bin"
			if [[ -n "$wrapper" ]]; then
				{
					printf '%s\n' '#!/bin/sh'
					printf 'exec %s daemon\n' "$bin"
				} >"$wrapper"
				chmod 0755 "$wrapper"
				program="$wrapper"
			fi
			python3 - "$HOME/Library/LaunchAgents/$name.plist" "$program" "$name" <<'PY'
import plistlib, sys

path, program, label = sys.argv[1:4]
with open(path, "wb") as handle:
    plistlib.dump({"Label": label, "ProgramArguments": [program]}, handle)
PY
			;;
	esac
}

case "${1:-all}" in
	service)
		export HOME="$tmp/home/service"
		mkdir -p "$HOME"
		bin="$HOME/.local/share/ao/daemon/ao"
		fake_daemon "$bin"
		write_service "$bin" "ao"
		out="$(run_installer --dry-run --channel test)"
		assert_contains "plan: layout=bundle installDir=$HOME/.local/share/ao" "$out" "bundle layout"
		assert_contains "channel=test" "$out" "channel"
		[[ "$out" == *"ao"* ]] || fail "service name missing: $out"
		ok service
		;;
	release)
		export HOME="$tmp/home/release"
		release="$HOME/.ao/host/releases/20260101000000-1"
		mkdir -p "$release/resources" "$HOME/.ao/host"
		fake_daemon "$release/resources/daemon/ao"
		ln -sfn "$release" "$HOME/.ao/host/current"
		if [[ "$(uname -s)" == Darwin ]]; then
			write_service "$HOME/.ao/host/current/resources/daemon/ao" "ao" "$HOME/.ao/host/run-daemon.sh"
		else
			write_service "$HOME/.ao/host/current/resources/daemon/ao" "ao"
		fi
		out="$(run_installer --dry-run --channel test)"
		assert_contains "plan: layout=release installDir=$HOME/.ao/host/current/resources" "$out" "release layout"
		ok release
		;;
	flags)
		export HOME="$tmp/home/flags"
		mkdir -p "$HOME/bin"
		case "$(uname -s)" in
			Linux) extra="--unit custom.service" ;;
			Darwin) extra="--label dev.custom" ;;
		esac
		out="$(run_installer --dry-run --channel test --layout binary --install-dir "$HOME/bin" $extra)"
		assert_contains "plan: layout=binary installDir=$HOME/bin" "$out" "explicit layout"
		assert_contains "custom" "$out" "explicit service"
		ok flags
		;;
	all)
		for name in service release flags; do
			"$0" "$name" >"$tmp/$name.out" 2>&1 || {
				cat "$tmp/$name.out" >&2
				fail "case $name"
			}
			tail -1 "$tmp/$name.out"
		done
		;;
	*)
		printf 'Usage: %s {all|service|release|flags}\n' "$0" >&2
		exit 2
		;;
esac
