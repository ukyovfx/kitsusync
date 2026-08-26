# KitsuSync v0.4.1 Release Checklist

Use this as the v0.4.1 release gate. Any unchecked item in the `Must Pass` section is a release blocker.

## Must Pass

- [ ] release tag/version matches the intended release (for example `v0.4.1`)
- [ ] the current release notes are ready
- [ ] README matches the current setup flow, scope, limitations, and roadmap
- [ ] `.env.example` and `conf.toml.example` are up to date
- [ ] tracked secrets are absent from the branch diff
- [ ] tracked runtime data and deploy artifacts are absent from the branch diff
- [ ] `.gitignore` blocks `.env*`, SQLite/runtime data, deploy data, and screenshot artifacts
- [ ] clean-clone startup procedure is documented and reviewed
- [ ] `go test ./src/... -count=1 -timeout=120s` passes
- [ ] `go vet ./src/...` passes
- [ ] `docker compose build app` succeeds
- [ ] `docker compose ps` shows `app` healthy
- [ ] `/api/setup/status` smoke test passes
- [ ] auth session smoke test passes
- [ ] notification routing regression test passes
- [ ] log redaction test passes
- [ ] polling logs still show `Connected to Kitsu`, `Got tasks`, and `Got taskStatuses`
- [ ] `/bot/login`, `/bot/setup`, `/bot/admin`, `/bot/admin/projects`, `/bot/admin/health`, and `/bot/docs/` were checked
- [ ] debug-only FileBrowser policy is documented correctly
- [ ] reverse proxy assumptions are documented correctly
- [ ] destructive migrations are not included
- [ ] production DB access is not required for release verification

## Should Pass Before Public Announcement

- [ ] screenshot TODO list is up to date
- [ ] setup wizard docs clearly explain the recommended first-run entry point
- [ ] v0.2.0-only items such as admin audit UI are called out explicitly
- [ ] security-sensitive changes were reviewed
- [ ] CHANGELOG.md was updated
- [ ] release notes mention Added / Changed / Fixed / Security items

## Human Verification Notes

- `sanitized_kitsu_schema.sql` and `sanitized_kitsu_sample.json` should be attached or linked if they are part of the release evidence set
- if release verification needs real Kitsu or Discord credentials, perform that step manually outside this checklist run
