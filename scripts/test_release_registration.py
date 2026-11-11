import importlib.util
import tempfile
import unittest
from pathlib import Path

MODULE = Path(__file__).resolve().parents[1] / 'deploy' / 'release_registration.py'
spec = importlib.util.spec_from_file_location('release_registration', MODULE)
registration = importlib.util.module_from_spec(spec)
spec.loader.exec_module(registration)


class RegistrationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.state = self.root / 'state'
        self.state.mkdir()
        self.dest = self.root / 'installed'
        self.dest.mkdir()
        self.source = self.root / 'new'
        self.source.mkdir()
        self.mapping = {}
        for name in ('one', 'two', 'three'):
            old = self.dest / name
            new = self.source / name
            old.write_bytes(('old-' + name).encode())
            new.write_bytes(('new-' + name).encode())
            self.mapping[name] = (old, new, 0o600)

    def test_success_replaces_all(self):
        registration.register(self.state, self.mapping)
        for name, (old, new, _) in self.mapping.items():
            self.assertEqual(old.read_bytes(), new.read_bytes(), name)
        self.assertFalse((self.state / 'release-registration.pending').exists())

    def test_failure_restores_every_predecessor(self):
        def fail(index):
            if index == 1:
                raise RuntimeError('injected failure')
        with self.assertRaisesRegex(RuntimeError, 'injected failure'):
            registration.register(self.state, self.mapping, fail)
        for name, (old, _, _) in self.mapping.items():
            self.assertEqual(old.read_bytes(), ('old-' + name).encode())
        self.assertFalse((self.state / 'release-registration.pending').exists())

    def test_interruption_is_recovered_on_retry(self):
        def interrupt(index):
            if index == 1:
                raise KeyboardInterrupt()
        with self.assertRaises(KeyboardInterrupt):
            registration.register(self.state, self.mapping, interrupt)
        self.assertTrue((self.state / 'release-registration.pending').exists())
        registration.recover(self.state, self.mapping)
        for name, (old, _, _) in self.mapping.items():
            self.assertEqual(old.read_bytes(), ('old-' + name).encode())
        self.assertFalse((self.state / 'release-registration.pending').exists())

    def test_recovery_refuses_unexpected_changes(self):
        def interrupt(index):
            if index == 0:
                raise KeyboardInterrupt()
        with self.assertRaises(KeyboardInterrupt):
            registration.register(self.state, self.mapping, interrupt)
        self.mapping['one'][0].write_bytes(b'changed elsewhere')
        with self.assertRaises(registration.RegistrationError):
            registration.recover(self.state, self.mapping)
        self.assertTrue((self.state / 'release-registration.pending').exists())


if __name__ == '__main__':
    unittest.main()
