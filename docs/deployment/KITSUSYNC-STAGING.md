# KitsuSync Staging on vfxstudio

Production continues to use the root-installed `kitsusync-deploy` release wrapper, its release provenance policy, the existing Compose project, and loopback port `127.0.0.1:8090`. Staging is a second Compose project named `kitsusync-staging`; it uses the fixed container name `kitsusync-staging`, loopback port `127.0.0.1:8091`, and Docker volume `kitsusync-staging-data`.

Staging mounts its named `/app/data` volume and a root-owned, read-only `/etc/kitsusync-staging/conf.toml` containing only non-secret runtime defaults required for startup. This config is independent from Production. The candidate image contains its own executable and `tpl` tree; host templates are never mounted. Docker's per-container log stream is separate. SQLite, persisted connection/config state, admin sessions, and the process-generated `runtime-secret.key` live in the staging data volume and are not shared with Production.

The Staging Compose definition disables polling and uses a normal, externally connected Docker bridge so the instance can reach Kitsu/Zou for reads after it is configured. The deploy helper verifies that the network belongs to the Staging Compose project and contains only its Staging container; it does not alter host firewall or general egress policy. Staging has no baked-in or copied `KITSU_HOSTNAME`, Kitsu credential, Production Discord credential, webhook, or Production state. After bootstrap, configure the Kitsu origin and a separate Staging Kitsu bot/principal through Staging's Connections UI, then use its connection test. The endpoint is therefore sourced from the operator's verified current Kitsu setup, not a hardcoded snapshot. Staging is ready for this one-time UI setup; `/ready` may report `setup_required` until it is complete.

Discord polling remains disabled with `KITSUSYNC_DISABLE_POLL=1`. Staging has an independent SQLite/data volume and config; Production Discord credentials and state are neither copied nor mounted, and the helper rejects known Discord credential environment variables in the container. This prevents Production Discord side effects. A dedicated Staging Discord destination can be configured separately in the future.

`kitsusync-staging-bootstrap` installs the root-owned `/usr/local/sbin/kitsusync-staging-deploy`, the isolated Compose definition, and one sudoers entry for `ukyo_vfx` that permits only that helper. The one-time bootstrap bundle is kept at `/var/tmp/kitsusync-staging-bootstrap-<sha>-v2` so an older failed staging attempt cannot be overwritten. The helper accepts one exact 40-character source SHA, reads the matching candidate bundle from `/var/tmp/kitsusync-staging-candidate-<sha>`, rejects symlinks and unexpected files, checks candidate provenance and all recorded file/image digests, validates Docker's loaded image identity, and deploys only project `kitsusync-staging`. It verifies the runtime build identity, Staging-only container/network membership, port binding, exact isolated mounts, polling/Discord environment policy, health/readiness and System Status DOM. Updates snapshot SQLite and the runtime secret before replacement, then restore and verify the previous candidate if the new candidate fails. It snapshots the published container set on 8090 before deployment and fails if that set changes. The prior first-deploy network failure remains unresolved; the successor helper emits separate attachment-count, network-name, and network-membership markers so the next authorized retry will identify the exact check without weakening validation.

Production is never selected by the Staging helper. Release deployments remain gated by the existing `kitsusync-deploy` wrapper and are not automated from candidate CI.

## Review tunnels

The per-user Scheduled Task `KitsuSync VFXStudio Review Tunnels` runs one hidden OpenSSH process with `BatchMode=yes`, `ExitOnForwardFailure=yes`, keepalives, and two fixed forwards:

- `127.0.0.1:18090` to vfxstudio `127.0.0.1:8090` (Production)
- `127.0.0.1:18091` to vfxstudio `127.0.0.1:8091` (Staging)

The task ignores duplicate starts and restarts a terminated SSH process. The runner exits without binding if either local port is occupied by anything except the exact expected SSH process.

## Candidate workflow

Candidate implementation, focused tests, CI/Security checks and immutable artifact provenance precede staging deployment. `scripts/deploy-kitsusync-staging-candidate.ps1` checks the exact SHA, candidate-only provenance and file digests, requires successful `CI` and `Security Audit` runs for that SHA, stages the bundle, then invokes the host helper through `sudo -n`. Deployment never promotes the artifact to release identity. Human review occurs on port 18091. Only explicit human acceptance can authorize merge, release, or Production deployment.
