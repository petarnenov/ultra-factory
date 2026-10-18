#!/usr/bin/env bash
# Pull-based daemon updates for a self-hosted AO host; see
# docs/self-hosted-remote.md for the channel, the layouts, and the rollback.
set -euo pipefail

usage() {
	cat <<'TEXT'
Usage: ao-self-update.sh [command] [options]

Commands:
  status               report the installed and available commits
  check                exit 0 when current, 10 when an update is available
  apply                download, verify, swap, restart, and verify readiness
  rollback             restore the previous build and restart
  version              print this script's revision

Options:
  --config PATH        config file (default: ~/.ao/self-update.json)
  --manifest-url URL   channel manifest URL or path (overrides the config)
  --channel NAME       channel name (overrides the config)
  --force              update even when sessions are busy or unchanged
  --dry-run            report what apply would do without changing anything
  --ready-timeout N    seconds to wait for the daemon to report ready (60)
  --json               emit a JSON report
  -h, --help           show this help

Exit codes: 0 current or updated, 1 failure, 3 deferred (busy sessions),
4 disabled, 10 update available (check only). A channel build that predates
the installed build is left alone unless --force is passed.
TEXT
}

command_name=""
config_path="${AO_SELF_UPDATE_CONFIG:-$HOME/.ao/self-update.json}"
manifest_override=""
channel_override=""
force=false
dry_run=false
json=false
ready_timeout=60

while (($#)); do
	case "$1" in
		status | check | apply | rollback | version) command_name="$1"; shift ;;
		--config) config_path="${2:?--config needs a path}"; shift 2 ;;
		--manifest-url) manifest_override="${2:?--manifest-url needs a URL}"; shift 2 ;;
		--channel) channel_override="${2:?--channel needs a name}"; shift 2 ;;
		--ready-timeout) ready_timeout="${2:?--ready-timeout needs seconds}"; shift 2 ;;
		--force) force=true; shift ;;
		--dry-run) dry_run=true; shift ;;
		--json) json=true; shift ;;
		-h | --help) usage; exit 0 ;;
		*) usage >&2; exit 2 ;;
	esac
done
command_name="${command_name:-check}"

log() { printf '[ao-self-update] %s\n' "$*"; }
warn() { printf '[ao-self-update] %s\n' "$*" >&2; }
die() {
	printf '[ao-self-update] %s\n' "$*" >&2
	exit 1
}

if [[ "$command_name" == version ]]; then
	printf 'ao-self-update %s\n' "$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --short=9 HEAD 2>/dev/null || echo unknown)"
	exit 0
fi

case "$(uname -s)" in
	Darwin) platform_key="darwin" ;;
	Linux) platform_key="linux" ;;
	*) die "unsupported platform: $(uname -s)" ;;
esac
case "$(uname -m)" in
	arm64 | aarch64) platform_key="$platform_key-arm64" ;;
	x86_64 | amd64) platform_key="$platform_key-amd64" ;;
	*) die "unsupported architecture: $(uname -m)" ;;
esac

for required in python3 curl tar; do
	command -v "$required" >/dev/null || die "$required is required"
done
if [[ "$(id -u)" == 0 ]]; then
	die "run as the user who owns the AO host, not as root"
fi

state_dir="$HOME/.ao/self-update"
staging_root="$state_dir/staging"
prev_dir="$state_dir/prev"
failed_root="$state_dir/failed"
lock_dir="$state_dir/lock"
mkdir -p "$staging_root"

sha256() {
	if command -v shasum >/dev/null; then
		shasum -a 256 "$@" | awk '{print $1}'
	else
		sha256sum "$@" | awk '{print $1}'
	fi
}

