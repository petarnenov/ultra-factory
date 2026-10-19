#!/usr/bin/env bash
# Bring the integration branch up to date with the release branch right after
# a promotion. Waits for the version bump, merges it, and pushes. See AGENTS.md.
set -euo pipefail

usage() {
	cat <<'TEXT'
Usage: promote-sync.sh [options]

Run this right after a dev -> main promotion merges. It waits for the
version-bump commit on origin/main, merges it into dev, and pushes dev.

Options:
  --remote NAME      git remote to use (default: origin)
  --source BRANCH    promoted branch to read (default: main)
  --branch NAME      integration branch to update (default: dev)
  --timeout SECONDS  how long to wait for the version bump (default: 600)
  --no-push          merge locally without pushing
  -h, --help         show this help
TEXT
}

log() { printf '[promote-sync] %s\n' "$*"; }
die() {
	printf '[promote-sync] %s\n' "$*" >&2
	exit 1
}

remote=origin
source_branch=main
branch=dev
timeout=600
push=1

while [ $# -gt 0 ]; do
	case "$1" in
		--remote) remote="${2:-}"; shift 2 ;;
		--source) source_branch="${2:-}"; shift 2 ;;
		--branch) branch="${2:-}"; shift 2 ;;
		--timeout) timeout="${2:-}"; shift 2 ;;
		--no-push) push=0; shift ;;
		-h | --help) usage; exit 0 ;;
		*) die "unknown argument: $1 (try --help)" ;;
	esac
done

case "$timeout" in
	'' | *[!0-9]*) die "--timeout must be a non-negative integer, got \"$timeout\"" ;;
esac

git rev-parse --git-dir >/dev/null 2>&1 || die "not inside a git work tree"

current="$(git rev-parse --abbrev-ref HEAD)"
if [ "$current" != "$branch" ]; then
	die "on \"$current\"; run this from the checkout that holds \"$branch\""
fi

if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
	die "working tree has uncommitted changes; commit or stash them first"
fi

log "fetching $remote"
git fetch --quiet --prune "$remote"

# The version-bump workflow commits to the promoted branch on every push, so
# that commit is the signal that the promotion has actually landed.
deadline=$(($(date +%s) + timeout))
log "waiting up to ${timeout}s for the version bump on $remote/$source_branch"
while :; do
	subject="$(git log -1 --format=%s "$remote/$source_branch")"
	case "$subject" in
		"chore(version): bump to "*) break ;;
	esac
	if [ "$(date +%s)" -ge "$deadline" ]; then
		msg="no version bump on $remote/$source_branch after ${timeout}s"
		die "$msg (head is \"$subject\"; is the promotion merged?)"
	fi
	sleep 5
	git fetch --quiet --prune "$remote"
done
log "version bump: $subject"

if git merge-base --is-ancestor "$remote/$source_branch" HEAD; then
	log "$branch already contains $remote/$source_branch"
else
	log "merging $remote/$source_branch into $branch"
	git merge --no-edit "$remote/$source_branch" ||
		die "merge failed; resolve the conflicts, commit, and push $branch"
fi

if [ "$push" = "1" ]; then
	log "pushing $branch"
	git push "$remote" "$branch"
else
	log "skipping push (--no-push)"
fi

log "$branch is at $(git rev-parse --short HEAD)"
log "watch the release jobs with: gh run list --branch $source_branch --limit 12"
