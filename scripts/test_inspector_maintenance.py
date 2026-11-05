import hashlib
import importlib.util
import json
from importlib.machinery import SourceFileLoader
import os
from pathlib import Path
import tempfile
import unittest
from types import SimpleNamespace
from unittest import SkipTest, mock


ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "deploy" / "kitsusync-inspector-maintenance"
SPEC = importlib.util.spec_from_loader(
    "kitsusync_inspector_maintenance", SourceFileLoader("kitsusync_inspector_maintenance", str(SOURCE)))
maintenance = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(maintenance)
TEST_UID = getattr(os, "getuid", lambda: 0)()


def digest(data):
    return hashlib.sha256(data).hexdigest()


class InspectorMaintenanceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.stage = self.root / "stage"
        self.state = self.root / "state"
        self.backups = self.root / "backups"
        self.sbin = self.root / "usr" / "local" / "sbin"
        self.libexec = self.root / "usr" / "local" / "libexec"
        for directory in (self.stage, self.state, self.backups, self.sbin, self.libexec):
            directory.mkdir(parents=True)
            if os.name != "nt":
                os.chmod(directory, 0o700)
        self.paths = maintenance.Paths(self.stage, self.state, self.backups,
                                       self.sbin / "kitsusync-inspect",
                                       self.libexec / "kitsusync-preflight-collect")
        self.old_inspector = b"#!/bin/bash\nprintf old-inspector\n"
        self.new_inspector = b"#!/bin/bash\nprintf new-inspector\n"
        self.new_collector = b"#!/usr/bin/python3\nprint('synthetic')\n"
        self.commit = "a" * 40
        self._write_artifact()
        self.paths.install[maintenance.FILES[0]].write_bytes(self.old_inspector)
        if os.name != "nt":
            os.chmod(self.paths.install[maintenance.FILES[0]], 0o700)

    def tearDown(self):
        self.temp.cleanup()

    def _write_artifact(self):
        content = {
            "install-inspection": b"maintenance-install-fixture",
            "rollback-inspection": b"maintenance-rollback-fixture",
            "INSTALL.txt": b"verify archive SHA before staging\n",
            maintenance.FILES[0]: self.new_inspector,
            maintenance.FILES[1]: self.new_collector,
        }
        files = {}
        for name, payload in content.items():
            path = self.stage / name
            path.write_bytes(payload)
            os.chmod(path, 0o700 if name.startswith(("install-", "rollback-")) else 0o600)
            files[name] = digest(payload)
        manifest = {
            "schema_version": 1,
            "source_commit": self.commit,
            "baseline_release": "v0.5.1",
            "expected_previous_inspect_sha256": digest(self.old_inspector),
            "expected_previous_collector": "absent",
            "approved_releases": {},
            "files": files,
        }
        (self.stage / "manifest.json").write_text(json.dumps(manifest, sort_keys=True) + "\n")
        os.chmod(self.stage / "manifest.json", 0o600)

    def test_fresh_install_upgrade_repeat_and_rollback(self):
        self.assertEqual(maintenance.install(self.paths, TEST_UID), "installed")
        self.assertEqual(self.paths.install[maintenance.FILES[0]].read_bytes(), self.new_inspector)
        self.assertEqual(self.paths.install[maintenance.FILES[1]].read_bytes(), self.new_collector)
        identity = json.loads(self.paths.identity.read_text())
        self.assertEqual(identity["source_commit"], self.commit)
        self.assertEqual(identity["inspection_tool_sha256"], digest(self.new_inspector))
        self.assertEqual(identity["preflight_collector_tool_sha256"], digest(self.new_collector))
        self.assertEqual(identity["approved_releases"], {})
        if os.name != "nt":
            for target in self.paths.install.values():
                self.assertEqual(target.stat().st_mode & 0o777, 0o700)
        self.assertFalse(self.paths.pending.exists())
        self.assertEqual(maintenance.install(self.paths, TEST_UID), "already-installed")
        self.assertEqual(maintenance.rollback(self.paths, TEST_UID), "rolled-back")
        self.assertEqual(self.paths.install[maintenance.FILES[0]].read_bytes(), self.old_inspector)
        self.assertFalse(self.paths.install[maintenance.FILES[1]].exists())
        self.assertFalse(self.paths.identity.exists())
        self.assertEqual(maintenance.install(self.paths, TEST_UID), "installed")

    def test_interruption_during_pair_replace_rolls_back_without_losing_prior_helper(self):
        def interrupt(index, _name):
            if index == 0:
                raise RuntimeError("fixture interruption")

        with self.assertRaisesRegex(RuntimeError, "fixture interruption"):
            maintenance.install(self.paths, TEST_UID, fault_hook=interrupt)
        self.assertEqual(self.paths.install[maintenance.FILES[0]].read_bytes(), self.old_inspector)
        self.assertFalse(self.paths.install[maintenance.FILES[1]].exists())
        self.assertFalse(self.paths.pending.exists())
        self.assertEqual(len(list(self.backups.glob("inspector-maintenance-*"))), 1)

    def test_interrupted_rollback_snapshot_does_not_block_safe_retry(self):
        write_atomic = maintenance._write_atomic
        interrupted = False

        def fail_first_snapshot(directory, name, *args, **kwargs):
            nonlocal interrupted
            if not interrupted and directory.parent == self.backups and name == maintenance.FILES[0]:
                interrupted = True
                raise OSError("synthetic snapshot interruption")
            return write_atomic(directory, name, *args, **kwargs)

        with mock.patch.object(maintenance, "_write_atomic", side_effect=fail_first_snapshot):
            with self.assertRaisesRegex(OSError, "snapshot interruption"):
                maintenance.install(self.paths, TEST_UID)
        self.assertEqual(self.paths.install[maintenance.FILES[0]].read_bytes(), self.old_inspector)
        self.assertFalse(self.paths.pending.exists())
        self.assertEqual(maintenance.install(self.paths, TEST_UID), "installed")

    def test_crash_left_pending_is_recovered_before_any_retry(self):
        def crash(index, _name):
            if index == 0:
                raise KeyboardInterrupt()

        with self.assertRaises(KeyboardInterrupt):
            maintenance.install(self.paths, TEST_UID, fault_hook=crash)
        self.assertTrue(self.paths.pending.exists())
        self.assertFalse(self.paths.identity.exists())
        with self.assertRaisesRegex(maintenance.MaintenanceError, "was rolled back"):
            maintenance.install(self.paths, TEST_UID)
        self.assertEqual(self.paths.install[maintenance.FILES[0]].read_bytes(), self.old_inspector)
        self.assertFalse(self.paths.install[maintenance.FILES[1]].exists())
        self.assertFalse(self.paths.pending.exists())
        self.assertFalse(self.paths.identity.exists())

    def test_crash_after_identity_marker_is_recovered_as_one_transaction(self):
        def crash(index, name):
            if name == self.paths.identity.name:
                raise KeyboardInterrupt()

        with self.assertRaises(KeyboardInterrupt):
            maintenance.install(self.paths, TEST_UID, fault_hook=crash)
        self.assertTrue(self.paths.pending.exists())
        self.assertTrue(self.paths.identity.exists())
        self.assertEqual(self.paths.install[maintenance.FILES[0]].read_bytes(), self.new_inspector)
        with self.assertRaisesRegex(maintenance.MaintenanceError, "was rolled back"):
            maintenance.install(self.paths, TEST_UID)
        self.assertEqual(self.paths.install[maintenance.FILES[0]].read_bytes(), self.old_inspector)
        self.assertFalse(self.paths.install[maintenance.FILES[1]].exists())
        self.assertFalse(self.paths.identity.exists())
        self.assertFalse(self.paths.pending.exists())

    def test_crash_during_explicit_rollback_is_recovered_before_retry(self):
        self.assertEqual(maintenance.install(self.paths, TEST_UID), "installed")
        with mock.patch.object(maintenance, "_remove_installed", side_effect=KeyboardInterrupt):
            with self.assertRaises(KeyboardInterrupt):
                maintenance.rollback(self.paths, TEST_UID)
        self.assertTrue(self.paths.pending.exists())
        self.assertEqual(self.paths.install[maintenance.FILES[0]].read_bytes(), self.old_inspector)
        self.assertTrue(self.paths.install[maintenance.FILES[1]].exists())
        self.assertTrue(self.paths.identity.exists())
        with self.assertRaisesRegex(maintenance.MaintenanceError, "was rolled back"):
            maintenance.install(self.paths, TEST_UID)
        self.assertEqual(self.paths.install[maintenance.FILES[0]].read_bytes(), self.old_inspector)
        self.assertFalse(self.paths.install[maintenance.FILES[1]].exists())
        self.assertFalse(self.paths.pending.exists())
        self.assertFalse(self.paths.identity.exists())

    def test_rollback_rejects_unexpected_helper_before_mutating_pair(self):
        self.assertEqual(maintenance.install(self.paths, TEST_UID), "installed")
        collector = self.paths.install[maintenance.FILES[1]]
        collector.write_bytes(b"unexpected replacement")
        with self.assertRaisesRegex(maintenance.MaintenanceError, "installed helper changed"):
            maintenance.rollback(self.paths, TEST_UID)
        self.assertEqual(self.paths.install[maintenance.FILES[0]].read_bytes(), self.new_inspector)
        self.assertEqual(collector.read_bytes(), b"unexpected replacement")
        collector.write_bytes(self.new_collector)
        if os.name != "nt":
            os.chmod(collector, 0o700)
        self.assertEqual(maintenance.rollback(self.paths, TEST_UID), "rolled-back")

    def test_corrupt_package_and_wrong_predecessor_fail_closed(self):
        (self.stage / maintenance.FILES[1]).write_bytes(b"corrupt")
        with self.assertRaisesRegex(maintenance.MaintenanceError, "digest mismatch"):
            maintenance.install(self.paths, TEST_UID)
        self.assertEqual(self.paths.install[maintenance.FILES[0]].read_bytes(), self.old_inspector)
        self._write_artifact()
        self.paths.install[maintenance.FILES[0]].write_bytes(b"unexpected helper")
        with self.assertRaisesRegex(maintenance.MaintenanceError, "predecessor"):
            maintenance.install(self.paths, TEST_UID)

    def test_wrong_file_owner_and_installed_mode_are_rejected(self):
        target = self.stage / "INSTALL.txt"
        with self.assertRaisesRegex(maintenance.MaintenanceError, "regular file"):
            maintenance._read_regular(target, TEST_UID + 1, 0o600)
        if os.name != "nt":
            os.chmod(target, 0o644)
            with self.assertRaisesRegex(maintenance.MaintenanceError, "regular file"):
                maintenance.install(self.paths, TEST_UID)
            os.chmod(target, 0o600)
            installed = self.paths.install[maintenance.FILES[0]]
            os.chmod(installed, 0o755)
            with self.assertRaisesRegex(maintenance.MaintenanceError, "regular file"):
                maintenance.install(self.paths, TEST_UID)

    def test_staged_file_replacement_after_manifest_read_does_not_change_installed_bytes(self):
        def replace_staged_file(index, _name):
            if index == 0:
                (self.stage / maintenance.FILES[0]).write_bytes(b"raced payload")

        self.assertEqual(maintenance.install(self.paths, TEST_UID, fault_hook=replace_staged_file), "installed")
        self.assertEqual(self.paths.install[maintenance.FILES[0]].read_bytes(), self.new_inspector)
        self.assertEqual(self.paths.install[maintenance.FILES[1]].read_bytes(), self.new_collector)

    def test_matching_helpers_without_maintenance_identity_are_not_claimed_as_complete(self):
        self.paths.install[maintenance.FILES[0]].write_bytes(self.new_inspector)
        self.paths.install[maintenance.FILES[1]].write_bytes(self.new_collector)
        if os.name != "nt":
            os.chmod(self.paths.install[maintenance.FILES[0]], 0o700)
            os.chmod(self.paths.install[maintenance.FILES[1]], 0o700)
        with self.assertRaisesRegex(maintenance.MaintenanceError, "identity record"):
            maintenance.install(self.paths, TEST_UID)

    def test_wrong_stage_mode_and_symlink_are_rejected(self):
        if os.name == "nt":
            raise SkipTest("POSIX directory modes are enforced by Linux CI")
        os.chmod(self.stage, 0o755)
        with self.assertRaisesRegex(maintenance.MaintenanceError, "directory"):
            maintenance.install(self.paths, TEST_UID)
        os.chmod(self.stage, 0o700)
        linked = self.stage / maintenance.FILES[1]
        linked.unlink()
        try:
            linked.symlink_to(self.stage / maintenance.FILES[0])
        except OSError as error:
            if os.name == "nt":
                raise SkipTest("Windows account lacks symlink privilege; Linux CI exercises O_NOFOLLOW") from error
            raise
        with self.assertRaises(maintenance.MaintenanceError):
            maintenance.install(self.paths, TEST_UID)

    def test_manifest_rejects_unexpected_artifact_files(self):
        (self.stage / "untrusted-helper").write_text("must not execute")
        with self.assertRaisesRegex(maintenance.MaintenanceError, "inventory"):
            maintenance.install(self.paths, TEST_UID)

    def test_manifest_rejects_malformed_post_tag_release_approval(self):
        manifest_path = self.stage / "manifest.json"
        manifest = json.loads(manifest_path.read_text())
        manifest["approved_releases"] = {"v0.5.2": {
            "version": "0.5.2", "source_commit": "b" * 40,
            "image_ref": "kitsusync:v0.5.2", "migration_compatibility": "wrong",
        }}
        manifest_path.write_text(json.dumps(manifest, sort_keys=True) + "\n")
        with self.assertRaisesRegex(maintenance.MaintenanceError, "manifest"):
            maintenance.install(self.paths, TEST_UID)


if __name__ == "__main__":
    unittest.main()
