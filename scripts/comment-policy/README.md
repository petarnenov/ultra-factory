# Code comment policy check

Handwritten code comments should use at most two physical lines and 80 characters per line, counting indentation and comment delimiters. Consecutive adjacent `//` lines are one comment. Markdown prose is not code-comment text.

`npm run comment:check` checks comments changed from `origin/main` and compares the current violation counts with `baseline.json`. It uses `go/parser` for Go and the TypeScript compiler scanner already in `frontend` for TypeScript, TSX, JavaScript, JSX, MJS, and CJS. It adds no parser dependency. The baseline records the initial per-file count of overlong blocks and lines; the baseline gate prevents those counts from increasing, while changed comments must satisfy the full rule.

The inventory excludes generated and vendored sources, `gen`, `dist`, and `out` directories, and semantic directives and license notices. The narrowly preserved directives include Go build/compiler directives, TypeScript checking directives, and exact formatter/linter directives such as `//nolint`. Copyright and SPDX notices remain intact. These are exclusions from enforcement, not a general linter suppression mechanism.

This first enforcement stage covers Go and TypeScript-family comments. The repository also contains SQL, shell, Python, Swift, Java, Terraform, CSS, HTML, Dockerfile, YAML, and other source formats. They remain under the written policy but are outside this parser-backed baseline and changed-comment check. Existing violations are inventoried debt, not remediated comments; this PR does not complete issue #193.

The second enforcement stage also covers Python, shell, and Dockerfile comments through `scripts/comment-policy/scan_comments.py`. Python uses `tokenize` from the Python standard library; shell and Dockerfile use a quote- and heredoc-aware lexer in the same helper. No third-party parser dependency is added. SQL, YAML, CSS, HTML, XML, Terraform, Java, Swift, C, and other formats remain outside automated enforcement.
