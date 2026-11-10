"""Run only in an isolated disposable Linux container, never on a host.

The input is an actual release ZIP; the test derives a synthetic updated bundle
by replacing the development tools and updating their provenance digests.
"""
import hashlib
import importlib.util
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import zipfile

ROOT = Path('/work')
STAGE = Path('/root/kitsusync-release-stage')
STATE = Path('/var/lib/kitsusync-deploy')
CONTROL = Path('/etc/kitsusync-deploy')
LIB = Path('/usr/local/libexec')
SBIN = Path('/usr/local/sbin')
ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'HOME': '/root'}


def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def run(command):
    return subprocess.run(command, env=ENV, text=True, stdout=subprocess.PIPE,
                          stderr=subprocess.STDOUT, timeout=90)


def main():
    if os.geteuid() != 0 or os.environ.get('KITSUSYNC_ISOLATED_TEST') != 'yes':
        raise SystemExit('isolated root test container required')
    source_zip = Path(sys.argv[1])
    expected = sys.argv[2]
    if digest(source_zip) != expected:
        raise SystemExit('input artifact digest mismatch')
    with tempfile.TemporaryDirectory() as work:
        work = Path(work)
        bundle = work / 'bundle'
        bundle.mkdir()
        with zipfile.ZipFile(source_zip) as archive:
            archive.extractall(bundle)
        replacements = {
            'kitsusync-bootstrap': 'bootstrap_tool_sha256',
            'kitsusync-deploy': 'deployment_tool_sha256',
            'kitsusync-release-apply': 'release_apply_tool_sha256',
            'release_zip.py': 'release_zip_tool_sha256',
            'release_registration.py': 'release_registration_tool_sha256',
        }
        fields = dict(line.split('=', 1) for line in (bundle / 'provenance.txt').read_text().splitlines())
        for name, field in replacements.items():
            data = (ROOT / 'deploy' / name).read_bytes().replace(b'\r\n', b'\n')
            (bundle / name).write_bytes(data)
            fields[field] = digest(bundle / name)
        (bundle / 'provenance.txt').write_text(''.join(f'{k}={v}\n' for k, v in fields.items()))
        for path in (STATE, CONTROL):
            path.mkdir(parents=True, mode=0o700)
            path.chmod(0o700)
        LIB.mkdir(parents=True, exist_ok=True)
        LIB.chmod(0o755)
        SBIN.mkdir(parents=True, exist_ok=True)
        SBIN.chmod(0o755)
        targets = {
            'kitsusync-deploy': SBIN, 'kitsusync-inspect': SBIN, 'kitsusync-release-apply': SBIN,
            'kitsusync-sqlite-backup': LIB, 'kitsusync-image-identity': LIB,
            'kitsusync-runtime-state': LIB, 'kitsusync-restore-state': LIB,
            'kitsusync-preflight-collect': LIB, 'release_zip.py': LIB, 'release_registration.py': LIB,
            'docker-compose.yml': CONTROL, 'deployment-mode': CONTROL,
            'kitsusync-image.tar': STATE,
        }
        for name, directory in targets.items():
            target = directory / name
            shutil.copyfile(bundle / name, target)
            target.chmod(0o600 if name.endswith('.py') or directory in (CONTROL, STATE) else 0o700)
        shutil.copyfile(bundle / 'provenance.txt', CONTROL / 'provenance')
        (CONTROL / 'provenance').chmod(0o600)
        spec = importlib.util.spec_from_file_location('registration_fixture', LIB / 'release_registration.py')
        registration = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(registration)
        updated_one, updated_two = work / 'updated-one', work / 'updated-two'
        updated_one.write_bytes(b'fixture target one')
        updated_two.write_bytes(b'fixture target two')
        destinations = {
            'compose': (CONTROL / 'docker-compose.yml', updated_one, 0o600),
            'provenance': (CONTROL / 'provenance', updated_two, 0o600),
        }
        previous = {name: digest(value[0]) for name, value in destinations.items()}

        def interrupt(index):
            if index == 0:
                raise KeyboardInterrupt()

        try:
            registration.register(STATE, destinations, interrupt)
        except KeyboardInterrupt:
            pass
        else:
            raise AssertionError('registration interruption was not injected')
        assert (STATE / 'release-registration.pending').exists()
        assert registration.recover(STATE, destinations)
        assert all(digest(value[0]) == previous[name] for name, value in destinations.items())
        assert not (STATE / 'release-registration.pending').exists()
        docker = Path('/usr/bin/docker')
        if docker.exists():
            raise SystemExit('test requires an image without Docker client')
        docker.write_text('#!/bin/sh\n[ "$1" = inspect ] && { echo 0.5.2; exit 0; }\nexit 1\n')
        docker.chmod(0o755)
        archive_path = work / 'updated.zip'

        def archive():
            with zipfile.ZipFile(archive_path, 'w', zipfile.ZIP_DEFLATED) as out:
                for path in bundle.iterdir():
                    out.write(path, path.name)
            return digest(archive_path)

        def apply(sha):
            return run([str(SBIN / 'kitsusync-release-apply'), '--zip', str(archive_path), '--sha256', sha])

        sha = archive()
        prior_provenance = digest(CONTROL / 'provenance')
        result = apply('0' * 64)
        assert result.returncode and 'SHA-256 mismatch' in result.stdout, result.stdout
        assert not STAGE.exists()
        assert digest(CONTROL / 'provenance') == prior_provenance
        archive_path.write_bytes(b'not a zip')
        result = apply(digest(archive_path))
        assert result.returncode and 'ZIP rejected' in result.stdout, result.stdout
        assert not STAGE.exists()
        original_provenance = (bundle / 'provenance.txt').read_text()
        (bundle / 'provenance.txt').write_text(original_provenance.replace(
            'release_commit=' + fields['release_commit'], 'release_commit=' + '0' * 40))
        result = apply(archive())
        assert result.returncode and 'provenance identity mismatch' in result.stdout, result.stdout
        assert not STAGE.exists()
        (bundle / 'provenance.txt').write_text(original_provenance)
        compose = bundle / 'docker-compose.yml'
        compose_data = compose.read_bytes()
        compose.write_bytes(compose_data + b'\n# tampered\n')
        result = apply(archive())
        assert result.returncode and 'staged Compose digest mismatch' in result.stdout, result.stdout
        assert digest(CONTROL / 'provenance') == prior_provenance
        compose.write_bytes(compose_data)
        (bundle / 'deployment-mode').write_text('recovery\n')
        result = apply(archive())
        assert result.returncode and 'mode mismatch' in result.stdout, result.stdout
        assert not STAGE.exists()
        (bundle / 'deployment-mode').write_text('normal\n')
        sha = archive()
        STAGE.mkdir(mode=0o700)
        (STAGE / 'keep').write_text('unchanged')
        result = apply(sha)
        assert result.returncode and 'existing release stage preserved' in result.stdout, result.stdout
        assert (STAGE / 'keep').read_text() == 'unchanged'
        (STAGE / 'keep').unlink()
        STAGE.rmdir()
        result = apply(sha)
        assert 'release validation passed' in result.stdout, result.stdout
        assert 'Registering verified release' in result.stdout, result.stdout
        assert 'Deploying registered release' in result.stdout, result.stdout
        # The isolated fixture deliberately has no production runtime; deploy
        # must fail without contacting any external service.
        assert result.returncode != 0, result.stdout
        assert not (STATE / 'release-registration.pending').exists()
        assert not STAGE.exists()
        assert (CONTROL / 'provenance').read_text() == (bundle / 'provenance.txt').read_text()
        print('release-apply-derived-bundle=PASS (deployment deliberately blocked)')


if __name__ == '__main__':
    main()
