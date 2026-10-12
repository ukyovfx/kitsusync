# KitsuSync v0.4.3

## Summary

v0.4.3 fixes Kitsu endpoint selection during fresh initialization. KitsuSync validates safe configured and local candidates before login and provides a secure operator-entered fallback when the supported deployment topology cannot be discovered automatically.

## Fixed

- Removed placeholder Kitsu hostname fallback from the login path.
- Preserved the Studio Manager-or-higher login requirement.
- Added bounded, read-only Kitsu API validation for operator-supplied endpoints.

## Changed

- Endpoint resolution order is explicit configuration, previously verified saved endpoint, supported local discovery, then validated operator input.
- A manually entered endpoint is persisted only after successful Kitsu authentication.
- Discovery source diagnostics remain safe and never include credentials or tokens.

## Upgrade notes

- No database migration is required.
- Preserve existing SQLite data, the runtime secret, and configuration together during upgrade.
- If Kitsu cannot be detected automatically, open `/bot/login` and enter the Kitsu base URL when prompted. KitsuSync verifies it before use.

## Validation

- Focused discovery and login tests pass in the supported CGO-enabled environment.
- Full deployment verification remains topology-dependent when the Kitsu service is host-loopback-only and unreachable from the KitsuSync container.
