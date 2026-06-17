# Repository Rename for v0.2.1

## Decision
The repository was renamed from `ukyovfx/kitsu-to-discord-task-notification` to `ukyovfx/kitsusync` for v0.2.1.

## Reason
The rename aligns the repository slug with the public product name KitsuSync.

## Canonical repository URL
Use the new canonical repository URL in current public docs and operator instructions:
- `https://github.com/ukyovfx/kitsusync.git`
- `git@github.com:ukyovfx/kitsusync.git`

## Redirect behavior
Older GitHub web and clone URLs may redirect for a time, but current docs should use the new canonical URL instead of relying on redirects.

## Historical notes
Historical notes and release-era policy documents remain historical records and should not be rewritten to hide the previous repository state. This includes:
- the v0.1.1 release notes
- `docs/notes/branding-policy-v0.1.1.md`
- `docs/notes/v0.1.1-release-readiness.md`

## Local clone follow-up
Existing local clones should update `origin` to the new repository URL:
```bash
git remote set-url origin git@github.com:ukyovfx/kitsusync.git
```

## Scope note
This rename note does not change release tags, GitHub Releases, upstream provenance, or application behavior.
