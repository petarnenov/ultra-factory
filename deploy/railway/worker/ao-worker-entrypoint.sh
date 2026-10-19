#!/usr/bin/env bash
# Entrypoint of the AO worker image: seeds the data dir, the listener config,
# the account token and git/gh credentials, then execs the daemon under tini.
set -euo pipefail

readonly ao_uid=10001 ao_gid=10001
readonly volume_root=/data
readonly data_dir=${AO_DATA_DIR:-/data/ao}
readonly home_dir=${HOME:-/data/home}
readonly api_base=http://127.0.0.1:${AO_PORT:-3001}
readonly config=$data_dir/mobile/config.json
readonly dry_run=${AO_ENTRYPOINT_DRY_RUN:-0}

log() { printf 'ao-worker: %s\n' "$*" >&2; }
die() {
	log "error: $*"
	exit 1
}

if (($# == 0)); then
	set -- ao daemon
fi

# A hosted worker's daemon may not stop itself through loopback /shutdown:
# restart the Railway service instead (same marker as the hub supervisor).
if [[ $* == "ao daemon" ]]; then
	export AO_SUPERVISED=1
fi

# Railway mounts volumes owned by root, and build-time ownership does not carry
# over. With RAILWAY_RUN_UID=0 it chowns the volume, then drops to uid 10001.
if [[ $(id -u) == 0 && $dry_run != 1 ]]; then
	for dir in "$volume_root" "$data_dir" "$home_dir"; do
		if [[ -d $dir && $(stat -c %u "$dir") != "$ao_uid" ]]; then
			log "handing $dir to uid $ao_uid"
			chown -R "$ao_uid:$ao_gid" "$dir"
		fi
	done
	install -d -m 0700 -o "$ao_uid" -g "$ao_gid" "$data_dir" "$home_dir"
	exec setpriv --reuid="$ao_uid" --regid="$ao_gid" --init-groups -- "$0" "$@"
fi

prepare_dirs() {
	local dir
	for dir in "$data_dir" "$home_dir"; do
		if ! mkdir -p "$dir" 2>/dev/null || [[ ! -w $dir ]]; then
			die "$dir is not writable by uid $(id -u). On Railway, set RAILWAY_RUN_UID=0 so the entrypoint can chown the volume."
		fi
		chmod 0700 "$dir" || die "cannot chmod $dir; it must be owned by uid $(id -u)"
	done
}

# Same alphabet and length as mobilebridge.GeneratePassword.
generate_password() {
	local chars=""
	while ((${#chars} < 8)); do
		chars+=$(head -c 64 /dev/urandom | LC_ALL=C tr -dc 'A-Za-z0-9')
	done
	printf '%s' "${chars:0:8}"
}

# write_config replaces config.json atomically, keeping it 0600 like
# mobilebridge.Save.
write_config() {
	local tmp
	mkdir -p "${config%/*}"
	chmod 0700 "${config%/*}"
	tmp=$(mktemp "${config%/*}/.config-XXXXXX.tmp")
	if ! jq "$@" >"$tmp"; then
		rm -f "$tmp"
		die "could not write $config"
	fi
	chmod 0600 "$tmp"
	mv "$tmp" "$config"
}

# The daemon restores the listener from config.json on boot (RestoreOnBoot):
# enabled, password, lastPort, noPublicTunnel. An existing file always wins.
seed_remote_host() {
	local password port saved_password
	if [[ -e $config ]]; then
		saved_password=$(jq -r '.password // ""' "$config") || die "$config is not valid JSON"
		if [[ -n ${AO_REMOTE_PASSWORD:-} && $AO_REMOTE_PASSWORD != "$saved_password" ]]; then
			log "AO_REMOTE_PASSWORD differs from the saved password; keeping the saved one (delete $config to re-seed)"
		fi
		if [[ $(jq -r '.enabled' "$config") != true ]]; then
			log "remote-host listener is disabled in $config; run \`ao remote-host enable\` to turn it on"
		fi
		log "remote-host password: ******** (saved in $config; \`ao remote-host status\` shows it)"
		return
	fi

	port=${AO_REMOTE_PORT:-3011}
	if [[ ! $port =~ ^[0-9]{1,5}$ ]] || ((10#$port < 1 || 10#$port > 65535)); then
		die "AO_REMOTE_PORT must be a port number, got '$port'"
	fi
	port=$((10#$port))
	if [[ -n ${AO_REMOTE_PASSWORD:-} ]]; then
		password=$AO_REMOTE_PASSWORD
	else
		password=$(generate_password)
	fi
	write_config -n --arg password "$password" --argjson port "$port" \
		'{enabled: true, password: $password, lastPort: $port, noPublicTunnel: true}'
	if [[ -n ${AO_REMOTE_PASSWORD:-} ]]; then
		log "remote-host config written: port $port, password from AO_REMOTE_PASSWORD"
	else
		log "remote-host config written: port $port, generated password (shown only on this start): $password"
	fi
}

# AO_REMOTE_ACCOUNT_TOKEN becomes accountTokenHash (sha256 hex); the daemon's
# account-token API is unusable here because it mints its own random token.
apply_account_token() {
	local hash
	[[ -e $config ]] || return 0
	if [[ -n ${AO_REMOTE_ACCOUNT_TOKEN:-} ]]; then
		if ((${#AO_REMOTE_ACCOUNT_TOKEN} < 32)); then
			log "warning: AO_REMOTE_ACCOUNT_TOKEN is shorter than 32 characters; use \`openssl rand -hex 32\`"
		fi
		hash=$(printf '%s' "$AO_REMOTE_ACCOUNT_TOKEN" | sha256sum | cut -d' ' -f1)
		if [[ $(jq -r '.accountTokenHash // ""' "$config") != "$hash" ]]; then
			# accountToken/accountTokenOwner belong to a token the daemon issued.
			write_config --arg hash "$hash" '.accountTokenHash = $hash | del(.accountToken, .accountTokenOwner)' "$config"
		fi
		log "account token armed from AO_REMOTE_ACCOUNT_TOKEN"
	elif jq -e '(.accountTokenHash // "") != "" and (.accountToken // "") == ""' "$config" >/dev/null; then
		# Only this script stores a hash without the token beside it; the
		# daemon's issuance always saves both. The variable is gone, so revoke.
		write_config 'del(.accountTokenHash)' "$config"
		log "account token revoked: AO_REMOTE_ACCOUNT_TOKEN is no longer set"
	fi
}

# Credentials stay in the environment, where the CLIs read them (gh, Claude
# Code); only git needs files in $HOME.
configure_credentials() {
	local name present=()
	if [[ -n ${AO_GIT_USER_NAME:-} ]]; then
		git config --global user.name "$AO_GIT_USER_NAME"
	fi
	if [[ -n ${AO_GIT_USER_EMAIL:-} ]]; then
		git config --global user.email "$AO_GIT_USER_EMAIL"
	fi
	if [[ -n ${GH_TOKEN:-}${GITHUB_TOKEN:-} ]]; then
		# Lets git push over HTTPS with the same token as gh.
		gh auth setup-git || log "warning: gh auth setup-git failed; git over HTTPS will not use the GitHub token"
	fi
	for name in GH_TOKEN GITHUB_TOKEN CLAUDE_CODE_OAUTH_TOKEN ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN OPENAI_API_KEY; do
		if [[ -n ${!name:-} ]]; then
			present+=("$name")
		fi
	done
	log "credentials from env: ${present[*]:-none}"
}

# report_when_ready logs what a hub needs to register this worker. It never
# prints the password.
report_when_ready() {
	local identity status attempt
	for ((attempt = 0; attempt < 120; attempt++)); do
		if curl --fail --silent --output /dev/null --max-time 2 "$api_base/readyz"; then
			break
		fi
		sleep 1
	done
	if ! identity=$(curl --fail --silent --show-error --max-time 5 "$api_base/api/v1/identity") ||
		! status=$(curl --fail --silent --show-error --max-time 5 "$api_base/api/v1/mobile/status"); then
		log "warning: daemon did not answer on $api_base; hostId and listener state are unknown"
		return
	fi
	log "hostId: $(jq -r '.hostId' <<<"$identity")"
	jq -r 'if .enabled
		then "remote-host listener: \(if .loopbackOnly then "127.0.0.1" else "0.0.0.0" end):\(.port)"
		else "remote-host listener: disabled" end' <<<"$status" | while IFS= read -r line; do log "$line"; done
}

prepare_dirs
seed_remote_host
apply_account_token
configure_credentials

if [[ $dry_run == 1 ]]; then
	[[ $* == "ao daemon" ]] && log "dry run; supervised daemon: AO_SUPERVISED=1"
	log "dry run; would exec: $*"
	exit 0
fi
if [[ $* == "ao daemon" ]]; then
	# The subshell exits at once, so the helper is re-parented to tini (which
	# reaps it) instead of becoming a zombie under the daemon.
	(report_when_ready &)
fi
exec "$@"
