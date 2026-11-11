import hashlib
import importlib.util
import tempfile
import unittest
import zipfile
import stat
from pathlib import Path

MODULE = Path(__file__).resolve().parents[1] / "deploy" / "release_zip.py"
spec = importlib.util.spec_from_file_location("release_zip", MODULE)
release_zip = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release_zip)


class ReleaseZipTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.archive = self.root / "bundle.zip"

    def build(self, entries):
        with zipfile.ZipFile(self.archive, "w") as archive:
            for name, data in entries:
                archive.writestr(name, data)
        return hashlib.sha256(self.archive.read_bytes()).hexdigest()

    def test_extract_and_verify(self):
        digest = self.build([(name, b"data") for name in release_zip.REQUIRED])
        dest = self.root / "stage"
        release_zip.extract(self.archive, digest, dest)
        self.assertEqual({p.name for p in dest.iterdir()}, release_zip.REQUIRED)

    def test_digest_mismatch_does_not_create_stage(self):
        self.build([(name, b"data") for name in release_zip.REQUIRED])
        dest = self.root / "stage"
        with self.assertRaises(ValueError):
            release_zip.extract(self.archive, "0" * 64, dest)
        self.assertFalse(dest.exists())

    def test_existing_stage_is_preserved(self):
        digest = self.build([(name, b"data") for name in release_zip.REQUIRED])
        dest = self.root / "stage"
        dest.mkdir()
        (dest / "existing").write_text("keep")
        with self.assertRaises(ValueError):
            release_zip.extract(self.archive, digest, dest)
        self.assertEqual((dest / "existing").read_text(), "keep")

    def test_rejects_unexpected_and_duplicate_entries(self):
        for extra in ("../escape", "docker-compose.yml"):
            with self.subTest(extra=extra):
                digest = self.build([(name, b"data") for name in release_zip.REQUIRED] + [(extra, b"x")])
                with self.assertRaises(ValueError):
                    release_zip.extract(self.archive, digest, self.root / "stage")

    def test_corrupt_zip_rejected(self):
        self.archive.write_bytes(b"not a zip")
        digest = hashlib.sha256(self.archive.read_bytes()).hexdigest()
        with self.assertRaises(ValueError):
            release_zip.extract(self.archive, digest, self.root / "stage")

    def test_symlink_entry_is_rejected(self):
        with zipfile.ZipFile(self.archive, "w") as archive:
            for name in release_zip.REQUIRED:
                info = zipfile.ZipInfo(name)
                if name == "docker-compose.yml":
                    info.create_system = 3
                    info.external_attr = (stat.S_IFLNK | 0o777) << 16
                archive.writestr(info, b"data")
        digest = hashlib.sha256(self.archive.read_bytes()).hexdigest()
        with self.assertRaises(ValueError):
            release_zip.extract(self.archive, digest, self.root / "stage")
        self.assertFalse((self.root / "stage").exists())


if __name__ == "__main__":
    unittest.main()
