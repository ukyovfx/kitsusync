import hashlib
from importlib.machinery import SourceFileLoader
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "deploy" / "kitsusync-deployment-maintenance"
SPEC = importlib.util.spec_from_loader("deployment_maintenance", SourceFileLoader("deployment_maintenance", str(SOURCE)))
maintenance = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(maintenance)
UID = getattr(os, "getuid", lambda: 0)()


def sha(data):
    return hashlib.sha256(data).hexdigest()


class DeploymentMaintenanceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        root = Path(self.temp.name)
        self.stage = root / "stage"
        self.control = root / "etc"
        self.state = root / "state"
        self.backups = root / "backups"
        self.sbin = root / "sbin"
        self.libexec = root / "libexec"
        for path in (self.stage, self.control, self.state, self.backups, self.sbin, self.libexec):
            path.mkdir(parents=True)
            if os.name != "nt":
                path.chmod(0o700)
        self.paths = maintenance.Paths(self.stage, self.control / "provenance",
                                       self.control / "deployment-maintenance.json",
                                       self.state, self.backups,
                                       self.sbin / "kitsusync-deploy",
                                       self.libexec / "kitsusync-runtime-state")
        self.old = {name: ("old-" + name).encode() for name in maintenance.TOOL_FILES}
        self.new = {name: ("new-" + name).encode() for name in maintenance.TOOL_FILES}
        self.source = "a" * 40
        for name, data in self.old.items():
            path = self.paths.installed[name]
            path.write_bytes(data)
            if os.name != "nt":
                path.chmod(0o700)
        values = dict(maintenance.BASELINE_PROVENANCE)
        values.update({name: sha(self.old[tool]) for name, tool in maintenance.PROVENANCE_TOOL_FIELDS.items()})
        self.provenance_original = "".join(f"{key}={value}\n" for key, value in values.items()).encode()
        self.paths.provenance.write_bytes(self.provenance_original)
        if os.name != "nt":
            self.paths.provenance.chmod(0o600)
        self._artifact()

    def tearDown(self):
        self.temp.cleanup()

    def _artifact(self):
        content = {"INSTALL.txt": b"verify immutable artifact digest\n",
                   "install-deployment": maintenance._entrypoint_bytes("install"),
                   "rollback-deployment": maintenance._entrypoint_bytes("rollback"),
                   "deployment_maintenance.py": SOURCE.read_bytes(), **self.new}
        files = {}
        for name, data in content.items():
            path = self.stage / name
            path.write_bytes(data)
            if os.name != "nt":
                path.chmod(0o700 if name.startswith(("install-", "rollback-")) else 0o600)
            files[name] = sha(data)
        manifest = {
            "schema_version": 1,
            "source_commit": self.source,
            "baseline_release": "v0.5.1",
            "baseline_source_commit": "f8a32bbba94d84068d9e96c18554ac4ab7b24540",
            "baseline_tools": {name: sha(data) for name, data in self.old.items()},
            "files": files,
        }
        (self.stage / "manifest.json").write_text(json.dumps(manifest, sort_keys=True) + "\n")
        if os.name != "nt":
            (self.stage / "manifest.json").chmod(0o600)

    def test_install_updates_wrapper_helpers_and_provenance_as_one_reversible_unit(self):
        self.assertEqual(maintenance.install(self.paths, UID), "installed")
        for name in maintenance.TOOL_FILES:
            self.assertEqual(self.paths.installed[name].read_bytes(), self.new[name])
        record = json.loads(self.paths.identity.read_text())
        self.assertEqual(record["source_commit"], self.source)
        self.assertEqual(record["previous_provenance_sha256"], sha(self.provenance_original))
        self.assertEqual(self.paths.provenance.read_bytes(), self.provenance_original)
        self.assertEqual(maintenance.install(self.paths, UID), "already-installed")
        self.assertEqual(maintenance.rollback(self.paths, UID), "rolled-back")
        for name in maintenance.TOOL_FILES:
            self.assertEqual(self.paths.installed[name].read_bytes(), self.old[name])
        self.assertEqual(self.paths.provenance.read_bytes(), self.provenance_original)
        self.assertFalse(self.paths.identity.exists())

    def test_interrupted_install_restores_every_predecessor_and_provenance(self):
        def interrupt(index, _name):
            if index == 1:
                raise RuntimeError("injected interruption")

        with self.assertRaisesRegex(RuntimeError, "injected interruption"):
            maintenance.install(self.paths, UID, fault_hook=interrupt)
        for name in maintenance.TOOL_FILES:
            self.assertEqual(self.paths.installed[name].read_bytes(), self.old[name])
        self.assertEqual(self.paths.provenance.read_bytes(), self.provenance_original)
        self.assertFalse(self.paths.pending.exists())
        self.assertFalse(self.paths.identity.exists())

    def test_partial_rollback_evidence_is_completed_on_retry(self):
        original_atomic = maintenance._atomic
        calls = 0

        def interrupt_after_one_backup(directory, name, data, uid, mode):
            nonlocal calls
            result = original_atomic(directory, name, data, uid, mode)
            calls += 1
            if calls == 1:
                raise OSError("injected interruption while saving rollback evidence")
            return result

        with mock.patch.object(maintenance, "_atomic", side_effect=interrupt_after_one_backup):
            with self.assertRaisesRegex(OSError, "injected interruption"):
                maintenance.install(self.paths, UID)
        self.assertEqual(self.paths.installed[maintenance.TOOL_FILES[0]].read_bytes(),
                         self.old[maintenance.TOOL_FILES[0]])
        self.assertFalse(self.paths.pending.exists())
        self.assertEqual(maintenance.install(self.paths, UID), "installed")
        for name in maintenance.TOOL_FILES:
            self.assertEqual((self.backups / ("deployment-maintenance-" + self.source) / name).read_bytes(),
                             self.old[name])
        self.assertTrue((self.backups / ("deployment-maintenance-" + self.source) / "rollback.json").is_file())

    def test_partial_rollback_evidence_mismatch_fails_closed(self):
        original_atomic = maintenance._atomic
        calls = 0

        def interrupt_after_one_backup(directory, name, data, uid, mode):
            nonlocal calls
            original_atomic(directory, name, data, uid, mode)
            calls += 1
            if calls == 1:
                raise OSError("injected interruption")

        with mock.patch.object(maintenance, "_atomic", side_effect=interrupt_after_one_backup):
            with self.assertRaisesRegex(OSError, "injected interruption"):
                maintenance.install(self.paths, UID)
        first = self.backups / ("deployment-maintenance-" + self.source) / maintenance.TOOL_FILES[0]
        first.write_bytes(b"untrusted changed rollback evidence")
        with self.assertRaisesRegex(maintenance.MaintenanceError, "evidence mismatch"):
            maintenance.install(self.paths, UID)
        self.assertEqual(self.paths.installed[maintenance.TOOL_FILES[0]].read_bytes(),
                         self.old[maintenance.TOOL_FILES[0]])

    def test_process_interruption_recovers_on_next_invocation(self):
        def crash(index, _name):
            if index == 0:
                raise KeyboardInterrupt()

        with self.assertRaises(KeyboardInterrupt):
            maintenance.install(self.paths, UID, fault_hook=crash)
        self.assertTrue(self.paths.pending.exists())
        with self.assertRaisesRegex(maintenance.MaintenanceError, "recovered"):
            maintenance.install(self.paths, UID)
        for name in maintenance.TOOL_FILES:
            self.assertEqual(self.paths.installed[name].read_bytes(), self.old[name])
        self.assertEqual(self.paths.provenance.read_bytes(), self.provenance_original)
        self.assertFalse(self.paths.pending.exists())
        self.assertFalse(self.paths.identity.exists())

    def test_interrupted_explicit_rollback_finishes_from_saved_predecessors(self):
        self.assertEqual(maintenance.install(self.paths, UID), "installed")
        marker = {"schema_version": 1, "operation": "rollback", "source_commit": self.source,
                  "target_files": {name: sha(self.old[name]) for name in maintenance.TOOL_FILES},
                  "current_files": {name: sha(self.new[name]) for name in maintenance.TOOL_FILES},
                  "current_identity_sha256": sha(self.paths.identity.read_bytes())}
        self.paths.pending.write_text(json.dumps(marker))
        if os.name != "nt":
            self.paths.pending.chmod(0o600)
        self.paths.installed[maintenance.TOOL_FILES[0]].write_bytes(self.old[maintenance.TOOL_FILES[0]])
        if os.name != "nt":
            self.paths.installed[maintenance.TOOL_FILES[0]].chmod(0o700)
        with self.assertRaisesRegex(maintenance.MaintenanceError, "recovered"):
            maintenance.install(self.paths, UID)
        for name in maintenance.TOOL_FILES:
            self.assertEqual(self.paths.installed[name].read_bytes(), self.old[name])
        self.assertEqual(self.paths.provenance.read_bytes(), self.provenance_original)
        self.assertFalse(self.paths.pending.exists())
        self.assertFalse(self.paths.identity.exists())

    def test_corrupt_artifact_wrong_predecessor_and_symlink_fail_closed(self):
        (self.stage / maintenance.TOOL_FILES[0]).write_bytes(b"altered")
        with self.assertRaises(maintenance.MaintenanceError):
            maintenance.install(self.paths, UID)
        self._artifact()
        self.paths.installed[maintenance.TOOL_FILES[0]].write_bytes(b"wrong predecessor")
        if os.name != "nt":
            self.paths.installed[maintenance.TOOL_FILES[0]].chmod(0o700)
        with self.assertRaises(maintenance.MaintenanceError):
            maintenance.install(self.paths, UID)
        self.paths.installed[maintenance.TOOL_FILES[0]].write_bytes(self.old[maintenance.TOOL_FILES[0]])
        if os.name != "nt":
            self.paths.installed[maintenance.TOOL_FILES[0]].chmod(0o700)
        if os.name != "nt":
            target = self.paths.installed[maintenance.TOOL_FILES[1]]
            target.unlink()
            target.symlink_to(self.stage / maintenance.TOOL_FILES[1])
            with self.assertRaises(maintenance.MaintenanceError):
                maintenance.install(self.paths, UID)

    def test_wrong_stage_and_artifact_modes_fail_closed(self):
        if os.name == "nt":
            self.skipTest("POSIX ownership and mode checks are exercised by Linux CI")
        self.stage.chmod(0o755)
        with self.assertRaisesRegex(maintenance.MaintenanceError, "directory check"):
            maintenance.install(self.paths, UID)
        self.stage.chmod(0o700)
        (self.stage / "install-deployment").chmod(0o600)
        with self.assertRaisesRegex(maintenance.MaintenanceError, "regular file check"):
            maintenance.install(self.paths, UID)

    def test_wrong_owner_is_rejected(self):
        if os.name == "nt":
            self.skipTest("POSIX ownership checks are exercised by Linux CI")
        with self.assertRaisesRegex(maintenance.MaintenanceError, "directory check"):
            maintenance._check_dir(self.stage, UID + 1, 0o700)
        with self.assertRaisesRegex(maintenance.MaintenanceError, "regular file check"):
            maintenance._read_regular(self.stage / "manifest.json", UID + 1, 0o600)

    def test_unknown_interruption_operation_fails_closed(self):
        self.assertEqual(maintenance.install(self.paths, UID), "installed")
        self.paths.pending.write_text(json.dumps({"schema_version": 1,
                                                  "operation": "unknown",
                                                  "source_commit": self.source}))
        if os.name != "nt":
            self.paths.pending.chmod(0o600)
        with self.assertRaisesRegex(maintenance.MaintenanceError, "journal is invalid"):
            maintenance.install(self.paths, UID)
        self.assertEqual(self.paths.installed[maintenance.TOOL_FILES[0]].read_bytes(),
                         self.new[maintenance.TOOL_FILES[0]])

    def test_identity_record_is_bound_to_exact_v051_provenance(self):
        self.assertEqual(maintenance.install(self.paths, UID), "installed")
        altered = self.paths.provenance.read_bytes().replace(b"version=0.5.1", b"version=9.9.9")
        self.paths.provenance.write_bytes(altered)
        with self.assertRaises(maintenance.MaintenanceError):
            maintenance.rollback(self.paths, UID)
        for name in maintenance.TOOL_FILES:
            self.assertEqual(self.paths.installed[name].read_bytes(), self.new[name])


if __name__ == "__main__":
    unittest.main()
