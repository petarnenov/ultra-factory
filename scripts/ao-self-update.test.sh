#!/usr/bin/env bash
# Tests for ao-self-update.sh: layouts, gates, checksums, and rollback.
# Run: scripts/ao-self-update.test.sh all
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/ao-self-update.sh"
tmp="$(mktemp -d)"
server_pid=""
trap 'rm -rf "$tmp"; [[ -n "$server_pid" ]] && kill "$server_pid" 2>/dev/null || true' EXIT

export HOME="$tmp/home"
mkdir -p "$HOME"

ok() { printf 'ok   %s\n' "$*"; }
fail() {
	printf 'FAIL %s\n' "$*" >&2
	exit 1
}
assert_eq() {
	[[ "$1" == "$2" ]] || fail "$3: expected '$2', got '$1'"
}
assert_contains() {
	case "$2" in
		*"$1"*) ;;
		*) fail "$3: '$1' not found in: $2" ;;
	esac
}

# A fake daemon whose state follows the .ready/.port files next to it.
make_daemon() {
	local path="$1" commit="$2" built="${3:-2026-01-01T00:00:00Z}"
	mkdir -p "$(dirname "$path")"
	cat >"$path" <<EOF
#!/bin/sh
dir="\$(cd "\$(dirname "\$0")" && pwd)"
case "\$1" in
  version) printf '%s\n' "$commit commit $commit built $built" ;;
  status)
    state=stopped
    [ -e "\$dir/.ready" ] && state=ready
    port=0
    [ -e "\$dir/.port" ] && port="\$(cat "\$dir/.port")"
    printf '{"state":"%s","port":%s,"executablePath":"%s"}\n' "\$state" "\$port" "\$dir/ao"
    ;;
  daemon) sleep 3600 ;;
esac
EOF
	chmod 0755 "$path"
}

make_bundle() {
	local path="$1" commit="$2" ready="$3"
	rm -rf "$tmp/stage"
	mkdir -p "$tmp/stage/ao-test-$platform/daemon" "$tmp/stage/ao-test-$platform/acp-runtime/node/bin"
	make_daemon "$tmp/stage/ao-test-$platform/daemon/ao" "$commit"
	[[ "$ready" == ready ]] && : >"$tmp/stage/ao-test-$platform/daemon/.ready"
	printf '%s\n' '#!/bin/sh' 'echo v24.0.0' >"$tmp/stage/ao-test-$platform/acp-runtime/node/bin/node"
	chmod +x "$tmp/stage/ao-test-$platform/acp-runtime/node/bin/node"
	tar -czf "$path" -C "$tmp/stage" "ao-test-$platform"
	rm -rf "$tmp/stage"
}

write_manifest() {
	local dir="$1" commit="$2" asset="$3" file="$4" size="$5" built="${6:-2026-01-01T00:00:00Z}"
	python3 - "$dir/manifest.json" "$commit" "$asset" "$file" "$size" "$built" <<'PY'
import json, pathlib, sys

path, commit, asset, file, size, built = sys.argv[1:7]
doc = {
    "channel": "test",
    "version": "9.9.9",
    "commit": commit,
    "builtAt": built,
    "assets": {asset: {"file": file, "sha256": "PENDING", "size": int(size)}},
}
pathlib.Path(path).write_text(json.dumps(doc, indent=2) + "\n")
PY
	digest="$(if command -v shasum >/dev/null; then shasum -a 256 "$dir/$file" | awk '{print $1}'; else sha256sum "$dir/$file" | awk '{print $1}'; fi)"
	python3 - "$dir/manifest.json" "$digest" <<'PY'
import json, pathlib, sys

path = pathlib.Path(sys.argv[1])
doc = json.loads(path.read_text())
for asset in doc["assets"].values():
    asset["sha256"] = sys.argv[2]
path.write_text(json.dumps(doc, indent=2) + "\n")
PY
}

write_config() {
	local layout="$1" install_dir="$2" restart_cmd="$3" extra="${4:-}"
	python3 - "$HOME/.ao/self-update.json" "$layout" "$install_dir" "$restart_cmd" "$extra" <<'PY'
import json, pathlib, shlex, sys

path, layout, install_dir, restart_cmd, extra = sys.argv[1:6]
doc = {
    "channel": "test",
    "enabled": True,
    "layout": layout,
    "installDir": install_dir,
    "graceMinutes": 60,
    "service": {"kind": "command", "restartCommand": ["/bin/sh", "-c", restart_cmd]},
}
if extra:
    doc.update(json.loads(extra))
pathlib.Path(path).parent.mkdir(parents=True, exist_ok=True)
pathlib.Path(path).write_text(json.dumps(doc, indent=2) + "\n")
PY
}

