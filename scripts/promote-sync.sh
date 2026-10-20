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
  --skip-wait        do not wait for a version bump (docs-only promotions get none)
  --allow-ahead      proceed even if the branch has commits the source lacks
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
allow_ahead=0
skip_wait=0

need_value() { [ $# -ge 2 ] || die "$1 needs a value (try --help)"; }

while [ $# -gt 0 ]; do
	case "$1" in
		--remote) need_value "$@"; remote="$2"; shift 2 ;;
		--source) need_value "$@"; source_branch="$2"; shift 2 ;;
		--branch) need_value "$@"; branch="$2"; shift 2 ;;
		--timeout) need_value "$@"; timeout="$2"; shift 2 ;;
		--no-push) push=0; shift ;;
		--skip-wait) skip_wait=1; shift ;;
		--allow-ahead) allow_ahead=1; shift ;;
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

git rev-parse --verify --quiet "$remote/$source_branch^{commit}" >/dev/null ||
	die "$remote/$source_branch not found; check --remote and --source"

# Any version bump at the head of the source branch would satisfy the wait
# below, including the previous release's. If the branch holds commits the
# source lacks, the promotion has not landed, so refuse instead of syncing early.
if [ "$allow_ahead" != "1" ] && ! git merge-base --is-ancestor HEAD "$remote/$source_branch"; then
	die "$branch has commits that $remote/$source_branch lacks; merge the promotion pull request ($branch -> $source_branch) first, or pass --allow-ahead"
fi

# The version-bump workflow commits to the promoted branch when the batch
# touches release inputs, so that commit signals that the promotion has landed.
deadline=$(($(date +%s) + timeout))
[ "$skip_wait" = "1" ] || log "waiting up to ${timeout}s for the version bump on $remote/$source_branch"
while [ "$skip_wait" != "1" ]; do
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
[ "$skip_wait" = "1" ] || log "version bump: $subject"

if git merge-base --is-ancestor "$remote/$source_branch" HEAD; then
	log "$branch already contains $remote/$source_branch"
else
	log "merging $remote/$source_branch into $branch"
	git merge --no-edit "$remote/$source_branch" ||
		die "merge failed (conflicts or local files in the way); resolve, commit, and push $branch"
fi

if [ "$push" = "1" ]; then
	if git rev-parse --verify --quiet "$remote/$branch^{commit}" >/dev/null &&
		! git merge-base --is-ancestor "$remote/$branch" HEAD; then
		die "local $branch is behind or diverged from $remote/$branch; pull it, then re-run"
	fi
	log "pushing $branch"
	git push "$remote" "$branch"
else
	log "skipping push (--no-push)"
fi

log "$branch is at $(git rev-parse --short HEAD)"
log "watch the release jobs with: gh run list --branch $source_branch --limit 12"
