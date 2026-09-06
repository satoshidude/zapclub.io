#!/usr/bin/env python3
"""Offline integration checks: real Git fixtures; blocked network and mocked app checks."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SOURCE = Path(__file__).resolve().parent.parent
if SOURCE.name == 'core':
    SOURCE = SOURCE.parent
REAL_GIT = shutil.which('git')

class ReleaseCLI(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='release-cli-test-')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name) / 'repo'
        self.root.mkdir()
        for name in ['release.sh', 'release-fast.sh']:
            if (SOURCE / name).exists(): shutil.copy2(SOURCE / name, self.root / name)
        for name in ['deploy', 'core/deploy', 'core/ops']:
            if (SOURCE / name).exists():
                shutil.copytree(SOURCE / name, self.root / name)
        # The real Lastpub smoke resets PATH. Replace the app-level smoke in the
        # fixture too: this suite tests CLI control flow, not live application health.
        smoke = self.root / 'deploy/smoke-test.sh'
        if smoke.exists(): smoke.write_text('#!/bin/sh\nexit 0\n')
        for name in ['relay', 'telegram-bot', 'frontend/node_modules']:
            (self.root / name).mkdir(parents=True, exist_ok=True)
        (self.root / 'relay/e2e.sh').write_text('#!/bin/sh\nexit 0\n')
        (self.root / 'relay/e2e.sh').chmod(0o755)
        (self.root / '.gitignore').write_text('frontend/node_modules/\n')
        self.git('init', '-q', '-b', 'main')
        self.git('config', 'user.email', 'fixture@example.invalid')
        self.git('config', 'user.name', 'Release fixture')
        self.git('add', '.')
        self.git('commit', '-qm', 'fixture')
        self.bin = Path(self.tmp.name) / 'bin'
        self.bin.mkdir()
        self.log = Path(self.tmp.name) / 'commands'
        script = '''#!/bin/sh
name=$(basename "$0")
if [ "$name" = git ]; then
    case "$1" in
        push) printf 'git push %s\\n' "$*" >> "$COMMAND_LOG"; exit 0 ;;
        *) exec "$REAL_GIT" "$@" ;;
    esac
fi
printf '%s %s\\n' "$name" "$*" >> "$COMMAND_LOG"
if [ "$name" = npm ]; then
    [ "${FAIL_CHECK:-0}" != 1 ] || exit 19
    if [ "${MUTATE_TREE:-0}" = 1 ]; then touch late-change; fi
    if [ "${MUTATE_HEAD:-0}" = 1 ]; then "$REAL_GIT" commit --allow-empty -qm changed; fi
fi
if [ "$name" = node ]; then printf '0.1.0\\n'; fi
if [ "$name" = curl ]; then
    commit=$("$REAL_GIT" rev-parse HEAD)
    short=$(printf '%s' "$commit" | cut -c1-7)
    printf '<html data-project-footer="1.2.0" data-build-commit="%s">/</span> %s</small></html>\\n' "$commit" "$short"
fi
exit 0
'''
        for name in ['git', 'npm', 'npx', 'node', 'go', 'python3', 'ssh', 'scp', 'curl']:
            file = self.bin / name
            file.write_text(script)
            file.chmod(0o755)
        self.env = dict(os.environ, PATH=f'{self.bin}:{os.environ["PATH"]}',
                        COMMAND_LOG=str(self.log), REAL_GIT=REAL_GIT,
                        TOWER_V2_DESCRIPTOR_ID='a' * 64)

    def git(self, *args):
        return subprocess.check_output([REAL_GIT, *args], cwd=self.root, text=True).strip()

    def run_release(self, *args, **env):
        return subprocess.run(['sh', './release.sh', *args], cwd=self.root,
                              env=dict(self.env, **env), text=True, capture_output=True)

    def commands(self):
        return self.log.read_text() if self.log.exists() else ''

    def assert_no_transfer(self):
        for command in ['git push', 'scp ', 'ssh ', 'curl ']:
            self.assertNotIn(command, self.commands())

    def test_help_and_invalid_arguments_have_no_side_effects(self):
        (self.root / 'unrelated').write_text('keep')
        for args, success in [(('--help',), True), (('--unknown',), False),
                              (('--check', '--deploy'), False), (('commit message',), False)]:
            result = self.run_release(*args, TOWER_V2_DESCRIPTOR_ID='')
            self.assertEqual(result.returncode == 0, success, result.stderr)
        self.assertEqual(self.commands(), '')
        self.assertTrue((self.root / 'frontend/node_modules').exists())

    def test_dry_run_is_read_only(self):
        result = self.run_release('--dry-run')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('No checks, uploads', result.stdout)
        self.assertEqual(self.commands(), '')
        (self.root / 'dirty').touch()
        self.assertNotEqual(self.run_release('--dry-run').returncode, 0)
        self.assertEqual(self.commands(), '')

    def test_check_allows_worktree_without_git_mutation_or_transport(self):
        self.git('switch', '-qc', 'codex/test')
        (self.root / 'dirty').touch()
        before = self.git('status', '--porcelain')
        head = self.git('rev-parse', 'HEAD')
        result = self.run_release('--check')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('npm ', self.commands())
        self.assert_no_transfer()
        self.assertEqual(before, self.git('status', '--porcelain'))
        self.assertEqual(head, self.git('rev-parse', 'HEAD'))

    def test_dirty_and_wrong_branch_reject_before_checks(self):
        (self.root / 'dirty').touch()
        self.assertNotEqual(self.run_release('--deploy').returncode, 0)
        (self.root / 'dirty').unlink()
        self.git('switch', '-qc', 'codex/test')
        self.assertNotEqual(self.run_release('--deploy').returncode, 0)
        self.assertEqual(self.commands(), '')

    def test_failed_checks_prevent_transfer(self):
        self.assertNotEqual(self.run_release('--deploy', FAIL_CHECK='1').returncode, 0)
        self.assert_no_transfer()

    def test_change_during_checks_prevents_transfer(self):
        self.assertNotEqual(self.run_release('--deploy', MUTATE_TREE='1').returncode, 0)
        self.assert_no_transfer()

    def test_head_change_during_checks_prevents_transfer(self):
        self.assertNotEqual(self.run_release('--deploy', MUTATE_HEAD='1').returncode, 0)
        self.assert_no_transfer()

    def test_full_check_never_transfers(self):
        result = self.run_release('--check', '--full')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assert_no_transfer()

    def test_lastpub_descriptor_still_required(self):
        if 'PROJECT=lastpub' not in (self.root / 'release.sh').read_text():
            self.skipTest('Lastpub only')
        self.assertNotEqual(self.run_release('--deploy', TOWER_V2_DESCRIPTOR_ID='').returncode, 0)
        self.assertEqual(self.commands(), '')

    def test_success_transfers_pinned_commit(self):
        commit = self.git('rev-parse', 'HEAD')
        result = self.run_release('--deploy')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(f'{commit}:refs/heads/main', self.commands())
        self.assertIn('scp ', self.commands())
        self.assertIn('ssh ', self.commands())
        self.assertEqual(commit, self.git('rev-parse', 'HEAD'))

if __name__ == '__main__':
    unittest.main()
