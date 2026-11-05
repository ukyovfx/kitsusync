import importlib.util
import hashlib
from importlib.machinery import SourceFileLoader
import io
import json
import os
from pathlib import Path
import shutil
import sqlite3
import tarfile
import tempfile
import unittest
from contextlib import closing, redirect_stdout
from types import SimpleNamespace
from unittest import SkipTest, mock


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
        if os.name != "nt":
            os.chmod(self.paths.backup_root, 0o700)
        self.db = self.paths.runtime_data / "sqlite.db"
        with closing(sqlite3.connect(self.db)) as db:
            db.execute("CREATE TABLE fixture (value TEXT)")
            db.execute("INSERT INTO fixture VALUES ('private-marker')")
            for table, columns in collector.V049_REQUIRED_COLUMNS.items():
                declaration = ", ".join(f'"{name}" TEXT' for name in sorted(columns))
                db.execute(f'CREATE TABLE "{table}" ({declaration})')
            db.commit()
        os.chmod(self.db, 0o600)

    def tearDown(self):
        self.temp.cleanup()

    def test_valid_runtime_database_is_read_only_and_secret_safe(self):
        before = self.db.read_bytes()
        result = collector.inspect_database(self.db, expected_uid=TEST_UID)
        self.assertEqual(result, {"size_bytes": len(before), "integrity": "ok",
                                  "migration_compatibility": "v0.4.9_to_v0.5.1"})
        self.assertEqual(self.db.read_bytes(), before)
        self.assertNotIn("private-marker", repr(result))

    def test_database_integrity_opens_the_validated_file_descriptor(self):
        opened_uris = []
        fake_db = mock.MagicMock()
        fake_db.execute.return_value.fetchall.return_value = [("ok",)]

        def record_uri(database_uri, **kwargs):
            opened_uris.append(database_uri)
            return fake_db

        with (mock.patch.object(collector.sys, "platform", "linux"),
              mock.patch.object(collector.sqlite3, "connect", side_effect=record_uri)):
            result = collector.inspect_database(self.db, expected_uid=TEST_UID)

        self.assertEqual(result["integrity"], "ok")
        self.assertEqual(len(opened_uris), 1)
        self.assertRegex(opened_uris[0], r"^file:/proc/self/fd/[0-9]+\?immutable=1&mode=ro$")

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

    def test_live_sqlite_wal_is_left_untouched_and_not_claimed_as_integral(self):
        connection = sqlite3.connect(self.db)
        try:
            self.assertEqual(connection.execute("PRAGMA journal_mode=WAL").fetchone()[0].lower(), "wal")
            connection.execute("INSERT INTO fixture VALUES ('wal-only-row')")
            connection.commit()
            sidecars = [Path(str(self.db) + suffix) for suffix in ("-wal", "-shm")]
            self.assertTrue(all(path.exists() for path in sidecars))
            before = {path.name: path.read_bytes() for path in sidecars}
            result = collector.inspect_database(self.db, expected_uid=TEST_UID)
            self.assertEqual(result["integrity"], "unknown")
            self.assertEqual(before, {path.name: path.read_bytes() for path in sidecars})
        finally:
            connection.close()

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
        os.chmod(backup / "backup-complete", 0o600)
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
        inventory = []
        for path in (backup, *backup.iterdir()):
            info = path.lstat()
            inventory.append((path.name, info.st_uid, info.st_gid,
                              oct(info.st_mode & 0o777), info.st_size))
        self.assertTrue(collector.validate_backup_directory(backup, expected_uid=TEST_UID),
                        repr(inventory))
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
        self.assertTrue(collector.rollback_image_retained(ref + "-AbC123x9", image_id, present))

    def _inventory_release_provenance(self):
        source = "a" * 40
        values = {
            "artifact_kind": "release", "version": "0.5.1", "release_tag": "v0.5.1",
            "source_commit": source, "source_id": source, "release_commit": source,
            "build_daemon_image_id": "sha256:" + "b" * 64,
            "image_ref": "kitsusync:v0.5.1",
            "image_config_digest": "sha256:" + "c" * 64,
            "image_manifest_digest": "sha256:" + "d" * 64,
            "image_content_digest": "sha256:" + "e" * 64,
        }
        values.update({key: "f" * 64 for key in collector.PROVENANCE_DIGESTS})
        path = self.paths.control_root / "provenance"
        path.write_text("".join(f"{key}={value}\n" for key, value in values.items()))
        os.chmod(path, 0o600)

    def _inventory_rollback_backup(self, image_id):
        backup = self._complete_backup()
        runtime = backup / "runtime"
        runtime.mkdir()
        (runtime / "plan.json").write_text("{}\n")
        (backup / "container-id").write_text("c" * 64 + "\n")
        (backup / "previous-image").write_text("kitsusync:v0.5.1\n")
        (backup / "previous-image-id").write_text(image_id + "\n")
        for name in ("previous-revision", "previous-source-id", "previous-version", "prior-validation",
                     "network", "target-provenance", "target.override.yml", "target-compose.json",
                     "backup-complete"):
            (backup / name).write_text("fixture\n")
        (backup / "production-compose.yml").write_text("services: {}\n")
        (backup / "compose.sha256").write_text(
            f"{hashlib.sha256((backup / 'production-compose.yml').read_bytes()).hexdigest()}  production-compose.yml\n")
        (backup / "env.sha256").write_text(
            f"{hashlib.sha256((backup / 'operator.env').read_bytes()).hexdigest()}  operator.env\n")
        backup_name = backup.name
        (backup / "rollback-image-ref").write_text(f"kitsusync:rollback-{backup_name}\n")
        hashed = [runtime / "plan.json"] + [backup / name for name in collector.RUNTIME_HASHED_ROOT_FILES]
        (backup / "runtime-state.sha256").write_text("".join(
            f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path}\n" for path in hashed))
        for path in backup.rglob("*"):
            if path.is_file():
                os.chmod(path, 0o600)
        os.chmod(runtime, 0o700)
        os.chmod(backup, 0o700)
        return backup

    def test_image_inventory_matches_full_ids_containers_and_verified_rollback(self):
        self._inventory_release_provenance()
        common_prefix = "a" * 12
        running_id = "sha256:" + common_prefix + "1" * 52
        stopped_id = "sha256:" + common_prefix + "2" * 52
        rollback_id, unused_id, release_id = ["sha256:" + value * 64 for value in "345"]
        running_container, stopped_container = "a" * 64, "b" * 64
        backup = self._inventory_rollback_backup(rollback_id)
        tags = ["ci-" + value * 40 for value in "12345"]
        image_rows = list(zip(tags, (running_id, rollback_id, unused_id, release_id, stopped_id)))
        image_rows.append(("v0.5.1", release_id))
        image_output = "".join(json.dumps({"Repository": "kitsusync", "Tag": tag, "ID": image_id}) + "\n"
                              for tag, image_id in image_rows)

        def docker(args, **_kwargs):
            if args[1:4] == ["image", "ls", "--no-trunc"]:
                return SimpleNamespace(returncode=0, stdout=image_output)
            if args[1:5] == ["ps", "--all", "--quiet", "--no-trunc"]:
                return SimpleNamespace(returncode=0, stdout=f"{running_container}\n{stopped_container}\n")
            if args[1] == "inspect":
                self.assertEqual(args[3], "{{.Id}}|{{.Image}}|{{.State.Status}}")
                self.assertEqual(args[4:], [running_container, stopped_container])
                return SimpleNamespace(returncode=0, stdout=(
                    f"{running_container}|{running_id}|running\n"
                    f"{stopped_container}|{stopped_id}|exited\n"))
            if args[1:4] == ["image", "inspect", "--format"]:
                self.assertEqual(args[-1], f"kitsusync:rollback-{backup.name}")
                return SimpleNamespace(returncode=0, stdout=rollback_id + "\n")
            raise AssertionError(f"unexpected Docker command: {args[1:5]}")

        result = collector.image_inventory(self.paths, TEST_UID, docker_runner=docker,
                                           backup_verifier=lambda _backup: True)
        self.assertEqual(result["schema_version"], 1)
        self.assertEqual(result["status"], "VERIFIED")
        rows = {row["tag"]: row for row in result["images"]}
        self.assertEqual(rows[f"kitsusync:{tags[0]}"]["classification"], "KEEP")
        self.assertEqual(rows[f"kitsusync:{tags[0]}"]["image_id"], running_id)
        self.assertEqual(rows[f"kitsusync:{tags[0]}"]["containers"], [
            {"container_id": running_container, "state": "running"}])
        self.assertEqual(rows[f"kitsusync:{tags[0]}"]["image_id"][:19],
                         rows[f"kitsusync:{tags[4]}"]["image_id"][:19])
        self.assertNotEqual(rows[f"kitsusync:{tags[0]}"]["image_id"],
                            rows[f"kitsusync:{tags[4]}"]["image_id"])
        self.assertEqual(rows[f"kitsusync:{tags[1]}"]["classification"], "KEEP")
        self.assertEqual(rows[f"kitsusync:{tags[1]}"]["rollback_refs"], [f"kitsusync:rollback-{backup.name}"])
        self.assertEqual(rows[f"kitsusync:{tags[2]}"]["classification"], "DELETE_CANDIDATE")
        self.assertEqual(rows[f"kitsusync:{tags[3]}"]["classification"], "KEEP")
        self.assertEqual(rows[f"kitsusync:{tags[3]}"]["release_refs"], ["kitsusync:v0.5.1"])
        self.assertEqual(rows[f"kitsusync:{tags[4]}"]["classification"], "KEEP")
        self.assertEqual(rows[f"kitsusync:{tags[4]}"]["containers"], [
            {"container_id": stopped_container, "state": "exited"}])

    def test_image_inventory_inspect_anomalies_fail_closed(self):
        self._inventory_release_provenance()
        rollback_id, candidate_id = "sha256:" + "6" * 64, "sha256:" + "7" * 64
        backup = self._inventory_rollback_backup(rollback_id)
        tag = "ci-" + "8" * 40
        image_output = json.dumps({"Repository": "kitsusync", "Tag": tag, "ID": candidate_id}) + "\n"
        first_container, second_container = "c" * 64, "d" * 64

        for anomaly in ("malformed", "id_mismatch", "duplicate_id"):
            def docker(args, **_kwargs):
                if args[1:4] == ["image", "ls", "--no-trunc"]:
                    return SimpleNamespace(returncode=0, stdout=image_output)
                if args[1:5] == ["ps", "--all", "--quiet", "--no-trunc"]:
                    ids = ([first_container, second_container] if anomaly == "malformed" else
                           [first_container] if anomaly == "id_mismatch" else
                           [first_container, first_container])
                    return SimpleNamespace(returncode=0, stdout="".join(f"{item}\n" for item in ids))
                if args[1:4] == ["image", "inspect", "--format"]:
                    self.assertEqual(args[-1], f"kitsusync:rollback-{backup.name}")
                    return SimpleNamespace(returncode=0, stdout=rollback_id + "\n")
                if args[1] == "inspect":
                    if anomaly == "malformed":
                        return SimpleNamespace(returncode=0, stdout=f"short|{candidate_id}|exited\n")
                    if anomaly == "id_mismatch":
                        return SimpleNamespace(returncode=0, stdout=f"{second_container}|{candidate_id}|running\n")
                    return SimpleNamespace(returncode=0, stdout=(
                        f"{first_container}|{candidate_id}|running\n"
                        f"{first_container}|{candidate_id}|exited\n"))
                raise AssertionError(f"unexpected Docker command: {args[1:5]}")

            result = collector.image_inventory(self.paths, TEST_UID, docker_runner=docker,
                                               backup_verifier=lambda _backup: True)
            self.assertEqual(result["status"], "UNKNOWN", anomaly)
            self.assertNotEqual(result["images"][0]["classification"], "DELETE_CANDIDATE", anomaly)

    def test_image_inventory_malformed_manifest_prevents_delete_candidate(self):
        self._inventory_release_provenance()
        rollback_id, candidate_id = "sha256:" + "1" * 64, "sha256:" + "2" * 64
        backup = self._inventory_rollback_backup(rollback_id)
        with (backup / "runtime-state.sha256").open("a") as stream:
            stream.write("tampered\n")
        tag = "ci-" + "3" * 40
        image_output = json.dumps({"Repository": "kitsusync", "Tag": tag, "ID": candidate_id}) + "\n"

        def docker(args, **_kwargs):
            if args[1:4] == ["image", "ls", "--no-trunc"]:
                return SimpleNamespace(returncode=0, stdout=image_output)
            if args[1:5] == ["ps", "--all", "--quiet", "--no-trunc"]:
                return SimpleNamespace(returncode=0, stdout="")
            raise AssertionError(f"unexpected Docker command: {args[1:5]}")

        result = collector.image_inventory(self.paths, TEST_UID, docker_runner=docker,
                                           backup_verifier=lambda _backup: True)
        self.assertEqual(result["status"], "UNKNOWN")
        self.assertEqual(result["images"][0]["classification"], "UNKNOWN")

    def test_image_inventory_invalid_ids_and_docker_failure_fail_closed(self):
        self._inventory_release_provenance()
        tag = "ci-" + "4" * 40
        malformed = json.dumps({"Repository": "kitsusync", "Tag": tag, "ID": "short"}) + "\n"

        def docker(args, **_kwargs):
            if args[1:4] == ["image", "ls", "--no-trunc"]:
                return SimpleNamespace(returncode=0, stdout=malformed)
            return SimpleNamespace(returncode=1, stdout="")

        result = collector.image_inventory(self.paths, TEST_UID, docker_runner=docker)
        self.assertEqual(result["status"], "UNKNOWN")
        self.assertEqual(result["images"][0]["image_id"], None)
        self.assertEqual(result["images"][0]["classification"], "UNKNOWN")

    def test_collector_no_argument_preflight_contract_is_unchanged(self):
        output = io.StringIO()
        with (mock.patch.object(collector.os, "geteuid", return_value=0, create=True),
              mock.patch.object(collector.sys, "argv", [str(SOURCE)]),
              mock.patch.object(collector.Paths, "production", return_value=self.paths),
              mock.patch.object(collector, "collect", return_value={"schema_version": 1}),
              redirect_stdout(output)):
            self.assertEqual(collector.main(), 0)
        self.assertEqual(output.getvalue(), '{"schema_version":1}\n')

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
        # New wrappers persist only the reconstruction plan. It is sufficient
        # for rollback and avoids a second copy of secret-bearing inspect data.
        (runtime / "inspect.json").unlink()
        lines = [f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path}"
                 for path in [runtime / "plan.json"]
                 + [backup / name for name in collector.RUNTIME_HASHED_ROOT_FILES]]
        (backup / "runtime-state.sha256").write_text("\n".join(lines) + "\n")
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

    def test_capacity_contract_reports_each_filesystem_and_legacy_safe_bound(self):
        roots = {
            "runtime": self.paths.runtime_data,
            "backup": self.paths.backup_root,
            "release_stage": self.paths.release_stage,
            "docker": self.paths.docker_root,
        }
        available = {"data": 2_000, "backups": 900, "release-stage": 700, "docker": 4_000}

        def fake_statvfs(path):
            return SimpleNamespace(f_bavail=available[path.name], f_frsize=1)

        result = collector.capacity_by_filesystem(
            {"runtime": 1_000, "backup": 800, "release_stage": 0, "docker": 2_000},
            roots,
            statvfs=fake_statvfs,
            device_id=lambda path: {"data": 1, "backups": 2, "release-stage": 3, "docker": 4}[path.name],
        )
        self.assertEqual(result["filesystems"], [
            {"roles": ["backup"], "available_bytes": 900, "required_bytes": 800},
            {"roles": ["docker"], "available_bytes": 4_000, "required_bytes": 2_000},
            {"roles": ["release_stage"], "available_bytes": 700, "required_bytes": 0},
            {"roles": ["runtime"], "available_bytes": 2_000, "required_bytes": 1_000},
        ])
        self.assertEqual(result["legacy"], {"available_bytes": 700, "required_bytes": 2_000})
        self.assertNotIn(str(self.root), repr(result))

    def test_capacity_aggregates_roles_on_same_device_and_fails_closed_on_unknown(self):
        roots = {"runtime": self.paths.runtime_data, "backup": self.paths.backup_root}

        def same_device(_path):
            return SimpleNamespace(f_bavail=3_000, f_frsize=1)

        result = collector.capacity_by_filesystem(
            {"runtime": 1_000, "backup": 1_500}, roots, statvfs=same_device,
            device_id=lambda _path: 7)
        self.assertEqual(result["filesystems"], [
            {"roles": ["backup", "runtime"], "available_bytes": 3_000, "required_bytes": 2_500}
        ])
        self.assertEqual(result["legacy"], {"available_bytes": 3_000, "required_bytes": 2_500})
        self.assertEqual(collector.capacity_by_filesystem(
            {"runtime": None, "backup": 1}, roots, statvfs=same_device, device_id=lambda _path: 7), {
                "filesystems": [{"roles": ["backup", "runtime"], "available_bytes": 3_000,
                                 "required_bytes": 1, "status": "unknown"}]
            })

    def test_capacity_shortfall_on_one_filesystem_remains_visible(self):
        roots = {"runtime": self.paths.runtime_data, "backup": self.paths.backup_root}
        available = {"data": 5_000, "backups": 500}
        result = collector.capacity_by_filesystem(
            {"runtime": 1_000, "backup": 800}, roots,
            statvfs=lambda path: SimpleNamespace(f_bavail=available[path.name], f_frsize=1),
            device_id=lambda path: 1 if path.name == "data" else 2)
        self.assertEqual(result["filesystems"], [
            {"roles": ["backup"], "available_bytes": 500, "required_bytes": 800},
            {"roles": ["runtime"], "available_bytes": 5_000, "required_bytes": 1_000},
        ])
        self.assertEqual(result["legacy"], {"available_bytes": 500, "required_bytes": 1_000})

    def test_image_layer_capacity_uses_only_verified_v051_layer_members(self):
        archive_path = self.root / "kitsusync-image.tar"
        layer = b"synthetic docker layer bytes"
        with tarfile.open(archive_path, "w:") as archive:
            manifest = json.dumps([{"RepoTags": ["kitsusync:v0.5.1"], "Layers": ["abc/layer.tar"]}]).encode()
            info = tarfile.TarInfo("manifest.json")
            info.size = len(manifest)
            archive.addfile(info, io.BytesIO(manifest))
            info = tarfile.TarInfo("abc/layer.tar")
            info.size = len(layer)
            archive.addfile(info, io.BytesIO(layer))
        os.chmod(archive_path, 0o600)
        self.assertEqual(collector.image_layer_bytes(archive_path, TEST_UID), len(layer))

        with tarfile.open(archive_path, "w:") as archive:
            manifest = json.dumps([{"RepoTags": ["kitsusync:v0.5.1"], "Layers": ["../escape.tar"]}]).encode()
            info = tarfile.TarInfo("manifest.json")
            info.size = len(manifest)
            archive.addfile(info, io.BytesIO(manifest))
            info = tarfile.TarInfo("escape.tar")
            info.size = len(layer)
            archive.addfile(info, io.BytesIO(layer))
        os.chmod(archive_path, 0o600)
        self.assertIsNone(collector.image_layer_bytes(archive_path, TEST_UID))

    def test_database_is_not_identified_without_verified_production_mount(self):
        self.assertFalse(collector.production_database_mount_verified(
            self.paths.runtime_data,
            runner=lambda *args, **kwargs: SimpleNamespace(returncode=1, stdout="")))

    def test_database_mount_check_requires_exact_unique_writable_bind(self):
        mounts = [{"Type": "bind", "Source": str(self.paths.runtime_data.resolve()),
                   "Destination": "/app/data", "RW": True}]
        def runner(args, **kwargs):
            if args[1] == "ps":
                return SimpleNamespace(returncode=0, stdout="a" * 64 + "\n")
            return SimpleNamespace(returncode=0, stdout=__import__("json").dumps(mounts))
        self.assertTrue(collector.production_database_mount_verified(self.paths.runtime_data, runner=runner))
        mounts[0]["Source"] = str(self.root / "other")
        self.assertFalse(collector.production_database_mount_verified(self.paths.runtime_data, runner=runner))
        mounts[0]["Source"] = str(self.paths.runtime_data.resolve())
        mounts.append(dict(mounts[0]))
        self.assertFalse(collector.production_database_mount_verified(self.paths.runtime_data, runner=runner))

    def test_maintenance_provenance_accepts_only_matching_inspector_and_collector(self):
        old_inspect = self.root / "old-inspect"
        current_inspect = self.root / "current-inspect"
        current_collector = self.root / "current-collector"
        deploy_tool = self.root / "deploy-tool"
        provenance = self.root / "provenance"
        identity = self.root / "inspection-maintenance.json"
        old_inspect.write_bytes(b"v0.5.1 inspector")
        current_inspect.write_bytes(b"maintained inspector")
        current_collector.write_bytes(b"maintained collector")
        deploy_tool.write_bytes(b"approved deploy tool")
        for path in (old_inspect, current_inspect, current_collector, deploy_tool):
            os.chmod(path, 0o600)
        provenance.write_text(
            f"deployment_tool_sha256={hashlib.sha256(deploy_tool.read_bytes()).hexdigest()}\n"
            f"inspection_tool_sha256={hashlib.sha256(old_inspect.read_bytes()).hexdigest()}\n",
            encoding="ascii")
        os.chmod(provenance, 0o600)
        record = {"schema_version": 1, "baseline_release": "v0.5.1", "source_commit": "a" * 40,
                  "inspection_tool_sha256": hashlib.sha256(current_inspect.read_bytes()).hexdigest(),
                  "preflight_collector_tool_sha256": hashlib.sha256(current_collector.read_bytes()).hexdigest()}
        identity.write_text(json.dumps(record), encoding="ascii")
        os.chmod(identity, 0o600)
        result = collector.inspect_tools(
            provenance, {"deployment_tool_sha256": deploy_tool,
                         "inspection_tool_sha256": current_inspect}, TEST_UID,
            identity, current_collector)
        self.assertTrue(result["wrapper_matches_baseline"])
        self.assertTrue(result["helpers_match_baseline"])
        self.assertEqual(result["inspection_maintenance"], "verified")
        record["preflight_collector_tool_sha256"] = "0" * 64
        identity.write_text(json.dumps(record), encoding="ascii")
        self.assertFalse(collector.inspect_tools(
            provenance, {"deployment_tool_sha256": deploy_tool,
                         "inspection_tool_sha256": current_inspect}, TEST_UID,
            identity, current_collector)["helpers_match_baseline"])


if __name__ == "__main__":
    unittest.main()
