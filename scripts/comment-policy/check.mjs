import { execFileSync } from 'node:child_process';
import { createRequire } from 'node:module';
import { readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const requireFrontend = createRequire(new URL('../../frontend/package.json', import.meta.url));
const ts = requireFrontend('typescript');
const baselinePath = 'scripts/comment-policy/baseline.json';
const isMain = process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href;
const extensions = new Set(['.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs']);

function excludedPath(file) {
  return /(^|\/)(vendor|node_modules|gen|dist|out)\//.test(file) || /\.generated\./.test(file);
}

function lineAt(text, offset) {
  let line = 1;
  for (let i = 0; i < offset; i++) if (text.charCodeAt(i) === 10) line++;
  return line;
}

function countComment(text, start, end) {
  const startLine = lineAt(text, start);
  const endLine = lineAt(text, Math.max(start, end - 1));
  const lines = endLine - startLine + 1;
  let long = 0;
  const physical = text.split(/\r?\n/);
  for (let n = startLine; n <= endLine; n++) {
    if ([...physical[n - 1]].length > 80) long++;
  }
  return { start: startLine, end: endLine, lines, long };
}

export function isExemptComment(comment) {
  const trimmed = comment.trimStart();
  return /^\/\/(?:go:|\+build\b| Copyright\b| SPDX-License-Identifier:| @ts-(?:check|nocheck|ignore|expect-error)\b| eslint-|nolint:|lint:ignore\b| prettier-ignore\b| cspell:| istanbul )/.test(trimmed)
    || /^\/\*.*(?:SPDX-License-Identifier:|Copyright\b)/s.test(trimmed)
    || /^\/\*\s*(?:eslint-|prettier-ignore\b)/.test(trimmed);
}

export function scanTS(source, file = 'source.ts') {
  const scriptKind = file.endsWith('.tsx') || file.endsWith('.jsx')
    ? ts.ScriptKind.TSX : file.endsWith('.js') || file.endsWith('.jsx')
      || file.endsWith('.mjs') || file.endsWith('.cjs') ? ts.ScriptKind.JS : ts.ScriptKind.TS;
  const tree = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, scriptKind);
  const ranges = new Map();
  const collect = (items) => {
    for (const range of items ?? []) ranges.set(`${range.pos}:${range.end}`, range);
  };
  const visit = (node) => {
    collect(ts.getLeadingCommentRanges(source, node.getFullStart()));
    collect(ts.getTrailingCommentRanges(source, node.getEnd()));
    ts.forEachChild(node, visit);
  };
  visit(tree);
  const tokens = [...ranges.values()].sort((a, b) => a.pos - b.pos).map((range) => ({
    start: range.pos, end: range.end, text: source.slice(range.pos, range.end),
    line: lineAt(source, range.pos),
  }));
  const comments = [];
  for (const token of tokens) {
    if (isExemptComment(token.text)) continue;
    const previous = comments.at(-1);
    const gap = previous && source.slice(previous.endOffset, token.start);
    if (previous && /^[ \t]*(?:\r?\n[ \t]*)?$/.test(gap)) {
      previous.endOffset = token.end;
      previous.end = lineAt(source, token.end - 1);
      previous.text += `${gap}${token.text}`;
      const combined = countComment(source, previous.startOffset, token.end);
      previous.lines = combined.lines;
      previous.long = Math.max(previous.long, combined.long);
      continue;
    }
    comments.push({ ...countComment(source, token.start, token.end), text: token.text,
      startOffset: token.start, endOffset: token.end });
  }
  return comments;
}

export function summary(comments) {
  return comments.reduce((sum, comment) => ({
    blocks: sum.blocks + (comment.lines > 2 ? 1 : 0),
    longLines: sum.longLines + comment.long,
  }), { blocks: 0, longLines: 0 });
}

function isGenerated(file) {
  const source = readFileSync(file, 'utf8');
  return /Code generated .*DO NOT EDIT|automatically generated/i.test(source.slice(0, 500));
}

function scanFile(file) {
  return scanTS(readFileSync(file, 'utf8'), file);
}

