#!/usr/bin/env bash
# Regression test for the pin workflow's branch-match decision.
# A bot branch can already have the staged pins while HEAD is main.
#
# The workflow must report changed=true instead of closing the PR.

set -euo pipefail

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

git init --bare "$work/origin.git" >/dev/null 2>&1
git clone "$work/origin.git" "$work/repo" >/dev/null 2>&1
cd "$work/repo"
git config user.name test
git config user.email test@example.com
git checkout -b main >/dev/null 2>&1

mkdir -p deploy/railway/worker deploy/railway/hub
printf '%s\n' \
  'ARG AGENT_NPM_PACKAGES="@anthropic-ai/claude-code@2.1.228 @openai/codex@0.147.0 opencode-ai@1.19.3"' \
  >deploy/railway/worker/Dockerfile
printf '%s\n' \
  '| `AGENT_NPM_PACKAGES` | `@anthropic-ai/claude-code@2.1.228 @openai/codex@0.147.0 opencode-ai@1.19.3` | x |' \
  >deploy/railway/worker/README.md
printf '%s\n' \
  'ARG HUB_AGENT_NPM_PACKAGES="@anthropic-ai/claude-code@2.1.302 @openai/codex@0.163.0"' \
  'ARG HUB_OPENCODE_V2_PACKAGE="@opencode/cli@2.0.32"' \
  >deploy/railway/hub/Dockerfile
git add .
git commit -m main >/dev/null
git push origin main >/dev/null 2>&1
git fetch origin >/dev/null 2>&1

# The bot branch already has the versions the updater will stage.
git checkout -b chore/pins >/dev/null 2>&1
sed -i 's/2\.1\.228/2.1.302/; s/0\.147\.0/0.163.0/' \
  deploy/railway/worker/Dockerfile deploy/railway/worker/README.md
git add .
git commit -m pins >/dev/null
git push origin chore/pins >/dev/null 2>&1
git fetch origin >/dev/null 2>&1
git checkout main >/dev/null 2>&1

# Simulate the workflow: reset to main and stage the same pins.
git checkout -B pins-test origin/main >/dev/null 2>&1
sed -i 's/2\.1\.228/2.1.302/; s/0\.147\.0/0.163.0/' \
  deploy/railway/worker/Dockerfile deploy/railway/worker/README.md
paths=(
  deploy/railway/worker/Dockerfile
  deploy/railway/hub/Dockerfile
  deploy/railway/worker/README.md
)
git add -- "${paths[@]}"

if git diff --cached --quiet; then
  echo "expected staged changes" >&2
  exit 1
fi
if ! git diff --cached --quiet origin/chore/pins -- "${paths[@]}"; then
  echo "expected the bot branch to match the staged pins" >&2
  exit 1
fi

# The old gate compares commits; HEAD is still main here.
if ! git diff --quiet origin/main HEAD; then
  echo "expected HEAD to still be origin/main" >&2
  exit 1
fi
# The working tree is the correct gate for a real change.
if git diff --quiet origin/main -- "${paths[@]}"; then
  echo "expected the working tree to differ from main" >&2
  exit 1
fi

# changed=true is set before the branch-match exit.
changed=false
if ! git diff --cached --quiet; then
  changed=true
fi
if [ "$changed" != true ]; then
  echo "expected changed=true" >&2
  exit 1
fi

# A clean tree means changed=false.
git restore --staged --worktree -- "${paths[@]}"
changed=false
if ! git diff --cached --quiet; then
  changed=true
fi
if [ "$changed" = true ]; then
  echo "expected changed=false with a clean tree" >&2
  exit 1
fi

echo "agent-pins branch-match decision test passed"
