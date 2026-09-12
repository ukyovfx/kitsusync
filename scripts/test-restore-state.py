#!/usr/bin/env python3
"""Run as root in isolated Linux CI; never targets host application paths."""

import hashlib
import importlib.machinery
import importlib.util
import io
import os
from pathlib import Path
import pwd
import grp
import sqlite3
import stat
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock


HELPER = Path(__file__).resolve().parents[1] / "deploy" / "kitsusync-restore-state"
loader = importlib.machinery.SourceFileLoader("restore_state", str(HELPER))
spec = importlib.util.spec_from_loader(loader.name, loader)
restore_state = importlib.util.module_from_spec(spec)
loader.exec_module(restore_state)


class RestoreTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="kitsusync-restore-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.backup = self.root / "backup"
        self.runtime = self.root / "runtime"
        self.backup.mkdir(mode=0o700)
        self.runtime.mkdir(mode=0o755)
        (self.runtime / "data").mkdir(mode=0o700)
        os.chown(self.runtime / "data", 10001, 10001)
        (self.runtime / "tpl").mkdir(mode=0o755)
        (self.runtime / "tpl" / "old.html").write_bytes(b"old template")
        (self.runtime / "conf.toml").write_bytes(b"old configuration")
        for name in ("sqlite.db", "runtime-secret.key", "sqlite.db-wal", "sqlite.db-shm"):
            path = self.runtime / "data" / name
            path.write_bytes(("old " + name).encode())
            os.chown(path, 10001, 10001)
            path.chmod(0o600)
        (self.runtime / "data" / "unrelated").write_bytes(b"preserve me")
        with sqlite3.connect(self.backup / "sqlite.db") as connection:
            connection.execute("CREATE TABLE proof(value TEXT)")
            connection.execute("INSERT INTO proof VALUES ('backup')")
        (self.backup / "runtime-secret.key").write_bytes(b"fixture-not-a-real-secret")
        (self.backup / "conf.toml").write_bytes(b"fixture configuration")
        (self.backup / "operator.env").write_bytes(b"APP_ENV=production\n")
        (self.backup / "backup-complete").touch()
        self.tar()
        self.manifest()

    def tar(self, unsafe=None):
        with tarfile.open(self.backup / "templates.tar", "w") as archive:
            directory = tarfile.TarInfo("tpl")
            directory.type = tarfile.DIRTYPE
            directory.mode = 0o755
            archive.addfile(directory)
            entry = tarfile.TarInfo(unsafe or "tpl/new.html")
            entry.mode = 0o644
            entry.size = 12
            archive.addfile(entry, io.BytesIO(b"new template"))

    def manifest(self):
        names = ["sqlite.db", "conf.toml", "templates.tar", "operator.env"]
        names.append("runtime-secret.key" if (self.backup / "runtime-secret.key").exists() else "runtime-secret.absent")
        lines = []
        for name in names:
            path = self.backup / name
            lines.append(hashlib.sha256(path.read_bytes()).hexdigest() + "  " + str(path))
        (self.backup / "persistent-state.sha256").write_text("\n".join(lines) + "\n")

    def state(self):
        return {str(path.relative_to(self.runtime)): (path.read_bytes(), stat.S_IMODE(path.stat().st_mode), path.stat().st_uid, path.stat().st_gid)
                for path in self.runtime.rglob("*") if path.is_file()}

    def run_restore(self):
        restore_state.restore(self.backup, self.runtime)

    def test_numeric_ownership_without_nss_accounts(self):
        with self.assertRaises(KeyError):
            pwd.getpwuid(10001)
        with self.assertRaises(KeyError):
            grp.getgrgid(10001)
        backup_before = {p.name: p.read_bytes() for p in self.backup.iterdir()}
        self.run_restore()
        for name in ("sqlite.db", "runtime-secret.key"):
            path = self.runtime / "data" / name
            self.assertEqual((path.stat().st_uid, path.stat().st_gid, stat.S_IMODE(path.stat().st_mode)), (10001, 10001, 0o600))
            self.assertEqual(path.read_bytes(), (self.backup / name).read_bytes())
        self.assertEqual((self.runtime / "tpl" / "new.html").read_bytes(), b"new template")
        self.assertFalse((self.runtime / "tpl" / "old.html").exists())
        self.assertEqual((self.runtime / "conf.toml").read_bytes(), b"fixture configuration")
        self.assertEqual(stat.S_IMODE((self.runtime / "conf.toml").stat().st_mode), 0o644)
        self.assertEqual((self.runtime / "data" / "unrelated").read_bytes(), b"preserve me")
        self.assertFalse((self.runtime / "data" / "sqlite.db-wal").exists())
        self.assertFalse((self.runtime / "data" / "sqlite.db-shm").exists())
        self.assertEqual(backup_before, {p.name: p.read_bytes() for p in self.backup.iterdir()})

    def test_restored_database_is_usable_by_numeric_nonroot_runtime(self):
        self.run_restore()
        self.root.chmod(0o755)

        def runtime_identity():
            os.setgroups([])
            os.setgid(10001)
            os.setuid(10001)

        result = subprocess.run([sys.executable, "-c", "import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); assert db.execute('SELECT value FROM proof').fetchone()==('backup',); db.execute('PRAGMA journal_mode=WAL'); db.execute(\"INSERT INTO proof VALUES ('nonroot')\"); db.commit()", str(self.runtime / "data" / "sqlite.db")], preexec_fn=runtime_identity, capture_output=True)
        self.assertEqual(result.returncode, 0, "numeric non-root database open/write failed")

    def test_absent_secret_is_restored_as_absent(self):
        (self.backup / "runtime-secret.key").unlink()
        (self.backup / "runtime-secret.absent").touch()
        self.manifest()
        self.run_restore()
        self.assertFalse((self.runtime / "data" / "runtime-secret.key").exists())

    def test_corrupt_backup_does_not_write_runtime(self):
        before = self.state()
        (self.backup / "conf.toml").write_bytes(b"corrupt")
        with self.assertRaises(restore_state.RestoreError):
            self.run_restore()
        self.assertEqual(before, self.state())
        self.assertFalse(list(self.runtime.rglob(".kitsusync-restore-*")))

    def test_incomplete_manifest_rejected(self):
        before = self.state()
        manifest = self.backup / "persistent-state.sha256"
        manifest.write_text(manifest.read_text().splitlines()[0] + "\n")
        with self.assertRaises(restore_state.RestoreError):
            self.run_restore()
        self.assertEqual(before, self.state())

    def test_invalid_database_rejected_even_with_valid_hash(self):
        before = self.state()
        (self.backup / "sqlite.db").write_bytes(b"not sqlite")
        self.manifest()
        with self.assertRaises(sqlite3.Error):
            self.run_restore()
        self.assertEqual(before, self.state())

    def test_template_traversal_rejected(self):
        before = self.state()
        for name in ("../escape", "/absolute", "tpl/../../escape"):
            with self.subTest(name=name):
                self.tar(name)
                self.manifest()
                with self.assertRaises(restore_state.RestoreError):
                    self.run_restore()
                self.assertEqual(before, self.state())

    def test_template_links_and_devices_rejected(self):
        before = self.state()
        for kind in (tarfile.SYMTYPE, tarfile.LNKTYPE, tarfile.CHRTYPE, tarfile.FIFOTYPE):
            with self.subTest(kind=kind):
                with tarfile.open(self.backup / "templates.tar", "w") as archive:
                    entry = tarfile.TarInfo("tpl")
                    entry.type = kind
                    entry.linkname = "/etc"
                    archive.addfile(entry)
                self.manifest()
                with self.assertRaises(restore_state.RestoreError):
                    self.run_restore()
                self.assertEqual(before, self.state())

    def test_numeric_chown_failure_happens_before_promotion(self):
        before = self.state()
        with mock.patch.object(restore_state.os, "fchown", side_effect=PermissionError("injected")):
            with self.assertRaises(PermissionError):
                self.run_restore()
        self.assertEqual(before, self.state())
        self.assertFalse(list(self.runtime.rglob(".kitsusync-restore-*")))

    def test_late_promotion_failure_restores_all_original_entries(self):
        before = self.state()
        replace = os.replace

        def fail_once(source, destination):
            if Path(source).name == "tpl" and Path(destination) == self.runtime / "tpl":
                raise OSError("injected promotion failure")
            return replace(source, destination)

        with mock.patch.object(restore_state.os, "replace", side_effect=fail_once):
            with self.assertRaises(OSError):
                self.run_restore()
        self.assertEqual(before, self.state())
        self.assertFalse(list(self.runtime.rglob(".kitsusync-restore-*")))

    def test_failed_compensation_keeps_originals(self):
        replace = os.replace
        failures = []

        def fail(source, destination):
            if (Path(destination) == self.runtime / "tpl"
                    and Path(source).name in ("tpl", "tpl.previous")):
                failures.append(Path(source).name)
                raise OSError("injected rename failure")
            return replace(source, destination)

        with mock.patch.object(restore_state.os, "replace", side_effect=fail):
            with self.assertRaises(restore_state.RestoreError):
                self.run_restore()
        self.assertEqual(failures, ["tpl", "tpl.previous"])
        retained = list(self.runtime.rglob("sqlite.db.previous"))
        self.assertEqual(len(retained), 1)
        self.assertEqual(retained[0].read_bytes(), b"old sqlite.db")
        self.assertEqual((self.backup / "conf.toml").read_bytes(), b"fixture configuration")


if __name__ == "__main__":
    if os.geteuid() != 0:
        raise SystemExit("Run with sudo in isolated Linux CI to test numeric ownership")
    unittest.main()
