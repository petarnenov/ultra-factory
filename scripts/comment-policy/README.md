# Code comment policy check

## Prerequisites

| Tool | Needed by | Why |
| --- | --- | --- |
| Node.js 24 | `npm run comment:check`, `npm run comment:test` | Runs `check.mjs` and `check.test.mjs`, as CI does. |
| `npm ci` in `frontend/` | Both commands | `check.mjs` loads the TypeScript compiler from `frontend/node_modules`. |
| Go (the version in `backend/go.mod`) | Both commands | `go run ./cmd/commentpolicy` scans Go comments; `comment:test` runs `go test ./cmd/commentpolicy`. |
| `python3` on `PATH` | Both commands | `scan_comments.py` scans Python, shell and Dockerfile comments. |

When `python3` is missing, the checker and its tests fail with a message naming `python3` and the affected languages (Python, shell, Dockerfile). They never skip those languages silently, because that would hide real violations.

Handwritten code comments should use at most two physical lines and 80 characters per line, counting indentation and comment delimiters. Consecutive adjacent `//` lines are one comment. Markdown prose is not code-comment text.

`npm run comment:check` checks comments changed from `COMMENT_POLICY_BASE`
and compares the current violation counts with `baseline.json`. The raw command
defaults that base to `origin/main`. CI sets it to the pull request's base
commit (`.github/workflows/comment-policy.yml`), and the `dev` flow targets
`dev`, so the raw default compares a `dev`-based branch with `main` and reports
changes outside the pull request.

Run the check the way CI runs it:

```bash
make ci-check ARGS="--only comment"
```

That resolves the pull request's base commit (or `origin/dev` when there is no
pull request yet), forces `GOTOOLCHAIN` to `backend/go.mod`'s version, and runs
the same test step as the workflow. Pass `--base <ref>` or set
`COMMENT_POLICY_BASE` to override the base deliberately.

The check uses `go/parser` for Go and the TypeScript compiler scanner already
in `frontend` for TypeScript, TSX, JavaScript, JSX, MJS, and CJS. It adds no
parser dependency. The baseline records the initial per-file count of overlong
blocks and lines; the baseline gate prevents those counts from increasing,
while changed comments must satisfy the full rule.

The inventory excludes generated and vendored sources, `gen`, `dist`, and `out` directories, and the files listed in `generatedFiles` in `check.mjs` (today `frontend/src/api/schema.ts`, which `npm run api` writes from the OpenAPI descriptions in `backend/internal/httpd/apispec`), and semantic directives and license notices. The narrowly preserved directives include Go build/compiler directives, TypeScript checking directives, and exact formatter/linter directives such as `//nolint`. Copyright and SPDX notices remain intact. These are exclusions from enforcement, not a general linter suppression mechanism.

This first enforcement stage covers Go and TypeScript-family comments. The repository also contains SQL, shell, Python, Swift, Java, Terraform, CSS, HTML, Dockerfile, YAML, and other source formats. They remain under the written policy but are outside this parser-backed baseline and changed-comment check. Existing violations are inventoried debt, not remediated comments; this PR does not complete issue #193.

The second enforcement stage also covers Python, shell, and Dockerfile comments through `scripts/comment-policy/scan_comments.py`. Python uses `tokenize` from the Python standard library; shell and Dockerfile use a quote- and heredoc-aware lexer in the same helper. No third-party parser dependency is added. SQL, YAML, CSS, HTML, XML, Terraform, Java, Swift, C, and other formats remain outside automated enforcement.