function changedLines(base) {
  const diff = execFileSync('git', ['diff', '--unified=0', base, '--'], { encoding: 'utf8' });
  const changes = new Map();
  let file;
  for (const line of diff.split('\n')) {
    if (line.startsWith('+++ b/')) file = line.slice(6);
    if (line.startsWith('@@ ')) {
      const match = line.match(/-(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))?/);
      if (!file || !match) continue;
      const oldStart = Number(match[1]);
      const oldCount = match[2] === undefined ? 1 : Number(match[2]);
      const newStart = Number(match[3]);
      const newCount = match[4] === undefined ? 1 : Number(match[4]);
      const entry = changes.get(file) ?? { added: [], deletions: [] };
      if (newCount > 0) entry.added.push([newStart, newStart + newCount - 1]);
      if (oldCount > newCount) entry.deletions.push({ oldStart, oldEnd: oldStart + oldCount - 1, newStart, oldCount, newCount });
      changes.set(file, entry);
    }
  }
  return changes;
}

function mapLine(line, hunk) {
  const delta = hunk.newCount - hunk.oldCount;
  if (line < hunk.oldStart) return line;
  if (line > hunk.oldEnd) return line + delta;
  return Math.max(1, hunk.newStart);
}

if (isMain) {
  const tracked = execFileSync('git', ['ls-files', '-z'], { encoding: 'utf8' }).split('\0').filter(Boolean);
  const sourceFiles = tracked.filter((file) => extensions.has(path.extname(file)) && !excludedPath(file));
  const goComments = JSON.parse(execFileSync('go', ['run', '-C', 'backend', './cmd/commentpolicy', '..'], { encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 }));
  const tsComments = new Map(sourceFiles.filter((file) => !isGenerated(file))
    .map((file) => [file, scanFile(file)]));
  const inventories = {};
  for (const [file, comments] of Object.entries(goComments)) {
    const counts = summary(comments);
    if (counts.blocks || counts.longLines) inventories[file] = counts;
  }
  for (const [file, comments] of tsComments) {
    const counts = summary(comments);
    if (counts.blocks || counts.longLines) inventories[file] = counts;
  }

  if (process.argv.includes('--write-baseline')) {
    writeFileSync(baselinePath, `${JSON.stringify({ version: 1, files: inventories }, null, 2)}\n`);
    process.exit(0);
  }

  const diagnostics = new Map();
  const add = (file, message) => diagnostics.set(file, [...(diagnostics.get(file) ?? []), message]);
  const base = process.env.COMMENT_POLICY_BASE || 'origin/main';
  const changed = changedLines(base);
  for (const [file, ranges] of changed) {
    const comments = file.endsWith('.go') ? goComments[file] ?? [] : tsComments.get(file) ?? [];
    const forced = [];
    if (ranges.deletions.length) {
      const oldSource = execFileSync('git', ['show', `${base}:${file}`], { encoding: 'utf8' });
      const oldComments = file.endsWith('.go')
        ? JSON.parse(execFileSync('go', ['run', '-C', 'backend', './cmd/commentpolicy', '--stdin'], { input: oldSource, encoding: 'utf8' }))
        : scanTS(oldSource, file);
      for (const hunk of ranges.deletions) {
        for (const oldComment of oldComments) {
          if (oldComment.start > hunk.oldEnd || oldComment.end < hunk.oldStart) continue;
          forced.push([mapLine(oldComment.start, hunk), mapLine(oldComment.end, hunk)]);
        }
      }
    }
    for (const comment of comments) {
      const touched = ranges.added.some(([start, end]) => comment.start <= end && comment.end >= start)
        || forced.some(([start, end]) => comment.start <= end && comment.end >= start);
      if (touched && (comment.lines > 2 || comment.long > 0)) {
        add(file, `${file}:${comment.start}: comment exceeds the two-line/80-column limit`);
      }
    }
  }

  const baseline = JSON.parse(readFileSync(baselinePath, 'utf8'));
  for (const [file, current] of Object.entries(inventories)) {
    const previous = baseline.files[file] ?? { blocks: 0, longLines: 0 };
    if (current.blocks > previous.blocks || current.longLines > previous.longLines) {
      add(file, `${file}: existing comment debt grew (blocks ${previous.blocks} → ${current.blocks}; long lines ${previous.longLines} → ${current.longLines})`);
    }
  }
  if (diagnostics.size) {
    for (const messages of diagnostics.values()) for (const message of messages) console.error(message);
    process.exitCode = 1;
  }
}
