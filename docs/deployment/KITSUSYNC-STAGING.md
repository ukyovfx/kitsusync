# KitsuSync Staging deployment contract

This document describes the **KitsuSync product's staging behavior and safety constraints**, not instructions for connecting to a specific studio server. Staging is for reviewing exact candidate builds before any separately authorized release or Production deployment.

## Isolation and identity

- Use a separate Compose project, container, persistent SQLite data volume, application configuration, logs, runtime secret, admin sessions and credentials for Staging. Never mount or copy Production state into the candidate.
- Staging networking and routing must be scoped to the explicitly configured Kitsu/Zou endpoint; the deployment helper validates the endpoint before starting the candidate. Never copy, print or expose Production Kitsu/Discord credentials or webhooks.
- Disable Discord polling by default with `KITSUSYNC_DISABLE_POLL=1`. If notifications need testing, use distinct Staging credentials and destinations.
- The Staging candidate contains its own application executable and templates; do not mount mutable Production templates.
- Production releases remain controlled by the supported `kitsusync-deploy` workflow; a Staging candidate does not become a Production release by being accepted for review.

## Candidate artifact and helper contract

- The candidate must be identified by an exact source commit SHA. Confirm that its CI and Security checks succeeded and verify the immutable artifact's source provenance, file digests, image identity and allowed file set.
- A routine candidate deployment must use the **already installed and verified** Staging helper. If the expected helper contract is absent or outdated, fail closed and require a separately reviewed infrastructure update; do not silently bootstrap or upgrade infrastructure.
- Deploy only to the isolated Staging Compose project. Verify container/network membership, exact mounts, configuration readability under the runtime user, polling restrictions, health, readiness, and admin routes.
- Before a Staging replacement, preserve the previous Staging SQLite/runtime-secret state and support verified rollback to the previous candidate.
- If that rollback itself fails, the helper never deletes the private import directory (it holds the SQLite backup and previous runtime secrets). It moves it to a unique `failed-rollback-<sha>-<UTC time>-<pid>` directory under the Staging state root, or leaves it in place if the move fails, and reports the actual location as `STAGING_ROLLBACK_BACKUP=<path>`. Treat that directory as secret-bearing and remove it only after manual recovery.
- Guard the Production runtime boundary: a Staging transaction must not select, replace, or alter Production containers, ports, data or release identity.

## Supported workflows

The product repository provides `scripts/deploy-kitsusync-staging-candidate.ps1` for exact-SHA routine candidate deployment and `scripts/upgrade-kitsusync-staging-helper.ps1` for separately authorized Staging helper maintenance. See the relevant scripts and `.github/workflows/staging-deploy.yml` for executable contracts; local operators must supply their own approved access and configuration.

The optional pull-request candidate workflow is disabled unless its required repository variables, credentials, access policy and deployment helper are deliberately configured. It must use trusted deployment tooling, verify the candidate's immutable artifact, and preserve concurrency and rollback boundaries.

## Human acceptance

Review Staging via an organization-approved access path. No particular SSH alias, tunnel port, per-user Scheduled Task, or studio-specific network topology is part of the public KitsuSync product contract. Human acceptance of a candidate does **not** authorize merge, release or Production deployment.

For initial application setup, see [Quick Start](../QUICK_START.md) and [Studio setup](../SETUP_FOR_STUDIOS.md).
