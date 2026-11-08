#!/usr/bin/env python3
"""Offline fixture checks for the CI source/auth boundary; never build an image."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('seal-context.py').resolve()


class SealContextTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / 'repo'
        self.repo.mkdir()
        self.git('init', '-q')
        (self.repo / 'backend').mkdir()
        (self.repo / 'backend/main.go').write_text('package main\n')
        (self.repo / '.forgejo').mkdir()
        self.commit()
        row = self.git('ls-tree', 'HEAD', 'backend/main.go').split()
        self.review = {'phase': 'fixture', 'reviewed_commit': self.git('rev-parse', 'HEAD').strip(),
                       'native_base': 'fixture', 'native_template': 'fixture',
                       'runtime': {'backend/main.go': {'mode': row[0], 'object': row[2]}}}
        self.write_review()

    def git(self, *args):
        return subprocess.check_output(['git', '-c', 'core.hooksPath=/dev/null', '-c', 'commit.gpgsign=false',
                                        '-c', 'user.name=Fixture', '-c', 'user.email=fixture@invalid', *args], cwd=self.repo).decode()

    def commit(self):
        self.git('add', '.')
        self.git('commit', '-qm', 'fixture')

    def write_review(self):
        (self.repo / '.forgejo/reviewed-runtime.json').write_text(json.dumps(self.review))
        self.commit()

    def seal(self, destination='context', **env):
        return subprocess.run(['python3', str(SCRIPT), str(self.root / destination)], cwd=self.repo,
                              env={**os.environ, 'GITHUB_SHA': self.git('rev-parse', 'HEAD').strip(), **env},
                              capture_output=True, text=True)

    def test_tracked_only_and_credential_paths_excluded(self):
        for name in ('.env', '.env.example', '.tokensave/fixture', '.secrets/docker/config.json'):
            p = self.repo / name
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_text('dummy fixture, never credentials')
        self.commit()  # Even mistakenly tracked credential paths must not enter.
        (self.repo / 'untracked').write_text('not tracked')
        (self.repo / 'backend/main.go').write_text('dirty worktree bytes')
        result = self.seal()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.root / 'context/backend/main.go').read_text(), 'package main\n')
        paths = [str(p.relative_to(self.root / 'context')) for p in (self.root / 'context').rglob('*')]
        self.assertNotIn('untracked', paths)
        self.assertFalse(any(p.startswith(('.env', '.secrets', '.tokensave', '.git/')) for p in paths))
        self.assertNotEqual(self.seal().returncode, 0)  # Never overlay an old context.

    def test_reviewed_runtime_change_is_rejected(self):
        (self.repo / 'backend/main.go').write_text('changed runtime\n')
        self.commit()
        self.assertNotEqual(self.seal().returncode, 0)

    def test_revision_mismatch_is_rejected(self):
        self.assertNotEqual(self.seal(GITHUB_SHA='0' * 40).returncode, 0)

    def test_symlink_is_rejected(self):
        (self.repo / 'escape').symlink_to('/tmp')
        self.commit()
        self.assertNotEqual(self.seal().returncode, 0)

    def test_archive_attributes_cannot_change_or_omit_tracked_bytes(self):
        (self.repo / '.gitattributes').write_text('backend/main.go export-ignore\n')
        self.commit()
        self.assertNotEqual(self.seal().returncode, 0)
        (self.repo / 'substitution').write_text('$Format:%H$\n')
        (self.repo / '.gitattributes').write_text('substitution export-subst\n')
        self.commit()
        self.assertNotEqual(self.seal('second').returncode, 0)

    def test_manifest_is_deterministic(self):
        self.assertEqual(self.seal('one').returncode, 0)
        first = (self.root / 'source.json').read_bytes()
        self.assertEqual(self.seal('two').returncode, 0)
        self.assertEqual(first, (self.root / 'source.json').read_bytes())


if __name__ == '__main__':
    unittest.main()
