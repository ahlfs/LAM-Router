#!/usr/bin/env python3
"""Make the Playground settings popover usable on phones (v4).

The popover is `absolute ... left-0 w-80` inside a `relative inline-block` wrapper
around the gear button, which sits at the right end of the composer. On a phone that
puts a 320px panel 237px off-screen at 390px and 110px off-screen at 320px, so the
controls are literally unreachable. v4 anchors it to the composer instead: the wrapper
goes static (so the composer, already `relative`, becomes the containing block) and the
panel spans the composer width above it.

Also raises the picker style block to v4 (same rules as v3 + the popover fix).
Idempotent: marker lr-mobile-v5 / class marker lr-pgs.
"""
from pathlib import Path
import argparse
import os
import re
import shutil
import subprocess
import time

CSS_FILE = str(Path(__file__).with_name("picker-v5.css"))
MARKER = "lr-mobile-v5"
STYLE_RE = re.compile(r'<style id="lr-mobile-picker">.*?</style>', re.S)

WRAP_OLD = 'className:"relative inline-block",children:[l.jsxs("button",{type:"button",onClick:()=>d(p=>!p),className:`relative flex h-7 size-7'
WRAP_NEW = WRAP_OLD[: len('className:"relative inline-block')] + ' lr-pgs-wrap' + WRAP_OLD[len('className:"relative inline-block'):]

POP_OLD = '"absolute bottom-full mb-3 left-0 z-[9999] w-80 sm:w-96 rounded-[14px] border border-[var(--line)] bg-[var(--surface)] p-3.5 shadow-2xl backdrop-blur-xl animate-in fade-in zoom-in-95 duration-150"'
POP_NEW = POP_OLD[:-1] + ' lr-pgs"'


def patch_chunk(path, backup_dir):
    src = open(path, encoding="utf-8").read()
    if "lr-pgs-wrap" in src:
        print(f"  skip (chunk already v4): {path}")
        return
    for old, new in ((WRAP_OLD, WRAP_NEW), (POP_OLD, POP_NEW)):
        n = src.count(old)
        if n != 1:
            raise SystemExit(f"anchor appears {n}x in {path}: {old[:60]}")
        src = src.replace(old, new)
    os.makedirs(backup_dir, exist_ok=True)
    shutil.copy2(path, os.path.join(backup_dir, os.path.basename(path) + ".orig"))
    open(path, "w", encoding="utf-8").write(src)
    tmp = "/tmp/lr-chunk-check.mjs"
    shutil.copy2(path, tmp)
    res = subprocess.run(["node", "--check", tmp], capture_output=True, text=True)
    if res.returncode != 0:
        raise SystemExit("node --check failed: " + res.stderr[:600])
    print(f"  patched settings popover markers: {path} (node --check ok)")


def patch_index(path, css, backup_dir):
    src = open(path, encoding="utf-8").read()
    m = STYLE_RE.search(src)
    if not m:
        raise SystemExit(f"no picker style block in {path}")
    if MARKER in m.group(0):
        print(f"  skip (style already {MARKER}): {path}")
        return
    os.makedirs(backup_dir, exist_ok=True)
    shutil.copy2(path, os.path.join(backup_dir, os.path.basename(path) + ".orig2"))
    src = src[: m.start()] + css + src[m.end():]
    open(path, "w", encoding="utf-8").write(src)
    print(f"  replaced style block: {path}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default=str(Path(__file__).resolve().parents[2]))
    args = ap.parse_args()

    css = open(CSS_FILE, encoding="utf-8").read().rstrip("\n")
    if MARKER not in css:
        raise SystemExit("css file lacks the marker")

    backup_dir = f"/tmp/lam-router-ui-backup-{time.strftime('%Y%m%d-%H%M%S')}"
    for dist in ("internal/webdist/dist", "web/dist"):
        base = os.path.join(args.root, dist)
        if not os.path.isdir(base):
            continue
        assets = os.path.join(base, "assets")
        for name in sorted(os.listdir(assets)):
            if re.fullmatch(r"playground-[A-Za-z0-9_\-]+\.js", name):
                patch_chunk(os.path.join(assets, name), backup_dir)
        patch_index(os.path.join(base, "index.html"), css, backup_dir)
    print(f"  backups: {backup_dir}")


if __name__ == "__main__":
    main()
