# Production bootstrap and deployment

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
- `/usr/local/libexec/kitsusync-preflight-collect` (`root:root`, `0700`)
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

The final `preflight_evidence_json=` line is a sanitized schema-version-1
summary for the local preflight evaluator. It contains no paths, hashes,
settings, or database values. If required evidence cannot be established, the
affected field is omitted or reported negative so the evaluator cannot return
PASS.

The collector invokes the already-installed `kitsusync-restore-state verify`
only after checking the fixed backup inventory. That verifier's `verify` mode
does not restore or write files. The collector does not create backups or
change Docker state. It identifies the SQLite database only when the sole
running `kitsusync/app` container has one writable bind mount from the fixed
runtime data directory to `/app/data`. Without that identity proof, database
integrity and migration compatibility remain unknown. It checks SQLite
integrity without opening or checkpointing WAL/SHM state; when either sidecar
exists, the database result is unknown and the sidecar files remain untouched.

The collector reports the additive `database.migration_compatibility` field
for the v0.4.9-to-v0.5.1 upgrade. It verifies the legacy table/column contract
needed by the migration; it does not execute a migration against Production.
An absent database, unreadable schema, active WAL/SHM, or missing required
legacy schema cannot be reported as compatible.

Capacity output keeps the schema-v1 `capacity` scalar when every filesystem
requirement is known. The additive `capacity_filesystems` list identifies the
operation roles sharing each filesystem and sums their simultaneous peak
requirements. The legacy scalar uses the least available space and greatest
required space across filesystems: this is conservative and may block a valid
layout, but a passing scalar proves every represented filesystem has enough
space. If any requirement or filesystem measurement is unknown, the legacy
`capacity` field is omitted so the existing evaluator remains BLOCKED rather
than interpreting unknown as zero. The updated wrapper loads and verifies the
target image while the prior runtime is still running. It writes other
rollback metadata before stopping the writer, then takes one SQLite online
backup from the quiesced database. Raw Docker inspect JSON is streamed through
the bounded verifier rather than written as a temporary file; the protected
backup retains only the verified reconstruction plan, not an additional raw
inspect or normalized environment copy. A failed quiesced backup restarts and
validates the same prior container, then removes only that invocation's
incomplete backup directory. If restart or validation fails, partial evidence
is retained and the wrapper stops with an explicit recovery error. No
unverified backup is accepted as rollback evidence.

