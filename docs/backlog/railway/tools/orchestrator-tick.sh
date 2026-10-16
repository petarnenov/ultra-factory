#!/usr/bin/env bash
# Будилник за RW оркестратора (issue #63).
#
# Докладите на работниците от отдалечени хостове не стигат до оркестратор на
# друга машина, така че нищо не го буди, когато работник приключи или CI стане
# зелен. Скриптът върви на машината на оркестратора (launchd, на всеки 5 минути).
# Когато оркестраторът е idle и отдалеченото състояние се е променило, му праща
# кратко резюме с `ao send`. Докато има работа в ход и нищо не се променя,
# побутва отново най-много на всеки RENUDGE_MINUTES. Никога не прекъсва активен ход.
#
# Освен това проверява инвариантите, които оркестраторът трябва да пази винаги
# (интеграционният бранч съдържа main, няма зелен немърджнат PR, няма мърджнат PR
# с отворени таск issues, няма idle работник без PR). Всяко нарушение се праща
# първо като ред „НАРУШЕНИЕ: ...“, записва се в лога като VIOLATION и се праща
# отново на всеки VIOLATION_RENUDGE_MINUTES, докато е налице.
#
# Употреба:
#   orchestrator-tick.sh run         един тик (това вика launchd)
#   orchestrator-tick.sh snapshot    печата състоянието, което тикът би пратил, без да праща
#   orchestrator-tick.sh install     инсталира и зарежда launchd агента
#   orchestrator-tick.sh uninstall   разтоварва и премахва launchd агента
#   orchestrator-tick.sh status      показва агента и последния тик
set -euo pipefail

SESSION="${RW_TICK_SESSION:-ultra-factory-1}"
HOSTS="${RW_TICK_HOSTS:-Linux IMac Z600}"
REPO="${RW_TICK_REPO:-petarnenov/ultra-factory}"
BASE="${RW_TICK_BASE:-feat/railway-hosting}"
INTERVAL_SECONDS="${RW_TICK_INTERVAL:-300}"
RENUDGE_MINUTES="${RW_TICK_RENUDGE_MINUTES:-30}"
# Колко често да се побутва отново, докато инвариант е нарушен и нищо не се променя.
VIOLATION_RENUDGE_MINUTES="${RW_TICK_VIOLATION_RENUDGE_MINUTES:-10}"
# Прагове на инвариантите.
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
	invariants || printf 'НАРУШЕНИЕ: проверката на инвариантите се провали; виж %s\n' "$STATE_DIR/tick.log"
}

# invariants печата по един ред „НАРУШЕНИЕ: ...“ за всяко нарушено правило и нищо,
# когато всички са спазени. Правилата, които оркестраторът трябва да пази винаги,
# се проверяват тук, в код, защото инструкции „след Y направи и X“ се пропускат.
invariants() {
	# 1. Интеграционният бранч съдържа origin/main.
	if git -C "$REPO_DIR" fetch -q origin main "$BASE" 2>/dev/null; then
		local behind shas
		behind="$(git -C "$REPO_DIR" rev-list --count "origin/$BASE..origin/main" 2>/dev/null || echo 0)"
		if ((behind > 0)); then
			shas="$(git -C "$REPO_DIR" log --format=%h -n 5 "origin/$BASE..origin/main" | paste -sd, -)"
			printf 'НАРУШЕНИЕ: %s изостава от main с %s commit(s) (%s) → git merge origin/main в %s и push\n' "$BASE" "$behind" "$shas" "$BASE"
		fi
	else
		printf 'НАРУШЕНИЕ: git fetch на origin се провали; провери мрежата и достъпа до репото\n'
	fi

	# 2–4 изискват състояние от GitHub и AO; проверяват се заедно в Python.
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

# 2. Зелен, мърджабилен PR не стои немърджнат.
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
        print(f"НАРУШЕНИЕ: #{pr['number']} ({pr['headRefName']}) е зелен и мърджабилен от {age} мин → провери по „Ревю и мърдж“ и мърджни")

# 3. Мърджнат PR не оставя отворени таск issues, към които сочи.
open_issues = {i["number"]: i["title"] for i in load("OPEN_ISSUES", [])}
close_re = re.compile(r"(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\s+#(\d+)")
for pr in load("MERGED_PRS", []):
    refs = sorted({int(n) for n in close_re.findall(pr.get("body") or "")})
    still_open = [n for n in refs if n in open_issues]
    if still_open:
        listed = ", ".join(f"#{n}" for n in still_open)
        print(f"НАРУШЕНИЕ: #{pr['number']} е мърджнат, но {listed} е още отворено → gh issue close по стъпка 5 на „Ревю и мърдж“")

# 4. Работник не стои idle без PR.
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
            print(f"НАРУШЕНИЕ: {s.get('id')} ({branch}) на {host} е idle {minutes_since(last)} мин без PR → ao send --host {host} да докладва какво го блокира")
PY
}

in_flight() { # any worker branch, open PR or violated invariant in the snapshot
	grep -q -E 'rw/|^  #[0-9]+|^НАРУШЕНИЕ:' "$1"
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

	local violations renudge_after
	violations="$(grep '^НАРУШЕНИЕ:' "$snap" || true)"
	renudge_after="$RENUDGE_MINUTES"
	if [[ -n "$violations" ]]; then
		renudge_after="$VIOLATION_RENUDGE_MINUTES"
		while IFS= read -r v; do log "VIOLATION ${v#НАРУШЕНИЕ: }"; done <<<"$violations"
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
ПЪРВО оправи тези нарушения, преди всичко друго в цикъла:
$violations
"
	fi
	msg="TICK (автоматичен будилник от orchestrator-tick.sh, не е съобщение от потребителя; причина: $reason).
$first
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
