# UI patches for `dist/`

This repository ships the dashboard only as built assets (`internal/webdist/dist` + the
`web/dist` mirror); there is no `web/src`. Every UI change therefore is a surgical patch of
minified JS/CSS. A later `make build-ui` (or any checkout of the authoring monorepo) replaces
`dist/` wholesale and silently drops those patches.

These scripts are that work, kept next to the code so it can be replayed. Each one is
idempotent (guarded by a marker string) and rewrites **both** copies of `dist`.

## Order

Run in numeric order after any `dist/` rebuild, then rebuild the binary:

```bash
cd <repo root>
for f in scripts/ui-patches/0*.py; do python3 "$f"; done
go build -o bin/lam-router ./cmd/lamrouter
```

| script | surface | what it fixes |
|---|---|---|
| `01-mobile-responsive.py` | model picker + `prompt-inject.html` | picker panel was a ~2% opaque glass panel with a fixed 340px width and a clipped, content-sized list; prompt-inject page had no media queries at all |
| `02-picker-geometry.py` | model picker | opaque fixed-geometry panel (360px desktop / 300px mobile list viewport) so switching provider no longer resizes the popover; settings popover anchored to the composer on phones |
| `03-playground-export.py` | Playground → Export Code | exported snippets now carry `temperature`, `max_tokens`, `reasoning_effort` (Python via `extra_body`) like the real request |
| `04-playground-persist.py` | Playground settings | system prompt / temperature / max tokens / Think survive a reload (`localStorage["lamrouter_playground_last"]`), app defaults stay the seed |
| `05-playground-helper-switch.py` | Playground settings | "AI helper requests" switch gates the follow-up-suggestion request (it was billed invisibly on every reply) |
| `06-playground-send-deps.py` | Playground send() | `sessions` added to the callback deps so title logic cannot read a stale array |
| `07-playground-autotitle-off.py` | Playground send() | disables the redundant auto-title request (local effect already names the session; its guard fired nondeterministically) |
| `08-pipeline-connected.py` | Dashboard → Neural Pipeline | no catalog fallback and no `connections.length` bypass: only providers with a connected status are drawn, plus a "No provider connected" empty state |
| `09-version-labels.py` | settings + login chunks | keeps the baked-in version strings in sync with `npm/package.json` |

`picker-v5.css` is the stylesheet injected by `02-picker-geometry.py`.

## Conventions

- Anchors must appear exactly once; the scripts abort otherwise. If a rebuild changes the
  chunk hashes, the anchors stay valid (they are content strings, not filenames) as long as
  the upstream markup did not change.
- Every script takes `--root <repo>` (defaults to this checkout) and copies the original file
  to `/tmp/lam-router-ui-backup-<timestamp>/` before writing.
- JS edits are gated with `node --check` on an `.mjs` copy; CSS/HTML edits are not.
- Class tokens added to a chunk should already exist in the compiled Tailwind CSS, or be
  driven by a marker class plus real CSS in `dist/index.html` (arbitrary and `sm:` variants
  that were never compiled silently do nothing).
