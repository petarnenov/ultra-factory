import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import readline from 'node:readline';
import { createRequire } from 'node:module';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { pathToFileURL } from 'node:url';

const requireFrontend = createRequire(new URL('../../frontend/package.json', import.meta.url));
const ts = requireFrontend('typescript');
const baselinePath = 'scripts/comment-policy/baseline.json';
const isMain = process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href;
const extensions = new Set(['.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs']);
const tsExtensions = extensions;

function excludedPath(file) {
  return /(^|\/)(vendor|node_modules|gen|dist|out)\//.test(file) || /\.generated\./.test(file);
}

const scanCommentsPath = path.join(path.dirname(fileURLToPath(import.meta.url)), 'scan_comments.py');

function hashLanguageFor(file) {
  const base = path.basename(file).toLowerCase();
  if (base === 'dockerfile' || base.startsWith('dockerfile.')) return 'dockerfile';
  if (path.extname(file) === '.py') return 'python';
  return 'shell';
}

function isHashSource(file) {
  return path.extname(file) === '.py' || path.extname(file) === '.sh'
    || path.extname(file) === '.bash' || path.basename(file).toLowerCase() === 'dockerfile'
    || path.basename(file).toLowerCase().startsWith('dockerfile.');
}

export const python3MissingMessage = 'comment-policy: python3 was not found on PATH. '
  + 'It is required to scan Python, shell and Dockerfile comments '
  + '(scripts/comment-policy/scan_comments.py). Install Python 3 and retry; '
  + 'these languages are not skipped because that would hide violations.';

export function scanHashLanguage(source, language) {
  let output;
  try {
    output = execFileSync('python3', [scanCommentsPath, language, '--stdin'],
      { input: source, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 });
  } catch (error) {
    if (error?.code === 'ENOENT') throw new Error(python3MissingMessage, { cause: error });
    throw error;
  }
  return JSON.parse(output);
}

function scanSourceForFile(file, source) {
  if (file.endsWith('.go')) {
    return JSON.parse(execFileSync('go', ['run', '-C', 'backend', './cmd/commentpolicy', '--stdin'],
      { input: source, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 }));
  }
  if (isHashSource(file)) return scanHashLanguage(source, hashLanguageFor(file));
  return scanTS(source, file);
}

export function scanPython(source) {
  return scanHashLanguage(source, 'python');
}

export function scanShell(source) {
  return scanHashLanguage(source, 'shell');
}

export function scanDockerfile(source) {
  return scanHashLanguage(source, 'dockerfile');
}

export const scanHashComments = scanHashLanguage;
export const scanHashSource = scanHashLanguage;

function lineStarts(text) {
  const starts = [0];
  for (let i = 0; i < text.length; i++) if (text.charCodeAt(i) === 10) starts.push(i + 1);
  return starts;
}

function lineAt(starts, offset) {
  let low = 0;
  let high = starts.length;
  while (low < high) {
    const middle = (low + high) >>> 1;
    if (starts[middle] <= offset) low = middle + 1;
    else high = middle;
  }
  return low;
}

function countComment(widths, starts, start, end) {
  const startLine = lineAt(starts, start);
  const endLine = lineAt(starts, Math.max(start, end - 1));
  let long = 0;
  for (let line = startLine; line <= endLine; line++) if (widths[line - 1] > 80) long++;
  return { start: startLine, end: endLine, lines: endLine - startLine + 1, long };
}

