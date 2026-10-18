#!/usr/bin/env bash
# Install the daemon self-update timer: detect the host, write the config,
# copy the updater, and enable the timer. See docs/self-hosted-remote.md.
set -euo pipefail

usage() {
	cat <<'TEXT'
Usage: install-self-update.sh [commands] [options]

Commands:
  --install            write the config, copy the updater, enable the timer
  --status             print the detected layout, service, and timer state
  --disable            stop and disable the timer, keeping the config
  --uninstall          remove the timer, the config, and the updater copy
  --now                run one `ao-self-update apply` after installing
  --check              run one `ao-self-update check` after installing

Options:
  --channel NAME       update channel (default: main)
  --repo OWNER/REPO    GitHub repository that publishes the channel
  --manifest-url URL   explicit channel manifest URL or path
  --layout KIND        bundle, release, or binary (default: detected)
  --install-dir DIR    directory that holds daemon/ and acp-runtime/
  --release-root DIR   release directory root for the release layout
  --binary-name NAME   daemon file name for the binary layout (default: ao)
  --cli-link PATH      ~/.local/bin/ao style symlink to keep pointing here
  --unit UNIT          systemd user unit that runs the daemon
  --label LABEL        launchd agent label that runs the daemon
  --interval-minutes N timer interval (default: 60)
  --grace-minutes N    quiet time before a busy host is updated (default: 20)
  --source PATH        updater script to install (default: ao-self-update.sh)
  --dry-run            print what would change without writing anything
  -h, --help           show this help
TEXT
}

log() { printf '[install-self-update] %s\n' "$*"; }
warn() { printf '[install-self-update] %s\n' "$*" >&2; }
die() {
	printf '[install-self-update] %s\n' "$*" >&2
	exit 1
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source_script="$script_dir/ao-self-update.sh"
config_path="${AO_SELF_UPDATE_CONFIG:-$HOME/.ao/self-update.json}"
state_dir="$HOME/.ao/self-update"
bin_dir="$HOME/.ao/bin"
target_script="$bin_dir/ao-self-update"
command_name=install
channel=main
repo=""
manifest_url=""
layout=""
install_dir=""
release_root=""
binary_name=""
cli_link=""
unit=""
label=""
interval_minutes=60
grace_minutes=20
dry_run=false
run_after=""

while (($#)); do
	case "$1" in
		--install | --status | --disable | --uninstall) command_name="${1#--}"; shift ;;
		--now) run_after=apply; shift ;;
		--check) run_after=check; shift ;;
		--channel) channel="${2:?--channel needs a name}"; shift 2 ;;
		--repo) repo="${2:?--repo needs OWNER/REPO}"; shift 2 ;;
		--manifest-url) manifest_url="${2:?--manifest-url needs a URL}"; shift 2 ;;
		--layout) layout="${2:?--layout needs a kind}"; shift 2 ;;
		--install-dir) install_dir="${2:?--install-dir needs a path}"; shift 2 ;;
		--release-root) release_root="${2:?--release-root needs a path}"; shift 2 ;;
		--binary-name) binary_name="${2:?--binary-name needs a name}"; shift 2 ;;
		--cli-link) cli_link="${2:?--cli-link needs a path}"; shift 2 ;;
		--unit) unit="${2:?--unit needs a unit name}"; shift 2 ;;
		--label) label="${2:?--label needs a label}"; shift 2 ;;
		--interval-minutes) interval_minutes="${2:?--interval-minutes needs minutes}"; shift 2 ;;
		--grace-minutes) grace_minutes="${2:?--grace-minutes needs minutes}"; shift 2 ;;
		--source) source_script="${2:?--source needs a path}"; shift 2 ;;
		--dry-run) dry_run=true; shift ;;
		-h | --help) usage; exit 0 ;;
		*) usage >&2; exit 2 ;;
	esac
done

for required in python3 curl tar; do
	command -v "$required" >/dev/null || die "$required is required"
done
if [[ "$(id -u)" == 0 ]]; then
	die "run as the user who owns the AO host, not as root"
fi

json_field() {
	python3 -c '
import json, sys

try:
    with open(sys.argv[1]) as handle:
        doc = json.load(handle)
except (OSError, ValueError):
    doc = {}
for key in sys.argv[2].split("."):
    doc = (doc or {}).get(key) if isinstance(doc, dict) else None
print("" if doc is None else doc)
' "$1" "$2"
}

installed_binary() {
	local exe=""
	if command -v ao >/dev/null; then
		exe="$(ao status --json 2>/dev/null | python3 -c '
import json, sys

try:
    print(json.load(sys.stdin).get("executablePath") or "")
except ValueError:
    print("")
' || true)"
		[[ -n "$exe" ]] || exe="$(command -v ao)"
	fi
	printf '%s\n' "$exe"
}

