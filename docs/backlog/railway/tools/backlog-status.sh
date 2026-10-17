#!/usr/bin/env bash
set -euo pipefail

REPO="${RW_STATUS_REPO:-petarnenov/ultra-factory}"
BACKLOG="${RW_STATUS_BACKLOG:-$(cd "$(dirname "$0")/../backlog" && pwd)}"
MODE="live"
FIXTURE=""
DRY_RUN=0

usage() {
	cat <<'EOF'
Usage: backlog-status.sh [--fixture FILE] [--dry-run]

Reads GitHub issues and pull requests, then updates backlog status lines and
the marked Status section in README.md. --fixture reads an offline JSON file;
--dry-run prints a unified diff without changing files.
EOF
}

while (($#)); do
	case "$1" in
		--fixture) MODE="fixture"; FIXTURE="${2:?missing fixture path}"; shift 2 ;;
		--dry-run|--print) DRY_RUN=1; shift ;;
		-h|--help) usage; exit 0 ;;
		*) printf 'Unknown argument: %s\n' "$1" >&2; usage >&2; exit 2 ;;
	esac
done

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
if [[ "$MODE" == fixture ]]; then
	jq -e '.issues | type == "array"' "$FIXTURE" >/dev/null
	jq -e '.pulls | type == "array"' "$FIXTURE" >/dev/null
	cp "$FIXTURE" "$tmp/data.json"
else
  gh issue list --repo "$REPO" --label rw --state all --limit 200 \
		--json number,title,state,labels,url >"$tmp/issues.json"
  gh api graphql --paginate \
		-f query='query($endCursor:String){repository(owner:"petarnenov",name:"ultra-factory"){pullRequests(first:100,states:[OPEN,CLOSED,MERGED],after:$endCursor){nodes{number title state isDraft mergedAt headRefName body url closingIssuesReferences(first:100){nodes{number}}} pageInfo{hasNextPage endCursor}}}}' \
		--jq '.data.repository.pullRequests.nodes' | jq -s 'add' >"$tmp/pulls.json"
  jq -n --slurpfile issues "$tmp/issues.json" --slurpfile pulls "$tmp/pulls.json" \
		'{issues:$issues[0],pulls:$pulls[0]}' >"$tmp/data.json"
fi

DATA_FILE="$tmp/data.json" BACKLOG_DIR="$BACKLOG" README_FILE="$BACKLOG/README.md" \
DRY_RUN="$DRY_RUN" python3 - <<'PY'
import difflib
import json
import os
import pathlib
import re
import subprocess
import tempfile

data = json.loads(pathlib.Path(os.environ["DATA_FILE"]).read_text())
backlog = pathlib.Path(os.environ["BACKLOG_DIR"])
readme = pathlib.Path(os.environ["README_FILE"])
issues = {}
for issue in data["issues"]:
    match = re.search(r"\[(RW-\d+(?:\.\d+){0,2})\]", issue.get("title", ""))
    if match:
        issues[match.group(1)] = issue

pulls = data["pulls"]
close_re = re.compile(r"(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\s+#(\d+)")

def refs(pr):
    result = {int(n) for n in close_re.findall(pr.get("body") or "")}
    closing = pr.get("closingIssuesReferences", [])
    if isinstance(closing, dict):
        closing = closing.get("nodes", [])
    result.update(int(i["number"]) for i in closing if isinstance(i, dict) and i.get("number"))
    return result

def pull_for(item_id, issue):
    number = int(issue["number"]) if issue else None
    candidates = []
    for pr in pulls:
        branch_match = pr.get("headRefName", "") == "rw/" + item_id
        if branch_match or (number is not None and number in refs(pr)):
            candidates.append(pr)
    # Prefer a merged PR, then an open PR, then the newest numbered candidate.
    return max(candidates, key=lambda pr: (bool(pr.get("mergedAt")), pr.get("state") == "OPEN", int(pr["number"]))) if candidates else None

def issue_status(issue, pr):
    labels = {label["name"] if isinstance(label, dict) else label for label in (issue or {}).get("labels", [])}
    closed = (issue or {}).get("state", "OPEN").upper() == "CLOSED"
    if closed and pr and pr.get("mergedAt"):
        return "done", False
    mismatch = closed and not (pr and pr.get("mergedAt"))
    if "needs-human" in labels:
        return "needs-human", mismatch
    if "status:blocked" in labels:
        return "blocked", mismatch
    if "status:in-review" in labels or (pr and pr.get("state") == "OPEN"):
        return "in review", mismatch
    if "status:in-progress" in labels:
        return "in progress", mismatch
    return "todo", mismatch

icons = {"done":"✅", "in review":"👀", "in progress":"🔄", "blocked":"⛔", "needs-human":"⛔", "todo":"⬜"}
files = {}
items = {}
for path in sorted(backlog.rglob("*.md")):
    if path.name not in ("EPIC.md", "STORY.md") and not re.fullmatch(r"RW-\d+(?:\.\d+){2}.*\.md", path.name):
        continue
    text = path.read_text()
    h1 = re.search(r"^#\s+(RW-\d+(?:\.\d+){0,2})\b.*$", text, re.M)
    if not h1:
        continue
    item_id = h1.group(1)
    issue = issues.get(item_id)
    pr = pull_for(item_id, issue)
    state, mismatch = issue_status(issue, pr)
    items[item_id] = {"id":item_id, "path":path, "name":h1.group(0).split("·")[-1].strip(), "issue":issue, "pr":pr, "state":state, "mismatch":mismatch}