function isLicenseNotice(comment) {
  const lines = comment.replace(/^\/\//, '').replace(/^\/\*/, '').replace(/\*\/$/, '').split(/\r?\n/);
  for (const raw of lines) {
    const line = raw.trim().replace(/^\*\s?/, '').trim();
    if (!line) continue;
    return line.startsWith('Copyright') || line.startsWith('SPDX-License-Identifier:');
  }
  return false;
}

function isDirectiveComment(comment) {
  const trimmed = comment.trimStart();
  return /^\/\/(?:go:|\+build\b| @ts-(?:check|nocheck|ignore|expect-error)\b| eslint-|nolint:|lint:ignore\b| prettier-ignore\b| cspell:| istanbul )/.test(trimmed)
    || /^\/\*\s*(?:eslint-|prettier-ignore\b)/.test(trimmed);
}

export function isExemptComment(comment) {
  return isDirectiveComment(comment) || isLicenseNotice(comment);
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
    if (node.kind === ts.SyntaxKind.JsxText) return;
    collect(ts.getLeadingCommentRanges(source, node.getFullStart()));
    collect(ts.getTrailingCommentRanges(source, node.getEnd()));
    if (node.kind === ts.SyntaxKind.JsxExpression && !node.expression) {
      const start = node.getStart(tree) + 1;
      const text = source.slice(start, node.end - 1);
      const scanner = ts.createScanner(ts.ScriptTarget.Latest, false, ts.LanguageVariant.Standard, text);
      while (scanner.scan() !== ts.SyntaxKind.EndOfFileToken) {
        const kind = scanner.getToken();
        if (kind !== ts.SyntaxKind.SingleLineCommentTrivia && kind !== ts.SyntaxKind.MultiLineCommentTrivia) continue;
        ranges.set(`${start + scanner.getTokenPos()}:${start + scanner.getTextPos()}`, {
          pos: start + scanner.getTokenPos(), end: start + scanner.getTextPos(),
        });
      }
    }
    for (const child of node.getChildren(tree)) visit(child);
  };
  visit(tree);
  const starts = lineStarts(source);
  const widths = source.split(/\r?\n/).map((line) => [...line].length);
  const tokens = [...ranges.values()].sort((a, b) => a.pos - b.pos).map((range) => ({
    start: range.pos, end: range.end, text: source.slice(range.pos, range.end),
    line: lineAt(starts, range.pos),
  }));
  const groups = [];
  for (const token of tokens) {
    const previousGroup = groups.at(-1);
    const previousToken = previousGroup?.at(-1);
    const gap = previousToken && source.slice(previousToken.end, token.start);
    if (previousGroup && /^[ \t]*(?:\r?\n[ \t]*)?$/.test(gap)) previousGroup.push(token);
    else groups.push([token]);
  }
  const comments = [];
  for (const group of groups) {
    const groupText = source.slice(group[0].start, group.at(-1).end);
    if (isLicenseNotice(groupText)) continue;
    let previous = null;
    for (const token of group) {
      if (isDirectiveComment(token.text)) {
        previous = null;
        continue;
      }
      const gap = previous && source.slice(previous.endOffset, token.start);
      if (previous && /^[ \t]*(?:\r?\n[ \t]*)?$/.test(gap)) {
        previous.endOffset = token.end;
        previous.end = lineAt(starts, token.end - 1);
        previous.text += `${gap}${token.text}`;
        const combined = countComment(widths, starts, previous.startOffset, token.end);
        previous.lines = combined.lines;
        previous.long = Math.max(previous.long, combined.long);
        continue;
      }
      previous = { ...countComment(widths, starts, token.start, token.end), text: token.text,
        startOffset: token.start, endOffset: token.end };
      comments.push(previous);
    }
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

export async function parseDiff(lines) {
  const changes = new Map();
  let file = null;
  const source = typeof lines === 'string' ? lines.split('\n') : lines;
  for await (const line of source) {
    if (line.startsWith('+++ /dev/null')) {
      file = null;
      continue;
    }
    if (line.startsWith('+++ b/')) {
      file = line.slice(6);
      continue;
    }
    if (!line.startsWith('@@ ')) continue;
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
  return changes;
}

async function changedLines(mergeBase) {
  // Stream Git's diff to a temporary file instead of one subprocess buffer.
  // Large integration diffs previously surfaced as ENOBUFS before evaluation.
  const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), 'comment-policy-'));
  try {
    const diffFile = path.join(outputDir, 'changed-lines.diff');
    let fileDescriptor;
    try {
      fileDescriptor = fs.openSync(diffFile, 'w');
      execFileSync('git', ['diff', '--no-renames', '--unified=0', mergeBase, '--'], {
        stdio: ['ignore', fileDescriptor, 'pipe'],
      });
    } finally {
      if (fileDescriptor !== undefined) fs.closeSync(fileDescriptor);
    }

    const input = fs.createReadStream(diffFile, { encoding: 'utf8' });
    const lines = readline.createInterface({ input, crlfDelay: Infinity });
    return await parseDiff(lines);
  } finally {
    fs.rmSync(outputDir, { recursive: true, force: true });
  }
}

