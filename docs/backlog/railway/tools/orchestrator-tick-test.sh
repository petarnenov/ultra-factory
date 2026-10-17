#!/usr/bin/env bash
# Offline test of orchestrator-tick.sh: the tick runs in --dry-run mode against
# the fixtures in testdata/orchestrator-tick, with fake `ao` and `gh` on PATH.
# No daemon, network, git checkout, or launchd is used.
# Run: bash docs/backlog/railway/tools/orchestrator-tick-test.sh
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
script="$here/orchestrator-tick.sh"
export TICK_FIXTURE="$here/testdata/orchestrator-tick"
work="$(mktemp -d)"
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

# tick [--live] [VAR=value...]: one tick against a fresh state dir. Output goes
# to $work/out, recorded `ao send` calls to $work/sent.
tick() {
	local mode=--dry-run
	if [[ "${1:-}" == --live ]]; then
		mode=
		shift
	fi
	if [[ "${KEEP_STATE:-0}" != 1 ]]; then rm -rf "$work/ao"; fi
	rm -f "$work/sent"
	env PATH="$TICK_FIXTURE/fakes:$PATH" AO_DATA_DIR="$work/ao" TICK_SENT="$work/sent" \
		RW_TICK_REPO_DIR= RW_TICK_HOSTS=Linux "$@" \
		bash "$script" $mode run >"$work/out" 2>&1
}

out_has() { grep -qF -- "$1" "$work/out"; }
nothing_sent() { [[ ! -e "$work/sent" && ! -e "$work/ao/rw-tick/last-sent.txt" ]]; }

test_green_pr_ticks() {
	tick && out_has 'dry run: would nudge uf-orchestrator-7 (state changed)' &&
		out_has 'TICK (automatic wake-up from orchestrator-tick.sh' &&
		out_has '#201 rw/RW-5.4.2 mergeable=MERGEABLE checks=green' &&
		out_has 'VIOLATION #201 (rw/RW-5.4.2) has been green and mergeable for' &&
		out_has 'uf-41 rw/RW-5.4.2 pr=#201 ci=passing idle' && nothing_sent
}

test_behind_main_via_compare_api() {
	tick TICK_COMPARE=compare-behind.json &&
		out_has 'VIOLATION: feat/railway-hosting is 2 commit(s) behind main (aaaaaaa,bbbbbbb)' && nothing_sent
}

test_merged_pr_with_open_issue() {
	echo '[{"number":190,"body":"Closes #120","mergedAt":"2026-10-16T12:00:00Z"}]' >"$work/prs-merged.json"
	cp -R "$TICK_FIXTURE" "$work/fixture"
	cp "$work/prs-merged.json" "$work/fixture/prs-merged.json"
	tick TICK_FIXTURE="$work/fixture" && out_has 'VIOLATION #190 is merged but #120 is still open' && nothing_sent
}

test_no_orchestrator_is_violation() {
	tick TICK_ORCHESTRATORS=orchestrators-none.json &&
		out_has 'VIOLATION no live orchestrator session in project ultra-factory; nobody can be woken' &&
		! out_has 'TICK (' && nothing_sent
}

test_two_orchestrators_is_violation() {
	tick TICK_ORCHESTRATORS=orchestrators-two.json &&
		out_has 'VIOLATION 2 live orchestrators in project ultra-factory (uf-orchestrator-7, uf-orchestrator-8)' &&
		! out_has 'TICK (' && nothing_sent
}

test_daemon_not_ready_is_violation() {
	tick TICK_STATUS=status-starting.json &&
		out_has 'VIOLATION the local AO daemon is not responding (state=starting health=degraded)' && nothing_sent
}

test_busy_orchestrator_skips() {
	tick TICK_SESSION=session-busy.txt && out_has 'orchestrator busy (status=working activity=active); skip' && nothing_sent
}

