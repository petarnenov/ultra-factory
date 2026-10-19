#!/usr/bin/env bash
# Tests for promote-sync.sh against throwaway repositories.
# Run: scripts/promote-sync.test.sh all
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/promote-sync.sh"
tmp="$(cd "$(mktemp -d)" && pwd -P)"
trap 'rm -rf "$tmp"' EXIT

# Commits must not depend on the identity configured on this machine.
export GIT_AUTHOR_NAME=promote-sync-test GIT_AUTHOR_EMAIL=test@example.invalid
export GIT_COMMITTER_NAME=promote-sync-test GIT_COMMITTER_EMAIL=test@example.invalid

ok() { printf 'ok   %s\n' "$*"; }
fail() {
	printf 'FAIL %s\n' "$*" >&2
	exit 1
}
assert_eq() {
	[ "$1" = "$2" ] || fail "$3: expected $1, got $2"
}

run_sync() {
	local dir="$1"
	shift
	(cd "$dir" && /bin/bash "$script" "$@")
}

expect_failure() {
	local name="$1"
	shift
	if run_sync "$@" >"$tmp/$name.out" 2>&1; then
		cat "$tmp/$name.out" >&2
		fail "$name: expected a non-zero exit"
	fi
}

# A bare origin plus a clone left on dev. origin/main carries a promotion
# merge and, unless $2 is empty, the version-bump commit on top of it.
fixture() {
	local name="$1" bump="$2"
	local root="$tmp/$name"
	mkdir -p "$root"
	origin="$root/origin.git"
	work="$root/work"
	git init --quiet --bare --initial-branch=main "$origin"
	git clone --quiet "$origin" "$work" 2>/dev/null
	printf 'v1\n' >"$work/app.txt"
	git -C "$work" add app.txt
	git -C "$work" commit --quiet -m "feat: initial"
	git -C "$work" push --quiet -u origin main
	git -C "$work" checkout --quiet -b dev
	printf 'work\n' >"$work/dev.txt"
	git -C "$work" add dev.txt
	git -C "$work" commit --quiet -m "feat(dev): work in progress"
	git -C "$work" push --quiet -u origin dev
	git -C "$work" checkout --quiet main
	git -C "$work" merge --quiet --no-ff -m "chore: promote dev into main (#1)" dev
	if [ -n "$bump" ]; then
		git -C "$work" commit --quiet --allow-empty -m "chore(version): bump to 0.0.2"
	fi
	git -C "$work" push --quiet origin main
	git -C "$work" checkout --quiet dev
}

case "${1:-all}" in
happy)
	fixture happy bump
	run_sync "$work" --timeout 0 >"$tmp/happy.out" 2>&1 || {
		cat "$tmp/happy.out" >&2
		fail happy
	}
	assert_eq "$(git -C "$origin" rev-parse refs/heads/main)" \
		"$(git -C "$work" rev-parse dev)" "dev after sync"
	assert_eq "$(git -C "$work" rev-parse dev)" \
		"$(git -C "$origin" rev-parse refs/heads/dev)" "pushed dev"
	ok happy
	;;
wait)
	# The bump lands after the sync starts: it must poll, not fail fast.
	fixture wait ""
	run_sync "$work" --timeout 60 >"$tmp/wait.out" 2>&1 &
	pid=$!
	sleep 2
	pusher="$tmp/wait/pusher"
	git clone --quiet "$origin" "$pusher"
	git -C "$pusher" commit --quiet --allow-empty -m "chore(version): bump to 0.0.2"
	git -C "$pusher" push --quiet origin main
	wait "$pid" || {
		cat "$tmp/wait.out" >&2
		fail wait
	}
	assert_eq "$(git -C "$origin" rev-parse refs/heads/main)" \
		"$(git -C "$work" rev-parse dev)" "dev after a late bump"
	ok wait
	;;
idempotent)
	fixture idempotent bump
	run_sync "$work" --timeout 0 >/dev/null 2>&1
	before="$(git -C "$work" rev-parse dev)"
	run_sync "$work" --timeout 0 >"$tmp/idempotent.out" 2>&1 || {
		cat "$tmp/idempotent.out" >&2
		fail idempotent
	}
	assert_eq "$before" "$(git -C "$work" rev-parse dev)" "second run"
	ok idempotent
	;;
no-push)
	fixture no-push bump
	remote_before="$(git -C "$origin" rev-parse refs/heads/dev)"
	run_sync "$work" --timeout 0 --no-push >/dev/null 2>&1
	assert_eq "$(git -C "$origin" rev-parse refs/heads/main)" \
		"$(git -C "$work" rev-parse dev)" "local dev after sync"
	assert_eq "$remote_before" "$(git -C "$origin" rev-parse refs/heads/dev)" \
		"remote dev stays put"
	ok no-push
	;;
no-bump)
	# A promotion merge without the version bump must not advance dev.
	fixture no-bump ""
	before="$(git -C "$work" rev-parse dev)"
	expect_failure no-bump "$work" --timeout 0
	assert_eq "$before" "$(git -C "$work" rev-parse dev)" "dev after timeout"
	ok no-bump
	;;
dirty)
	fixture dirty bump
	printf 'edit\n' >>"$work/app.txt"
	before="$(git -C "$work" rev-parse dev)"
	expect_failure dirty "$work" --timeout 0
	assert_eq "$before" "$(git -C "$work" rev-parse dev)" "dev after dirty run"
	ok dirty
	;;
wrong-branch)
	fixture wrong-branch bump
	git -C "$work" checkout --quiet -b feature
	before="$(git -C "$work" rev-parse dev)"
	expect_failure wrong-branch "$work" --timeout 0
	assert_eq "$before" "$(git -C "$work" rev-parse dev)" "dev after wrong branch"
	ok wrong-branch
	;;
all)
	for name in happy wait idempotent no-push no-bump dirty wrong-branch; do
		"$0" "$name" >"$tmp/$name.log" 2>&1 || {
			cat "$tmp/$name.log" >&2
			fail "case $name"
		}
		tail -1 "$tmp/$name.log"
	done
	;;
*)
	printf 'Usage: %s {all|happy|wait|idempotent|no-push|no-bump|dirty|wrong-branch}\n' "$0" >&2
	exit 2
	;;
esac