run_update() {
	# run_update <expected-exit> <label> [args...]
	local expected="$1" label="$2"
	shift 2
	local status=0
	set +e
	"$script" "$@" >"$tmp/out" 2>"$tmp/err"
	status=$?
	set -e
	if [[ "$status" != "$expected" ]]; then
		cat "$tmp/out" "$tmp/err" >&2
		fail "$label: exit $status, expected $expected"
	fi
	printf '%s\n' "$(cat "$tmp/out" "$tmp/err")"
}

start_session_server() {
	local port="$1" state="$2" age_seconds="$3"
	cat >"$tmp/server.py" <<'PY'
import datetime, json, sys
from http.server import BaseHTTPRequestHandler, HTTPServer

port, state, age = int(sys.argv[1]), sys.argv[2], float(sys.argv[3])
seen = datetime.datetime.now(datetime.timezone.utc) - datetime.timedelta(seconds=age)
body = json.dumps(
    {
        "sessions": [
            {
                "id": "s_1",
                "isTerminated": False,
                "activity": {"state": state, "lastActivityAt": seen.isoformat()},
            }
        ]
    }
).encode()


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


HTTPServer(("127.0.0.1", port), Handler).serve_forever()
PY
	python3 "$tmp/server.py" "$port" "$state" "$age_seconds" &
	server_pid="$!"
	for _ in $(seq 1 50); do
		curl -fsS --max-time 1 "http://127.0.0.1:$port/api/v1/sessions" >/dev/null 2>&1 && return 0
		sleep 0.1
	done
	fail "session server did not start on port $port"
}

case "$(uname -s)" in
	Darwin) platform="darwin-$(uname -m | sed 's/x86_64/amd64/;s/arm64/arm64/')" ;;
	Linux) platform="linux-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')" ;;
esac

case "${1:-all}" in
	current)
		install="$tmp/installs/current/ao"
		mkdir -p "$install/daemon"
		make_daemon "$install/daemon/ao" "aaaa111"
		: >"$install/daemon/.ready"
		make_bundle "$tmp/b.tar.gz" "aaaa111" ready
		write_manifest "$tmp" "aaaa111" "$platform" "b.tar.gz" "$(wc -c <"$tmp/b.tar.gz")"
		write_config bundle "$install" 'true'
		out="$(run_update 0 current check --manifest-url "$tmp/manifest.json")"
		assert_contains "outcome: current" "$out" "current check"
		out="$(run_update 0 current apply --manifest-url "$tmp/manifest.json")"
		assert_contains "outcome: current" "$out" "current apply"
		ok current
		;;
	available)
		install="$tmp/installs/available/ao"
		mkdir -p "$install/daemon"
		make_daemon "$install/daemon/ao" "aaaa111"
		make_bundle "$tmp/b.tar.gz" "bbbb222" ready
		write_manifest "$tmp" "bbbb222" "$platform" "b.tar.gz" "$(wc -c <"$tmp/b.tar.gz")"
		write_config bundle "$install" 'true'
		out="$(run_update 10 available check --manifest-url "$tmp/manifest.json")"
		assert_contains "outcome: available" "$out" "available check"
		out="$(run_update 0 available apply --dry-run --manifest-url "$tmp/manifest.json")"
		assert_contains "dry run: would fetch" "$out" "dry run"
		ok available
		;;
	channel-behind)
		install="$tmp/installs/behind/ao"
		mkdir -p "$install/daemon"
		make_daemon "$install/daemon/ao" "cccc333" 2026-06-01T00:00:00Z
		: >"$install/daemon/.ready"
		make_bundle "$tmp/b.tar.gz" "bbbb222" ready
		write_manifest "$tmp" "bbbb222" "$platform" "b.tar.gz" "$(wc -c <"$tmp/b.tar.gz")" 2026-01-01T00:00:00Z
		write_config bundle "$install" "printf restarted >> $tmp/restarts"
		out="$(run_update 0 channel-behind check --manifest-url "$tmp/manifest.json")"
		assert_contains "outcome: channel-behind" "$out" "behind check"
		out="$(run_update 0 channel-behind apply --ready-timeout 8 --manifest-url "$tmp/manifest.json")"
		assert_contains "outcome: channel-behind" "$out" "behind apply"
		assert_eq "$("$install/daemon/ao" version | awk '{print $3}')" "cccc333" "behind install untouched"
		out="$(run_update 0 channel-behind apply --force --ready-timeout 8 --manifest-url "$tmp/manifest.json")"
		assert_contains "outcome: updated" "$out" "forced behind apply"
		assert_eq "$("$install/daemon/ao" version | awk '{print $3}')" "bbbb222" "forced install"
		ok channel-behind
		;;
	apply-bundle)
		install="$tmp/installs/bundle/ao"
		mkdir -p "$install/daemon"
		make_daemon "$install/daemon/ao" "aaaa111"
		: >"$install/daemon/.ready"
		: >"$install/acp-runtime-old"
		make_bundle "$tmp/b.tar.gz" "bbbb222" ready
		write_manifest "$tmp" "bbbb222" "$platform" "b.tar.gz" "$(wc -c <"$tmp/b.tar.gz")"
		write_config bundle "$install" "printf restarted >> $tmp/restarts"
		out="$(run_update 0 apply-bundle apply --ready-timeout 8 --manifest-url "$tmp/manifest.json")"
		assert_contains "outcome: updated" "$out" "apply outcome"
		assert_eq "$("$install/daemon/ao" version | awk '{print $3}')" "bbbb222" "installed commit"
		assert_eq "$("$HOME/.ao/self-update/prev/daemon/ao" version | awk '{print $3}')" "aaaa111" "previous commit kept"
		[[ -f "$tmp/restarts" ]] || fail "service was not restarted"
		assert_contains '"outcome": "updated"' "$(cat "$HOME/.ao/self-update/state.json")" "state file"
		ok apply-bundle
		;;
	checksum)
		install="$tmp/installs/checksum/ao"
		mkdir -p "$install/daemon"
		make_daemon "$install/daemon/ao" "aaaa111"
		: >"$install/daemon/.ready"
		make_bundle "$tmp/b.tar.gz" "bbbb222" ready
		write_manifest "$tmp" "bbbb222" "$platform" "b.tar.gz" "$(wc -c <"$tmp/b.tar.gz")"
		python3 - "$tmp/manifest.json" <<'PY'
