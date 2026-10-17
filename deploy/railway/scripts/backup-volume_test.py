#!/usr/bin/env python3
"""Exercise backup transport locally, with no Railway access."""
import os
from pathlib import Path
import sqlite3
import subprocess
import tarfile
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('backup-volume.sh')


class BackupTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.data = self.root / 'data'
        (self.data / 'ao').mkdir(parents=True)
        (self.data / 'hub').mkdir()
        self.db = sqlite3.connect(self.data / 'ao/ao.db')
        self.addCleanup(self.db.close)
        self.db.execute('PRAGMA journal_mode=WAL')
        self.db.execute('CREATE TABLE sessions (id TEXT)')
        self.db.execute("INSERT INTO sessions VALUES ('persisted')")
        self.db.commit()
        (self.data / 'hub/hosts.json').write_text('[{"password":"fixture-secret"}]')
        self.bin = self.root / 'bin'
        self.bin.mkdir()
        self.fake('railway', '''#!/usr/bin/env python3
import os, subprocess, sys
assert sys.argv[1:9] == ['ssh', '--project', 'project', '--environment', 'environment', '--service', 'service', '--']
if os.environ.get('FAIL_SSH'): sys.exit(7)
if os.environ.get('BAD_OUTPUT'):
    print('SSH banner: not a backup'); sys.exit(0)
command = sys.argv[9].replace('/data/', os.environ['TEST_DATA'] + '/')
sys.exit(subprocess.call(command, shell=True))
''')
        # A real SQLite online backup, with the same CLI arguments as production.
        self.fake('sqlite3', '''#!/usr/bin/env python3
import sqlite3, sys
assert sys.argv[1] == '-readonly'
with sqlite3.connect('file:' + sys.argv[2] + '?mode=ro', uri=True) as source:
    if sys.argv[3] == '.timeout 10000':
        assert sys.argv[4].startswith(".backup '")
        with sqlite3.connect(sys.argv[4][9:-1]) as destination: source.backup(destination)
    else:
        assert sys.argv[3] == 'PRAGMA integrity_check;'
        print(source.execute(sys.argv[3]).fetchone()[0])
''')
        self.env = {**os.environ, 'PATH': f'{self.bin}:{os.environ["PATH"]}', 'TEST_DATA': str(self.data)}
        self.output = self.root / 'snapshot.tar.gz'

    def fake(self, name, content):
        script = self.bin / name
        script.write_text(content)
        script.chmod(0o700)

    def run_backup(self, *flags):
        return subprocess.run([str(SCRIPT), *flags, 'project', 'environment', 'service', str(self.output)], env=self.env, capture_output=True, text=True)

    def test_consistent_wal_snapshot_and_private_output(self):
        result = self.run_backup()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn('fixture-secret', result.stdout)
        self.assertEqual(self.output.stat().st_mode & 0o777, 0o600)
        with tarfile.open(self.output) as archive:
            self.assertEqual(sorted(archive.getnames()), ['ao.db', 'hosts.json'])
            restored = self.root / 'restored.db'
            restored.write_bytes(archive.extractfile('ao.db').read())
            with sqlite3.connect(restored) as db:
                self.assertEqual(db.execute('SELECT id FROM sessions').fetchall(), [('persisted',)])
        self.assertEqual(list((self.data / 'ao').glob('backup.*')), [])
        self.assertNotEqual(self.run_backup().returncode, 0)

    def test_dry_run_never_calls_railway(self):
        self.env['FAIL_SSH'] = '1'
        self.assertEqual(self.run_backup('--dry-run').returncode, 0)
        self.assertFalse(self.output.exists())

    def test_transport_failure_never_publishes(self):
        for variable in ['FAIL_SSH', 'BAD_OUTPUT']:
            self.env[variable] = '1'
            self.assertNotEqual(self.run_backup().returncode, 0)
            self.assertFalse(self.output.exists())
            del self.env[variable]

    def test_missing_registry_fails_closed(self):
        (self.data / 'hub/hosts.json').unlink()
        self.assertNotEqual(self.run_backup().returncode, 0)
        self.assertFalse(self.output.exists())


if __name__ == '__main__':
    unittest.main()