def rank(state):
    return {"needs-human":5,"blocked":5,"in review":4,"in progress":3,"todo":2,"done":1}.get(state, 0)

def rollup(children):
    if children and all(c["state"] == "done" for c in children):
        return "done"
    return max((c["state"] for c in children), key=rank, default="todo")

# Parent state is the roll-up of child items, independent of parent issue closure.
for item_id, item in items.items():
    if item_id.count(".") != 1:
        continue
    children = [x for k,x in items.items() if k.startswith(item_id + ".") and k.count(".") == 2]
    item["state"] = rollup(children)
    item["mismatch"] = any(c["mismatch"] for c in children)
for item_id, item in items.items():
    if item_id.count(".") != 0:
        continue
    children = [x for k,x in items.items() if k.startswith(item_id + ".") and k.count(".") == 1]
    item["state"] = rollup(children)
    item["mismatch"] = any(c["mismatch"] for c in children)

def pr_text(pr):
    return f"[#{pr['number']}]({pr.get('url') or 'https://github.com/petarnenov/ultra-factory/pull/' + str(pr['number'])})" if pr else "—"

def issue_text(issue):
    return f"[#{issue['number']}]({issue.get('url') or 'https://github.com/petarnenov/ultra-factory/issues/' + str(issue['number'])})" if issue else "—"

rows = ["## Status", "", "Status rules: ✅ done requires a closed issue and a merged linked PR; 👀 in review; 🔄 in progress; ⛔ blocked or needs-human; ⬜ todo. ⚠️ marks a closed issue without a merged linked PR; it remains open-status, never done. A merged PR does not close an open issue for status purposes (including docs-only work). A merged umbrella PR can satisfy child tasks when its closing references name them. PRs are linked by closing issue references, closing keywords in the body, or an exact `rw/<task-id>` head branch.", "", "| Task | Issue | Status | PR |", "|---|---:|---|---:|"]
for epic_id in sorted((k for k in items if k.count(".") == 0), key=lambda x: tuple(map(int,x.split("-RW")[-1].split("."))) if False else tuple(map(int,re.findall(r"\d+",x)))):
    epic = items[epic_id]
    for story_id in sorted((k for k in items if k.startswith(epic_id + ".") and k.count(".") == 1), key=lambda x: tuple(map(int,re.findall(r"\d+",x)))):
        story = items[story_id]
        for task_id in sorted((k for k in items if k.startswith(story_id + ".") and k.count(".") == 2), key=lambda x: tuple(map(int,re.findall(r"\d+",x)))):
            task = items[task_id]
            warning = " ⚠️" if task["mismatch"] else ""
            rows.append(f"| {task_id} {task['name']} | {issue_text(task['issue'])} | {icons[task['state']]} {task['state']}{warning} | {pr_text(task['pr'])} |")
        rows.append(f"| **{story_id} {story['name']}** | {issue_text(story['issue'])} | **{icons[story['state']]} {story['state']}** | {pr_text(story['pr'])} |")
    rows.append(f"| **{epic_id} {epic['name']}** | {issue_text(epic['issue'])} | **{icons[epic['state']]} {epic['state']}** | {pr_text(epic['pr'])} |")
rows.extend(["", "<!-- status:begin -->", "", "<!-- status:end -->"])
section = "\n".join(rows)
table_content = "\n".join(rows[:rows.index("<!-- status:begin -->")]).rstrip()
old_readme = readme.read_text()
markers = re.compile(r"<!-- status:begin -->.*?<!-- status:end -->", re.S)
if markers.search(old_readme):
    new_readme = markers.sub("<!-- status:begin -->\n" + table_content + "\n\n<!-- status:end -->", old_readme, count=1)
else:
    new_readme = old_readme.rstrip() + "\n\n<!-- status:begin -->\n" + table_content + "\n\n<!-- status:end -->\n"

changed = {readme: new_readme}
for item in items.values():
    icon_state = f"{icons[item['state']]} {item['state']}"
    if item["pr"]:
        icon_state += f" (#{item['pr']['number']})"
    status_line = f"**Status:** {icon_state}"
    content = item["path"].read_text()
    content = re.sub(r"^\*\*Status:\*\*.*\n", "", content, count=1, flags=re.M)
    content = re.sub(r"^(# .*\n)(?:\n)*", lambda match: match.group(1) + status_line + "\n\n", content, count=1, flags=re.M)
    changed[item["path"]] = content

mismatches = [x for x in items.values() if x["mismatch"] and x["id"].count(".") == 2]
if mismatches:
    print("Status mismatch: closed issues without a merged linked PR: " + ", ".join(f"{x['id']} (#{x['issue']['number']})" for x in mismatches))
else:
    print("Status mismatch: none")

for path, content in changed.items():
    original = path.read_text()
    if original == content:
        continue
    if os.environ["DRY_RUN"] == "1":
        diff = difflib.unified_diff(original.splitlines(True), content.splitlines(True), fromfile=str(path), tofile=str(path))
        print("".join(diff), end="")
    else:
        path.write_text(content)
PY
