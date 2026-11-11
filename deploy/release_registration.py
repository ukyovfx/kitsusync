"""Reversible replacement of a fixed inventory of registered release files.

The caller must hold the deployment lock for the whole operation and supply
only fixed, trusted paths. A pending journal must block deployment.
"""
import hashlib
import json
import os
import shutil
import stat
import tempfile
from pathlib import Path


class RegistrationError(ValueError):
    pass


def _sync_dir(path):
    if os.name != 'nt':
        fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)


def _hash(path):
    digest = hashlib.sha256()
    with path.open('rb') as source:
        for block in iter(lambda: source.read(1024 * 1024), b''):
            digest.update(block)
    return digest.hexdigest()


def _check_regular(path):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or (hasattr(os, 'geteuid') and info.st_uid != os.geteuid()):
        raise RegistrationError('registration input is not a trusted regular file')


def _atomic_copy(source, target, mode, expected=None):
    fd, temporary = tempfile.mkstemp(prefix='.registration-', dir=target.parent)
    try:
        with os.fdopen(fd, 'wb') as out, source.open('rb') as inp:
            shutil.copyfileobj(inp, out, 1024 * 1024)
            out.flush()
            os.fsync(out.fileno())
            os.fchmod(out.fileno(), mode) if hasattr(os, 'fchmod') else None
        if expected is not None and _hash(Path(temporary)) != expected:
            raise RegistrationError('registration copy digest mismatch')
        os.replace(temporary, target)
        _sync_dir(target.parent)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def _pending(state):
    return Path(state) / 'release-registration.pending'


def _cleanup(journal, names):
    for name in names:
        (journal / name).unlink()
    (journal / 'manifest.json').unlink()
    journal.rmdir()
    _sync_dir(journal.parent)


def recover(state, mapping):
    journal = _pending(state)
    if not os.path.lexists(journal):
        return False
    if journal.is_symlink() or not journal.is_dir():
        raise RegistrationError('unsafe registration journal')
    try:
        manifest_path = journal / 'manifest.json'
        _check_regular(manifest_path)
        manifest = json.loads(manifest_path.read_text(encoding='utf-8'))
        if set(manifest) != set(mapping):
            raise RegistrationError('registration journal inventory mismatch')
        # Validate the entire rollback before changing any destination.
        for name, (target, _, mode) in mapping.items():
            if not name.isidentifier():
                raise RegistrationError('invalid registration inventory name')
            _check_regular(target)
            _check_regular(journal / name)
            record = manifest[name]
            if record['mode'] != mode or _hash(journal / name) != record['before']:
                raise RegistrationError('registration backup does not match journal')
            if _hash(target) not in (record['before'], record['after']):
                raise RegistrationError('registered file changed outside transaction')
        for name, (target, _, mode) in mapping.items():
            _atomic_copy(journal / name, target, mode, manifest[name]['before'])
        _cleanup(journal, mapping)
        return True
    except (KeyError, TypeError, ValueError, OSError) as exc:
        raise RegistrationError('registration recovery requires inspection') from exc


def register(state, mapping, fault_hook=None):
    journal = _pending(state)
    if os.path.lexists(journal):
        raise RegistrationError('registration recovery is pending')
    for name, (target, source, _) in mapping.items():
        if not name.isidentifier():
            raise RegistrationError('invalid registration inventory name')
        _check_regular(target)
        _check_regular(source)
    journal.mkdir(mode=0o700)
    _sync_dir(journal.parent)
    manifest = {}
    for name, (target, source, mode) in mapping.items():
        _atomic_copy(target, journal / name, 0o600)
        manifest[name] = {'before': _hash(journal / name), 'after': _hash(source), 'mode': mode}
    # No destination has changed until the durable journal exists.
    manifest_tmp = journal / 'manifest-input'
    manifest_tmp.write_text(json.dumps(manifest, sort_keys=True), encoding='utf-8')
    _atomic_copy(manifest_tmp, journal / 'manifest.json', 0o600)
    manifest_tmp.unlink()
    try:
        for index, (name, (target, source, mode)) in enumerate(mapping.items()):
            if _hash(source) != manifest[name]['after']:
                raise RegistrationError('registration source changed')
            _atomic_copy(source, target, mode, manifest[name]['after'])
            if _hash(target) != manifest[name]['after']:
                raise RegistrationError('registered file digest mismatch')
            if fault_hook:
                fault_hook(index)
    except Exception:
        recover(state, mapping)
        raise
    _cleanup(journal, mapping)
