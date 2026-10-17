#!/usr/bin/env bash
# Supervisor of the AO hub image. It runs under tini and starts two processes:
#
#   - `ao daemon`, the orchestrator. Its remote-host listener is enabled and
#     bound to 127.0.0.1:3011 (loopbackOnly), reachable only by the BFF in this
#     container. The loopback API on 3001 stays internal.
#   - the hub BFF (packages/hub-bff), the only public listener, on $PORT.
#   - optionally, with AO_HUB_ORCHESTRATOR_TICK=1, the orchestrator wake-up
#     loop (`ao-orchestrator-tick.sh loop`, RW-5.4.2). It sends the hub
#     orchestrator a TICK with PR and CI state every 5 minutes.
#
# If the daemon or the BFF exits, the other is stopped and this script exits
# with the first one's status, so Railway restarts the whole container. The
# tick loop starts after AO_HUB_TICK_START_DELAY seconds and is not critical:
# when it exits it is logged and restarted after
# AO_HUB_TICK_RESTART_DELAY seconds, and the hub keeps running. SIGTERM or
# SIGINT stops every child and exits 0.
#
#   1. As root (Railway with RAILWAY_RUN_UID=0): hand the root-owned volume to
#      uid 10001 and re-run this script as that user, as the worker entrypoint
#      does.
#   2. Create AO_DATA_DIR, HOME, and AO_HUB_DATA_DIR (mode 0700).
#   3. Seed or repair <AO_DATA_DIR>/mobile/config.json: the listener is enabled,
#      loopback-only, on port 3011. The password is generated once and kept.
#      It is the BFF's bearer for the daemon and is never printed.
#   4. exec-style supervision of the two processes above.
#
# AO_HUB_SUPERVISOR_DRY_RUN=1 runs steps 2-3 and exits without starting the
# processes (see supervisor_test.sh). AO_HUB_DAEMON_COMMAND, AO_HUB_BFF_MAIN and
# AO_HUB_TICK_COMMAND override the commands for tests.
set -euo pipefail

readonly ao_uid=10001 ao_gid=10001
readonly volume_root=/data
readonly data_dir=${AO_DATA_DIR:-/data/ao}
readonly hub_dir=${AO_HUB_DATA_DIR:-/data/hub}
readonly home_dir=${HOME:-/data/home}
readonly config=$data_dir/mobile/config.json
# The BFF's hub entry targets this port (packages/hub-bff, "The hub entry"):
# AO_HUB_DAEMON_URL is http://127.0.0.1:3011.
readonly daemon_port=3011
readonly dry_run=${AO_HUB_SUPERVISOR_DRY_RUN:-0}
readonly bff_main=${AO_HUB_BFF_MAIN:-/opt/ao/hub/bff/src/main.ts}
read -r -a daemon_command <<<"${AO_HUB_DAEMON_COMMAND:-ao daemon}"
readonly tick_enabled=${AO_HUB_ORCHESTRATOR_TICK:-0}
# The first tick waits for the daemon to become ready.
readonly tick_start_delay=${AO_HUB_TICK_START_DELAY:-60}
readonly tick_restart_delay=${AO_HUB_TICK_RESTART_DELAY:-60}
read -r -a tick_command <<<"${AO_HUB_TICK_COMMAND:-/usr/local/bin/ao-orchestrator-tick.sh loop}"

log() { printf 'ao-hub: %s\n' "$*" >&2; }
die() {
	log "error: $*"
	exit 1
}

# Railway mounts volumes owned by root. With RAILWAY_RUN_UID=0 the script
# starts as root, chowns the volume once, and drops to uid 10001 for everything
# else, both processes included.
if [[ $(id -u) == 0 && $dry_run != 1 ]]; then
	for dir in "$volume_root" "$data_dir" "$home_dir" "$hub_dir"; do
		if [[ -d $dir && $(stat -c %u "$dir") != "$ao_uid" ]]; then
			log "handing $dir to uid $ao_uid"
			chown -R "$ao_uid:$ao_gid" "$dir"
		fi
	done
	install -d -m 0700 -o "$ao_uid" -g "$ao_gid" "$data_dir" "$home_dir" "$hub_dir"
	exec setpriv --reuid="$ao_uid" --regid="$ao_gid" --init-groups -- "$0" "$@"
fi

prepare_dirs() {
	local dir
	for dir in "$data_dir" "$home_dir" "$hub_dir"; do
		if ! mkdir -p "$dir" 2>/dev/null || [[ ! -w $dir ]]; then
			die "$dir is not writable by uid $(id -u). On Railway, set RAILWAY_RUN_UID=0 so the supervisor can chown the volume."
		fi
		chmod 0700 "$dir" || die "cannot chmod $dir; it must be owned by uid $(id -u)"
	done
}

