#!/usr/bin/env python3
"""Mobile responsiveness patch for the LAM-Router dashboard.

Fixes two surfaces that shipped desktop-only:
  * Playground model picker (lazy chunk playground-*.js): the popover was a
    2%-opaque glass panel with a fixed 340px width and a grid capped at 380px
    with overflow:hidden, so on a phone the page text showed through it, the
    panel ran off-screen, and the model list was clipped mid-row.
  * Prompt Inject standalone page (dist/prompt-inject.html): no media queries at
    all; the combo summary squeezed name + target + status + preview into one
    line, the mode select kept a 236px min-width, and inputs used 12px text
    (which makes iOS zoom on focus).

Idempotent: every edit is guarded by a marker string.
Usage: python3 lr-mobile-ui-patch.py [--root <LAM-Router checkout>] [--check-only]
"""
from pathlib import Path
import argparse
import hashlib
import os
import re
import shutil
import subprocess
import sys
import time

MARKER_CSS = "lr-mobile-v1"
MARKER_CLASS = "lr-mp"

PROMPT_CSS = """  /* lr-mobile-v1 — phones: stack, unclamp, thumb-sized targets */
  @media (max-width: 640px) {
    .wrap { max-width: none; }
    h1 { font-size: 20px; }
    .sub { font-size: 12px; line-height: 1.55; }
    .stack { margin-top: 14px; gap: 12px; }
    .card { padding: 14px; border-radius: 12px; }
    .card-head { gap: 8px; }
    .card-head > div:first-child { min-width: 0; }
    .card-hint { font-size: 11px; }

    /* 16px keeps iOS Safari from zooming the page on focus */
    textarea { font-size: 16px; min-height: 104px; }
    input.field { font-size: 16px; min-width: 0; flex: 1 1 100%; height: 42px; }

    /* action rows: full-width select, then counter + wrapping buttons */
    .row { gap: 8px; }
    .row .spacer { display: none; }
    .row .counter { margin-right: auto; }
    #globalModeSlot, [data-role="mode-slot"] { flex: 1 1 100%; min-width: 0; }
    .dd { width: 100%; min-width: 0; }
    .dd-btn { min-width: 0; width: 100%; max-width: none; height: 42px; font-size: 13px; }
    .dd-pop { left: 0; right: 0; width: auto; max-width: none; }
    .dd-item { padding: 10px 8px; }
    button.btn, .file-label { min-height: 40px; padding: 9px 14px; font-size: 12.5px; }
    .pv-btn { min-height: 40px; }
    /* primary save owns a full-width row on a phone */
    #globalSave, [data-role="save"] { flex: 1 1 100%; order: 9; min-height: 44px; }

    /* combo rows: name + status on line one, routing target and preview below */
    .combo-summary { flex-wrap: wrap; row-gap: 4px; padding: 11px 12px; }
    .combo-name { min-width: 0; overflow-wrap: anywhere; }
    .pill { order: 2; margin-left: auto; }
    .chev { order: 3; margin-left: 6px; }
    .combo-target { order: 4; flex: 1 1 100%; min-width: 0; overflow-wrap: anywhere; line-height: 1.45; }
    .preview-text {
      order: 5; flex: 1 1 100%; max-width: none; white-space: normal;
      display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 2; overflow: hidden;
    }
    .combo-body { padding: 12px; }
    .combo-body textarea { min-height: 132px; }
  }
"""

