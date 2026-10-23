import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import {
  chmodSync,
  mkdtempSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

function runCoverage({ failure } = {}) {
  const temp = mkdtempSync(path.join(os.tmpdir(), 'ao-coverage-target-'));
  const bin = path.join(temp, 'bin');
  const log = path.join(temp, 'commands.log');
  const go = path.join(bin, 'go');
  const npm = path.join(bin, 'npm');
  const report = path.join(ROOT, 'backend', 'coverage.out');
  const previousReport = readExistingReport(report);
  try {
    mkdirSync(bin);
  writeFileSync(go, `#!/bin/sh
printf 'go %s\\n' "$*" >> "$COVERAGE_TARGET_LOG"
if [ "$1" = test ]; then : > coverage.out; fi
if [ "$1 $2" = 'test -p' ] && [ "${'${FAIL_BACKEND:-}'}" = 1 ]; then exit 17; fi
if [ "$1 $2" = 'tool cover' ]; then
  if [ "${'${FAIL_SUMMARY:-}'}" = 1 ]; then exit 18; fi
  printf 'total: (statements) 12.3%%\\n'
fi
`);
    writeFileSync(npm, `#!/bin/sh
printf 'npm %s forks=%s\\n' "$*" "${'${AO_VITEST_MAX_FORKS:-}'}" >> "$COVERAGE_TARGET_LOG"
if [ "${'${FAIL_FRONTEND:-}'}" = 1 ]; then exit 19; fi
`);
    chmodSync(go, 0o755);
    chmodSync(npm, 0o755);
    writeFileSync(log, '');
    const result = spawnSync('make', ['-j8', 'coverage'], {
      cwd: ROOT,
      encoding: 'utf8',
      env: {
        ...process.env,
        PATH: `${bin}${path.delimiter}${process.env.PATH}`,
        COVERAGE_TARGET_LOG: log,
        AO_VITEST_MAX_FORKS: '2',
        GO_TEST_PARALLELISM: '2',
        ...failure,
      },
    });
    return {
      status: result.status,
      output: `${result.stdout ?? ''}${result.stderr ?? ''}`,
      commands: readFileSync(log, 'utf8').trim().split('\n').filter(Boolean),
    };
  } finally {
    if (previousReport === null) rmSync(report, { force: true });
    else writeFileSync(report, previousReport);
    rmSync(temp, { recursive: true, force: true });
  }
}

function readExistingReport(report) {
  try {
    return readFileSync(report);
  } catch (error) {
    if (error.code === 'ENOENT') return null;
    throw error;
  }
}

test('coverage target runs backend and frontend serially with configured caps', () => {
  const result = runCoverage();
  assert.equal(result.status, 0, result.output);
  assert.deepEqual(result.commands.map((line) => line.split(' ')[0]), [
    'go', 'go', 'npm',
  ]);
  assert.match(result.commands[0], /test -p 2 -coverprofile=coverage\.out -cover/);
  assert.match(result.commands[2], /forks=2/);
  assert.match(result.commands[2], /--coverage\.reportOnFailure/);
  assert.match(result.output, /12\.3%/);
  assert.match(result.output, /Backend coverage report: backend\/coverage\.out/);
  assert.match(result.output, /Frontend coverage report: frontend\/coverage\/index\.html/);
});

test('coverage target stops after a backend failure', () => {
  const result = runCoverage({ failure: { FAIL_BACKEND: '1' } });
  assert.notEqual(result.status, 0);
  assert.deepEqual(result.commands.map((line) => line.split(' ')[0]), ['go', 'go']);
});

test('coverage target propagates backend summary and frontend failures', () => {
  const summary = runCoverage({ failure: { FAIL_SUMMARY: '1' } });
  assert.notEqual(summary.status, 0);
  assert.deepEqual(summary.commands.map((line) => line.split(' ')[0]), ['go', 'go']);

  const frontend = runCoverage({ failure: { FAIL_FRONTEND: '1' } });
  assert.notEqual(frontend.status, 0);
  assert.deepEqual(frontend.commands.map((line) => line.split(' ')[0]), [
    'go', 'go', 'npm',
  ]);
});
