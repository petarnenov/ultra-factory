#!/usr/bin/env bash
# Wake-up ticker for the RW orchestrator (issue #63).
#
# Worker reports from remote hosts never reach an orchestrator on another
# machine, so nothing wakes it when a worker finishes or CI turns green. This
# script runs on the orchestrator's machine (launchd, every 5 minutes). When the
# orchestrator is idle and the remote state changed, it sends the orchestrator a
# compact summary with `ao send`. While work is in flight and nothing changed, it
# re-nudges at most every RENUDGE_MINUTES. It never interrupts an active turn.
#
# Usage:
#   orchestrator-tick.sh run         one tick (what launchd calls)
#   orchestrator-tick.sh snapshot    print the state a tick would send, send nothing
#   orchestrator-tick.sh install     install and load the launchd agent
#   orchestrator-tick.sh uninstall   unload and remove the launchd agent
#   orchestrator-tick.sh status      show the agent and the last tick
set -euo pipefail

SESSION="${RW_TICK_SESSION:-ultra-factory-1}"
HOSTS="${RW_TICK_HOSTS:-Linux IMac Z600}"
REPO="${RW_TICK_REPO:-petarnenov/ultra-factory}"
BASE="${RW_TICK_BASE:-feat/railway-hosting}"
INTERVAL_SECONDS="${RW_TICK_INTERVAL:-300}"
RENUDGE_MINUTES="${RW_TICK_RENUDGE_MINUTES:-30}"

STATE_DIR="${AO_DATA_DIR:-$HOME/.ao}/rw-tick"
LABEL="dev.aoagents.rw-orchestrator-tick"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
SCRIPT_PATH="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }

field() { # field <name> from `ao session get` output on stdin
	awk -v k="$1:" '$1 == k { $1 = ""; sub(/^ /, ""); print; exit }'
}

snapshot() {
	local h
	for h in $HOSTS; do
		printf '[%s]\n' "$h"
		ao session ls --host "$h" 2>&1 | awk '/ rw\// { print "  " $1, $3, "pr=" $4, "ci=" $5, $(NF-2) }' || true
	done
	printf '[open PRs -> %s]\n' "$BASE"
	gh pr list -R "$REPO" --base "$BASE" --state open \
		--json number,headRefName,mergeable,statusCheckRollup \
		--jq '.[] | "  #\(.number) \(.headRefName) mergeable=\(.mergeable) checks=\([.statusCheckRollup[] | (.conclusion // .status)] | if length == 0 then "none" elif all(. == "SUCCESS" or . == "SKIPPED" or . == "NEUTRAL") then "green" elif any(. == "FAILURE" or . == "CANCELLED" or . == "TIMED_OUT") then "red" else "pending" end)"' 2>&1 || true
}

in_flight() { # any worker branch or open PR in the snapshot
	grep -q -E 'rw/|^  #[0-9]+' "$1"
}

run_tick() {
	mkdir -p "$STATE_DIR"
	chmod 700 "$STATE_DIR"
	local info status activity
	if ! info="$(ao session get "$SESSION" 2>&1)"; then
		log "orchestrator $SESSION not reachable: $info"
		return 0
	fi
	if [[ "$(field terminated <<<"$info")" == true ]]; then
		log "orchestrator $SESSION is terminated; nothing to wake"
		return 0
	fi
	status="$(field status <<<"$info")"
	activity="$(field activity <<<"$info")"
	if [[ "$activity" == active || "$status" == working ]]; then
		log "orchestrator busy (status=$status activity=$activity); skip"
		return 0
	fi

	local snap="$STATE_DIR/snapshot.txt" last="$STATE_DIR/last-sent.txt" stamp="$STATE_DIR/last-sent.at"
	snapshot >"$snap"
	if ! in_flight "$snap"; then
		log "nothing in flight; skip"
		return 0
	fi

	local reason=""
	if [[ ! -f "$last" ]] || ! cmp -s "$snap" "$last"; then
		reason="state changed"
	else
		local now sent age
		now="$(date +%s)"
		sent="$(cat "$stamp" 2>/dev/null || echo 0)"
		age=$(((now - sent) / 60))
		if ((age >= RENUDGE_MINUTES)); then
			reason="no change for ${age}m while work is in flight"
		fi
	fi
	if [[ -z "$reason" ]]; then
		log "unchanged, nudged recently; skip"
		return 0
	fi

	local msg
	msg="TICK (автоматичен будилник от orchestrator-tick.sh, не е съобщение от потребителя; причина: $reason).
Докладите на работниците от другите машини не стигат до теб (issue #63), затова ето текущото състояние:
$(cat "$snap")
Направи пълен цикъл по раздел „Будилник и цикъл на съгласуване“ от инструкциите си: мърджни всеки зелен PR, който отговаря на критериите, побутни застоялите работници, пусни всеки таск с мърджнати зависимости, обнови #59. Не чакай ao report от отдалечени работници."
	if ao send --session "$SESSION" --message "$msg" >/dev/null 2>&1; then
		cp "$snap" "$last"
		date +%s >"$stamp"
		log "nudged $SESSION ($reason)"
	else
		log "ao send to $SESSION failed"
	fi
}

install_agent() {
	mkdir -p "$STATE_DIR" "$(dirname "$PLIST")"
	local path_env="$PATH"
	cat >"$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>$LABEL</string>
	<key>ProgramArguments</key>
	<array><string>/bin/bash</string><string>$SCRIPT_PATH</string><string>run</string></array>
	<key>EnvironmentVariables</key>
	<dict><key>PATH</key><string>$path_env</string></dict>
	<key>StartInterval</key><integer>$INTERVAL_SECONDS</integer>
	<key>RunAtLoad</key><true/>
	<key>StandardOutPath</key><string>$STATE_DIR/tick.log</string>
	<key>StandardErrorPath</key><string>$STATE_DIR/tick.log</string>
</dict>
</plist>
EOF
	launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
	launchctl bootstrap "gui/$(id -u)" "$PLIST"
	log "installed $LABEL (every ${INTERVAL_SECONDS}s), log: $STATE_DIR/tick.log"
}

uninstall_agent() {
	launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
	if [[ -f "$PLIST" ]]; then
		mv "$PLIST" "$STATE_DIR/$LABEL.plist.removed"
	fi
	log "uninstalled $LABEL"
}

case "${1:-}" in
run) run_tick ;;
snapshot) snapshot ;;
install) install_agent ;;
uninstall) uninstall_agent ;;
status)
	launchctl print "gui/$(id -u)/$LABEL" 2>/dev/null | grep -E 'state =|last exit|run interval' || echo "agent not loaded"
	tail -n 5 "$STATE_DIR/tick.log" 2>/dev/null || true
	;;
*)
	echo "usage: $0 {run|snapshot|install|uninstall|status}" >&2
	exit 2
	;;
esac
