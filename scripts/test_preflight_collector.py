import importlib.util
import hashlib
from importlib.machinery import SourceFileLoader
import os
from pathlib import Path
import shutil
import sqlite3
import tarfile
import tempfile
import unittest
from contextlib import closing
from types import SimpleNamespace
from unittest import SkipTest


ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "deploy" / "kitsusync-preflight-collect"
SPEC = importlib.util.spec_from_loader(
    "kitsusync_preflight_collector", SourceFileLoader("kitsusync_preflight_collector", str(SOURCE))
)
collector = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(collector)
TEST_UID = getattr(os, "getuid", lambda: 0)()


class CollectorTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.paths = collector.Paths.for_fixture(self.root)
        for path in (self.paths.runtime_data, self.paths.backup_root,
                     self.paths.state_root, self.paths.control_root,
                     self.paths.release_stage, self.paths.docker_root):
            path.mkdir(parents=True, exist_ok=True)
        self.db = self.paths.runtime_data / "sqlite.db"
        with closing(sqlite3.connect(self.db)) as db:
            db.execute("CREATE TABLE fixture (value TEXT)")
            db.execute("INSERT INTO fixture VALUES ('private-marker')")
            db.commit()
        os.chmod(self.db, 0o600)

    def tearDown(self):
        self.temp.cleanup()

    def test_valid_runtime_database_is_read_only_and_secret_safe(self):
        before = self.db.read_bytes()
        result = collector.inspect_database(self.db, expected_uid=TEST_UID)
        self.assertEqual(result, {"size_bytes": len(before), "integrity": "ok"})
        self.assertEqual(self.db.read_bytes(), before)
        self.assertNotIn("private-marker", repr(result))

    def test_missing_database_is_blocked(self):
        result = collector.inspect_database(self.root / "missing.db", expected_uid=TEST_UID)
        self.assertEqual(result["integrity"], "unknown")

    def test_missing_backup_inventory_is_blocked(self):
        self.assertIsNone(collector.newest_backup(self.paths.backup_root, TEST_UID))

    def test_corrupt_database_fails_integrity(self):
        self.db.write_bytes(b"not a sqlite database")
        result = collector.inspect_database(self.db, expected_uid=TEST_UID)
        self.assertEqual(result["integrity"], "failed")

    def test_symlink_database_is_blocked(self):
        linked = self.root / "linked.db"
        try:
            linked.symlink_to(self.db)
        except OSError as error:
            if os.name == "nt":
                raise SkipTest("Windows account lacks symlink privilege; Linux CI exercises O_NOFOLLOW") from error
            raise
        result = collector.inspect_database(linked, expected_uid=TEST_UID)
        self.assertEqual(result["integrity"], "unknown")

    def test_uncheckpointed_wal_does_not_claim_integrity(self):
        Path(str(self.db) + "-wal").write_bytes(b"pending-wal")
        os.chmod(Path(str(self.db) + "-wal"), 0o600)
        result = collector.inspect_database(self.db, expected_uid=TEST_UID)
        self.assertEqual(result["integrity"], "unknown")

    def test_existing_sqlite_sidecars_are_not_opened(self):
        Path(str(self.db) + "-wal").write_bytes(b"wal")
        Path(str(self.db) + "-shm").write_bytes(b"shm")
        os.chmod(Path(str(self.db) + "-wal"), 0o600)
        os.chmod(Path(str(self.db) + "-shm"), 0o600)
        before = {suffix: Path(str(self.db) + suffix).read_bytes() for suffix in ("-wal", "-shm")}
        result = collector.inspect_database(self.db, expected_uid=TEST_UID)
        self.assertEqual(result["integrity"], "unknown")
        self.assertEqual(before, {suffix: Path(str(self.db) + suffix).read_bytes() for suffix in ("-wal", "-shm")})

    def test_backup_validation_failure_does_not_echo_file_content(self):
        backup = self.paths.backup_root / "20261012T000000Z-abcdef012345"
        backup.mkdir()
        (backup / "sqlite.db").write_text("secret-cookie-marker")
        result = collector.validate_backup_directory(backup, expected_uid=TEST_UID)
        self.assertFalse(result)
        self.assertNotIn("secret-cookie-marker", repr(result))

    def _complete_backup(self):
        backup = self.paths.backup_root / "20261012T000000Z-abcdef012345"
        backup.mkdir()
        with closing(sqlite3.connect(backup / "sqlite.db")) as db:
            db.execute("CREATE TABLE fixture (id INTEGER)")
            db.execute("INSERT INTO fixture VALUES (1)")
            db.commit()
        (backup / "conf.toml").write_text("safe fixture configuration")
        (backup / "operator.env").write_text("SAFE_FIXTURE=1\n")
        (backup / "runtime-secret.absent").write_bytes(b"")
        with tarfile.open(backup / "templates.tar", "w:") as archive:
            root = tarfile.TarInfo("tpl/")
            root.type = tarfile.DIRTYPE
            root.mode = 0o755
            archive.addfile(root)
        (backup / "backup-complete").write_text("")
        names = ("sqlite.db", "conf.toml", "templates.tar", "operator.env", "runtime-secret.absent")
        lines = []
        for name in names:
            path = backup / name
            os.chmod(path, 0o600)
            lines.append(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {name}")
        (backup / "persistent-state.sha256").write_text("\n".join(lines) + "\n")
        os.chmod(backup / "persistent-state.sha256", 0o600)
        os.chmod(backup, 0o700)
        return backup

    def test_complete_backup_passes_read_only_validation(self):
        backup = self._complete_backup()
        before = {path.name: path.read_bytes() for path in backup.iterdir() if path.is_file()}
        self.assertTrue(collector.validate_backup_directory(backup, expected_uid=TEST_UID))
        after = {path.name: path.read_bytes() for path in backup.iterdir() if path.is_file()}
        self.assertEqual(before, after)

    def test_backup_hash_mismatch_fails_closed(self):
        backup = self._complete_backup()
        (backup / "operator.env").write_text("private-token-marker")
        result = collector.validate_backup_directory(backup, expected_uid=TEST_UID)
        self.assertFalse(result)
        self.assertNotIn("private-token-marker", repr(result))

    def test_tool_baseline_mismatch_is_false(self):
        tools = {"deployment_tool_sha256": self.root / "deploy",
                 "inspection_tool_sha256": self.root / "inspect",
                 "sqlite_backup_tool_sha256": self.root / "backup",
                 "image_identity_tool_sha256": self.root / "identity",
                 "runtime_state_tool_sha256": self.root / "runtime-state",
                 "restore_state_tool_sha256": self.root / "restore-state"}
        provenance = self.paths.control_root / "provenance"
        lines = []
        for index, (key, path) in enumerate(tools.items(), start=1):
            path.write_bytes(f"tool-{index}".encode())
            os.chmod(path, 0o600)
            lines.append(f"{key}={hashlib.sha256(path.read_bytes()).hexdigest()}")
        provenance.write_text("\n".join(lines) + "\n")
        os.chmod(provenance, 0o600)
        self.assertTrue(all(collector.inspect_tools(provenance, tools, TEST_UID).values()))
        tools["deployment_tool_sha256"].write_text("substituted")
        self.assertFalse(collector.inspect_tools(provenance, tools, TEST_UID)["wrapper_matches_baseline"])
        tools["deployment_tool_sha256"].write_bytes(b"tool-1")
        tools["restore_state_tool_sha256"].write_text("substituted-helper")
        self.assertFalse(collector.inspect_tools(provenance, tools, TEST_UID)["helpers_match_baseline"])

    def test_missing_or_mismatched_rollback_image_fails_closed(self):
        image_id = "sha256:" + "a" * 64
        ref = "kitsusync:rollback-20261012T000000Z-abcdef012345"
        missing = lambda *args, **kwargs: SimpleNamespace(returncode=1, stdout="")
        wrong = lambda *args, **kwargs: SimpleNamespace(returncode=0, stdout="sha256:" + "b" * 64 + "\n")
        present = lambda *args, **kwargs: SimpleNamespace(returncode=0, stdout=image_id + "\n")
        self.assertFalse(collector.rollback_image_retained(ref, image_id, missing))
        self.assertFalse(collector.rollback_image_retained(ref, image_id, wrong))
        self.assertTrue(collector.rollback_image_retained(ref, image_id, present))

    def test_complete_rollback_unit_and_tampering(self):
        backup = self.paths.backup_root / "20261012T000000Z-abcdef012345"
        backup.mkdir()
        runtime = backup / "runtime"
        runtime.mkdir()
        (runtime / "inspect.json").write_text("{}")
        (runtime / "plan.json").write_text("{}")
        for name in collector.RUNTIME_ROOT_FILES:
            if name not in {"runtime-state.sha256", "compose.sha256", "env.sha256"}:
                (backup / name).write_text("fixture")
        compose = backup / "production-compose.yml"
        env = backup / "operator.env"
        compose.write_text("services: {}\n")
        env.write_text("SAFE_FIXTURE=1\n")
        for manifest_name, path in (("compose.sha256", compose), ("env.sha256", env)):
            digest = hashlib.sha256(path.read_bytes()).hexdigest()
            (backup / manifest_name).write_text(f"{digest}  {path}\n")
        hashed_files = [runtime / "inspect.json", runtime / "plan.json"]
        hashed_files.extend(backup / name for name in collector.RUNTIME_HASHED_ROOT_FILES)
        lines = [f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path}"
                 for path in hashed_files]
        (backup / "runtime-state.sha256").write_text("\n".join(lines) + "\n")
        for path in backup.rglob("*"):
            if path.is_file():
                os.chmod(path, 0o600)
        os.chmod(runtime, 0o700)
        os.chmod(backup, 0o700)
        self.assertTrue(collector.validate_rollback_unit(backup, TEST_UID))
        (runtime / "plan.json").write_text("substituted")
        self.assertFalse(collector.validate_rollback_unit(backup, TEST_UID))

    def _complete_recovery_bundle(self, mode="recovery"):
        stage = self.paths.release_stage
        stage.mkdir(exist_ok=True)
        payload = {filename: f"fixture:{filename}".encode()
                   for filename in collector.PROVENANCE_DIGESTS.values()}
        config = "sha256:" + "a" * 64
        manifest = "sha256:" + "b" * 64
        content = "sha256:" + "c" * 64
        payload["kitsusync-image-identity"] = (
            "import sys\nprint('image_config_digest=" + config + "')\n"
            "print('image_manifest_digest=" + manifest + "')\n"
            "print('image_content_digest=" + content + "')\n"
        ).encode()
        for filename, data in payload.items():
            path = stage / filename
            path.write_bytes(data)
            os.chmod(path, 0o600)
        commit = "f8a32bbba94d84068d9e96c18554ac4ab7b24540"
        values = {
            "artifact_kind": "release", "version": "0.5.1", "release_tag": "v0.5.1",
            "source_commit": commit, "source_id": commit, "release_commit": commit,
            "image_ref": "kitsusync:v0.5.1", "image_config_digest": config,
            "image_manifest_digest": manifest, "image_content_digest": content,
        }
        for key, filename in collector.PROVENANCE_DIGESTS.items():
            values[key] = hashlib.sha256((stage / filename).read_bytes()).hexdigest()
        (stage / "deployment-mode").write_text(mode + "\n")
        (stage / "provenance.txt").write_text("".join(f"{key}={value}\n" for key, value in values.items()))
        for name in ("deployment-mode", "provenance.txt"):
            os.chmod(stage / name, 0o600)
        os.chmod(stage, 0o700)
        return stage

    @staticmethod
    def _trusted_identity_result(*_args, **_kwargs):
        return SimpleNamespace(returncode=0, stdout=(
            "image_config_digest=sha256:" + "a" * 64 + "\n"
            "image_manifest_digest=sha256:" + "b" * 64 + "\n"
            "image_content_digest=sha256:" + "c" * 64 + "\n"))

    def test_recovery_bundle_matches_schema_and_source(self):
        stage = self._complete_recovery_bundle()
        result = collector.inspect_bundle(stage, TEST_UID, identity_runner=self._trusted_identity_result)
        self.assertTrue(result["source_matches_release"])
        self.assertTrue(result["digest_verified"])
        self.assertTrue(result["provenance_verified"])
        self.assertEqual(result["mode"], "recovery")
        self.assertNotIn("image_content_digest", repr(result))

    def test_normal_bundle_is_recognized_for_evaluator_rejection(self):
        result = collector.inspect_bundle(self._complete_recovery_bundle("normal"), TEST_UID,
                                          identity_runner=self._trusted_identity_result)
        self.assertEqual(result["mode"], "normal")

    def test_bundle_identity_script_is_never_executed_without_trusted_runner(self):
        stage = self._complete_recovery_bundle()
        marker = self.root / "executed"
        (stage / "kitsusync-image-identity").write_text(
            f"from pathlib import Path\nPath({str(marker)!r}).write_text('ran')\n")
        provenance = stage / "provenance.txt"
        lines = provenance.read_text().splitlines()
        identity_key = "image_identity_tool_sha256="
        identity_hash = hashlib.sha256((stage / "kitsusync-image-identity").read_bytes()).hexdigest()
        provenance.write_text("\n".join(
            identity_key + identity_hash if line.startswith(identity_key) else line for line in lines
        ) + "\n")
        result = collector.inspect_bundle(stage, TEST_UID)
        self.assertFalse(result["provenance_verified"])
        self.assertFalse(marker.exists())

    def test_bundle_hash_tampering_fails_closed(self):
        stage = self._complete_recovery_bundle()
        with (stage / "kitsusync-image.tar").open("ab") as stream:
            stream.write(b"tampered")
        result = collector.inspect_bundle(stage, TEST_UID)
        self.assertFalse(result["digest_verified"])
        self.assertFalse(result["provenance_verified"])

    def test_unavailable_capacity_is_not_fabricated(self):
        result = collector.peak_capacity(None, None)
        self.assertEqual(result, {})

    def test_capacity_shortfall_is_reported_without_raw_paths(self):
        result = collector.peak_capacity(99, 100)
        self.assertEqual(result, {"available_bytes": 99, "required_bytes": 100})


if __name__ == "__main__":
    unittest.main()
