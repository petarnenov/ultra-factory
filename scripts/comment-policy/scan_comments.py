#!/usr/bin/env python3
"""Extract comment ranges for hash-comment languages.

Python mode uses the standard-library tokenizer so comment-like text inside
strings, triple-quoted strings, and docstrings is not mistaken for comments.
Shell and Dockerfile modes use a small quote- and heredoc-aware lexer.
"""

import argparse
import io
import json
import re
import sys
import tokenize


def is_exempt(text):
    stripped = text.strip()
    if stripped.startswith("#"):
        stripped = stripped[1:].lstrip()
    lowered = stripped.lower()
    if stripped.startswith("!"):
        return True
    directives = (
        "type:", "pyright:", "mypy:", "noqa", "pylint:", "flake8:", "ruff:",
        "fmt:", "isort:", "shellcheck", "hadolint", "coding=", "coding:",
    )
    return any(lowered.startswith(item) for item in directives)


def is_license_notice(text):
    for raw in text.splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            line = line.lstrip("#").strip()
        if not line:
            continue
        return line.startswith("Copyright") or line.startswith("SPDX-License-Identifier:")
    return False


def group_entries(entries, source_lines):
    """Group consecutive comment lines and drop exempt/licensed groups."""
    groups = []
    current = []
    previous_line = None

    def flush():
        nonlocal current
        if not current:
            return
        text = "\n".join(item[3] for item in current)
        if is_license_notice(text):
            current = []
            return
        start = current[0][0]
        end = current[-1][0]
        long_lines = sum(
            1 for item in current if len(source_lines[item[0] - 1]) > 80
        )
        groups.append(
            {"start": start, "end": end, "lines": end - start + 1, "long": long_lines}
        )
        current = []

    for line_number, kind, source_line, comment_text in entries:
        if is_exempt(comment_text):
            flush()
            previous_line = None
            continue
        if current and line_number == previous_line + 1:
            current.append((line_number, kind, source_line, comment_text))
        else:
            flush()
            current.append((line_number, kind, source_line, comment_text))
        previous_line = line_number
    flush()
    return groups


def scan_python(source):
    entries = []
    reader = io.StringIO(source).readline
    tokens = []
    try:
        for token in tokenize.generate_tokens(reader):
            tokens.append(token)
    except tokenize.TokenError:
        pass

    for token in tokens:
        if token.type == tokenize.COMMENT:
            entries.append((token.start[0], "full", token.line, token.string.strip()))
    source_lines = source.splitlines()
    # Keep enough entries for short files without a trailing newline.
    if not source_lines:
        source_lines = [""]
    return group_entries(entries, source_lines)


HEREDOC_RE = re.compile(
    r'<<-?\s*(?:"([^"]+)"|\'([^\']+)\'|([A-Za-z_][A-Za-z0-9_]*))'
)


def find_hash_comment(line):
    quote = None
    index = 0
    length = len(line)
    while index < length:
        char = line[index]
        if quote == "'":
            if char == "'":
                quote = None
        elif quote == '"':
            if char == "\\":
                index += 1
            elif char == '"':
                quote = None
        else:
            if char == "'":
                quote = "'"
            elif char == '"':
                quote = '"'
            elif char == "\\" and index + 1 < length:
                index += 1
            elif char == "#" and (
                index == 0
                or line[index - 1].isspace()
                or line[index - 1] in ";&|(){}"
            ):
                return line[:index], True
        index += 1
    return line, False


def scan_hash(source, language):
    lines = source.splitlines()
    entries = []
    heredoc = None
    for index, raw in enumerate(lines, 1):
        if heredoc is not None:
            if raw.strip() == heredoc:
                heredoc = None
            continue
        code, is_comment = find_hash_comment(raw)
        marker_match = HEREDOC_RE.search(code)
        if marker_match:
            heredoc = next(value for value in marker_match.groups() if value)
        if raw.lstrip().startswith("#"):
            entries.append((index, "full", raw, raw.strip()))
        elif is_comment and code.strip():
            comment_start = raw.find("#", len(code))
            comment_text = raw[comment_start:].strip() if comment_start >= 0 else raw.strip()
            entries.append((index, "inline", raw, comment_text))
    source_lines = lines if lines else [""]
    return group_entries(entries, source_lines)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("language", choices=("python", "shell", "dockerfile"))
    parser.add_argument("--stdin", action="store_true")
    parser.add_argument("path", nargs="?")
    args = parser.parse_args()

    if args.stdin:
        source = sys.stdin.read()
    else:
        if not args.path:
            parser.error("path is required unless --stdin is used")
        with open(args.path, "r", encoding="utf-8", errors="replace") as handle:
            source = handle.read()

    if args.language == "python":
        result = scan_python(source)
    else:
        result = scan_hash(source, args.language)
    json.dump(result, sys.stdout, separators=(",", ":"))
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