function mapLine(line, hunk) {
  const delta = hunk.newCount - hunk.oldCount;
  if (line < hunk.oldStart) return line;
  if (line > hunk.oldEnd) return line + delta;
  return Math.max(1, hunk.newStart);
}

export function mapDeletedCommentRanges(oldComments, deletions) {
  const ranges = [];
  for (const hunk of deletions) {
    for (const comment of oldComments) {
      if (comment.start > hunk.oldEnd || comment.end < hunk.oldStart) continue;
      ranges.push([mapLine(comment.start, hunk), mapLine(comment.end, hunk)]);
    }
  }
  return ranges;
}

async function main() {
  const tracked = execFileSync('git', ['ls-files', '-z'], { encoding: 'utf8' }).split('\0').filter(Boolean);
  const sourceFiles = tracked.filter((file) => existsSync(file) && !excludedPath(file)
    && (extensions.has(path.extname(file)) || isHashSource(file)));
  const goComments = JSON.parse(execFileSync('go', ['run', '-C', 'backend', './cmd/commentpolicy', '..'], { encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 }));
  const tsSourceFiles = sourceFiles.filter((file) => !isHashSource(file) && !isGenerated(file));
  const hashSourceFiles = sourceFiles.filter((file) => isHashSource(file) && !isGenerated(file));
  const tsComments = new Map(tsSourceFiles.map((file) => [file, scanFile(file)]));
  const hashComments = new Map(hashSourceFiles.map((file) => [file, scanHashLanguage(readFileSync(file, 'utf8'), hashLanguageFor(file))]));
  const inventories = {};
  for (const [file, comments] of Object.entries(goComments)) {
    const counts = summary(comments);
    if (counts.blocks || counts.longLines) inventories[file] = counts;
  }
  for (const [file, comments] of tsComments) {
    const counts = summary(comments);
    if (counts.blocks || counts.longLines) inventories[file] = counts;
  }

  for (const [file, comments] of hashComments) {
    const counts = summary(comments);
    if (counts.blocks || counts.longLines) inventories[file] = counts;
  }

  if (process.argv.includes('--write-baseline')) {
    writeFileSync(baselinePath, `${JSON.stringify({ version: 1, files: inventories }, null, 2)}\n`);
    process.exit(0);
  }

  const diagnostics = new Map();
  const add = (file, message) => diagnostics.set(file, [...(diagnostics.get(file) ?? []), message]);
  const configuredBase = process.env.COMMENT_POLICY_BASE || 'origin/main';
  const mergeBase = execFileSync('git', ['merge-base', configuredBase, 'HEAD'], { encoding: 'utf8' }).trim();
  const changed = await changedLines(mergeBase);
  for (const [file, ranges] of changed) {
    if (!file.endsWith('.go') && !extensions.has(path.extname(file)) && !isHashSource(file)) continue;
    const hasComments = file.endsWith('.go')
      ? Object.hasOwn(goComments, file) : isHashSource(file)
        ? hashComments.has(file) : tsComments.has(file);
    if (!hasComments) continue;
    const comments = file.endsWith('.go') ? goComments[file] : isHashSource(file)
      ? hashComments.get(file) : tsComments.get(file);
    const forced = [];
    if (ranges.deletions.length) {
      const oldSource = execFileSync('git', ['show', `${mergeBase}:${file}`], { encoding: 'utf8' });
      const oldComments = scanSourceForFile(file, oldSource);
      forced.push(...mapDeletedCommentRanges(oldComments, ranges.deletions));
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

if (isMain) {
  try {
    await main();
  } catch (error) {
    if (error?.message !== python3MissingMessage) throw error;
    console.error(error.message);
    process.exitCode = 1;
  }
}