SPA_CSS = """<style id="lr-mobile-picker">
        /* lr-mobile-v3 — the model picker must be an opaque, fixed-geometry sheet.
           Resolving --card (#ffffff06) proved bg-card/95 painted ~2% white, so page
           text showed through the list. And a content-sized list resized the popover
           on every provider switch, moving the rows under the cursor: the content
           area now has one fixed height at every provider/filter state. */
        .lr-mp.lr-mp {
            background-color: var(--popover, #0f1011);
        }
        .lr-mp-grid.lr-mp-grid {
            height: 360px;
            min-height: 0;
            max-height: none;
        }
        .lr-mp-providers.lr-mp-providers,
        .lr-mp-models.lr-mp-models {
            height: 100%;
            max-height: none;
        }
        @media (max-width: 640px) {
            .lr-mp.lr-mp {
                left: 0;
                right: 0;
                width: auto;
                max-width: none;
                max-height: max(240px, calc(100vh - 18rem));
                max-height: max(240px, calc(100dvh - 18rem));
                padding: 8px;
                overflow: hidden;
            }
            .lr-mp-grid.lr-mp-grid {
                display: flex;
                flex-direction: column;
                flex: 0 0 auto;
                height: 300px;
                min-height: 0;
                max-height: none;
                gap: 8px;
                overflow: hidden;
            }
            .lr-mp-providers.lr-mp-providers {
                display: flex;
                flex: 0 0 auto;
                flex-direction: row;
                align-items: center;
                gap: 6px;
                height: auto;
                max-height: none;
                overflow-x: auto;
                overflow-y: hidden;
                padding: 0 0 8px;
                border-right: 0;
                border-bottom: 1px solid var(--border, rgba(255, 255, 255, 0.08));
                scrollbar-width: none;
            }
            .lr-mp-providers.lr-mp-providers::-webkit-scrollbar { display: none; }
            .lr-mp-providers.lr-mp-providers > * { margin-top: 0; flex: 0 0 auto; }
            .lr-mp-providers.lr-mp-providers > :first-child { display: none; }
            .lr-mp-providers.lr-mp-providers > button {
                width: auto;
                min-height: 38px;
                padding: 6px 11px;
                gap: 8px;
                white-space: nowrap;
            }
            .lr-mp-models.lr-mp-models {
                flex: 1 1 auto;
                height: auto;
                min-height: 0;
                max-height: none;
                overflow-y: auto;
                overscroll-behavior: contain;
            }
            .lr-mp-models.lr-mp-models > button { min-height: 44px; }
        }
    </style>"""

CHUNK_EDITS = [
    (
        '"absolute left-0 sm:left-4 bottom-full z-50 mb-2 w-[340px] sm:w-[560px] rounded-xl border border-border bg-card/95 p-2 shadow-2xl backdrop-blur-xl text-foreground flex flex-col font-sans"',
        '"absolute left-0 sm:left-4 bottom-full z-50 mb-2 w-[340px] sm:w-[560px] rounded-xl border border-border bg-card/95 p-2 shadow-2xl backdrop-blur-xl text-foreground flex flex-col font-sans lr-mp"',
    ),
    (
        '"grid grid-cols-1 sm:grid-cols-12 gap-2 min-h-[260px] max-h-[380px] overflow-hidden"',
        '"grid grid-cols-1 sm:grid-cols-12 gap-2 min-h-[260px] max-h-[380px] overflow-hidden lr-mp-grid"',
    ),
    (
        '"sm:col-span-4 border-r border-border pr-1.5 space-y-1 overflow-y-auto max-h-[360px] text-xs font-mono"',
        '"sm:col-span-4 border-r border-border pr-1.5 space-y-1 overflow-y-auto max-h-[360px] text-xs font-mono lr-mp-providers"',
    ),
    (
        '} overflow-y-auto space-y-1 p-0.5 max-h-[360px]`',
        '} overflow-y-auto space-y-1 p-0.5 max-h-[360px] lr-mp-models`',
    ),
]


def sha(path):
    with open(path, "rb") as f:
        return hashlib.sha256(f.read()).hexdigest()[:12]


def backup(path, backup_dir, tag):
    os.makedirs(backup_dir, exist_ok=True)
    dest = os.path.join(backup_dir, tag)
    shutil.copy2(path, dest)
    return dest


