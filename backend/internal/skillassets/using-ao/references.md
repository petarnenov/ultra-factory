# Quick Reference

Natural-language-to-command mappings for common AO tasks.

| You want to... | Command |
|---|---|
| Show me this webpage / open this page | `ao preview "<url>"` |
| Start an existing configured dev app | `ao preview start [configuration]` |
| Check or stop the worker's managed dev app | `ao preview status` / `ao preview stop` |
| Show this Markdown or HTML file without a server | `ao preview "<workspace-path>"` |
| Hand off a newly created browser-displayable artifact | `ao preview "<workspace-path>"` immediately after writing the primary artifact |
| Inspect and verify this webpage as the agent | `ao browser open "<url>"`, then `ao browser snapshot` |
| Click or fill a page element | `ao browser snapshot`, then `ao browser click <ref>` or `ao browser fill <ref> "<text>"` |
| Check frontend runtime failures | `ao browser errors` and `ao browser console` |
| Diagnose a request/API/CORS/auth/redirect failure when normal page evidence is insufficient | `ao browser network start`, reproduce once, then `ao browser network stop` |
| Check network capture without enabling it | `ao browser network status` or `ao browser network list` |
| Open the user's real Chromium debugging surface | `ao browser devtools open` |
| Close the shared DevTools window when explicitly requested | `ao browser devtools close` |
| Capture the page | `ao browser screenshot [path]` |
| Spawn a worker on issue N | `ao spawn --project <p> --issue N --name "<=100 chars>" --prompt "..."` |
| Message a running agent | `ao send --session <id> --message "..."` |
| Answer a worker's pending structured question | `ao session input <id>`, then `--request-id <request-id> --action accept --answer <field>=<value>` |
| Kill a session | `ao session kill <id>` |
| List sessions | `ao session ls` |
| Register a repo as a project | `ao project add --path <abs-path> --name <name>` |
| List projects | `ao project ls` |
| Check installed agent harnesses and readiness | `ao agent ls --refresh` |
| Save a repetitive agent task or exact command as a Cue | `ao cue list --json`, then `ao cue create --name "..." --prompt "..."` or `--command "..."` |
| Rename a worker session (not the orchestrator) | `ao session rename <id> "<name>"` |
| Restore a killed session | `ao session restore <id>` |
| Clean up terminated sessions | `ao session cleanup` |
| Make a Docker container this session starts survive AO cleanup | `docker run --label ao.session=$AO_SESSION_ID --label ao.spare=true ...` |
| See a session's details | `ao session get <id>` |
| See which live sessions use the most memory | `ao session top` (`--host <host>` to sample a paired machine) |
| See a session's owner, coordinator scope, and project root | `ao session owner <id>` |
| Assign, reassign, or clear a worker's coordinator owner | `ao session assign <worker> --owner <coordinator-id\|root>` (live root only) |
| List a project's coordinators and the workers they own | `ao coordinator ls --project <id>` |
| Send the root a status summary or escalation (coordinator only) | `ao coordinator report --state <planning\|working\|blocked\|done> [--worker <id>]... [--escalate <reason>]` |
| List linked remote workers and their local owner | `ao remote-worker ls [--project <id>]` |
| Assign or clear a linked remote worker's owner | `ao remote-worker assign <host-id> <remote-session-id> --owner <coordinator-id\|root>` |
| Open the desktop app (operator-only; refused inside AO sessions) | `ao start` |
| Check the daemon is up | `ao status` |
| Run health checks | `ao doctor` |
| Clear the preview panel | `ao preview clear` |
| List orchestrator sessions | `ao orchestrator ls` |
| Claim an existing PR for the current session | `ao session claim-pr <pr-ref>` (`AO_SESSION_ID`) |
| Claim an existing PR for another session | `ao session claim-pr <id> <pr-ref>` |
| Merge a pull request (root only; a coordinator or worker is refused with `MERGE_ROOT_ONLY`) | `ao pr merge <pr-number>` |
| Resolve pull-request review threads | `ao pr resolve-comments <pr-number> [comment-id...]` |
| Submit a code review verdict | `ao review submit <session-id> --run <run-id> --verdict approved` |
| Get an independent / adversarial review of an AO worker's implementation PR | `ao review trigger` (inside the owning worker), or `ao review trigger <worker-session-id>` |
| Review someone else's PR | Spawn or use a normal review-only worker without claiming the target PR; submit the requested provider review directly |
| Inspect, cancel, or rerun a review | `ao review ls [session-id]`, `ao review cancel [session-id]`, or `ao review trigger [session-id] --rerun` |
| Switch a running session's agent | `ao session switch-agent <id> <target-harness>` |
| Configure a project's default branch or model | `ao project set-config <id> --default-branch <branch> --model <model>` |
| Import projects from a legacy AO install | `ao import --dry-run` (preview), then `ao import -y` |
