#!/usr/bin/env bash
# Supervisor of the AO hub image. It runs under tini and starts two required
# processes plus the optional Tailscale daemon and orchestrator tick:

#   - `ao daemon`, the orchestrator. Its remote-host listener is enabled and
#     bound to 127.0.0.1:3011 (loopbackOnly), reachable only by the BFF here;

#     the loopback API on 3001 stays internal.
#   - the hub BFF (packages/hub-bff), the only public listener, on $PORT.

#   - optionally, when TS_AUTHKEY is set, tailscaled in userspace mode with a
#     loopback-only SOCKS5 listener. Its auth key is piped to `tailscale up`;

#     it is not passed to any child environment or command line.
#   - optionally, with AO_HUB_ORCHESTRATOR_TICK=1, the orchestrator wake-up

#     loop (`ao orchestrator tick loop`). It wakes the idle hub orchestrator
#     with worker, PR and CI state every 5 minutes.

# If the daemon, the BFF, or an enabled tailscaled exits, the other processes
# are stopped and this script exits with the first one's status, so Railway

# restarts the whole container. The tick loop starts after
# AO_HUB_TICK_START_DELAY seconds and is not critical: when it exits it is

# logged and restarted after AO_HUB_TICK_RESTART_DELAY seconds, and the hub
# keeps running. SIGTERM or SIGINT stops every child and exits 0.

#   1. As root (RAILWAY_RUN_UID=0): hand the root-owned volume to uid 10001 and
#      re-run this script as that user, as the worker entrypoint does.

#   2. Create AO_DATA_DIR, HOME, and AO_HUB_DATA_DIR (mode 0700).
#   3. Seed or repair <AO_DATA_DIR>/mobile/config.json: the listener is

#      enabled, loopback-only, on port 3011. The password is generated once and
#      kept; it is the BFF's bearer for the daemon and is never printed.

#   4. Sync the image's agent CLIs (AO_HUB_NPM_SEED and the OpenCode 2 seed)
#      into their volume prefixes, so harness updates from the UI persist.

#   5. exec-style supervision of the two processes above.

# AO_HUB_SUPERVISOR_DRY_RUN=1 runs steps 2-4 and exits without starting the
# processes; the AO_HUB_DAEMON_COMMAND, AO_HUB_BFF_MAIN and AO_HUB_TICK_COMMAND

# overrides select the test commands (see supervisor_test.sh).
set -euo pipefail

readonly ao_uid=10001 ao_gid=10001
readonly volume_root=/data
readonly data_dir=${AO_DATA_DIR:-/data/ao}
readonly hub_dir=${AO_HUB_DATA_DIR:-/data/hub}
readonly remotes_file=$hub_dir/remotes.json
readonly home_dir=${HOME:-/data/home}
readonly tailscale_dir=${AO_HUB_TAILSCALE_DIR:-/data/tailscale}
readonly tailscale_socket=$tailscale_dir/tailscaled.sock
readonly tailscale_state=$tailscale_dir/tailscaled.state
readonly tailscale_socks=127.0.0.1:1055
readonly tailscale_authkey=${TS_AUTHKEY:-}
# 1232 keeps the WireGuard outer packet at or below Railway's 1316 MTU, so
# large TCP segments survive; the netstack advertises the matching TCP MSS.
readonly tailscale_mtu=${TS_DEBUG_MTU:-1232}
readonly tailscale_enabled=$([[ -n $tailscale_authkey ]] && printf 1 || printf 0)
# The key belongs only to this supervisor shell. Descendants receive a clean
# environment, and tailscale up reads the value through an anonymous pipe.
unset AO_REMOTE_TAILNET_SOCKS
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
read -r -a tick_command <<<"${AO_HUB_TICK_COMMAND:-ao orchestrator tick loop}"

log() { printf 'ao-hub: %s\n' "$*" >&2; }
die() {
	log "error: $*"
	exit 1
}

# Railway mounts volumes as root. With RAILWAY_RUN_UID=0 the script starts
# as root, chowns the volume once, then drops to uid 10001 for both processes.
if [[ $(id -u) == 0 && $dry_run != 1 ]]; then
	root_dirs=("$volume_root" "$data_dir" "$home_dir" "$hub_dir")
	[[ $tailscale_enabled == 0 ]] || root_dirs+=("$tailscale_dir")
	for dir in "${root_dirs[@]}"; do
		if [[ -d $dir && $(stat -c %u "$dir") != "$ao_uid" ]]; then
			log "handing $dir to uid $ao_uid"
			chown -R "$ao_uid:$ao_gid" "$dir"
		fi
	done
	install -d -m 0700 -o "$ao_uid" -g "$ao_gid" "$data_dir" "$home_dir" "$hub_dir"
	[[ $tailscale_enabled == 0 ]] || install -d -m 0700 -o "$ao_uid" -g "$ao_gid" "$tailscale_dir"
	exec setpriv --reuid="$ao_uid" --regid="$ao_gid" --init-groups -- "$0" "$@"
