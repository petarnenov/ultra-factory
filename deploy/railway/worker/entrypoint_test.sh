#!/usr/bin/env bash
# Тества логиката за предварително записване на ao-worker-entrypoint.sh с
# AO_ENTRYPOINT_DRY_RUN=1: нищо не се изпълнява с exec и не стартира daemon.
# Изисква bash, git, jq и coreutils. Пускане: deploy/railway/worker/entrypoint_test.sh
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/ao-worker-entrypoint.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# Stub-овете записват всяко извикване. gh не трябва да е истинският, а ao/curl
# никога не трябва да се пускат при dry run.
mkdir -p "$tmp/bin"
for tool in ao curl gh; do
	printf '%s\n' '#!/bin/sh' "echo \"$tool \$*\" >> \"\$TEST_CALLS\"" > "$tmp/bin/$tool"
	chmod +x "$tmp/bin/$tool"
done

failures=0
fail() {
	echo "FAIL: $*" >&2
	failures=$((failures + 1))
}

# run <case> [VAR=value ...] [-- command...]: пуска entrypoint-а в чиста среда
# със собствени HOME и AO_DATA_DIR. Задава $data, $home, $config, $out (общия
# изход) и $status.
run() {
	local case=$1
	shift
	local -a vars=() args=()
	while (($#)) && [[ $1 != -- ]]; do
		vars+=("$1")
		shift
	done
	if (($#)); then
		shift
		args=("$@")
	fi
	data="$tmp/$case/data/ao"
	home="$tmp/$case/data/home"
	config="$data/mobile/config.json"
	export TEST_CALLS="$tmp/$case/calls"
	status=0
	out=$(env -i PATH="$tmp/bin:$PATH" HOME="$home" AO_DATA_DIR="$data" \
		AO_ENTRYPOINT_DRY_RUN=1 GIT_CONFIG_NOSYSTEM=1 TEST_CALLS="$TEST_CALLS" \
		"${vars[@]}" bash "$script" "${args[@]}" 2>&1) || status=$?
}

mode() { stat -c %a "$1"; }
field() { jq -r "$1" "$config"; }
calls() { cat "$TEST_CALLS" 2>/dev/null || true; }

# Парола от средата, записана веднъж с изричен порт. Кавичките и обратните
# наклонени черти трябва да оцелеят при JSON кодирането.
password='pa"ss\word'
run env-password AO_REMOTE_PASSWORD="$password" AO_REMOTE_PORT=4011
[[ $status == 0 ]] || fail "env-password: exit $status: $out"
[[ $(mode "$data") == 700 && $(mode "$home") == 700 ]] || fail "env-password: data/home are not 0700"
[[ $(mode "$config") == 600 ]] || fail "env-password: config.json mode is $(mode "$config"), want 600"
[[ $(mode "${config%/*}") == 700 ]] || fail "env-password: mobile dir is not 0700"
[[ $(field .password) == "$password" ]] || fail "env-password: password is $(field .password)"
[[ $(field .enabled) == true && $(field .lastPort) == 4011 && $(field .noPublicTunnel) == true ]] ||
	fail "env-password: unexpected config $(cat "$config")"
[[ $out == *"would exec: ao daemon"* ]] || fail "env-password: no dry-run exec line: $out"
[[ $out != *"$password"* ]] || fail "env-password: password from env was logged"
[[ -z $(calls) ]] || fail "env-password: dry run called $(calls)"

# Рестартът запазва записания config дори когато паролата в средата е сменена.
cp "$config" "$tmp/saved.json"
run env-password AO_REMOTE_PASSWORD=other-password
[[ $status == 0 ]] || fail "restart: exit $status: $out"
cmp -s "$config" "$tmp/saved.json" || fail "restart: config.json changed: $(cat "$config")"
[[ $out == *"differs from the saved password"* ]] || fail "restart: no warning about the ignored env password: $out"
[[ $out == *"password: ********"* ]] || fail "restart: password not masked: $out"

# Без среда: генерира парола от 8 знака и я показва само при първия старт.
run generated
[[ $status == 0 ]] || fail "generated: exit $status: $out"
generated=$(field .password)
[[ $generated =~ ^[A-Za-z0-9]{8}$ ]] || fail "generated: bad password '$generated'"
[[ $(field .lastPort) == 3011 ]] || fail "generated: default port is $(field .lastPort)"
[[ $out == *"generated password (shown only on this start): $generated"* ]] || fail "generated: password not logged: $out"
run generated
[[ $(field .password) == "$generated" ]] || fail "generated: restart changed the password"
[[ $out != *"$generated"* ]] || fail "generated: password logged again on restart"

# Акаунт токен: активира се като sha256 hex, заменя токен, издаден от
# daemon-а, и се отнема, щом променливата бъде махната.
token=$(printf '%064d' 7)
run token AO_REMOTE_PASSWORD=pw123456
jq '.accountTokenHash = "old" | .accountToken = "old-token" | .accountTokenOwner = "user-a"' "$config" > "$tmp/issued.json"
cp "$tmp/issued.json" "$config"
run token AO_REMOTE_ACCOUNT_TOKEN="$token"
[[ $status == 0 ]] || fail "token: exit $status: $out"
want=$(printf '%s' "$token" | sha256sum | cut -d' ' -f1)
[[ $(field .accountTokenHash) == "$want" ]] || fail "token: hash is $(field .accountTokenHash), want $want"
[[ $(field '.accountToken // "unset"') == unset && $(field '.accountTokenOwner // "unset"') == unset ]] ||
	fail "token: daemon-issued token fields were kept"
[[ $(field .password) == pw123456 && $(mode "$config") == 600 ]] || fail "token: password or mode changed"
[[ $out != *"$token"* ]] || fail "token: token was logged"
run token
[[ $(field '.accountTokenHash // "unset"') == unset ]] || fail "token: hash kept after the variable was unset"
[[ $out == *"account token revoked"* ]] || fail "token: no revocation log: $out"

# Токен, издаден от daemon-а (хеш с токена до него), не е наш, за да го отнемаме.
cp "$tmp/issued.json" "$config"
run token
[[ $(field .accountTokenHash) == old && $(field .accountToken) == old-token ]] || fail "token: daemon-issued token was revoked"

run short-token AO_REMOTE_ACCOUNT_TOKEN=short
[[ $out == *"shorter than 32 characters"* ]] || fail "short-token: no warning: $out"

# git идентичност и credential helper на gh от средата.
run creds AO_GIT_USER_NAME="AO Worker" AO_GIT_USER_EMAIL=worker@example.com GH_TOKEN=ghp_test ANTHROPIC_API_KEY=sk-test
[[ $status == 0 ]] || fail "creds: exit $status: $out"
[[ $(HOME="$home" GIT_CONFIG_NOSYSTEM=1 git config --global user.name) == "AO Worker" ]] || fail "creds: git user.name not set"
[[ $(HOME="$home" GIT_CONFIG_NOSYSTEM=1 git config --global user.email) == worker@example.com ]] || fail "creds: git user.email not set"
[[ $(calls) == "gh auth setup-git" ]] || fail "creds: gh calls: $(calls)"
[[ $out == *"credentials from env: GH_TOKEN ANTHROPIC_API_KEY"* ]] || fail "creds: credential summary: $out"
[[ $out != *ghp_test* && $out != *sk-test* ]] || fail "creds: a credential value was logged"
run no-creds
[[ -z $(calls) && ! -e $home/.gitconfig ]] || fail "no-creds: gh or git configured without env"
[[ $out == *"credentials from env: none"* ]] || fail "no-creds: credential summary: $out"

# Подадена команда замества daemon-а.
run command -- sh -c 'echo hi'
[[ $out == *"would exec: sh -c echo hi"* ]] || fail "command: $out"

# Съществуващ изключен config се оставя непроменен, с подсказка.
run disabled
jq '.enabled = false' "$config" > "$tmp/disabled.json" && cp "$tmp/disabled.json" "$config"
run disabled
[[ $(field .enabled) == false && $out == *"listener is disabled"* ]] || fail "disabled: $out"

for port in 0 65536 abc 123456 -1; do
	run "bad-port-$port" AO_REMOTE_PORT="$port"
	[[ $status != 0 && $out == *"AO_REMOTE_PORT must be a port number"* ]] || fail "bad-port-$port: exit $status: $out"
	[[ ! -e $config ]] || fail "bad-port-$port: config written"
done
run leading-zero AO_REMOTE_PORT=03011
[[ $(field .lastPort) == 3011 ]] || fail "leading-zero: lastPort is $(field .lastPort)"

# Volume без права за запис се проваля с подсказката за Railway (безсмислено като root).
if [[ $(id -u) != 0 ]]; then
	mkdir -p "$tmp/readonly/data"
	chmod 500 "$tmp/readonly/data"
	run readonly
	chmod 700 "$tmp/readonly/data"
	[[ $status != 0 && $out == *RAILWAY_RUN_UID=0* ]] || fail "readonly: exit $status: $out"
fi

if ((failures)); then
	echo "$failures check(s) failed" >&2
	exit 1
fi
echo "entrypoint_test.sh: ok"
