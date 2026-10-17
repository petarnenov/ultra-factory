#!/usr/bin/env bash
set -euo pipefail

SCRIPT="$(cd "$(dirname "$0")" && pwd)/backlog-status.sh"
FIXTURE="$(cd "$(dirname "$0")" && pwd)/testdata/backlog-status.json"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/railway/tools"
cp "$SCRIPT" "$TMP/railway/tools/backlog-status.sh"
cp -R "$(dirname "$SCRIPT")/../backlog" "$TMP/railway/backlog"
RW_STATUS_BACKLOG="$TMP/railway/backlog" "$TMP/railway/tools/backlog-status.sh" --fixture "$FIXTURE"
cp -R "$TMP/railway/backlog" "$TMP/first"
RW_STATUS_BACKLOG="$TMP/railway/backlog" "$TMP/railway/tools/backlog-status.sh" --fixture "$FIXTURE"
diff -ru "$TMP/first" "$TMP/railway/backlog"
README="$TMP/railway/backlog/README.md"
grep -q '✅ done.*\[#60\]' "$README"
grep -q '⚠️.*\[#999\]' "$README"
grep -q 'RW-1.4.2.*⬜ todo.*\[#83\]' "$README"
grep -q 'RW-1.2.1.*🔄 in progress' "$README"
grep -q 'RW-1.2.2.*⛔ needs-human' "$README"
grep -q 'RW-2.2.1.*⛔ blocked' "$README"
grep -q '^\*\*Status:\*\* ✅ done (#60)$' "$TMP/railway/backlog/RW-1-rabotnik-v-railway/RW-1.1-adr/RW-1.1.1-adr-trusted-proxy.md"
grep -q '^\*\*Status:\*\* 👀 in review (#999)$' "$TMP/railway/backlog/RW-1-rabotnik-v-railway/RW-1.3-listener-zad-proxy/RW-1.3.1-trusted-proxy-lockout.md"
if RW_STATUS_BACKLOG="$TMP/railway/backlog" "$TMP/railway/tools/backlog-status.sh" --fixture "$FIXTURE" --dry-run 2>&1 | grep -q '^Status mismatch: closed issues without a merged linked PR: RW-1.3.1 (#31)$'; then :; else exit 1; fi
printf 'backlog-status fixture test passed\n'