Runtime reconstruction preserves and strictly compares custom endpoint MACs
and all `IPAMConfig` address intent. It normalizes only a `02:42:<IPv4>` MAC
that exactly matches Docker's derived MAC for the inspected endpoint address.
Additional
endpoints are attached after container creation with their saved identity.
Docker documents that `POST /networks/{id}/connect` only began applying the
endpoint `MacAddress` field in Engine API v1.54. A runtime with secondary
networks on an older API is therefore rejected by the reconstruction probe
before the prior container is stopped. See the [Docker Engine API version
history](https://docs.docker.com/reference/api/engine/version-history/).

These controls make backup ENOSPC a fail-safe abort while the original
database remains in place; they do not prove an arbitrary finite capacity
bound or guarantee recovery if Docker's filesystem becomes unavailable while
replacement/rollback container metadata is created. The runtime database/WAL,
backup directory, release stage and Docker image store may be on different
filesystems. The collector therefore continues to report backup capacity as
unknown/BLOCKED rather than infer a PASS from free space or an older backup.
Runtime metadata may contain sensitive configuration, so no payload is read or
emitted to estimate it. A verified rollback backup also proves only its
captured snapshot, not that it includes writes made after capture.

## Inspector maintenance artifact

The accepted-master CI workflow builds a separate
`kitsusync-inspector-maintenance-<source-sha>` artifact only on a push to
`master`. It is not part of the application release bundle and cannot deploy
the application. The artifact contains the fixed maintenance entry points,
the updated inspector and collector, a source-commit/file-digest manifest, and
the byte-for-byte SHA-256 of the v0.5.1 predecessor inspector. The workflow
retains the artifact for 90 days. PR CI tests the builder and maintenance
behavior but does not publish a root-installable artifact.

Updating the already-installed root-owned helpers requires a separate,
one-time administrator action after the change is accepted on `master`. Do
not attempt it through the current unprivileged inspector permission. Before
the action, the administrator must obtain the artifact from the successful
master run for the exact accepted source SHA, verify the GitHub Actions
artifact digest against run metadata and the inner tar.gz SHA-256 against the
builder's `artifact_sha256` output, then stage exactly its six files at
`/root/kitsusync-inspection-maintenance` (`root:root`, directory
`0700`; manifest and payload files `0600`; fixed `install-inspection` and
`rollback-inspection` entry points `0700`). The installer independently
checks source identity, baseline predecessor SHA, complete inventory, hashes,
owner, mode, regular-file type and symlink/TOCTOU state before touching either
installed helper.

The single install action is:

```bash
/root/kitsusync-inspection-maintenance/install-inspection
```

It accepts no arguments, runs only as root, uses fixed paths, and atomically
replaces each of the preflight collector and `kitsusync-inspect`. It saves the
previous inspector and a pending recovery journal under
`/var/backups/kitsusync-deploy/` and `/var/lib/kitsusync-deploy/`. After both
files are verified, it writes a root-only `inspection-maintenance.json`
record binding the source commit and exact inspector/collector hashes. The
preflight collector accepts maintained helper hashes only when both installed
files match this record; deployment-wrapper provenance remains unchanged. The
record is included in the pending recovery/rollback transaction: rollback
restores the prior record or removes it if none existed. On an ordinary failure
it restores the prior pair. If interrupted between replacements, the next
invocation first restores the previous pair and matching identity record,
clears the recovery journal, exits without retrying installation, and requires
administrator review before a deliberate retry. The explicit rollback entry
point is:

```bash
/root/kitsusync-inspection-maintenance/rollback-inspection
```

Rollback is allowed only while installed helpers still match the maintenance
artifact's target digests. If identity, ownership, mode, or recovery evidence
does not match, the operation fails closed and preserves evidence for manual
administrator recovery. No root maintenance action has been performed by this
PR work.

## Deployment-helper maintenance artifact

The accepted-master CI workflow builds a separate
`kitsusync-deployment-maintenance-<source-sha>` artifact. It is not part of the
v0.5.1 application Release Bundle and cannot deploy the application. It
contains the fixed deploy wrapper, runtime-state helper, narrowly scoped
installer/rollback implementation, and a complete SHA-256 manifest bound to
the source commit and exact v0.5.1 predecessor tool blobs. The workflow retains
the artifact for 90 days. PR CI verifies inventory, modes, hashes, and
predecessor identities; it does not publish a root-installable artifact.

Installing it requires a separate one-time administrator action after the
change is accepted on `master`. Obtain the artifact from the successful
exact-master run, verify the GitHub artifact digest and inner archive SHA-256,
then stage exactly these seven files under
`/root/kitsusync-deployment-maintenance`: `INSTALL.txt`,
`install-deployment`, `rollback-deployment`, `deployment_maintenance.py`,
`kitsusync-deploy`, `kitsusync-runtime-state`, and `manifest.json`. The stage
directory must be `root:root` mode `0700`; payloads and manifest must be
regular root-owned files mode `0600`, except the two fixed entry points at
`0700`. The installer checks exact inventory, source, file hashes, immutable
v0.5.1 predecessors, ownership, modes, symlink/hardlink state, and fixed paths
before changing installed files.

The only install command is:

```bash
/root/kitsusync-deployment-maintenance/install-deployment
```

It accepts no arguments, runs only as root, serializes against the deploy
wrapper through `/var/lib/kitsusync-deploy/deploy.lock`, and atomically
replaces only `/usr/local/sbin/kitsusync-deploy` and
`/usr/local/libexec/kitsusync-runtime-state`. It never rewrites the protected
v0.5.1 provenance, invokes arbitrary commands, or adds sudoers rules, services,
daemons, or listeners. A root-owned identity record binds the new helper
hashes to the exact prior provenance digest; the wrapper permits a legacy
digest mismatch only while that record and unchanged v0.5.1 provenance agree.
If interrupted, the next fixed maintenance invocation restores the saved
predecessors, clears the journal, reports recovery, and exits without retrying
installation. Inspect state before a deliberate retry.

The fixed rollback command is:

```bash
/root/kitsusync-deployment-maintenance/rollback-deployment
```

It restores only the recorded v0.5.1 predecessor pair and prior identity, and
only while installed files and provenance still match the maintenance record.
No helper update, root action, application deployment, or Production change
has been performed by this PR work.

## Production update approval checklist

The read-only preflight is evidence gathering, not authorization to deploy.
Before a separately approved Production update, an operator must verify all
items below against the same candidate source SHA and the live host:

- [ ] The released v0.5.1 candidate has successful exact-source CI, Security,
  CodeQL, normal release bundle provenance, and immutable image identity.
- [ ] The installed deploy wrapper and inspection helpers have approved owner,
  mode, source digest, and recorded provenance. Any deploy-wrapper or
  inspector maintenance is completed only through its dedicated immutable
  artifact and separately approved one-time administrator action.
- [ ] Runtime image, source revision, Compose identity, and rollback image all
  match the recorded immutable identities. The existing rollback unit and old
  image remain available.
- [ ] Runtime readiness is `ready` in normal mode. `setup_required` is an
  absolute stop for normal deployment; do not force or reinterpret it.
- [ ] The fixed Production SQLite path is proven to be the live app container's
  `/app/data` bind mount; integrity is `ok`; migration compatibility is
  `v0.4.9_to_v0.5.1`; no unresolved WAL/SHM or schema state remains.
- [ ] The existing protected backup inventory and restore verifier pass. The
  operator understands that this verifies the captured backup only; it does
  not prove that it contains writes made after its capture.
- [ ] Peak space is known and sufficient for every filesystem role, including
  the single quiesced SQLite backup, runtime state, and Docker image/container
  storage. An omitted or `unknown` capacity requirement is BLOCKED, even though
  backup ENOSPC is designed to restart the prior runtime safely.
- [ ] The normal `kitsusync-deploy` wrapper is the only deployment path and its
  automatic rollback evidence is available. Direct Production Compose is
  prohibited.

This PR does not perform helper maintenance, create a fresh SQLite snapshot,
or deploy the application. Those are distinct administrator actions requiring
explicit approval. Backup capacity remains unresolved: the collector does not
claim an arbitrary finite upper bound for the runtime-dependent writes. A
sanitized, filesystem-specific capacity attestation for the exact installed
wrapper and current runtime is still required. Its output must contain only
per-filesystem available/required byte counts and status, and must not print or
persist environment/inspect payload values. If it cannot prove sufficiency,
provision or resize the relevant filesystem and repeat the attestation. Do not
substitute a guessed reserve, prior backup size, or free space alone, and do
not treat a clean collector exit as deployment approval.

If the approved wrapper reports a failure, use only the wrapper's documented
rollback path and verify the restored immutable image, runtime state,
`/health`, and the applicable readiness contract. If rollback is incomplete
or the runtime reports `setup_required`, stop. Do not use direct Compose,
remove data, or manually substitute an image to force recovery.

## Normal deployment

```bash
sudo /usr/local/sbin/kitsusync-deploy
```

The deploy command accepts no arguments. It uses fixed root-owned inputs and
Compose project `kitsusync`. It loads and verifies the approved immutable image
while the prior runtime is still active, captures and validates the runtime
reconstruction plan, and creates a stopped rollback probe before quiescing the
writer. It takes one final SQLite online backup while the app is stopped, then
marks the rollback unit complete before removing or replacing the active
container. A normal deployment passes only when `/ready` reports `ready`.

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