load_config() {
	[[ -f "$config_path" ]] || die "no self-update config at $config_path; run scripts/install-self-update.sh"
	eval "$(python3 -c '
import json, shlex, sys

with open(sys.argv[1]) as handle:
    cfg = json.load(handle)


def q(value):
    return shlex.quote(str(value))


def qlist(values):
    return "(" + " ".join(q(v) for v in values) + ")"


service = cfg.get("service") or {}
enabled = "true" if cfg.get("enabled", True) else "false"
print("enabled=" + q(enabled))
print("channel=" + q(cfg.get("channel", "main")))
print("repo=" + q(cfg.get("repo", "")))
print("manifest_url=" + q(cfg.get("manifestUrl", "")))
print("layout=" + q(cfg.get("layout", "bundle")))
print("install_dir=" + q(cfg.get("installDir", "")))
print("release_root=" + q(cfg.get("releaseRoot", "")))
print("binary_name=" + q(cfg.get("binaryName", "ao")))
print("cli_link=" + q(cfg.get("cliLink", "")))
print("grace_minutes=" + q(cfg.get("graceMinutes", 20)))
print("service_kind=" + q(service.get("kind", "command")))
print("service_unit=" + q(service.get("unit", "")))
print("service_label=" + q(service.get("label", "")))
print("service_restart=" + qlist(service.get("restartCommand") or []))
print("busy_states=" + qlist(cfg.get("busyStates") or ["active", "waiting_input", "blocked"]))
print("env_pairs=" + qlist(k + "=" + v for k, v in (cfg.get("env") or {}).items()))
' "$config_path")"
	[[ -n "$channel_override" ]] && channel="$channel_override"
	[[ -n "$manifest_override" ]] && manifest_url="$manifest_override"
	if [[ -z "$manifest_url" && -n "$repo" ]]; then
		manifest_url="https://github.com/$repo/releases/download/daemon-channel-$channel/manifest.json"
	fi
	case "$layout" in
		bundle | release | binary) ;;
		*) die "unsupported layout: $layout (bundle, release, binary)" ;;
	esac
	[[ -n "$install_dir" && "$install_dir" != "/" ]] || die "config needs a real installDir"
	if [[ "$layout" == release && -z "$release_root" ]]; then
		die "the release layout needs releaseRoot"
	fi
}

require_enabled() {
	if [[ "$enabled" != true ]]; then
		log "self-update is disabled in $config_path"
		exit 4
	fi
}

require_manifest() {
	[[ -n "$manifest_url" ]] || die "config needs repo or manifestUrl"
}

current_binary() {
	case "$layout" in
		binary) printf '%s/%s\n' "$install_dir" "$binary_name" ;;
		*) printf '%s/daemon/ao\n' "$install_dir" ;;
	esac
}