import json, pathlib, sys

path = pathlib.Path(sys.argv[1])
doc = json.loads(path.read_text())
for asset in doc["assets"].values():
    asset["sha256"] = "0" * 64
path.write_text(json.dumps(doc, indent=2) + "\n")
PY
		write_config bundle "$install" 'true'
		out="$(run_update 1 checksum apply --manifest-url "$tmp/manifest.json" 2>&1 || true)"
		assert_contains "checksum mismatch" "$out" "checksum refusal"
		assert_eq "$("$install/daemon/ao" version | awk '{print $3}')" "aaaa111" "install untouched"
		ok checksum
		;;
	busy)
		install="$tmp/installs/busy/ao"
		mkdir -p "$install/daemon"
		make_daemon "$install/daemon/ao" "aaaa111"
		: >"$install/daemon/.ready"
		port=39871
		printf '%s\n' "$port" >"$install/daemon/.port"
		start_session_server "$port" active 0
		make_bundle "$tmp/b.tar.gz" "bbbb222" ready
		write_manifest "$tmp" "bbbb222" "$platform" "b.tar.gz" "$(wc -c <"$tmp/b.tar.gz")"
		write_config bundle "$install" 'true'
		out="$(run_update 3 busy apply --manifest-url "$tmp/manifest.json" 2>&1 || true)"
		assert_contains "deferring" "$out" "busy deferral"
		assert_eq "$("$install/daemon/ao" version | awk '{print $3}')" "aaaa111" "install untouched while busy"
		out="$(run_update 0 busy apply --force --ready-timeout 8 --manifest-url "$tmp/manifest.json")"
		assert_contains "outcome: updated" "$out" "forced update"
		kill "$server_pid" 2>/dev/null || true
		wait "$server_pid" 2>/dev/null || true
		server_pid=""
		ok busy
		;;
	idle)
		install="$tmp/installs/idle/ao"
		mkdir -p "$install/daemon"
		make_daemon "$install/daemon/ao" "aaaa111"
		: >"$install/daemon/.ready"
		port=39872
		printf '%s\n' "$port" >"$install/daemon/.port"
		start_session_server "$port" waiting_input 7200
		make_bundle "$tmp/b.tar.gz" "bbbb222" ready
		write_manifest "$tmp" "bbbb222" "$platform" "b.tar.gz" "$(wc -c <"$tmp/b.tar.gz")"
		write_config bundle "$install" 'true'
		out="$(run_update 0 idle apply --ready-timeout 8 --manifest-url "$tmp/manifest.json")"
		assert_contains "outcome: updated" "$out" "quiet session does not block"
		kill "$server_pid" 2>/dev/null || true
		wait "$server_pid" 2>/dev/null || true
		server_pid=""
		ok idle
		;;
	rollback-unready)
		install="$tmp/installs/rollback/ao"
		mkdir -p "$install/daemon"
		make_daemon "$install/daemon/ao" "aaaa111"
		: >"$install/daemon/.ready"
		make_bundle "$tmp/b.tar.gz" "bbbb222" broken
		write_manifest "$tmp" "bbbb222" "$platform" "b.tar.gz" "$(wc -c <"$tmp/b.tar.gz")"
		write_config bundle "$install" 'true'
		out="$(run_update 1 rollback-unready apply --ready-timeout 4 --manifest-url "$tmp/manifest.json" 2>&1 || true)"
		assert_contains "rolling back" "$out" "rollback message"
		assert_eq "$("$install/daemon/ao" version | awk '{print $3}')" "aaaa111" "previous build restored"
		assert_contains '"outcome": "rolled-back"' "$(cat "$HOME/.ao/self-update/state.json")" "rollback state"
		ok rollback-unready
		;;
	rollback)
		install="$tmp/installs/rollback-cmd/ao"
		mkdir -p "$install/daemon" "$HOME/.ao/self-update/prev"
		make_daemon "$install/daemon/ao" "bbbb222"
		: >"$install/daemon/.ready"
		make_daemon "$HOME/.ao/self-update/prev/daemon/ao" "aaaa111"
		: >"$HOME/.ao/self-update/prev/daemon/.ready"
		write_config bundle "$install" 'true'
		out="$(run_update 0 rollback rollback)"
		assert_contains "outcome: rolled-back" "$out" "rollback outcome"
		assert_eq "$("$install/daemon/ao" version | awk '{print $3}')" "aaaa111" "previous build restored"
		ok rollback
		;;
	apply-binary)
		install="$tmp/installs/bin/usr/local/bin"
		mkdir -p "$install"
		make_daemon "$install/ao" "aaaa111"
		: >"$install/.ready"
		make_bundle "$tmp/b.tar.gz" "bbbb222" ready
		write_manifest "$tmp" "bbbb222" "$platform" "b.tar.gz" "$(wc -c <"$tmp/b.tar.gz")"
		write_config binary "$install" 'true'
		out="$(run_update 0 apply-binary apply --ready-timeout 8 --manifest-url "$tmp/manifest.json")"
		assert_contains "outcome: updated" "$out" "binary outcome"
		assert_eq "$("$install/ao" version | awk '{print $3}')" "bbbb222" "binary replaced"
		assert_eq "$("$HOME/.ao/self-update/prev/ao" version | awk '{print $3}')" "aaaa111" "previous binary kept"
		ok apply-binary
		;;
	apply-release)
		release_root="$tmp/host"
		mkdir -p "$release_root/releases/20260101000000-1/resources/tmux/bin" \
			"$release_root/releases/20260101000000-1/resources/daemon"
		make_daemon "$release_root/releases/20260101000000-1/resources/daemon/ao" "aaaa111"
		: >"$release_root/releases/20260101000000-1/resources/daemon/.ready"
		printf '%s\n' '#!/bin/sh' 'echo tmux' >"$release_root/releases/20260101000000-1/resources/tmux/bin/tmux"
		chmod +x "$release_root/releases/20260101000000-1/resources/tmux/bin/tmux"
		ln -sfn "$release_root/releases/20260101000000-1" "$release_root/current"
		make_bundle "$tmp/b.tar.gz" "bbbb222" ready
		write_manifest "$tmp" "bbbb222" "$platform" "b.tar.gz" "$(wc -c <"$tmp/b.tar.gz")"
		write_config release "$release_root/current/resources" 'true' \
			"{\"releaseRoot\": \"$release_root\"}"
		out="$(run_update 0 apply-release apply --ready-timeout 8 --manifest-url "$tmp/manifest.json")"
		assert_contains "outcome: updated" "$out" "release outcome"
		assert_eq "$("$release_root/current/resources/daemon/ao" version | awk '{print $3}')" "bbbb222" "current release updated"
		[[ -L "$release_root/current" ]] || fail "current is no longer a symlink"
		[[ -x "$release_root/current/resources/tmux/bin/tmux" ]] || fail "tmux was not preserved"
		[[ -d "$release_root/releases/20260101000000-1" ]] || fail "previous release was pruned too early"
		ok apply-release
		;;
	all)
		for name in current available channel-behind apply-bundle checksum busy idle rollback-unready rollback apply-binary apply-release; do
			"$0" "$name" >"$tmp/$name.out" 2>&1 || {
				cat "$tmp/$name.out" >&2
				fail "case $name"
			}
			tail -1 "$tmp/$name.out"
		done
		;;
	*)
		printf 'Usage: %s {all|current|available|channel-behind|apply-bundle|checksum|busy|idle|rollback-unready|rollback|apply-binary|apply-release}\n' "$0" >&2
		exit 2
		;;
esac