def patch_prompt_page(path, backup_dir):
    src = open(path, encoding="utf-8").read()
    if MARKER_CSS in src:
        print(f"  skip (already patched): {path}")
        return False
    anchor = "\n</style>\n</head>"
    if src.count(anchor) != 1:
        raise SystemExit(f"anchor </style></head> appears {src.count(anchor)}x in {path}")
    backup(path, backup_dir, os.path.basename(path) + ".orig")
    src = src.replace(anchor, "\n" + PROMPT_CSS + anchor)
    open(path, "w", encoding="utf-8").write(src)
    print(f"  patched mobile CSS: {path}")
    return True


def patch_index(path, backup_dir):
    src = open(path, encoding="utf-8").read()
    if 'id="lr-mobile-picker"' in src:
        print(f"  skip (already patched): {path}")
        return False
    anchor = "</head>"
    if src.count(anchor) != 1:
        raise SystemExit(f"anchor </head> appears {src.count(anchor)}x in {path}")
    backup(path, backup_dir, os.path.basename(path) + ".orig")
    src = src.replace(anchor, SPA_CSS + "\n</head>")
    open(path, "w", encoding="utf-8").write(src)
    print(f"  patched picker CSS: {path}")
    return True


def patch_chunk(path, backup_dir):
    src = open(path, encoding="utf-8").read()
    if MARKER_CLASS in src:
        print(f"  skip (already patched): {path}")
        return False
    for old, new in CHUNK_EDITS:
        n = src.count(old)
        if n != 1:
            raise SystemExit(f"anchor appears {n}x in {path}: {old[:60]}")
        src = src.replace(old, new)
    backup(path, backup_dir, os.path.basename(path) + ".orig")
    open(path, "w", encoding="utf-8").write(src)
    print(f"  patched picker chunk: {path}")

    tmp = "/tmp/lr-chunk-check.mjs"
    shutil.copy2(path, tmp)
    res = subprocess.run(["node", "--check", tmp], capture_output=True, text=True)
    if res.returncode != 0:
        raise SystemExit("node --check failed: " + res.stderr[:800])
    print("  node --check: ok")
    return True


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default=str(Path(__file__).resolve().parents[2]))
    ap.add_argument("--check-only", action="store_true")
    args = ap.parse_args()

    stamp = time.strftime("%Y%m%d-%H%M%S")
    backup_dir = f"/tmp/lr-ui-backup-{stamp}"
    dists = [os.path.join(args.root, d) for d in ("internal/webdist/dist", "web/dist")]
    changed = []

    for dist in dists:
        if not os.path.isdir(dist):
            continue
        print(dist)
        prompt_page = os.path.join(dist, "prompt-inject.html")
        index = os.path.join(dist, "index.html")
        chunks = [f for f in os.listdir(os.path.join(dist, "assets"))
                  if re.fullmatch(r"playground-[A-Za-z0-9_\-]+\.js", f)]
        before = {p: sha(p) for p in [prompt_page, index] if os.path.exists(p)}
        for path in [prompt_page, index]:
            if os.path.exists(path):
                if path.endswith("prompt-inject.html"):
                    patch_prompt_page(path, backup_dir)
                else:
                    patch_index(path, backup_dir)
        if not chunks:
            print("  no playground chunk in this copy")
        for chunk in chunks:
            patch_chunk(os.path.join(dist, "assets", chunk), backup_dir)
        for path in before:
            after = sha(path)
            state = "changed" if before[path] != after else "unchanged"
            changed.append(f"{path}: {before[path]} -> {after} ({state})")

    print("\nsummary")
    for line in changed:
        print("  " + line)
    print(f"  backups: {backup_dir}")

    if args.check_only:
        print("check-only: no build")
        return

    print("\nnext: go build -o bin/lam-router ./cmd/lamrouter && pm2 restart lamrouter")
    if not os.path.isdir(backup_dir):
        print("note: nothing was patched (already applied?)")


if __name__ == "__main__":
    sys.exit(main())