run_ao() {
	local bin="$1"
	shift
	if ((${#env_pairs[@]})); then
		env "${env_pairs[@]}" "$bin" "$@"
	else
		"$bin" "$@"
	fi
}

installed_facts() {
	local bin out
	bin="$(current_binary)"
	[[ -x "$bin" ]] || die "no daemon binary at $bin"
	out="$(run_ao "$bin" version 2>/dev/null || true)"
	[[ -n "$out" ]] || die "could not run $bin version"
	installed_version="$(printf '%s' "$out" | awk '{print $1}')"
	installed_commit="$(printf '%s' "$out" | awk '{print $3}')"
	installed_built="$(printf '%s' "$out" | awk '{print $5}')"
	target_version=""
	target_commit=""
}

fetch_url() {
	local url="$1" target="$2" limit="${3:-600}"
	case "$url" in
		http://* | https://*) curl -fsSL --retry 3 --max-time "$limit" "$url" -o "$target" ;;
		file://*) cp "${url#file://}" "$target" ;;
		*) cp "$url" "$target" ;;
	esac
}

asset_url() {
	local file="$1" base
	case "$manifest_url" in
		http://* | https://* | file://*) base="${manifest_url%/*}" ;;
		*) base="$(dirname "$manifest_url")" ;;
	esac
	printf '%s/%s\n' "$base" "$file"
}

read_manifest() {
	local path="$1"
	read -r manifest_channel manifest_version manifest_commit asset_name asset_sha asset_size \
		manifest_built < <(
		python3 -c '
import json, sys

with open(sys.argv[1]) as handle:
    doc = json.load(handle)
asset = (doc.get("assets") or {}).get(sys.argv[2])
if not asset:
    raise SystemExit(f"manifest has no asset for {sys.argv[2]}")
values = [doc.get("channel"), doc.get("version"), doc.get("commit")]
values += [asset.get("file"), asset.get("sha256"), asset.get("size")]
for value in values:
    if value in (None, ""):
        raise SystemExit(f"manifest is missing a value for {sys.argv[2]}")
values.append(doc.get("builtAt") or "")
print(" ".join(str(v) for v in values))
' "$path" "$platform_key"
	)
}

# channel_behind returns 0 when the channel build predates the installed one.
# Absolute build times are the only ordering a host can check offline.
channel_behind() {
	[[ -n "$manifest_built" && -n "$installed_built" ]] || return 1
	python3 -c '
import datetime, sys


def parse(value):
    try:
        return datetime.datetime.fromisoformat(str(value).replace("Z", "+00:00"))
    except ValueError:
        return None


channel = parse(sys.argv[1])
installed = parse(sys.argv[2])
if channel is None or installed is None:
    sys.exit(1)
sys.exit(0 if channel < installed else 1)
' "$manifest_built" "$installed_built"
}

# gate_busy returns 0 when nothing would be interrupted, 1 when sessions are
# still working inside the grace window. A stopped daemon cannot be busy.
gate_busy() {
	local status port body
	status="$(run_ao "$(current_binary)" status --json 2>/dev/null || true)"
	port=""
	if [[ -n "$status" ]]; then
		port="$(printf '%s' "$status" | python3 -c '
import json, sys
try:
    print(json.load(sys.stdin).get("port", "") or "")
except ValueError:
    print("")
')"
	fi
	[[ -n "$port" ]] || return 0
	body="$(curl -fsS --max-time 5 "http://127.0.0.1:$port/api/v1/sessions?active=true" 2>/dev/null || true)"
	if [[ -z "$body" ]]; then
		warn "could not read sessions from port $port; continuing"
		return 0
	fi
	((${#busy_states[@]})) || return 0
	python3 -c '
import datetime, json, re, sys

grace = float(sys.argv[1]) * 60
busy_states = set(sys.argv[2:])
now = datetime.datetime.now(datetime.timezone.utc)
try:
    sessions = (json.loads(sys.stdin.read()) or {}).get("sessions") or []
except ValueError:
    print("could not parse the session list", file=sys.stderr)
    sys.exit(0)


def parse(value):
    text = re.sub(r"(\.\d{6})\d+", r"\1", str(value)).replace("Z", "+00:00")
    try:
        return datetime.datetime.fromisoformat(text)
    except ValueError:
        return None


busy = []
for session in sessions:
    if session.get("isTerminated"):
        continue
    activity = session.get("activity") or {}
    state = activity.get("state") or ""
    if state not in busy_states:
        continue
    seen = parse(activity.get("lastActivityAt") or "")
    if seen is None or (now - seen).total_seconds() < grace:
        busy.append((session.get("id", "?"), state, seen))

if busy:
    for sid, state, seen in busy:
        when = seen.isoformat() if seen else "unknown"
        print(f"BUSY {sid} {state} last={when}")
    sys.exit(10)
' "$grace_minutes" "${busy_states[@]}" <<<"$body" || return 1
	return 0
}

service_active() {
	case "$service_kind" in
		systemd) systemctl --user is-active --quiet "$service_unit" ;;
		launchd) launchctl print "gui/$(id -u)/$service_label" >/dev/null 2>&1 ;;
		command) return 0 ;;
		*) return 1 ;;
	esac
}

