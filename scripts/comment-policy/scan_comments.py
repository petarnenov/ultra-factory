#!/usr/bin/env python3
"""Extract comment ranges for hash-comment languages.

Python mode uses the standard-library tokenizer so comment-like text inside
strings, triple-quoted strings, and docstrings is not mistaken for comments.
Shell and Dockerfile modes use a quote- and heredoc-aware lexer.
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
        "syntax=", "escape=",
    )
    return any(lowered.startswith(item) for item in directives)


def is_license_notice(text):
    for raw in text.splitlines():
        line = raw.strip()
        if line.startswith("#"):
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

    for line_number, _kind, _source_line, comment_text in entries:
        if is_exempt(comment_text):
            flush()
            previous_line = None
            continue
        if current and line_number == previous_line + 1:
            current.append((line_number, "full", "", comment_text))
        else:
            flush()
            current.append((line_number, "full", "", comment_text))
        previous_line = line_number
    flush()
    return groups


def scan_python(source):
    entries = []
    tokens = []
    try:
        for token in tokenize.generate_tokens(io.StringIO(source).readline):
            tokens.append(token)
    except tokenize.TokenError:
        pass

    for token in tokens:
        if token.type == tokenize.COMMENT:
            entries.append((token.start[0], "full", token.line, token.string.strip()))
    source_lines = source.splitlines() or [""]
    return group_entries(entries, source_lines)


def find_heredoc_marker(text):
    """Return a heredoc terminator found in unquoted shell code, if any."""
    index = 0
    quote = None
    length = len(text)
    while index < length:
        char = text[index]
        if quote == "'":
            if char == "'":
                quote = None
            index += 1
            continue
        if quote == '"':
            if char == "\\" and index + 1 < length:
                index += 2
                continue
            if char == '"':
                quote = None
            index += 1
            continue
        if char == "'":
            quote = "'"
            index += 1
            continue
        if char == '"':
            quote = '"'
            index += 1
            continue
        if char == "\\" and index + 1 < length:
            index += 2
            continue
        if text.startswith("<<", index) and not text.startswith("<<<", index) and not (index > 0 and text[index - 1] == "<"):
            cursor = index + 2
            if cursor < length and text[cursor] == "-":
                cursor += 1
            while cursor < length and text[cursor].isspace():
                cursor += 1
            if cursor < length and text[cursor] != "<":
                for pattern in (r'"[^"]*"', r"'[^']*'", r"[A-Za-z_][A-Za-z0-9_]*"):
                    match = re.match(pattern, text[cursor:])
                    if match:
                        terminator = match.group(0).strip("\"'")
                        if not in_arithmetic(text, index):
                            return terminator
                        break
        index += 1
    return None


def in_arithmetic(text, position):
    for match in re.finditer(r"\$\(\((.*?)\)\)", text, re.S):
        if match.start() < position < match.end():
            return True
    return False


def find_comment_index(raw, quote_state):
    index = 0
    quote = quote_state
    length = len(raw)
    while index < length:
        char = raw[index]
        if quote == "'":
            if char == "'":
                quote = None
            index += 1
        elif quote == '"':
            if char == "\\" and index + 1 < length:
                index += 2
            elif char == '"':
                quote = None
                index += 1
            else:
                index += 1
        else:
            if char == "'":
                quote = "'"
                index += 1
            elif char == '"':
                quote = '"'
                index += 1
            elif char == "\\" and index + 1 < length:
                index += 2
            elif char == "#" and (
                index == 0
                or raw[index - 1].isspace()
                or raw[index - 1] in ";&|(){}"
            ) and not (
                index >= 2 and raw[index - 1] == "{" and raw[index - 2] == "$"
            ):
                return index, quote
            else:
                index += 1
    return -1, quote


def scan_hash(source, language):
    lines = source.splitlines()
    entries = []
    heredoc = None
    quote_state = None
    for index, raw in enumerate(lines, 1):
        if heredoc is not None:
            if raw.strip() == heredoc:
                heredoc = None
            continue
        if quote_state is None:
            marker = find_heredoc_marker(raw)
            if marker:
                heredoc = marker
        comment_index, quote_state = find_comment_index(raw, quote_state)
        stripped = raw.lstrip()
        starts_with_hash = stripped.startswith("#")
        if starts_with_hash and comment_index == len(raw) - len(stripped):
            entries.append((index, "full", raw, stripped))
        elif comment_index >= 0:
            entries.append((index, "inline", raw, raw[comment_index:].strip()))
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
