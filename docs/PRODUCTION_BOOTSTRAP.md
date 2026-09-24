# Production bootstrap and deployment

> **Version-specific procedure:** this page documents the v0.4.6 bootstrap and
> migration bundle. Do not use its artifact name, release tag, or source commit
> for another release. Before a bootstrap or deployment for a newer version,
> verify that release's workflow result, exact source commit, artifact contents,
> and provenance against the repository and CI. This guide does not establish
> which version is currently deployed in production.

Production has one supported execution path: the root-installed, no-argument
`/usr/local/sbin/kitsusync-deploy`. Direct production Docker Compose execution
is unsupported.

## One-time root bootstrap

Use the verified `kitsusync-v0.4.6-deployment` GitHub Actions artifact. Before
running anything, verify that its workflow completed successfully for release
tag `v0.4.6` and source commit
`b7b30157cb90c4500e8b00d3c26ac7038f5c8c10`.

For a purged host, dispatch the workflow with `deployment_mode=fresh-install`.
The artifact contains a digest-bound credential-free `conf.toml`, the templates
from the immutable application source, an empty data-directory contract, and a
protected environment seed. It never contains a Kitsu or Discord credential.

An administrator stages the artifact at the fixed path
`/root/kitsusync-release-stage`. The directory must be owned by root with mode
`0700`; every staged file must be owned by root with mode `0600`. Do not place
credentials in shell arguments or terminal output. For `fresh-install`, an
optional root-owned mode `0600` file named `fresh-kitsu-hostname` contains the
single non-secret HTTP(S) Kitsu authority URL. Bootstrap validates that it has
no userinfo, query, or fragment before constructing `.env.local`. When the file
is absent, KitsuSync starts with an empty authority and remains
`setup_required`; credentials are still entered only through the supported UI.
Existing update modes continue copying the protected runtime `.env.local` into
the root-only control directory without printing its contents.

After staging and verifying the files, the one-time root action is:

```bash
/bin/bash /root/kitsusync-release-stage/kitsusync-bootstrap
```

The bootstrap accepts no arguments. It verifies the archive, Compose file and
tool and fresh-seed digests before installing:

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

## Fresh installation

`fresh-install` requires an empty KitsuSync container/network/runtime state and
does not require a prior image or rollback snapshot. Bootstrap reconstructs the
runtime seed with root-owned configuration/templates and a UID/GID `10001:10001`
mode `0700` data directory. Deploy loads the approved immutable image, creates
only the `kitsusync` Compose app and network, binds `127.0.0.1:8090`, and accepts
only `HTTP 503` with readiness `setup_required` as its initial success state.
The container must also be running, Docker-healthy, use UID/GID `10001:10001`,
and expose `/health` as HTTP 200.

When `fresh-kitsu-hostname` supplies an authority, the wrapper additionally
requires `curl` executed inside the new KitsuSync container to receive HTTP
200 from `KITSU_HOSTNAME/api/`. This validates the actual container-to-proxy-to-
Zou route. Do not gate the installation on a host curl to the Docker bridge
address: host self-routing and host firewall policy may reject that path even
when the bridged container path is correct.

For the vfxstudio loopback Zou topology, the host-only proxy remains bound to
the Docker bridge and accepts only Docker address space. Its `/api/` locations
use `proxy_pass http://127.0.0.1:5000/`; nginx removes the `/api/` prefix, so
the direct Zou upstream receives `/` for `/api/` and `/status` for
`/api/status`. This matches Zou's direct API routing and must not be changed to
prefix preservation without independently changing the upstream contract.

On success, deployment mode changes atomically to `normal`; subsequent updates
use the existing backup/rollback path after setup reaches `ready`. On failure,
the wrapper removes only the new KitsuSync app container, project network, and
files created in the still-marked fresh data directory. The verified seed and
control plane remain for an identical retry. It never changes Kitsu/Zou,
PostgreSQL, Redis, nginx, Tailscale, firewall, or SSH configuration.
