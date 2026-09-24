# KitsuSync preview-only deployment design

## Intent and constraints

The goal is to run one exact pull-request candidate on the operator's vfxstudio host for visual review. This preview is explicitly non-release and must not be accepted by the normal release deployment entry point. The existing release provenance rules remain unchanged. The preview uses the existing KitsuSync Compose project and persistent runtime, keeps the application bound to `127.0.0.1:8090`, and may leave `/ready` in `setup_required` when the UI routes are available. Preview data is disposable. Merge, release, and deployment to any other host are out of scope.

## Approaches considered

1. **Broaden the release wrapper.** Add candidate exceptions to `kitsusync-deploy`. This is the least code, but makes it easier for a preview artifact to pass a release entry point and weakens the release contract. Rejected.
2. **Copy the full deployment transaction into a preview wrapper.** This preserves a separate entry point but duplicates backup, replacement, validation, and rollback logic. The copies could diverge. Rejected.
3. **Keep policy checks in separate entry points and share a root-owned transaction core.** `kitsusync-deploy` retains strict release-only provenance checks; `kitsusync-preview-deploy` enforces candidate-only identity and a visible preview marker. Both invoke the same transaction core for the existing backup, deployment, runtime checks, and rollback. Recommended.

## Components and trust boundaries

- `kitsusync-deploy` remains the release entry point. Its release artifact kind, commit/tag equality, versioned image reference, readiness modes, and provenance checks remain strict and covered by existing tests.
- `kitsusync-preview-deploy` is a separate root-owned entry point installed in the existing deployment control plane. It requires an explicit expected 40-character source SHA and an explicit preview confirmation token, rejects any source or image identity mismatch, and accepts only CI candidate bundle metadata (`artifact_kind=candidate`, `source_commit=source_id=expected SHA`, empty release commit/tag, and image `kitsusync:ci-<SHA>`).
- The preview bundle is copied to the existing root-protected staging paths only after the operator-side GitHub run/artifact is confirmed to belong to the exact PR head. On-host validation checks the explicit SHA, bundle file hashes, image/archive identity, Compose digest, installed helper digests, OCI revision/source/version labels, and preview image reference before loading or replacing a container. It does not infer candidate identity from a version or branch name.
- A common root-owned transaction core consumes a validated deployment identity and a constrained mode. It owns the lock, runtime preflight, complete rollback backup, Compose replacement, runtime identity/health/route probes, and recovery. It does not decide whether an identity is a release or preview; only the dedicated wrapper policy checks may construct its validated input.
- Preview runtime identity is visibly labelled `PREVIEW / NON-RELEASE` in deployment output/logs and uses `kitsusync:ci-<SHA>`. It is never written as a release tag or release provenance.

## Runtime behavior

The preview uses the current `kitsusync` Compose project, service, mounted configuration/data/template locations, and host port. It requires exactly one expected `kitsusync-app-1` container after replacement, a `127.0.0.1:8090` port binding, the expected candidate SHA in the runtime OCI revision and source identity, image health, `/health` HTTP 200 with an OK body, and UI route responses within the existing unauthenticated admin-boundary contract. `/ready` accepts only `ready`/200 or `setup_required`/503 with matching build identity; degraded, missing, malformed, or mismatched readiness fails deployment and triggers the existing rollback.

The preview wrapper cannot target an alternate Compose project, host, port, mount set, or runtime path. Existing persistent test data may be backed up by the transaction and then replaced by the candidate application; no unrelated host, network, account, or secret configuration changes are part of this feature.

## Failure handling

All candidate provenance and staged-file checks happen before the transaction changes runtime state. A failed backup or preflight leaves the current container untouched. A failed image load, replacement, identity check, health/readiness probe, or route probe uses the shared recovery path and reports whether rollback was verified. The preview path must not offer a force, skip-validation, no-backup, or no-rollback option. A missing candidate identity or untrusted staged file fails closed.

## Validation

- Unit/contract tests prove release wrapper rejection of candidate provenance and preview wrapper rejection of release provenance, wrong SHA, wrong image tag, non-empty release identity, and missing preview confirmation.
- The existing deployment-transaction integration harness is extended to execute the preview entry point against disposable Docker fixtures. It verifies exact candidate labels, loopback binding, setup-required acceptance, route checks, backup, and rollback after target validation failure.
- Existing release deployment transaction checks continue to pass without weakening their assertions.
- Required CI checks remain Linux/CGO `go test`, `go vet`, Compose validation, deployment transaction tests, image/bundle provenance checks, and security/hardening scripts.
- The vfxstudio operation stages an immutable bundle for the exact PR head SHA, validates it on-host, then invokes one interactive breakglass command for privileged Linux/Docker validation and deployment. Final review evidence records source SHA, image/container identity, binding, `/health`, `/ready`, and both requested UI page responses.

## Non-goals

- No change to normal release eligibility, release tags, release artifacts, or the release wrapper's readiness policy.
- No merge, release, or automatic promotion from preview to production.
- No network exposure or proxy/listener/firewall changes.
- No state migration or attempt to preserve disposable visual-review test data beyond the deployment transaction's required rollback backup.
- No generic multi-host or arbitrary-image deployment interface.