# Same character set as the worker entrypoint's password generator.
generate_password() {
	local chars=""
	while ((${#chars} < 32)); do
		chars+=$(head -c 64 /dev/urandom | LC_ALL=C tr -dc 'A-Za-z0-9')
	done
	printf '%s' "${chars:0:32}"
}

# write_config replaces config.json atomically and keeps it 0600, like
# mobilebridge.Save. The arguments are a command that prints the new file.
write_config() {
	local tmp
	tmp=$(mktemp "${config%/*}/.config-XXXXXX.tmp")
	if ! "$@" >"$tmp"; then
		rm -f "$tmp"
		die "could not write $config"
	fi
	chmod 0600 "$tmp"
	mv "$tmp" "$config"
}

# seed_daemon_listener makes the daemon restore its remote-host listener on
# boot (RestoreOnBoot) bound to loopback only. An existing password is kept so
# a restart does not change the BFF's credential. The listener settings are
# forced on every start: a config that exposes 0.0.0.0 or another port would
# bypass the container boundary this image relies on.
seed_daemon_listener() {
	mkdir -p "${config%/*}"
	chmod 0700 "${config%/*}"
	if [[ ! -e $config ]]; then
		write_config jq -n --arg password "$(generate_password)" --argjson port "$daemon_port" \
			'{enabled: true, password: $password, lastPort: $port, noPublicTunnel: true, loopbackOnly: true}'
		log "daemon listener config written: 127.0.0.1:$daemon_port"
		return
	fi
	jq -e . "$config" >/dev/null || die "$config is not valid JSON"
	if ! jq -e --argjson port "$daemon_port" \
		'.enabled == true and .loopbackOnly == true and .lastPort == $port and (.password // "") != ""' "$config" >/dev/null; then
		if [[ $(jq -r '(.password // "") != ""' "$config") == true ]]; then
			write_config jq --argjson port "$daemon_port" \
				'.enabled = true | .loopbackOnly = true | .lastPort = $port' "$config"
		else
			write_config jq --arg password "$(generate_password)" --argjson port "$daemon_port" \
				'.enabled = true | .loopbackOnly = true | .lastPort = $port | .password = $password' "$config"
		fi
		log "daemon listener config repaired: 127.0.0.1:$daemon_port"
	fi
}

stopping=0
daemon_pid=
bff_pid=
tick_pid=

stop_children() {
	stopping=1
	[[ -n $daemon_pid ]] && kill -TERM "$daemon_pid" 2>/dev/null || true
	[[ -n $bff_pid ]] && kill -TERM "$bff_pid" 2>/dev/null || true
	[[ -n $tick_pid ]] && kill -TERM "$tick_pid" 2>/dev/null || true
}

# start_tick starts the tick loop after $1 seconds. The delay runs in the same
# child, so the supervisor never blocks on it and SIGTERM ends it at once.
start_tick() {
	(
		trap 'exit 0' TERM
		if (($1 > 0)); then
			sleep "$1" &
			wait $!
		fi
		exec "${tick_command[@]}"
	) &
	tick_pid=$!
}

supervise() {
	local status=0 exited
	trap stop_children TERM INT
	"${daemon_command[@]}" &
	daemon_pid=$!
	node "$bff_main" &
	bff_pid=$!
	log "started ao daemon (pid $daemon_pid) and the hub BFF (pid $bff_pid)"
	if [[ $tick_enabled == 1 ]]; then
		start_tick "$tick_start_delay"
		log "started the orchestrator tick loop (pid $tick_pid, first tick in ${tick_start_delay}s)"
	fi

	# wait -n returns when the first child exits. A signal trapped above
	# interrupts it with a status above 128 and sends SIGTERM to every child.
	# An exit of the tick loop only restarts it.
	while :; do
		status=0
		exited=
		wait -n -p exited || status=$?
		if ((stopping)) || [[ -z $tick_pid || $exited != "$tick_pid" ]]; then
			break
		fi
		log "orchestrator tick loop exited with status $status; restarting in ${tick_restart_delay}s"
		start_tick "$tick_restart_delay"
	done
	if ((stopping)); then
		status=0
	else
		if kill -0 "$daemon_pid" 2>/dev/null; then exited="hub BFF"; else exited="ao daemon"; fi
		log "$exited exited with status $status; stopping the other process"
	fi
	stop_children
	wait "$daemon_pid" 2>/dev/null || true
	wait "$bff_pid" 2>/dev/null || true
	[[ -n $tick_pid ]] && wait "$tick_pid" 2>/dev/null || true
	exit "$status"
}

prepare_dirs
seed_daemon_listener

if [[ $dry_run == 1 ]]; then
	log "dry run; would start: ${daemon_command[*]} and node $bff_main"
	[[ $tick_enabled == 1 ]] && log "dry run; would start the tick loop: ${tick_command[*]}"
	exit 0
fi
supervise