test_unauthenticated_gh_is_violation() {
	tick TICK_GH_AUTH=0 && out_has 'VIOLATION gh is not authenticated' && nothing_sent
}

test_pause_flag_skips() {
	rm -rf "$work/ao"
	env AO_DATA_DIR="$work/ao" bash "$script" pause "redeploy" >/dev/null &&
		[[ -f "$work/ao/rw-tick/paused" ]] &&
		KEEP_STATE=1 tick && out_has 'paused (redeploy by ' && ! out_has 'TICK (' && nothing_sent &&
		env AO_DATA_DIR="$work/ao" bash "$script" resume >/dev/null && [[ ! -e "$work/ao/rw-tick/paused" ]]
}

test_pause_env_skips() {
	tick RW_TICK_PAUSED=1 && out_has 'paused (RW_TICK_PAUSED=1); skip' && nothing_sent
}

# Without --dry-run the first tick sends once; the second, with nothing
# changed and a recent nudge, sends nothing.
test_live_sends_once() {
	tick --live && [[ $(grep -c '^send --session uf-orchestrator-7 --message TICK' "$work/sent") == 1 ]] &&
		out_has 'nudged uf-orchestrator-7 (state changed)' &&
		KEEP_STATE=1 tick --live RW_TICK_VIOLATION_RENUDGE_MINUTES=60 &&
		out_has 'unchanged, nudged recently; skip' && [[ ! -e "$work/sent" ]]
}

# The hub's loop repeats the tick, logs to tick.log and stops on SIGTERM.
test_loop_repeats_and_stops_on_sigterm() {
	local pid status=0
	rm -rf "$work/ao"
	env PATH="$TICK_FIXTURE/fakes:$PATH" AO_DATA_DIR="$work/ao" TICK_SENT="$work/sent" RW_TICK_REPO_DIR= \
		RW_TICK_PAUSED=1 RW_TICK_INTERVAL=1 bash "$script" loop 2>/dev/null &
	pid=$!
	sleep 2.5
	kill -TERM "$pid"
	wait "$pid" || status=$?
	[[ $status == 0 ]] && grep -q 'tick loop started (every 1s)' "$work/ao/rw-tick/tick.log" &&
		(($(grep -c 'paused (RW_TICK_PAUSED=1); skip' "$work/ao/rw-tick/tick.log") >= 2))
}

test_install_refused_off_macos() {
	[[ "$(uname -s)" == Darwin ]] && return 0
	local status=0
	env AO_DATA_DIR="$work/ao" bash "$script" install >"$work/out" 2>&1 || status=$?
	[[ $status == 2 ]] && out_has 'launchd is macOS only'
}

check "a green mergeable PR produces a TICK (dry run sends nothing)" test_green_pr_ticks
check "behind-main check uses the GitHub compare API without a checkout" test_behind_main_via_compare_api
check "a merged PR with an open task issue is a violation" test_merged_pr_with_open_issue
check "zero live orchestrators only log a VIOLATION" test_no_orchestrator_is_violation
check "two live orchestrators only log a VIOLATION" test_two_orchestrators_is_violation
check "a daemon that is not ready only logs a VIOLATION" test_daemon_not_ready_is_violation
check "a busy orchestrator is not interrupted" test_busy_orchestrator_skips
check "unauthenticated gh is logged as a VIOLATION" test_unauthenticated_gh_is_violation
check "the maintenance flag pauses the tick; resume clears it" test_pause_flag_skips
check "RW_TICK_PAUSED=1 pauses the tick" test_pause_env_skips
check "a live tick sends once and dedupes the next one" test_live_sends_once
check "loop repeats the tick and stops on SIGTERM" test_loop_repeats_and_stops_on_sigterm
check "install is refused outside macOS" test_install_refused_off_macos

if ((failures > 0)); then
	printf '%d check(s) failed\n' "$failures"
	exit 1
fi
printf 'orchestrator-tick fixture test passed\n'
