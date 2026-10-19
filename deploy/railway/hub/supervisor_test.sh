#!/usr/bin/env bash
# Checks hub-supervisor.sh without Docker, the daemon, or the BFF: the listener
# seed and repair, fail-fast when either child exits, and SIGTERM shutdown.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
supervisor=$here/hub-supervisor.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failures=0
unset TS_AUTHKEY AO_REMOTE_TAILNET_SOCKS AO_SUPERVISED

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

test_exports_remotes_file_to_daemon() {
	local output="$work/daemon-env" fake="$work/daemon-env.sh" pid
	cat >"$fake" <<EOF
#!/usr/bin/env bash
printf '%s' "\$AO_REMOTES_FILE" >"$output"
sleep 6132
EOF
	chmod +x "$fake"
	AO_HUB_SUPERVISOR_DRY_RUN=0 AO_HUB_NPM_SEED= AO_DATA_DIR="$work/ao" AO_HUB_DATA_DIR="$work/hub" HOME="$work/home" \
		AO_HUB_DAEMON_COMMAND="$fake" AO_HUB_BFF_MAIN="$work/sleep.js" bash "$supervisor" 2>"$work/remotes.log" &
	pid=$!
	for _ in $(seq 1 50); do [[ -f $output ]] && break; sleep 0.1; done
	kill -TERM "$pid" 2>/dev/null || true
	wait "$pid" || true
	[[ -f $output && $(cat "$output") == "$work/hub/remotes.json" ]]
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

opencode_v2_prefix=$work/home/.local/share/opencode-v2-home/npm

sync_opencode() {
	AO_HUB_SUPERVISOR_DRY_RUN=1 AO_DATA_DIR="$work/ao" AO_HUB_DATA_DIR="$work/hub" HOME="$work/home" \
		AO_HUB_NPM_SEED= AO_HUB_OPENCODE_V2_SEED="$work/oc-seed" bash "$supervisor" 2>"$work/oc-sync.log"
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

test_seeds_opencode_v2_into_private_prefix() {
	fake_package "$work/oc-seed" "@opencode/cli" 2.0.32 opencode
	sync_opencode && [[ $("$opencode_v2_prefix/bin/opencode") == 2.0.32 ]]
}

test_keeps_newer_opencode_v2_ui_update() {
	fake_package "$opencode_v2_prefix" "@opencode/cli" 2.1.0 opencode
	sync_opencode && [[ $("$opencode_v2_prefix/bin/opencode") == 2.1.0 ]] &&
		! grep -q '@opencode/cli' "$work/oc-sync.log"
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

# A critical child that exits cleanly (status 0) must still be a failure: the
# platform did not restart a clean container exit (issue #247).
test_clean_bff_exit_is_a_failure() {
	local status=0
	AO_HUB_SUPERVISOR_DRY_RUN=0 AO_HUB_NPM_SEED= AO_DATA_DIR="$work/ao" AO_HUB_DATA_DIR="$work/hub" HOME="$work/home" \
		AO_HUB_DAEMON_COMMAND="sleep 6136" AO_HUB_BFF_MAIN="$work/exit0.js" \
		timeout 20 bash "$supervisor" 2>"$work/exit0.log" || status=$?
	[[ $status == 1 ]] && grep -q 'hub BFF exited with status 0 and no termination signal' "$work/exit0.log" &&
		! pgrep -a -x sleep | grep -q ' 6136$'
}

test_clean_daemon_exit_is_a_failure() {
	local status=0
	AO_HUB_SUPERVISOR_DRY_RUN=0 AO_HUB_NPM_SEED= AO_DATA_DIR="$work/ao" AO_HUB_DATA_DIR="$work/hub" HOME="$work/home" \
		AO_HUB_DAEMON_COMMAND="$work/exit0.sh" AO_HUB_BFF_MAIN="$work/sleep.js" \
		timeout 20 bash "$supervisor" 2>"$work/daemon-exit0.log" || status=$?
	[[ $status == 1 ]] && grep -q 'ao daemon exited with status 0 and no termination signal' "$work/daemon-exit0.log"
}

# AO_SUPERVISED marks the daemon only, so its loopback /shutdown refuses while
# the BFF keeps its normal environment.
test_marks_only_the_daemon_as_supervised() {
	local pid status=0
	cat >"$work/daemon-supervised.sh" <<'EOF'
#!/usr/bin/env bash
printf '%s' "${AO_SUPERVISED:-unset}" >"$AO_HUB_TEST_DIR/daemon-supervised"
exec sleep 6137
EOF
	cat >"$work/bff-supervised.js" <<'EOF'
require('node:fs').writeFileSync(process.env.AO_HUB_TEST_DIR + '/bff-supervised',
  process.env.AO_SUPERVISED || 'unset');
setInterval(() => {}, 1000);
EOF
	chmod +x "$work/daemon-supervised.sh"
	AO_HUB_TEST_DIR="$work" AO_HUB_SUPERVISOR_DRY_RUN=0 AO_HUB_NPM_SEED= AO_DATA_DIR="$work/ao" AO_HUB_DATA_DIR="$work/hub" HOME="$work/home" \
		AO_HUB_DAEMON_COMMAND="$work/daemon-supervised.sh" AO_HUB_BFF_MAIN="$work/bff-supervised.js" \
		bash "$supervisor" 2>"$work/supervised.log" &
	pid=$!
	for _ in $(seq 1 50); do
		[[ -f $work/daemon-supervised && -f $work/bff-supervised ]] && break
		sleep 0.1
	done
	kill -TERM "$pid" 2>/dev/null || true
	wait "$pid" || status=$?
	[[ $status == 0 ]] && [[ $(cat "$work/daemon-supervised") == 1 ]] &&
		[[ $(cat "$work/bff-supervised") == unset ]]
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

# With AO_HUB_ORCHESTRATOR_TICK=1 a tick loop that exits is restarted and the
# daemon and BFF keep running; SIGTERM then stops all three.
test_tick_loop_restarts_without_stopping_the_hub() {
	local pid status=0
	printf '#!/usr/bin/env bash\necho run >>"%s"\nexit 3\n' "$work/tick-runs" >"$work/tick.sh"
	AO_HUB_SUPERVISOR_DRY_RUN=0 AO_DATA_DIR="$work/ao" AO_HUB_DATA_DIR="$work/hub" HOME="$work/home" \
		AO_HUB_DAEMON_COMMAND="sleep 6131" AO_HUB_BFF_MAIN="$work/sleep.js" \
		AO_HUB_ORCHESTRATOR_TICK=1 AO_HUB_TICK_COMMAND="bash $work/tick.sh" AO_HUB_TICK_START_DELAY=0 AO_HUB_TICK_RESTART_DELAY=1 \
		bash "$supervisor" 2>"$work/tick.log" &
	pid=$!
	sleep 3.5
	kill -0 "$pid" && pgrep -a -x sleep | grep -q ' 6131$' &&
		(($(wc -l <"$work/tick-runs") >= 2)) &&
		grep -q 'orchestrator tick loop exited with status 3; restarting in 1s' "$work/tick.log" || {
		kill -TERM "$pid"
		return 1
	}
	kill -TERM "$pid"
	wait "$pid" || status=$?
	[[ $status == 0 ]] && ! pgrep -a -x sleep | grep -q ' 6131$'
}

# The tick loop is opt-in.
test_tick_loop_is_off_by_default() {
	seed && ! grep -q 'tick loop' "$work/seed.log" &&
		AO_HUB_ORCHESTRATOR_TICK=1 seed && grep -q 'would start the tick loop: ao orchestrator tick loop' "$work/seed.log"
}

test_tailnet_dry_run_is_opt_in() {
	seed && ! grep -q 'Tailscale' "$work/seed.log" &&
		TS_AUTHKEY=test-key AO_HUB_SUPERVISOR_DRY_RUN=1 AO_HUB_NPM_SEED= AO_DATA_DIR="$work/ao" AO_HUB_DATA_DIR="$work/hub" HOME="$work/home" \
			bash "$supervisor" 2>"$work/tailnet-dry-run.log" &&
		grep -q 'would start Tailscale userspace SOCKS5 on 127.0.0.1:1055' "$work/tailnet-dry-run.log" &&
		! grep -q 'test-key' "$work/tailnet-dry-run.log" &&
		! grep -e 'tailscale' -e 'TS_AUTHKEY' "$config"
}

test_tailnet_passes_gate_to_daemon_bff_and_tick() {
	local authkey="fixture-$(head -c 8 /dev/urandom | od -An -tx1 | tr -d ' \\n')" pid status=0
	cat >"$work/fake-tailscaled.js" <<'EOF'
const net = require('node:net');
const socketPath = process.argv.find((arg) => arg.startsWith('--socket='))?.slice('--socket='.length);
if (!socketPath || process.env.TS_AUTHKEY) process.exit(41);
require('node:fs').writeFileSync(process.env.AO_HUB_TEST_DIR + '/tailscaled.mtu', String(process.env.TS_DEBUG_MTU));
const server = net.createServer();
server.listen(socketPath);
const stop = () => server.close(() => process.exit(0));
process.on('SIGTERM', stop);
process.on('SIGINT', stop);
EOF
	cat >"$work/fake-tailscaled" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
! env | grep -q '^TS_AUTHKEY='
exec node "$AO_HUB_TEST_DIR/fake-tailscaled.js" "$@"
EOF
	cat >"$work/fake-tailscale" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
! env | grep -q '^TS_AUTHKEY='
! env | grep -q '^AO_REMOTE_TAILNET_SOCKS='
bytes=$(cat | wc -c | tr -d ' ')
printf 'stdin-bytes=%s\n' "$bytes" >"$AO_HUB_TEST_DIR/tailscale-up.log"
printf '%s\n' "$@" >>"$AO_HUB_TEST_DIR/tailscale-up.log"
EOF
	cat >"$work/daemon-env.sh" <<'EOF'
#!/usr/bin/env bash
env >"$AO_HUB_TEST_DIR/daemon.env"
exec sleep 6134
EOF
	cat >"$work/bff-env.js" <<'EOF'
require('node:fs').writeFileSync(process.env.AO_HUB_TEST_DIR + '/bff.env', Object.entries(process.env).map(([k,v]) => k+'='+v).join('\n'));
setInterval(() => {}, 1000);
EOF
	cat >"$work/tick-env.sh" <<'EOF'
#!/usr/bin/env bash
env >"$AO_HUB_TEST_DIR/tick.env"
exec sleep 6135
EOF
	chmod +x "$work/fake-tailscaled" "$work/fake-tailscale" "$work/daemon-env.sh" "$work/tick-env.sh"
	mkdir -p "$work/data"
	TS_AUTHKEY="$authkey" TS_HOSTNAME=test-hub AO_HUB_TAILSCALED="$work/fake-tailscaled" AO_HUB_TAILSCALE="$work/fake-tailscale" \
		AO_HUB_TEST_DIR="$work" AO_HUB_TAILSCALE_DIR="$work/data/tailscale" AO_DATA_DIR="$work/data/ao" AO_HUB_DATA_DIR="$work/data/hub" HOME="$work/data/home" \
		AO_HUB_DAEMON_COMMAND="bash $work/daemon-env.sh" AO_HUB_BFF_MAIN="$work/bff-env.js" \
		AO_HUB_ORCHESTRATOR_TICK=1 AO_HUB_TICK_COMMAND="bash $work/tick-env.sh" AO_HUB_TICK_START_DELAY=0 \
		bash "$supervisor" 2>"$work/tailnet-supervisor.log" &
	pid=$!
	for _ in {1..100}; do
		[[ -s $work/daemon.env && -s $work/bff.env && -s $work/tick.env && -s $work/tailscale-up.log ]] && break
		sleep 0.1
	done
	if ! [[ -s $work/daemon.env && -s $work/bff.env && -s $work/tick.env && -s $work/tailscale-up.log ]]; then
		kill -TERM "$pid" 2>/dev/null || true
		wait "$pid" || true
		return 1
	fi
	kill -TERM "$pid"
	wait "$pid" || status=$?
	[[ $status == 0 ]] && grep -q '^stdin-bytes=[1-9]' "$work/tailscale-up.log" && \
		grep -q -- '--auth-key=file:/dev/stdin' "$work/tailscale-up.log" && \
		grep -q -- '--hostname=test-hub' "$work/tailscale-up.log" && \
		grep -q -- '--advertise-tags=tag:ao-hub' "$work/tailscale-up.log" && \
		grep -q '^AO_REMOTE_TAILNET_SOCKS=127.0.0.1:1055$' "$work/daemon.env" && \
		grep -q '^AO_REMOTE_TAILNET_SOCKS=127.0.0.1:1055$' "$work/bff.env" && \
		grep -q '^AO_REMOTE_TAILNET_SOCKS=127.0.0.1:1055$' "$work/tick.env" && \
		[[ $(cat "$work/tailscaled.mtu") == 1232 ]] && \
		! grep -q '^TS_AUTHKEY=' "$work/daemon.env" "$work/bff.env" "$work/tick.env" && \
		[[ $(stat -c %a "$work/data/tailscale") == 700 ]] && \
		! grep -R -F -q "$authkey" "$work/data" "$work/tailnet-supervisor.log" "$work/tailscale-up.log"
}

test_tailscaled_exit_stops_hub() {
	cat >"$work/failing-tailscaled.js" <<'EOF'
const net = require('node:net');
const socketPath = process.argv.find((arg) => arg.startsWith('--socket='))?.slice('--socket='.length);
if (!socketPath) process.exit(41);
const server = net.createServer();
server.listen(socketPath, () => setTimeout(() => process.exit(7), 800));
EOF
	cat >"$work/failing-tailscaled" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
exec node "$AO_HUB_TEST_DIR/failing-tailscaled.js" "$@"
EOF
	chmod +x "$work/failing-tailscaled"
	local status=0
	TS_AUTHKEY=test-key AO_HUB_TAILSCALED="$work/failing-tailscaled" AO_HUB_TAILSCALE="$work/fake-tailscale" \
		AO_HUB_TEST_DIR="$work" AO_HUB_TAILSCALE_DIR="$work/data/tailscale-fail" AO_DATA_DIR="$work/data/ao-fail" AO_HUB_DATA_DIR="$work/data/hub-fail" HOME="$work/data/home" \
		AO_HUB_DAEMON_COMMAND="sleep 6135" AO_HUB_BFF_MAIN="$work/bff-env.js" \
		timeout 10 bash "$supervisor" 2>"$work/tailscaled-fail.log" || status=$?
	[[ $status == 7 ]] && grep -q 'tailscaled exited with status 7' "$work/tailscaled-fail.log" &&
		! pgrep -a -x sleep | grep -q ' 6135$'
}

printf 'process.exit(3);\n' >"$work/exit3.js"
printf 'process.exit(0);\n' >"$work/exit0.js"
printf '#!/usr/bin/env bash\nexit 0\n' >"$work/exit0.sh"
chmod +x "$work/exit0.sh"
printf 'setInterval(() => {}, 1000);\n' >"$work/sleep.js"
mkdir -p "$work/home"

check "seeds a loopback-only listener on 3011 with a 0600 config" test_seeds_loopback_listener
check "exports the hub remotes file to the daemon" test_exports_remotes_file_to_daemon
check "keeps the password and repairs the listener on restart" test_keeps_password_and_repairs_listener
check "rejects an invalid config.json" test_rejects_invalid_json
check "never prints the generated password" test_never_logs_password
check "seeds the image's agent CLIs into the volume prefix" test_seeds_agent_prefix
check "keeps an agent CLI updated from the UI" test_keeps_newer_ui_update
check "replaces an older copy with a newer image version" test_newer_image_replaces_older_copy
check "seeds OpenCode 2 into the daemon's private npm prefix" test_seeds_opencode_v2_into_private_prefix
check "keeps an OpenCode 2 copy updated from the UI" test_keeps_newer_opencode_v2_ui_update
check "exits with the status of the first child that exits" test_exits_when_one_child_exits
check "a clean BFF exit is reported as a failure" test_clean_bff_exit_is_a_failure
check "a clean daemon exit is reported as a failure" test_clean_daemon_exit_is_a_failure
check "only the daemon runs with AO_SUPERVISED=1" test_marks_only_the_daemon_as_supervised
check "SIGTERM stops both processes and exits 0" test_sigterm_stops_both
check "a tick loop that exits is restarted without stopping the hub" test_tick_loop_restarts_without_stopping_the_hub
check "the tick loop is opt-in (AO_HUB_ORCHESTRATOR_TICK=1)" test_tick_loop_is_off_by_default
check "Tailscale remains opt-in and does not log the key" test_tailnet_dry_run_is_opt_in
check "Tailscale key travels over stdin and the SOCKS dial gate reaches only the daemon, BFF and tick loop" test_tailnet_passes_gate_to_daemon_bff_and_tick
check "tailscaled exit stops the hub" test_tailscaled_exit_stops_hub

if ((failures > 0)); then
	printf '%d check(s) failed\n' "$failures"
	exit 1
fi
printf 'all checks passed\n'
