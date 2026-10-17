#!/usr/bin/env bash
# Supervisor of the AO hub image. It runs under tini and starts two processes:
#
#   - `ao daemon`, the orchestrator. Its remote-host listener is enabled and
#     bound to 127.0.0.1:3011 (loopbackOnly), reachable only by the BFF in this
#     container. The loopback API on 3001 stays internal.
#   - the hub BFF (packages/hub-bff), the only public listener, on $PORT.
#
# If either process exits, the other is stopped and this script exits with the
# first one's status, so Railway restarts the whole container. SIGTERM or
# SIGINT stops both and exits 0.
#
#   1. As root (Railway with RAILWAY_RUN_UID=0): hand the root-owned volume to
#      uid 10001 and re-run this script as that user, as the worker entrypoint
#      does.
#   2. Create AO_DATA_DIR, HOME, and AO_HUB_DATA_DIR (mode 0700).
#   3. Seed or repair <AO_DATA_DIR>/mobile/config.json: the listener is enabled,
#      loopback-only, on port 3011. The password is generated once and kept.
#      It is the BFF's bearer for the daemon and is never printed.
#   4. Sync the image's agent CLIs (AO_HUB_NPM_SEED) into the npm prefix on the
#      volume (NPM_CONFIG_PREFIX), so harness updates made from the UI persist.
#   5. exec-style supervision of the two processes above.
#
# AO_HUB_SUPERVISOR_DRY_RUN=1 runs steps 2-3 and exits without starting the
# processes (see supervisor_test.sh). AO_HUB_DAEMON_COMMAND and AO_HUB_BFF_MAIN
# override the two commands for tests.
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

# sync_agent_prefix copies each agent CLI the image installed under
# AO_HUB_NPM_SEED into NPM_CONFIG_PREFIX on the volume when the volume lacks it
# or has an older version, with the package's bin links. A version updated
# from the UI (npm install -g into that prefix) is newer and is kept, so it
# survives restarts; a newer image version replaces an older one.
sync_agent_prefix() {
	local seed=${AO_HUB_NPM_SEED:-} dest=${NPM_CONFIG_PREFIX:-}
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

stopping=0
daemon_pid=
bff_pid=

stop_children() {
	stopping=1
	[[ -n $daemon_pid ]] && kill -TERM "$daemon_pid" 2>/dev/null || true
	[[ -n $bff_pid ]] && kill -TERM "$bff_pid" 2>/dev/null || true
}

supervise() {
	local status=0 exited
	trap stop_children TERM INT
	"${daemon_command[@]}" &
	daemon_pid=$!
	node "$bff_main" &
	bff_pid=$!
	log "started ao daemon (pid $daemon_pid) and the hub BFF (pid $bff_pid)"

	# wait -n returns when the first child exits. A signal trapped above
	# interrupts it with a status above 128 and sends SIGTERM to both children.
	wait -n || status=$?
	if ((stopping)); then
		status=0
	else
		if kill -0 "$daemon_pid" 2>/dev/null; then exited="hub BFF"; else exited="ao daemon"; fi
		log "$exited exited with status $status; stopping the other process"
	fi
	stop_children
	wait "$daemon_pid" 2>/dev/null || true
	wait "$bff_pid" 2>/dev/null || true
	exit "$status"
}

prepare_dirs
seed_daemon_listener
sync_agent_prefix

if [[ $dry_run == 1 ]]; then
	log "dry run; would start: ${daemon_command[*]} and node $bff_main"
	exit 0
fi
supervise