# discover_service reads the unit or agent that runs the daemon, which names
# both the service and its binary even when ao is not on PATH.
discover_systemd() {
	local file exec exe args
	for file in "$HOME"/.config/systemd/user/*.service; do
		[[ -f "$file" ]] || continue
		exec="$(grep -h '^ExecStart=' "$file" 2>/dev/null | head -1 || true)"
		exec="${exec#ExecStart=}"
		exec="${exec#-}"
		exec="${exec//%h/$HOME}"
		exe="${exec%% *}"
		args="${exec#"$exe"}"
		[[ "$(basename "$exe")" == ao ]] || continue
		case " $args " in *" daemon "*) ;; *) continue ;; esac
		[[ -x "$exe" ]] || continue
		unit="$(basename "$file" .service)"
		service_binary="$exe"
		return 0
	done
	return 1
}

discover_launchd() {
	local found
	found="$(python3 -c '
import glob, os, plistlib, re, sys

exec_line = re.compile(r"^\s*exec\s+(\S+)\s+daemon\b", re.M)


def from_program(program):
    if os.path.basename(program) == "ao":
        return program
    try:
        with open(program) as handle:
            text = handle.read()
    except (OSError, UnicodeDecodeError):
        return ""
    found = exec_line.search(text)
    if found and os.path.basename(found.group(1)) == "ao":
        return found.group(1)
    return ""


for path in sorted(glob.glob(os.path.expanduser("~/Library/LaunchAgents/*.plist"))):
    try:
        with open(path, "rb") as handle:
            doc = plistlib.load(handle)
    except Exception:
        continue
    args = [str(value) for value in doc.get("ProgramArguments") or []]
    if not args:
        continue
    binary = args[0] if args[1:] and args[1] == "daemon" else from_program(args[0])
    if not binary or os.path.basename(binary) != "ao":
        continue
    label = doc.get("Label") or os.path.basename(path)[: -len(".plist")]
    print("%s\t%s" % (label, binary))
    sys.exit(0)
sys.exit(1)
')" || return 1
	label="${found%%$'\t'*}"
	service_binary="${found#*$'\t'}"
	return 0
}

discover_service() {
	case "$(uname -s)" in
		Linux) discover_systemd ;;
		Darwin) discover_launchd ;;
		*) return 1 ;;
	esac
}

detect_layout() {
	local exe="$1" candidate stamp_dir releases_dir root_dir
	exe="$(python3 -c 'import os, sys; print(os.path.realpath(sys.argv[1]))' "$exe")"
	binary_name="$(basename "$exe")"
	if [[ "$(basename "$(dirname "$exe")")" != daemon ]]; then
		layout="binary"
		install_dir="$(dirname "$exe")"
		return 0
	fi
	candidate="$(dirname "$(dirname "$exe")")"
	if [[ "$(basename "$candidate")" == resources ]]; then
		stamp_dir="$(dirname "$candidate")"
		releases_dir="$(dirname "$stamp_dir")"
		root_dir="$(dirname "$releases_dir")"
		if [[ "$(basename "$releases_dir")" == releases && -L "$root_dir/current" ]]; then
			layout="release"
			release_root="$root_dir"
			install_dir="$root_dir/current/resources"
			return 0
		fi
	fi
	layout="bundle"
	install_dir="$candidate"
}

detect_systemd_unit() {
	local binary="$1" file exec
	for file in "$HOME"/.config/systemd/user/*.service; do
		[[ -f "$file" ]] || continue
		exec="$(grep -h '^ExecStart=' "$file" 2>/dev/null | head -1 || true)"
		exec="${exec//%h/$HOME}"
		[[ -n "$exec" ]] || continue
		case "$exec" in
			*"$binary"* | *"$install_dir"*) printf '%s\n' "$(basename "$file" .service)"; return 0 ;;
		esac
	done
	return 1
}

detect_launchd_label() {
	local binary="$1"
	python3 -c '
import glob, os, plistlib, sys

binary = os.path.realpath(sys.argv[1])
for path in sorted(glob.glob(os.path.expanduser("~/Library/LaunchAgents/*.plist"))):
    try:
        with open(path, "rb") as handle:
            doc = plistlib.load(handle)
    except Exception:
        continue
    args = doc.get("ProgramArguments") or []
    if not args:
        continue
    if os.path.realpath(str(args[0])) == binary:
        print(doc.get("Label") or os.path.basename(path)[: -len(".plist")])
        sys.exit(0)
sys.exit(1)
' "$binary"
}

detect_service() {
	local binary="$1"
	[[ -n "$unit" || -n "$label" ]] && return 0
	case "$(uname -s)" in
		Linux)
			unit="$(detect_systemd_unit "$binary" || true)"
			[[ -n "$unit" ]] || die "no systemd user unit runs $binary; pass --unit"
			;;
		Darwin)
			label="$(detect_launchd_label "$binary" || true)"
			[[ -n "$label" ]] || die "no LaunchAgent runs $binary; pass --label"
			;;
	esac
	return 0
}

collect_env_json() {
	python3 -c '
import json, os, sys

names = ["AO_DATA_DIR", "AO_RUN_FILE", "AO_PORT"]
found = {name: os.environ[name] for name in names if os.environ.get(name)}
print(json.dumps(found))
'
}

write_config() {
	local env_json="$1"
	if "$dry_run"; then
		log "would write $config_path"
		return 0
	fi
	mkdir -p "$(dirname "$config_path")"
	python3 -c '
import json, sys

path, channel, repo, manifest_url, layout = sys.argv[1:6]
install_dir, release_root, binary_name, cli_link = sys.argv[6:10]
kind, unit, label, grace, env_json = sys.argv[10:15]
doc = {
    "channel": channel,
    "enabled": True,
    "layout": layout,
    "installDir": install_dir,
    "binaryName": binary_name,
    "graceMinutes": int(grace),
    "service": {"kind": kind},
    "env": json.loads(env_json),
}
if repo:
    doc["repo"] = repo
if manifest_url:
    doc["manifestUrl"] = manifest_url
if release_root:
    doc["releaseRoot"] = release_root
if cli_link:
    doc["cliLink"] = cli_link
if unit:
    doc["service"]["unit"] = unit
if label:
    doc["service"]["label"] = label
with open(path, "w") as handle:
    json.dump(doc, handle, indent=2, sort_keys=True)
    handle.write("\n")
' "$config_path" "$channel" "$repo" "$manifest_url" "$layout" "$install_dir" \
		"$release_root" "$binary_name" "$cli_link" "$service_kind" "$unit" "$label" \
		"$grace_minutes" "$env_json"
	log "wrote $config_path"
}

install_updater() {
	if "$dry_run"; then
		log "would copy $source_script to $target_script"
		return 0
	fi
	[[ -f "$source_script" ]] || die "no updater script at $source_script"
	mkdir -p "$bin_dir"
	install -m 0755 "$source_script" "$target_script"
	log "installed $target_script"
}

systemd_paths() {
	unit_dir="$HOME/.config/systemd/user"
	service_file="$unit_dir/ao-self-update.service"
	timer_file="$unit_dir/ao-self-update.timer"
}

install_systemd() {
	systemd_paths
	if "$dry_run"; then
		log "would write $service_file and $timer_file, then enable the timer"
		return 0
	fi
	mkdir -p "$unit_dir" "$state_dir/logs"
	cat >"$service_file" <<EOF
[Unit]
Description=AO daemon self-update

[Service]
Type=oneshot
ExecStart=$target_script apply
EOF
	cat >"$timer_file" <<EOF
[Unit]
Description=Check the AO daemon update channel

[Timer]
OnBootSec=5m
OnUnitActiveSec=${interval_minutes}m
Unit=ao-self-update.service

[Install]
WantedBy=timers.target
EOF
	systemctl --user daemon-reload
	systemctl --user enable --now ao-self-update.timer
	log "enabled ao-self-update.timer every ${interval_minutes}m"
	log "keep it running after logout with: sudo loginctl enable-linger $(id -un)"
}

install_launchd() {
	local plist="$HOME/Library/LaunchAgents/dev.aoagents.self-update.plist"
	if "$dry_run"; then
		log "would write $plist with StartInterval $((interval_minutes * 60))"
		return 0
	fi
	mkdir -p "$(dirname "$plist")" "$state_dir/logs"
	AO_SELF_UPDATE_PLIST="$plist" python3 -c '
import os, plistlib

plist_path = os.environ["AO_SELF_UPDATE_PLIST"]
label = "dev.aoagents.self-update"
doc = {
    "Label": label,
    "ProgramArguments": [os.path.expanduser("~/.ao/bin/ao-self-update"), "apply"],
    "StartInterval": int(os.environ["AO_SELF_UPDATE_INTERVAL"]),
    "RunAtLoad": False,
    "StandardOutPath": os.path.expanduser("~/.ao/self-update/logs/update.log"),
    "StandardErrorPath": os.path.expanduser("~/.ao/self-update/logs/update.err.log"),
}
with open(plist_path, "wb") as handle:
    plistlib.dump(doc, handle)
'
	launchctl bootout "gui/$(id -u)/dev.aoagents.self-update" 2>/dev/null || true
	launchctl bootstrap "gui/$(id -u)" "$plist"
	log "loaded dev.aoagents.self-update every ${interval_minutes}m"
}

install_timer() {
	case "$(uname -s)" in
		Linux) install_systemd ;;
		Darwin) AO_SELF_UPDATE_INTERVAL="$((interval_minutes * 60))" install_launchd ;;
		*) die "unsupported platform: $(uname -s)" ;;
	esac
}

disable_timer() {
	case "$(uname -s)" in
		Linux)
			if ! "$dry_run"; then
				systemctl --user disable --now ao-self-update.timer 2>/dev/null || true
			fi
			log "disabled ao-self-update.timer"
			;;
		Darwin)
			if ! "$dry_run"; then
				launchctl bootout "gui/$(id -u)/dev.aoagents.self-update" 2>/dev/null || true
			fi
			log "unloaded dev.aoagents.self-update"
			;;
	esac
}

uninstall() {
	disable_timer
	if "$dry_run"; then
		return 0
	fi
	systemd_paths
	rm -f "$HOME/Library/LaunchAgents/dev.aoagents.self-update.plist" "$service_file" "$timer_file"
	if [[ -f "$config_path" ]]; then
		rm -f "$config_path"
		log "removed $config_path"
	fi
	rm -f "$target_script"
	[[ -d "$unit_dir" ]] && systemctl --user daemon-reload 2>/dev/null || true
	log "kept $state_dir (state, previous build, and logs)"
}

print_status() {
	local exe detected="${layout:-}"
	exe="$(installed_binary)"
	printf 'binary:  %s\n' "${exe:-unknown}"
	if [[ -f "$config_path" ]]; then
		printf 'config:  %s\n' "$config_path"
		printf '  channel:     %s\n' "$(json_field "$config_path" channel)"
		printf '  layout:      %s\n' "$(json_field "$config_path" layout)"
		printf '  installDir:  %s\n' "$(json_field "$config_path" installDir)"
		printf '  service:     %s %s\n' "$(json_field "$config_path" service.kind)" \
			"$(json_field "$config_path" service.unit)$(json_field "$config_path" service.label)"
		printf '  enabled:     %s\n' "$(json_field "$config_path" enabled)"
	else
		printf 'config:  (none)\n'
	fi
	printf 'updater: %s\n' "$([[ -x "$target_script" ]] && printf '%s' "$target_script" || printf '(not installed)')"
	case "$(uname -s)" in
		Linux)
			printf 'timer:   %s\n' "$(systemctl --user is-active ao-self-update.timer 2>/dev/null || echo inactive)"
			;;
		Darwin)
			if launchctl print "gui/$(id -u)/dev.aoagents.self-update" >/dev/null 2>&1; then
				printf 'timer:   loaded\n'
			else
				printf 'timer:   not loaded\n'
			fi
			;;
	esac
	[[ -n "$detected" ]] && printf 'detected layout: %s\n' "$detected"
}

if [[ "$command_name" == status ]]; then
	print_status
	exit 0
fi

if [[ "$command_name" == uninstall ]]; then
	uninstall
	exit 0
fi

if [[ "$command_name" == disable ]]; then
	disable_timer
	exit 0
fi

binary="$(installed_binary)"
if [[ -z "$binary" && -z "$unit" && -z "$label" ]]; then
	if discover_service; then
		binary="$service_binary"
		log "found the daemon through the service that runs it"
	fi
fi
if [[ -z "$layout" || -z "$install_dir" ]]; then
	[[ -n "$binary" ]] || die "no ao daemon found; pass --install-dir and --layout"
	detect_layout "$binary"
	log "detected layout $layout at $install_dir"
fi
[[ -z "$binary_name" ]] && binary_name=ao
case "$layout" in
	bundle | release | binary) ;;
	*) die "unsupported layout: $layout (bundle, release, binary)" ;;
esac
if [[ "$layout" == release && -z "$release_root" ]]; then
	die "the release layout needs --release-root"
fi
if [[ -z "$unit" && -z "$label" ]]; then
	if [[ -n "$binary" ]]; then
		detect_service "$binary"
	else
		discover_service || die "no service runs the daemon; pass --unit or --label"
	fi
fi

case "$(uname -s)" in
	Linux) service_kind=systemd ;;
	Darwin) service_kind=launchd ;;
	*) die "unsupported platform: $(uname -s)" ;;
esac

log "plan: layout=$layout installDir=$install_dir service=$service_kind $unit$label channel=$channel"
env_json="$(collect_env_json)"
write_config "$env_json"
install_updater
install_timer

if [[ -n "$run_after" ]]; then
	log "running: ao-self-update $run_after"
	if "$dry_run"; then
		log "dry run: skipped"
	else
		"$target_script" "$run_after" || log "the first $run_after exited non-zero (see the message above)"
	fi
fi

log "done; check state any time with: $target_script status"
