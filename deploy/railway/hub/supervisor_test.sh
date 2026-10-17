#!/usr/bin/env bash
# Checks hub-supervisor.sh without Docker, the daemon, or the BFF: the
# daemon-listener seed and repair, the fail-fast rule when either child exits,
# and SIGTERM shutdown. Run: bash deploy/railway/hub/supervisor_test.sh
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
supervisor=$here/hub-supervisor.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failures=0

check() {
	local name=$1
	shift
	if "$@"; then
		printf 'ok   %s\n' "$name"
	else
		printf 'FAIL %s\n' "$name"
		failures=$((failures + 1))
	fi
}

# Runs the supervisor's preparation steps against a scratch data dir.
seed() {
	AO_HUB_SUPERVISOR_DRY_RUN=1 AO_HUB_NPM_SEED= AO_DATA_DIR="$work/ao" AO_HUB_DATA_DIR="$work/hub" HOME="$work/home" \
		bash "$supervisor" 2>"$work/seed.log"
}

config=$work/ao/mobile/config.json

test_seeds_loopback_listener() {
	seed && jq -e '.enabled == true and .loopbackOnly == true and .lastPort == 3011 and (.password | length == 32)' "$config" >/dev/null &&
		[[ $(stat -c %a "$config") == 600 ]]
}

test_keeps_password_and_repairs_listener() {
	local before after
	before=$(jq -r .password "$config")
	jq '.loopbackOnly = false | .lastPort = 3999' "$config" >"$work/c.json" && mv "$work/c.json" "$config"
	seed && after=$(jq -r .password "$config") &&
		[[ $before == "$after" ]] && jq -e '.loopbackOnly == true and .lastPort == 3011' "$config" >/dev/null
}

test_rejects_invalid_json() {
	echo '{not json' >"$config"
	! seed && grep -q 'not valid JSON' "$work/seed.log"
	rm -f "$config"
}

test_never_logs_password() {
	seed
	local password
	password=$(jq -r .password "$config")
	! grep -q "$password" "$work/seed.log"
}

# fake_package writes a minimal npm package and its relative bin link.
fake_package() {
	local prefix=$1 pkg=$2 version=$3 bin=$4
	mkdir -p "$prefix/lib/node_modules/$pkg" "$prefix/bin"
	printf '{"name":"%s","version":"%s"}\n' "$pkg" "$version" >"$prefix/lib/node_modules/$pkg/package.json"
	printf '#!/bin/sh\necho %s\n' "$version" >"$prefix/lib/node_modules/$pkg/cli.js"
	chmod +x "$prefix/lib/node_modules/$pkg/cli.js"
	ln -sfn "../lib/node_modules/$pkg/cli.js" "$prefix/bin/$bin"
}

sync_agents() {
	AO_HUB_SUPERVISOR_DRY_RUN=1 AO_DATA_DIR="$work/ao" AO_HUB_DATA_DIR="$work/hub" HOME="$work/home" \
		AO_HUB_NPM_SEED="$work/seed" NPM_CONFIG_PREFIX="$work/prefix" bash "$supervisor" 2>"$work/sync.log"
}

test_seeds_agent_prefix() {
	fake_package "$work/seed" "@acme/claude" 2.0.0 claude
	fake_package "$work/seed" tool 1.0.0 tool
	sync_agents && [[ $("$work/prefix/bin/claude") == 2.0.0 && $("$work/prefix/bin/tool") == 1.0.0 ]]
}

test_keeps_newer_ui_update() {
	fake_package "$work/prefix" "@acme/claude" 2.5.0 claude
	sync_agents && [[ $("$work/prefix/bin/claude") == 2.5.0 ]] && ! grep -q '@acme/claude' "$work/sync.log"
}

test_newer_image_replaces_older_copy() {
	fake_package "$work/seed" tool 1.10.0 tool
	sync_agents && [[ $("$work/prefix/bin/tool") == 1.10.0 && $("$work/prefix/bin/claude") == 2.5.0 ]] &&
		grep -q 'agent CLI tool 1.0.0 -> 1.10.0' "$work/sync.log"
}

# A fake "daemon" that stays up, and a fake BFF that exits with status 3: the
# supervisor must stop the daemon and exit with 3.
test_exits_when_one_child_exits() {
	local status=0
	AO_HUB_SUPERVISOR_DRY_RUN=0 AO_HUB_NPM_SEED= AO_DATA_DIR="$work/ao" AO_HUB_DATA_DIR="$work/hub" HOME="$work/home" \
		AO_HUB_DAEMON_COMMAND="sleep 6130" AO_HUB_BFF_MAIN="$work/exit3.js" \
		timeout 20 bash "$supervisor" 2>"$work/exit.log" || status=$?
	[[ $status == 3 ]] && grep -q 'hub BFF exited with status' "$work/exit.log" && ! pgrep -a -x sleep | grep -q ' 6130$'
}

# SIGTERM to the supervisor must stop both children and exit 0.
test_sigterm_stops_both() {
	local pid status=0
	AO_HUB_SUPERVISOR_DRY_RUN=0 AO_HUB_NPM_SEED= AO_DATA_DIR="$work/ao" AO_HUB_DATA_DIR="$work/hub" HOME="$work/home" \
		AO_HUB_DAEMON_COMMAND="sleep 6130" AO_HUB_BFF_MAIN="$work/sleep.js" \
		bash "$supervisor" 2>"$work/term.log" &
	pid=$!
	sleep 1
	kill -TERM "$pid"
	wait "$pid" || status=$?
	[[ $status == 0 ]] && ! pgrep -a -x sleep | grep -q ' 6130$'
}

printf 'process.exit(3);\n' >"$work/exit3.js"
printf 'setInterval(() => {}, 1000);\n' >"$work/sleep.js"
mkdir -p "$work/home"

check "seeds a loopback-only listener on 3011 with a 0600 config" test_seeds_loopback_listener
check "keeps the password and repairs the listener on restart" test_keeps_password_and_repairs_listener
check "rejects an invalid config.json" test_rejects_invalid_json
check "never prints the generated password" test_never_logs_password
check "seeds the image's agent CLIs into the volume prefix" test_seeds_agent_prefix
check "keeps an agent CLI updated from the UI" test_keeps_newer_ui_update
check "replaces an older copy with a newer image version" test_newer_image_replaces_older_copy
check "exits with the status of the first child that exits" test_exits_when_one_child_exits
check "SIGTERM stops both processes and exits 0" test_sigterm_stops_both

if ((failures > 0)); then
	printf '%d check(s) failed\n' "$failures"
	exit 1
fi
printf 'all checks passed\n'
