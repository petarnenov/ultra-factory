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
# It also checks invariants the orchestrator must keep true (integration branch
# contains main, no green PR left unmerged, no merged PR with open task issues,
# no worker idle without a PR). Each violation is sent first as a
# "VIOLATION: ..." line, logged as VIOLATION, and re-sent every
# VIOLATION_RENUDGE_MINUTES while it persists.
#
# The orchestrator is the single live orchestrator session of RW_TICK_PROJECT,
# found on every tick, so a respawned orchestrator is picked up without
# reinstalling. RW_TICK_SESSION pins a session id instead. When the local daemon
# does not answer, or no single live orchestrator exists, nobody can be woken:
# the tick logs a VIOLATION to tick.log and sends nothing.
#
# Usage:
#   orchestrator-tick.sh run         one tick (what launchd calls)
#   orchestrator-tick.sh snapshot    print the state a tick would send, send nothing
#   orchestrator-tick.sh install     install and load the launchd agent
#   orchestrator-tick.sh uninstall   unload and remove the launchd agent
#   orchestrator-tick.sh status      show the agent and the last tick
set -euo pipefail

PROJECT="${RW_TICK_PROJECT:-ultra-factory}"
# Empty: find the project's orchestrator on every tick.
SESSION_OVERRIDE="${RW_TICK_SESSION:-}"
HOSTS="${RW_TICK_HOSTS:-Linux IMac Z600}"
REPO="${RW_TICK_REPO:-petarnenov/ultra-factory}"
BASE="${RW_TICK_BASE:-feat/railway-hosting}"
INTERVAL_SECONDS="${RW_TICK_INTERVAL:-300}"
RENUDGE_MINUTES="${RW_TICK_RENUDGE_MINUTES:-30}"
# While an invariant is violated and nothing changed, re-nudge this often.
VIOLATION_RENUDGE_MINUTES="${RW_TICK_VIOLATION_RENUDGE_MINUTES:-10}"
# Invariant thresholds.
GREEN_PR_MAX_MINUTES="${RW_TICK_GREEN_PR_MAX_MINUTES:-10}"
IDLE_WORKER_MAX_MINUTES="${RW_TICK_IDLE_WORKER_MAX_MINUTES:-30}"
REPO_DIR="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"

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
	printf '[invariants]\n'
	invariants || printf 'VIOLATION: the invariant check failed; see %s\n' "$STATE_DIR/tick.log"
}