fi

unset TS_AUTHKEY

prepare_dirs() {
	local dir
	for dir in "$data_dir" "$home_dir" "$hub_dir"; do
		if ! mkdir -p "$dir" 2>/dev/null || [[ ! -w $dir ]]; then
			die "$dir is not writable by uid $(id -u). On Railway, set RAILWAY_RUN_UID=0 so the supervisor can chown the volume."
		fi
		chmod 0700 "$dir" || die "cannot chmod $dir; it must be owned by uid $(id -u)"
	done
}

export AO_REMOTES_FILE=$remotes_file

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
# loopback only, keeping an existing password; settings are forced every start.
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

# version_newer exits 0 when version $1 is newer than $2 (major.minor.patch;
# prerelease and build suffixes are ignored).
version_newer() {
	node -e '
		const parse = (v) => v.split(/[-+]/)[0].split(".").map((n) => Number.parseInt(n, 10) || 0);
		const [a, b] = process.argv.slice(1).map(parse);
		for (let i = 0; i < 3; i++) if ((a[i] ?? 0) !== (b[i] ?? 0)) process.exit((a[i] ?? 0) > (b[i] ?? 0) ? 0 : 1);
		process.exit(1);
	' "$1" "$2"
}

package_version() {
	node -p 'require(process.argv[1]).version' "$1/package.json" 2>/dev/null
}

# sync_agent_prefix seeds the agent CLIs from $1 into the volume prefix $2 when
# missing or older, with bin links; a newer copy from the UI is kept.
sync_agent_prefix() {
	local seed=${1:-} dest=${2:-}
	[[ -n $seed && -n $dest && -d $seed/lib/node_modules ]] || return 0
	mkdir -p "$dest/lib/node_modules" "$dest/bin" || die "$dest is not writable by uid $(id -u)"
	local dir pkg seed_version dest_version link target
	local -a synced=()
	for dir in "$seed"/lib/node_modules/*/ "$seed"/lib/node_modules/@*/*/; do
		[[ -f $dir/package.json ]] || continue
		pkg=${dir#"$seed/lib/node_modules/"}
		pkg=${pkg%/}
		seed_version=$(package_version "$seed/lib/node_modules/$pkg")
		dest_version=$(package_version "$dest/lib/node_modules/$pkg" || true)
		if [[ -n $dest_version ]] && ! version_newer "$seed_version" "$dest_version"; then
			continue
		fi
		mkdir -p "$(dirname "$dest/lib/node_modules/$pkg")"
		rm -rf "$dest/lib/node_modules/$pkg"
		cp -a "$seed/lib/node_modules/$pkg" "$dest/lib/node_modules/$pkg"
		synced+=("$pkg")
		log "agent CLI $pkg ${dest_version:-(none)} -> $seed_version"
	done
	((${#synced[@]})) || return 0
	# npm's bin links are relative (../lib/node_modules/<pkg>/...), so a copied
	# link resolves inside the volume prefix.
	for link in "$seed"/bin/*; do
		[[ -L $link ]] || continue
		target=$(readlink "$link")
		for pkg in "${synced[@]}"; do
			if [[ $target == *"/node_modules/$pkg/"* ]]; then
				cp -a --remove-destination "$link" "$dest/bin/"
			fi
		done
	done
}

# opencode_v2_prefix is the daemon's private OpenCode 2 npm prefix: a sibling
# of the user data home, so the v2 `opencode` bin never lands on PATH.
opencode_v2_prefix() {
	local parent=${XDG_DATA_HOME:-${HOME:-/data/home}/.local/share}
	if [[ ${parent##*/} == opencode-v2-home ]]; then
		printf '%s' "$parent"
	else
		printf '%s' "$parent/opencode-v2-home"
	fi
}

stopping=0
daemon_pid=
bff_pid=
tick_pid=
tailscale_pid=

# Services and the tick loop get the tailnet dial gate only after tailscale
# up succeeds; in disabled mode inherited copies of both variables are removed.
run_child() {
	if [[ $tailscale_enabled == 1 ]]; then
		exec env -u TS_AUTHKEY AO_REMOTE_TAILNET_SOCKS="$tailscale_socks" "$@"
	else
		exec env -u TS_AUTHKEY -u AO_REMOTE_TAILNET_SOCKS "$@"
	fi
}

stop_children() {
	stopping=1
	[[ -n $daemon_pid ]] && kill -TERM "$daemon_pid" 2>/dev/null || true
	[[ -n $bff_pid ]] && kill -TERM "$bff_pid" 2>/dev/null || true
	[[ -n $tick_pid ]] && kill -TERM "$tick_pid" 2>/dev/null || true
	[[ -n $tailscale_pid ]] && kill -TERM "$tailscale_pid" 2>/dev/null || true
}

start_tailnet() {
	[[ $tailscale_enabled == 1 ]] || return 0
	mkdir -p "$tailscale_dir" || die "cannot create $tailscale_dir"
	chmod 0700 "$tailscale_dir" || die "cannot protect $tailscale_dir"
	rm -f "$tailscale_socket"
	TS_DEBUG_MTU="$tailscale_mtu" "${AO_HUB_TAILSCALED:-/usr/local/bin/tailscaled}" \
		--tun=userspace-networking \
		--socks5-server="$tailscale_socks" \
		--state="$tailscale_state" \
		--socket="$tailscale_socket" &
	tailscale_pid=$!
	local attempt
	for attempt in {1..100}; do
		kill -0 "$tailscale_pid" 2>/dev/null || die "tailscaled exited before its socket became ready"
		[[ -S $tailscale_socket ]] && break
		sleep 0.1
	done
	[[ -S $tailscale_socket ]] || die "tailscaled socket did not become ready"
	local hostname=${TS_HOSTNAME:-ao-hub}
	if ! { printf '%s' "$tailscale_authkey" | \
		"${AO_HUB_TAILSCALE:-/usr/local/bin/tailscale}" --socket="$tailscale_socket" \
		up --auth-key=file:/dev/stdin --hostname="$hostname" --advertise-tags=tag:ao-hub; } >/dev/null 2>&1; then
		die "Tailscale authentication failed"
	fi
	log "tailnet is up; loopback SOCKS5 proxy ready"
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
		run_child "${tick_command[@]}"
	) &
	tick_pid=$!
}

