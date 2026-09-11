# Production bootstrap and deployment

Production has one supported execution path: the root-installed, no-argument
`/usr/local/sbin/kitsusync-deploy`. Direct production Docker Compose execution
is unsupported.

## One-time root bootstrap

Use the verified `kitsusync-v0.4.6-deployment` GitHub Actions artifact. Before
running anything, verify that its workflow completed successfully for release
tag `v0.4.6` and source commit
`b7b30157cb90c4500e8b00d3c26ac7038f5c8c10`.

An administrator stages the artifact at the fixed path
`/root/kitsusync-release-stage`. The directory must be owned by root with mode
`0700`; every staged file must be owned by root with mode `0600`. Do not place
credentials in shell arguments or terminal output. The existing protected
`/home/ukyo_vfx/kitsusync/.env.local` is copied into the root-only control
directory without printing its contents.

After staging and verifying the files, the one-time root action is:

```bash
/bin/bash /root/kitsusync-release-stage/kitsusync-bootstrap
```

The bootstrap accepts no arguments. It verifies the archive, Compose file and
tool digests before installing:

- `/usr/local/sbin/kitsusync-deploy` (`root:root`, `0700`)
- `/usr/local/sbin/kitsusync-inspect` (`root:root`, `0700`)
- `/usr/local/libexec/kitsusync-sqlite-backup` (`root:root`, `0700`)
- `/usr/local/libexec/kitsusync-image-identity` (`root:root`, `0700`)
- `/etc/kitsusync-deploy/` (`root:root`, `0700`)
- `/var/lib/kitsusync-deploy/` (`root:root`, `0700`)
- `/var/backups/kitsusync-deploy/` (`root:root`, `0700`)
- `/etc/sudoers.d/kitsusync-deploy` (`root:root`, `0440`)

The sudo policy permits only the two exact commands below, with no arguments
and no caller-supplied environment. It does not grant Docker-group access or a
general root shell.

## Normal inspection

```bash
sudo /usr/local/sbin/kitsusync-inspect
```

Inspection is read-only. It prints approved identity/status fields and
environment variable names only. It never prints environment values. If an
entrypoint or command contains a credential-like argument name, the entire
command field is redacted.

## Normal deployment

```bash
sudo /usr/local/sbin/kitsusync-deploy
```

The deploy command accepts no arguments. It uses fixed root-owned inputs and
Compose project `kitsusync`, creates and verifies an online SQLite backup plus
the complete compatible runtime state before the first Docker mutation, then
loads and verifies the approved immutable image. A normal deployment passes
only when `/ready` reports `ready`.

The v0.4.6 bundle carries the explicit one-time `legacy-migration` policy for
the already-approved runtime that predates `/ready`. Bootstrap records that
runtime's immutable image ID. The new v0.4.6 runtime must still pass the normal
`ready` contract. If rollback is needed, the previous runtime is checked using
its actual `/health` and missing-`/ready` contract. A successful migration
atomically retires legacy mode to `normal`; it is never selected from runtime
state or operator arguments.

Backups use SQLite's online backup API. WAL/SHM files are not copied over the
consistent snapshot; stale sidecars are removed before restoration. Preserve
the SQLite snapshot, runtime secret (or its absence marker), `conf.toml`,
templates, protected environment, prior immutable image and captured runtime
contract as one rollback unit.
