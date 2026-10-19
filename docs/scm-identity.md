# Project SCM identity and machine-local secrets

AO can bind one GitHub account to a project, so every worker session for that
project pushes and reads GitHub as that account even when the machine has a
different account active in `gh`. The project stores only the login. The token
lives on the machine that runs the session.

## Bind an account to a project

At add time:

```bash
ao project add --path /path/to/repo --github-account work-account
```

When configuring an existing project:

```bash
ao project set-config my-project --github-account work-account
```

`set-config` replaces the whole project config. To keep other fields, read
`ao project get my-project --json`, add `scm.github.account`, and submit the
whole object with `--config-json`. An empty `--github-account ""` clears the
binding.

The resulting config is non-secret and shareable:

```json
{ "scm": { "github": { "account": "work-account" } } }
```

Only one GitHub account per project is supported in v1.

## Store the token on each machine

Run this on every machine that executes the project's sessions. The token is
read from stdin, never from an argument:

```bash
gh auth token --user work-account | ao secret set github work-account
# or paste it when prompted
ao secret set github work-account
```

List the logins stored on this machine (values are never printed):

```bash
ao secret ls
```

Remove one:

```bash
ao secret rm github work-account
```

The current backend stores the token at
`$AO_DATA_DIR/secrets/github/<login>` with directory mode `0700` and file mode
`0600`. It refuses to read a token file whose mode is broader than `0600` on
platforms with POSIX mode bits. The store is machine-local: a token does not
travel with a project, a remote-spawn request, or a Railway deploy.

## How AO resolves the identity

For a project with a bound account:

1. AO reads `scm.github.account` from the project config.
2. AO reads that login's token from the local secret store, on the machine that
   will run the session. For `ao spawn --host <machine>`, that is the target
   machine; the request carries only the project id.
3. AO injects `GH_TOKEN` into the session environment. `gh`, the gh git
   credential helper, and `git push` over HTTPS use it instead of `gh`'s active
   account.
4. Daemon-side SCM reads and actions for that project use the same account
   token rather than the machine-wide token.

If the token is absent, the spawn fails before creating a session or workspace:

```
project "my-project" is bound to GitHub account "work-account", but no token
is stored on machine "worker-1"; run "ao secret set github work-account" on
that machine
```

The daemon returns this as `SCM_IDENTITY_UNAVAILABLE` with `projectId`,
`account`, `machine`, `fix`, and `blocked: true` details so the orchestrator can
surface it as a blocked session instead of a silent one.

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
  issues, or commits. Those surfaces carry the account login only.
- `ao secret set` reads the token from stdin. Do not pass a token as a command
  argument or put one in a remote URL.
- The daemon never transmits a token with `ao spawn --host`; the target machine
  resolves its own copy.
- `ao secret ls` returns logins only. The secret read path is used only to
  construct a session environment or an SCM request.
- A token in a project's `env` block is still a config secret; do not use that
  workaround. Use the project SCM account instead.
- Treat `$AO_DATA_DIR/secrets/github` as sensitive. Do not copy it between
  machines, commit it, or include it in a support bundle.