# invariants prints one "VIOLATION: ..." line per violated rule, nothing when all
# hold. Rules the orchestrator must keep true at all times are checked here, in
# code, because "remember to do X after Y" instructions get skipped.
invariants() {
	# 1. The integration branch contains origin/main.
	if git -C "$REPO_DIR" fetch -q origin main "$BASE" 2>/dev/null; then
		local behind shas
		behind="$(git -C "$REPO_DIR" rev-list --count "origin/$BASE..origin/main" 2>/dev/null || echo 0)"
		if ((behind > 0)); then
			shas="$(git -C "$REPO_DIR" log --format=%h -n 5 "origin/$BASE..origin/main" | paste -sd, -)"
			printf 'VIOLATION: %s is %s commit(s) behind main (%s) → git merge origin/main into %s and push\n' "$BASE" "$behind" "$shas" "$BASE"
		fi
	else
		printf 'VIOLATION: git fetch from origin failed; check the network and repository access\n'
	fi

	# 2-4 need GitHub and AO state; evaluated together in Python.
	local open_prs merged_prs open_issues sessions h
	open_prs="$(gh pr list -R "$REPO" --base "$BASE" --state open --json number,headRefName,mergeable,statusCheckRollup 2>/dev/null || echo '[]')"
	merged_prs="$(gh pr list -R "$REPO" --base "$BASE" --state merged --limit 50 --json number,body,mergedAt 2>/dev/null || echo '[]')"
	open_issues="$(gh issue list -R "$REPO" --label rw --state open --limit 200 --json number,title 2>/dev/null || echo '[]')"
	sessions="{"
	for h in $HOSTS; do
		sessions+="\"$h\": $(ao session ls --host "$h" --json 2>/dev/null || echo '{}'),"
	done
	sessions="${sessions%,}}"
	OPEN_PRS="$open_prs" MERGED_PRS="$merged_prs" OPEN_ISSUES="$open_issues" SESSIONS="$sessions" \
		GREEN_MAX="$GREEN_PR_MAX_MINUTES" IDLE_MAX="$IDLE_WORKER_MAX_MINUTES" python3 - <<'PY'
import json, os, re
from datetime import datetime, timezone

now = datetime.now(timezone.utc)

def parse_time(value):
    if not value or value.startswith("0001-"):
        return None
    value = re.sub(r"\.\d+", "", value).replace("Z", "+00:00")
    try:
        return datetime.fromisoformat(value)
    except ValueError:
        return None

def minutes_since(t):
    return int((now - t).total_seconds() // 60)

def load(name, default):
    try:
        return json.loads(os.environ.get(name) or "")
    except ValueError:
        return default

green_max = int(os.environ["GREEN_MAX"])
idle_max = int(os.environ["IDLE_MAX"])

# 2. A green, mergeable PR does not sit unmerged.
for pr in load("OPEN_PRS", []):
    checks = pr.get("statusCheckRollup") or []
    results = [(c.get("conclusion") or c.get("state") or c.get("status") or "").upper() for c in checks]
    if pr.get("mergeable") != "MERGEABLE" or not checks:
        continue
    if not all(r in ("SUCCESS", "SKIPPED", "NEUTRAL") for r in results):
        continue
    done = [parse_time(c.get("completedAt")) for c in checks]
    done = [d for d in done if d]
    if not done:
        continue
    age = minutes_since(max(done))
    if age >= green_max:
        print(f"VIOLATION: #{pr['number']} ({pr['headRefName']}) has been green and mergeable for {age} min → review it per \"Review and merge\" and merge it")

# 3. A merged PR leaves none of its referenced task issues open.
open_issues = {i["number"]: i["title"] for i in load("OPEN_ISSUES", [])}
close_re = re.compile(r"(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\s+#(\d+)")
for pr in load("MERGED_PRS", []):
    refs = sorted({int(n) for n in close_re.findall(pr.get("body") or "")})
    still_open = [n for n in refs if n in open_issues]
    if still_open:
        listed = ", ".join(f"#{n}" for n in still_open)
        print(f"VIOLATION: #{pr['number']} is merged but {listed} is still open → gh issue close per step 5 of \"Review and merge\"")

# 4. A worker does not sit idle without a PR.
for host, payload in load("SESSIONS", {}).items():
    items = payload.get("data", []) if isinstance(payload, dict) else payload
    for s in items or []:
        branch = s.get("branch") or ""
        if not branch.startswith("rw/") or s.get("isTerminated"):
            continue
        if s.get("activity") != "idle" or s.get("prs"):
            continue
        last = parse_time(s.get("lastActivityAt"))
        if last and minutes_since(last) >= idle_max:
            print(f"VIOLATION: {s.get('id')} ({branch}) on {host} has been idle for {minutes_since(last)} min without a PR → ao send --host {host} asking what blocks it")
PY
}

in_flight() { # any worker branch, open PR or violated invariant in the snapshot
	grep -q -E 'rw/|^  #[0-9]+|^VIOLATION:' "$1"
}

# daemon_problem prints why the local AO daemon cannot serve a tick, nothing
# when it is ready.
daemon_problem() {
	local out
	if ! out="$(ao status --json 2>&1)"; then
		printf 'ao status failed: %s' "$out"
		return 0
	fi
	STATUS="$out" python3 - <<'PY'
import json, os
try:
    d = json.loads(os.environ["STATUS"])
except ValueError:
    print("ao status returned unreadable output", end="")
else:
    state, health = d.get("state"), d.get("health")
    if state != "ready" or health != "ok":
        print(f"state={state} health={health}", end="")
PY
}

# resolve_orchestrator prints the id of the orchestrator to wake. On failure it
# prints the reason and returns 1.
resolve_orchestrator() {
	local out
	if [[ -n "$SESSION_OVERRIDE" ]]; then
		if ! out="$(ao session get "$SESSION_OVERRIDE" 2>&1)"; then
			printf 'orchestrator %s (RW_TICK_SESSION) does not exist: %s' "$SESSION_OVERRIDE" "$out"
			return 1
		fi
		if [[ "$(field terminated <<<"$out")" == true ]]; then
			printf 'orchestrator %s (RW_TICK_SESSION) is terminated' "$SESSION_OVERRIDE"
			return 1
		fi
		printf '%s' "$SESSION_OVERRIDE"
		return 0
	fi
	if ! out="$(ao orchestrator ls --json 2>&1)"; then
		printf 'ao orchestrator ls failed: %s' "$out"
		return 1
	fi
	ORCHS="$out" PROJECT="$PROJECT" python3 - <<'PY'
import json, os, sys
project = os.environ["PROJECT"]
try:
    items = json.loads(os.environ["ORCHS"]).get("data") or []
except ValueError:
    print("ao orchestrator ls returned unreadable output", end="")
    sys.exit(1)
live = [s["id"] for s in items if s.get("projectId") == project and not s.get("isTerminated")]
if len(live) == 1:
    print(live[0], end="")
elif not live:
    print(f"no live orchestrator session in project {project}", end="")
    sys.exit(1)
else:
    print(f"{len(live)} live orchestrators in project {project} ({', '.join(live)}); set RW_TICK_SESSION to the RW one", end="")
    sys.exit(1)
PY
}

run_tick() {
	mkdir -p "$STATE_DIR"
	chmod 700 "$STATE_DIR"
	local problem
	problem="$(daemon_problem)"
	if [[ -n "$problem" ]]; then
		log "VIOLATION the local AO daemon is not responding ($problem); nobody can be woken → start the AO app or run ao start"
		return 0
	fi
	local SESSION
	if ! SESSION="$(resolve_orchestrator)"; then
		log "VIOLATION $SESSION; nobody can be woken → spawn the RW orchestrator, or set RW_TICK_SESSION and reinstall"
		return 0
	fi
	local info status activity
	if ! info="$(ao session get "$SESSION" 2>&1)"; then
		log "VIOLATION orchestrator $SESSION not reachable: $info"
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

	local violations renudge_after
	violations="$(grep '^VIOLATION:' "$snap" || true)"
	renudge_after="$RENUDGE_MINUTES"
	if [[ -n "$violations" ]]; then
		renudge_after="$VIOLATION_RENUDGE_MINUTES"
		while IFS= read -r v; do log "VIOLATION ${v#VIOLATION: }"; done <<<"$violations"
	fi

	local reason=""
	if [[ ! -f "$last" ]] || ! cmp -s "$snap" "$last"; then
		reason="state changed"
	else
		local now sent age
		now="$(date +%s)"
		sent="$(cat "$stamp" 2>/dev/null || echo 0)"
		age=$(((now - sent) / 60))
		if ((age >= renudge_after)); then
			if [[ -n "$violations" ]]; then
				reason="invariant still violated after ${age}m"
			else
				reason="no change for ${age}m while work is in flight"
			fi
		fi
	fi
	if [[ -z "$reason" ]]; then
		log "unchanged, nudged recently; skip"
		return 0
	fi

	local msg first=""
	if [[ -n "$violations" ]]; then
		first="
FIRST fix these violations, before anything else in the loop:
$violations
"
	fi
	msg="TICK (automatic wake-up from orchestrator-tick.sh, not a message from the user; reason: $reason).
$first
Reports from workers on other machines do not reach you (issue #63), so here is the current state:
$(cat "$snap")
Run a full pass per \"Wake-up and reconcile loop\" in your instructions: merge every green PR that meets the criteria, nudge stalled workers, start every task whose dependencies are merged, update #59. Do not wait for ao report from remote workers."
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
