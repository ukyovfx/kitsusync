# KitsuSync v0.4.1

## Summary

v0.4.1 is a maintenance release for the operator-facing setup and administration flow delivered in v0.4.0. The v0.4.0 tag and history remain unchanged.

## Changes

- Documentation and version metadata identify this release as v0.4.1.
- The public setup guide matches the current seven-step Production connection flow.
- The documentation asset cache key is refreshed so updated guidance is loaded after deployment.
- The existing KitsuSync UI, Discord routing behavior, and stored data model are unchanged from v0.4.0.

## Validation

- Focused documentation route tests passed.
- The v0.4.1 candidate was built from a clean clone and `/health` returned HTTP 200.
- The existing v0.4.0 tag was preserved and no tag or GitHub Release was created.

## Upgrade Notes

No database migration is required for v0.4.1. Preserve the existing SQLite data, runtime secret key, and operator configuration together during upgrade.
