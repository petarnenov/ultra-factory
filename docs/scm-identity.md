# Project SCM identity and machine-local secrets

AO can bind one GitHub account to a project, so every worker, orchestrator and
reviewer session for that project pushes and reads GitHub as that account. The
binding is **per machine**, exactly like project settings: each daemon owns its
project config, and `aggregateProjects` links boards without syncing settings.
The project stores only the login. The token lives on the machine that runs the
session.

## Bind an account on one machine

At add time on that machine:

```bash
ao project add --path /path/to/repo --github-account work-account
```

When configuring an existing project:

```bash
ao project set-config my-project --github-account work-account
# on a paired AO machine (this --host is the AO host selector):
ao project set-config my-project --github-account work-account --host z600
```

`set-config` replaces the whole project config. To keep other fields, read
`ao project get my-project --json`, add `scm.github.account`, and submit the
whole object with `--config-json`. An empty `--github-account ""` clears the
binding.

Setting the binding on one machine never changes another machine's project
config, and a remote spawn request carries only the project id, never the
binding or a token.

The resulting config is non-secret:

```json
{ "scm": { "github": { "account": "work-account" } } }
```

Only one GitHub account per project is supported in v1. The host is derived from
the project's repository origin (`github.com`, `geowealth.ghe.com`, another
GitHub Enterprise host).

## Store the token on each machine

Run this on every machine that executes the project's sessions. The token is
read from stdin, never from an argument:

```bash
gh auth token --user work-account | ao secret set github work-account
# enterprise GitHub host (--github-host selects the GitHub host, not a machine):
gh auth token --hostname geowealth.ghe.com | ao secret set github work-account --github-host geowealth.ghe.com
```

List the accounts stored on this machine (hosts and logins only, never values):

```bash
ao secret ls
# github/github.com/hobby
# github/geowealth.ghe.com/work-account
```

Remove one:

```bash
ao secret rm github work-account
ao secret rm github work-account --github-host geowealth.ghe.com
```

The current backend stores each token at
`$AO_DATA_DIR/secrets/github/<host>/<login>` with directory mode `0700` and file
mode `0600`. It refuses to read a token file whose mode is broader than `0600`
on platforms with POSIX mode bits. The store is machine-local: a token does not
travel with a project, a remote-spawn request, or a Railway deploy. The same
login on two hosts has two separate tokens.

## How AO resolves the identity

For a project with a bound account:

1. AO reads `scm.github.account` from the project config and derives the
   GitHub host from the repository origin.
2. AO reads that host/login token from the local secret store, on the machine
   that will run the session. For `ao spawn --host <machine>`, that is the
   target machine; the request carries only the project id.
3. AO injects the host-specific variables into the session environment:
   - `github.com`: `GH_TOKEN`
   - any other GitHub host: `GH_HOST` plus `GH_ENTERPRISE_TOKEN` and
     `GITHUB_ENTERPRISE_TOKEN`; `GH_TOKEN` is not set for that session
4. Daemon-side SCM reads and actions for that project use the same host/account
   token rather than the machine-wide token.

The machine's own `gh` active account is only a fallback for projects without a
binding. Two concurrent projects bound to different accounts each see their own
identity; AO never relies on a single `GH_TOKEN` in the daemon environment.

If the token is absent, the spawn fails before creating a session or workspace:

```
project "my-project" is bound to GitHub account "work-account" on host
"github.com", but no token is stored on machine "worker-1"; run
"ao secret set github work-account" on that machine
```

For an enterprise host the fix names it:

```
ao secret set github work-account --github-host geowealth.ghe.com
```

The daemon returns this as `SCM_IDENTITY_UNAVAILABLE` with `projectId`, `host`,
`account`, `machine`, `fix`, and `blocked: true` details so the orchestrator can
surface it as a blocked session instead of a silent one. A work project on a
hobby-only machine, or a hobby project on a work-only machine, fails this way
by design.

## No binding: unchanged fallback

A project without `scm.github.account` keeps today's behavior. The SCM client
tries, in order:

1. `AO_GITHUB_TOKEN`
2. `GH_TOKEN`
3. `GITHUB_TOKEN`
4. `gh auth token`

Sessions receive no AO-injected GitHub token in this mode, so they use the
machine's own `gh`/git configuration exactly as before.

## Security rules

- A token is never written to project config, session records, the daemon API
  responses, the desktop UI, logs, journals, reports, telemetry, prompts,
  issues, or commits. Those surfaces carry the host and account login only.
- `ao secret set` reads the token from stdin. Do not pass a token as a command
  argument or put one in a remote URL.
- The daemon never transmits a token with `ao spawn --host`; the target machine
  resolves its own copy.
- `ao secret ls` returns hosts and logins only. The secret read path is used only
  to construct a session environment or an SCM request.
- A token in a project's `env` block is still a config secret; do not use that
  workaround. Use the project SCM account instead.
- Treat `$AO_DATA_DIR/secrets/github` as sensitive. Do not copy it between
  machines, commit it, or include it in a support bundle.