supervise() {
	local status=0 exited
	trap stop_children TERM INT
	start_tailnet
	run_child "${daemon_command[@]}" &
	daemon_pid=$!
	run_child node "$bff_main" &
	bff_pid=$!
	log "started ao daemon (pid $daemon_pid) and the hub BFF (pid $bff_pid)"
	if [[ $tick_enabled == 1 ]]; then
		start_tick "$tick_start_delay"
		log "started the orchestrator tick loop (pid $tick_pid, first tick in ${tick_start_delay}s)"
	fi

	# wait -n returns when the first child exits; a trapped signal interrupts it
	# and SIGTERMs every child, while an exiting tick loop is only restarted.
	while :; do
		status=0
		exited=
		wait -n -p exited || status=$?
		if ((stopping)) || { [[ -z $tick_pid || $exited != "$tick_pid" ]] && [[ -z $tailscale_pid || $exited != "$tailscale_pid" ]]; }; then
			break
		fi
		if [[ -n $tailscale_pid && $exited == "$tailscale_pid" ]]; then
			local tailscale_status=$status
			((status != 0)) || status=1
			log "tailscaled exited with status $tailscale_status; stopping the hub"
			break
		fi
		log "orchestrator tick loop exited with status $status; restarting in ${tick_restart_delay}s"
		start_tick "$tick_restart_delay"
	done
	if ((stopping)); then
		status=0
	else
		if [[ -n $tailscale_pid ]] && ! kill -0 "$tailscale_pid" 2>/dev/null; then
			exited="tailscaled"
		elif kill -0 "$daemon_pid" 2>/dev/null; then exited="hub BFF"; else exited="ao daemon"; fi
		log "$exited exited with status $status; stopping the other processes"
	fi
	stop_children
	wait "$daemon_pid" 2>/dev/null || true
	wait "$bff_pid" 2>/dev/null || true
	[[ -n $tick_pid ]] && wait "$tick_pid" 2>/dev/null || true
	[[ -n $tailscale_pid ]] && wait "$tailscale_pid" 2>/dev/null || true
	exit "$status"
}

prepare_dirs
seed_daemon_listener
sync_agent_prefix "${AO_HUB_NPM_SEED:-}" "${NPM_CONFIG_PREFIX:-}"
sync_agent_prefix "${AO_HUB_OPENCODE_V2_SEED:-}" "$(opencode_v2_prefix)/npm"

if [[ $dry_run == 1 ]]; then
	log "dry run; would start: ${daemon_command[*]} and node $bff_main"
	[[ $tailscale_enabled == 0 ]] || log "dry run; would start Tailscale userspace SOCKS5 on $tailscale_socks"
	[[ $tick_enabled == 1 ]] && log "dry run; would start the tick loop: ${tick_command[*]}"
	exit 0
fi
supervise