restart_service() {
	case "$service_kind" in
		systemd) systemctl --user restart "$service_unit" ;;
		launchd) launchctl kickstart -k "gui/$(id -u)/$service_label" ;;
		command)
			((${#service_restart[@]})) || die "the command service kind needs restartCommand"
			"${service_restart[@]}"
			;;
		*) die "unsupported service kind: $service_kind" ;;
	esac
}

realpath_of() { python3 -c 'import os, sys; print(os.path.realpath(sys.argv[1]))' "$1"; }

# wait_ready polls the daemon until it reports ready from this install.
wait_ready() {
	local bin expected deadline status state exe
	bin="$(current_binary)"
	expected="$(realpath_of "$bin")"
	deadline=$((SECONDS + ready_timeout))
	while ((SECONDS < deadline)); do
		status="$(run_ao "$bin" status --json 2>/dev/null || true)"
		if [[ -n "$status" ]]; then
			read -r state exe < <(printf '%s' "$status" | python3 -c '
import json, sys
try:
    doc = json.load(sys.stdin)
except ValueError:
    doc = {}
print(doc.get("state", "-"), doc.get("executablePath", "-"))
')
			if [[ "$state" == ready ]]; then
				if [[ "$layout" == binary || "$(realpath_of "$exe" 2>/dev/null || true)" == "$expected" ]]; then
					return 0
				fi
				warn "daemon is ready from $exe, expected $expected"
			fi
		fi
		sleep 2
	done
	return 1
}

apply_bundle() {
	local root="$1" entry target
	local dirs=(daemon acp-runtime)
	[[ -d "$root/daemon" ]] || die "staged bundle has no daemon directory"
	[[ -d "$root/tmux" ]] && dirs+=(tmux)
	target="$install_dir"
	rm -rf "$prev_dir.new"
	mkdir -p "$prev_dir.new"
	for entry in "${dirs[@]}"; do
		[[ -e "$target/$entry" ]] && mv "$target/$entry" "$prev_dir.new/$entry"
	done
	for entry in "${dirs[@]}"; do
		[[ -e "$root/$entry" ]] || continue
		mv "$root/$entry" "$target/$entry"
	done
	rm -rf "$prev_dir"
	mv "$prev_dir.new" "$prev_dir"
}

rollback_bundle() {
	local failed="$failed_root/$target_commit" entry
	mkdir -p "$failed"
	for entry in daemon acp-runtime tmux; do
		[[ -e "$install_dir/$entry" ]] && mv "$install_dir/$entry" "$failed/$entry"
		[[ -e "$prev_dir/$entry" ]] && mv "$prev_dir/$entry" "$install_dir/$entry"
	done
	return 0
}

apply_binary() {
	local root="$1"
	[[ -f "$root/daemon/ao" ]] || die "staged bundle has no daemon/ao"
	rm -rf "$prev_dir.new"
	mkdir -p "$prev_dir.new"
	[[ -e "$install_dir/$binary_name" ]] && cp -p "$install_dir/$binary_name" "$prev_dir.new/$binary_name"
	install -m 0755 "$root/daemon/ao" "$install_dir/$binary_name.new"
	mv "$install_dir/$binary_name.new" "$install_dir/$binary_name"
	rm -rf "$prev_dir"
	mv "$prev_dir.new" "$prev_dir"
	[[ -d "$root/acp-runtime" ]] && warn "the binary layout ignores acp-runtime in the bundle"
	return 0
}

rollback_binary() {
	local failed="$failed_root/$target_commit"
	mkdir -p "$failed"
	[[ -e "$install_dir/$binary_name" ]] && cp -p "$install_dir/$binary_name" "$failed/$binary_name"
	[[ -e "$prev_dir/$binary_name" ]] || die "no previous binary to restore"
	install -m 0755 "$prev_dir/$binary_name" "$install_dir/$binary_name"
}

apply_release() {
	local root="$1" current_target current_dir new_dir entry pruned=()
	[[ -d "$root/daemon" ]] || die "staged bundle has no daemon directory"
	current_target="$(readlink "$release_root/current" 2>/dev/null || true)"
	current_dir=""
	if [[ -n "$current_target" ]]; then
		case "$current_target" in
			/*) current_dir="$current_target" ;;
			*) current_dir="$release_root/$current_target" ;;
		esac
	fi
	new_dir="$release_root/releases/$(date +%Y%m%d%H%M%S)-$$"
	mkdir -p "$new_dir/resources"
	if [[ -n "$current_dir" && -d "$current_dir/resources" ]]; then
		for entry in "$current_dir/resources"/*; do
			[[ -e "$entry" ]] || continue
			case "${entry##*/}" in
				daemon | acp-runtime) continue ;;
			esac
			cp -R "$entry" "$new_dir/resources/"
		done
	fi
	mv "$root/daemon" "$new_dir/resources/daemon"
	[[ -d "$root/acp-runtime" ]] && mv "$root/acp-runtime" "$new_dir/resources/acp-runtime"
	[[ -d "$root/tmux" ]] && mv "$root/tmux" "$new_dir/resources/tmux"
	printf '%s\n' "$current_target" >"$state_dir/prev.target"
	ln -sfn "$new_dir" "$release_root/current"
	log "installed $new_dir"
	for entry in "$release_root"/releases/*; do
		[[ -d "$entry" && ! -L "$entry" ]] || continue
		[[ "$entry" == "$new_dir" || "$entry" == "$current_dir" ]] && continue
		[[ "${entry##*/}" =~ ^[0-9]{14}-[0-9]+$ ]] || continue
		pruned+=("$entry")
	done
	if ((${#pruned[@]})); then
		rm -rf -- "${pruned[@]}"
	fi
}

rollback_release() {
	local previous target
	previous="$(cat "$state_dir/prev.target" 2>/dev/null || true)"
	[[ -n "$previous" ]] || die "no previous release recorded"
	case "$previous" in
		/*) target="$previous" ;;
		*) target="$release_root/$previous" ;;
	esac
	[[ -d "$target" ]] || die "previous release is gone: $target"
	ln -sfn "$target" "$release_root/current"
}

apply_target() {
	case "$layout" in
		bundle) apply_bundle "$1" ;;
		binary) apply_binary "$1" ;;
		release) apply_release "$1" ;;
	esac
}

rollback_target() {
	case "$layout" in
		bundle) rollback_bundle ;;
		binary) rollback_binary ;;
		release) rollback_release ;;
	esac
}

record_state() {
	local outcome="$1"
	python3 - "$state_dir/state.json" "$outcome" "$channel" "$installed_version" "$installed_commit" "$target_version" "$target_commit" <<'PY'
import datetime, json, sys

path, outcome, channel, old_version, old_commit, new_version, new_commit = sys.argv[1:8]
now = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
with open(path, "w") as handle:
    json.dump(
        {
            "outcome": outcome,
            "channel": channel,
            "from": {"version": old_version, "commit": old_commit},
            "to": {"version": new_version, "commit": new_commit},
            "at": now,
        },
        handle,
        indent=2,
        sort_keys=True,
    )
    handle.write("\n")
PY
}

emit_report() {
	local outcome="$1"
	if "$json"; then
		python3 - "$outcome" "$channel" "$installed_version" "$installed_commit" "$target_version" "$target_commit" <<'PY'
import json, sys

outcome, channel, version, commit, target_version, target_commit = sys.argv[1:7]
report = {
    "outcome": outcome,
    "channel": channel,
    "installed": {"version": version, "commit": commit},
}
if target_commit:
    report["available"] = {"version": target_version, "commit": target_commit}
print(json.dumps(report, indent=2, sort_keys=True))
PY
		return 0
	fi
	printf 'channel: %s\ninstalled: %s commit %s\n' "$channel" "$installed_version" "$installed_commit"
	if [[ -n "$target_commit" ]]; then
		printf 'available: %s commit %s\n' "$target_version" "$target_commit"
	fi
	printf 'outcome: %s\n' "$outcome"
}

load_config

case "$command_name" in
	status | check)
		installed_facts
		manifest_path="$(mktemp "$staging_root/manifest.XXXXXX")"
		trap 'rm -f "$manifest_path"' EXIT
		fetch_url "$manifest_url" "$manifest_path" 120 || die "could not read the channel manifest at $manifest_url"
		read_manifest "$manifest_path"
		[[ "$manifest_channel" == "$channel" ]] || warn "manifest channel $manifest_channel differs from configured $channel"
		if [[ "$installed_commit" == "$manifest_commit" ]]; then
			emit_report current
			exit 0
		fi
		if channel_behind; then
			log "the channel build $manifest_version ($manifest_commit) predates the installed build; nothing to do"
			emit_report channel-behind
			exit 0
		fi
		target_version="$manifest_version"
		target_commit="$manifest_commit"
		if [[ "$command_name" == status ]]; then
			emit_report available
			exit 0
		fi
		require_enabled
		emit_report available
		exit 10
		;;
	apply)
		require_enabled
		require_manifest
		installed_facts
		manifest_path="$(mktemp "$staging_root/manifest.XXXXXX")"
		trap 'rm -f "$manifest_path"' EXIT
		fetch_url "$manifest_url" "$manifest_path" 120 || die "could not read the channel manifest at $manifest_url"
		read_manifest "$manifest_path"
		if [[ "$installed_commit" == "$manifest_commit" && "$force" != true ]]; then
			emit_report current
			exit 0
		fi
		if [[ "$force" != true ]] && channel_behind; then
			log "the channel build $manifest_version ($manifest_commit) predates the installed build; nothing to do"
			emit_report channel-behind
			exit 0
		fi
		target_version="$manifest_version"
		target_commit="$manifest_commit"
		if [[ "$force" != true ]] && ! gate_busy; then
			warn "deferring: sessions are still busy (use --force to update anyway)"
			exit 3
		fi
		log "updating $installed_version ($installed_commit) -> $target_version ($target_commit)"
		asset="$(asset_url "$asset_name")"
		if "$dry_run"; then
			log "dry run: would fetch $asset"
			log "dry run: layout $layout at $install_dir, service $service_kind, restart skipped"
			emit_report available
			exit 0
		fi
		if ! mkdir "$lock_dir" 2>/dev/null; then
			owner="$(cat "$lock_dir/pid" 2>/dev/null || true)"
			if [[ -n "$owner" ]] && ! kill -0 "$owner" 2>/dev/null; then
				rm -rf "$lock_dir"
				mkdir "$lock_dir"
			else
				log "another self-update run holds the lock"
				exit 0
			fi
		fi
		printf '%s\n' "$$" >"$lock_dir/pid"
		cleanup() {
			rm -rf "$lock_dir"
			rm -f "$manifest_path"
		}
		trap 'cleanup' EXIT
		was_active=0
		service_active && was_active=1

		work="$staging_root/$target_commit"
		rm -rf "$work"
		mkdir -p "$work"
		archive="$work/$asset_name"
		log "downloading $asset_name from $asset"
		fetch_url "$asset" "$archive" || die "download failed: $asset"
		actual="$(sha256 "$archive")"
		[[ "$actual" == "$asset_sha" ]] || die "checksum mismatch for $asset_name (expected $asset_sha, got $actual)"
		extract="$work/extracted"
		mkdir -p "$extract"
		tar -xzf "$archive" -C "$extract"
		root="$extract"
		if [[ ! -d "$extract/daemon" ]]; then
			entries=("$extract"/*)
			if ((${#entries[@]} == 1)) && [[ -d "${entries[0]}" ]]; then
				root="${entries[0]}"
			fi
		fi
		bundle_commit="$(run_ao "$root/daemon/ao" version 2>/dev/null | awk '{print $3}')" || die "the staged bundle does not run"
		[[ "$bundle_commit" == "$target_commit" ]] || die "the staged bundle reports commit $bundle_commit, expected $target_commit"
		if [[ "$was_active" == 1 ]]; then
			log "restarting $service_kind $service_unit$service_label"
		else
			log "the host service is not running; swapping files only"
		fi
		apply_target "$root"
		if [[ "$was_active" == 0 ]]; then
			record_state updated-not-restarted
			emit_report updated
			exit 0
		fi
		restart_service || true
		if ! wait_ready; then
			warn "the updated daemon did not become ready; rolling back"
			rollback_target
			restart_service || true
			if wait_ready; then
				record_state rolled-back
				warn "rolled back to $installed_version ($installed_commit)"
			else
				record_state rollback-failed
				warn "the rollback did not become ready; inspect the service logs"
			fi
			exit 1
		fi
		if [[ -n "$cli_link" ]]; then
			expected_link="$(current_binary)"
			if [[ ! -e "$cli_link" && ! -L "$cli_link" ]]; then
				mkdir -p "$(dirname "$cli_link")"
				ln -sfn "$expected_link" "$cli_link"
			elif [[ -L "$cli_link" && "$(readlink "$cli_link")" == "$expected_link" ]]; then
				log "$cli_link already points at this install"
			else
				warn "$cli_link is not a symlink to $expected_link; left unchanged"
			fi
		fi
		record_state updated
		log "updated to $(run_ao "$(current_binary)" version 2>/dev/null || printf '%s' "$target_version")"
		rm -rf "$work"
		emit_report updated
		;;
	rollback)
		require_enabled
		installed_facts
		rollback_target
		if ! "$dry_run" && service_active; then
			restart_service || true
			if wait_ready; then
				log "rolled back and ready"
			else
				warn "rolled back but the daemon did not become ready"
				exit 1
			fi
		fi
		record_state rolled-back
		emit_report rolled-back
		;;
	*) usage >&2; exit 2 ;;
esac
