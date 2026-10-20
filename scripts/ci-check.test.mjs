import test from 'node:test';
import assert from 'node:assert/strict';
import {
  goVersionFromMod,
  parseArgs,
  parseOnly,
  repositoryFromRemote,
  toolchainFromGoVersion,
} from './ci-check.mjs';

test('defaults to every policy check and fetches the base', () => {
  assert.deepEqual(parseArgs([]), {
    only: ['comment', 'issue', 'lint'],
    pr: null,
    base: null,
    fetch: true,
    help: false,
    explicitOnly: false,
  });
});

test('selects checks and records an explicit --only', () => {
  assert.deepEqual(parseOnly('comment, lint'), ['comment', 'lint']);
  assert.deepEqual(parseOnly('comment,comment'), ['comment']);
  const options = parseArgs(['--only', 'comment,lint']);
  assert.deepEqual(options.only, ['comment', 'lint']);
  assert.equal(options.explicitOnly, true);
});

test('rejects unknown checks and malformed arguments', () => {
  assert.throws(() => parseOnly('comment,coverage'), /unknown check/);
  assert.throws(() => parseOnly(''), /at least one check/);
  assert.throws(() => parseArgs(['--pr', 'abc']), /positive integer/);
  assert.throws(() => parseArgs(['--pr']), /positive integer/);
  assert.throws(() => parseArgs(['--wat']), /unknown argument/);
});

test('parses a pull request, base override, and --no-fetch', () => {
  const options = parseArgs(['--pr', '344', '--base', 'origin/dev', '--no-fetch']);
  assert.equal(options.pr, 344);
  assert.equal(options.base, 'origin/dev');
  assert.equal(options.fetch, false);
});

test('derives owner/repo from the origin URL forms', () => {
  const expected = 'petarnenov/ultra-factory';
  assert.equal(repositoryFromRemote('https://github.com/petarnenov/ultra-factory.git'), expected);
  assert.equal(repositoryFromRemote('git@github.com:petarnenov/ultra-factory.git'), expected);
  assert.equal(repositoryFromRemote('ssh://git@github.com/petarnenov/ultra-factory'), expected);
  assert.equal(repositoryFromRemote('https://github.com/petarnenov/ultra-factory/'), expected);
});

test('refuses a remote that is not a GitHub repository', () => {
  assert.throws(() => repositoryFromRemote('https://example.com/owner/repo.git'),
    /cannot derive owner\/repo/);
  assert.throws(() => repositoryFromRemote(''), /cannot derive owner\/repo/);
});

test('reads the go directive and builds a toolchain name', () => {
  assert.equal(goVersionFromMod('module example\n\ngo 1.27.1\n'), '1.27.1');
  assert.equal(goVersionFromMod('go 1.27\n'), '1.27');
  assert.throws(() => goVersionFromMod('module example\n'), /no go directive/);
  assert.equal(toolchainFromGoVersion('1.27.1'), 'go1.27.1');
  assert.equal(toolchainFromGoVersion('1.27'), 'go1.27.0');
});
