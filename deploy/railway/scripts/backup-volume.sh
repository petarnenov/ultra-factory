#!/usr/bin/env bash
# Export a consistent SQLite snapshot and the hub registry; never a live DB.
set -euo pipefail
umask 077

usage() {
  echo 'Usage: backup-volume.sh [--dry-run] PROJECT ENVIRONMENT SERVICE OUTPUT.tar.gz' >&2
}
dry_run=false
if [[ ${1:-} == --dry-run ]]; then dry_run=true; shift; fi
if [[ $# != 4 || -z $1 || -z $2 || -z $3 || -z $4 ]]; then usage; exit 2; fi
project=$1 environment=$2 service=$3 output=$4
if [[ -e $output || -L $output ]]; then echo 'Refusing to overwrite the destination' >&2; exit 1; fi

# Use text transport because the Railway SSH transport may allocate a terminal.
# The snapshot and registry are confidential; temporary files are private.
remote=$(cat <<'REMOTE'
set -eu
umask 077
command -v sqlite3 >/dev/null
command -v tar >/dev/null
command -v base64 >/dev/null
test -f /data/ao/ao.db
test -f /data/hub/hosts.json
snapshot=$(mktemp -d /data/ao/backup.XXXXXXXX)
trap 'rm -rf "$snapshot"' EXIT HUP INT TERM
sqlite3 -readonly /data/ao/ao.db ".timeout 10000" ".backup '$snapshot/ao.db'"
test "$(sqlite3 -readonly "$snapshot/ao.db" 'PRAGMA integrity_check;')" = ok
cp /data/hub/hosts.json "$snapshot/hosts.json"
tar -czf "$snapshot/backup.tar.gz" -C "$snapshot" ao.db hosts.json
base64 "$snapshot/backup.tar.gz"
REMOTE
)
# Railway accepts a remote command string. Quote its shell argument explicitly.
quoted_remote="'${remote//\'/\'\\\'\'}'"
command=(railway ssh --project "$project" --environment "$environment" --service "$service" -- "sh -c $quoted_remote")
if $dry_run; then
  printf '%q ' "${command[@]}"; printf '\n'
  exit 0
fi
command -v railway >/dev/null
command -v python3 >/dev/null
staging=$(mktemp -d "$(dirname "$output")/.ao-backup.XXXXXXXX")
trap 'rm -rf "$staging"' EXIT HUP INT TERM
"${command[@]}" >"$staging/encoded"
# Strict decoding and archive validation catch SSH banners and truncated output.
python3 - "$staging/encoded" "$staging/backup.tar.gz" <<'PY'
import base64, pathlib, sys, tarfile
encoded, archive = map(pathlib.Path, sys.argv[1:])
archive.write_bytes(base64.b64decode(b''.join(encoded.read_bytes().split()), validate=True))
with tarfile.open(archive, 'r:gz') as backup:
    members = backup.getmembers()
    if sorted(m.name for m in members) != ['ao.db', 'hosts.json'] or not all(m.isfile() for m in members):
        raise SystemExit('Unexpected backup archive contents')
PY
# A hard link publishes atomically, without overwriting a concurrent file.
ln "$staging/backup.tar.gz" "$output"
printf 'Backup saved to %s\n' "$output"
