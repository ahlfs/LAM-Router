#!/usr/bin/env python3
"""Step 3b: give send() a fresh sessions array so auto-title stops being skipped.

`et` (send) closed over `n` (sessions) without listing it as a dependency, so right after
creating a session it saw the pre-creation array, `st` was undefined and the title request
silently never fired. Idempotent marker: i,n,LrPgAuxOn.
"""
from pathlib import Path
import argparse, os, re, shutil, subprocess, time

OLD = '}},[u,m,W,h,i,LrPgAuxOn]),de=x.useCallback'
NEW = '}},[u,m,W,h,i,n,LrPgAuxOn]),de=x.useCallback'

def main():
    ap = argparse.ArgumentParser(); ap.add_argument("--root", default=str(Path(__file__).resolve().parents[2]))
    args = ap.parse_args()
    backup_dir = f"/tmp/lam-router-ui-backup-{time.strftime('%Y%m%d-%H%M%S')}"
    for dist in ("internal/webdist/dist", "web/dist"):
        assets = os.path.join(args.root, dist, "assets")
        if not os.path.isdir(assets):
            continue
        for name in sorted(os.listdir(assets)):
            if not re.fullmatch(r"playground-[A-Za-z0-9_\-]+\.js", name):
                continue
            path = os.path.join(assets, name)
            src = open(path, encoding="utf-8").read()
            if 'i,n,LrPgAuxOn' in src:
                print(f"  skip (already patched): {path}"); continue
            n = src.count(OLD)
            if n != 1:
                raise SystemExit(f"anchor appears {n}x in {path}")
            src = src.replace(OLD, NEW)
            os.makedirs(backup_dir, exist_ok=True)
            shutil.copy2(path, os.path.join(backup_dir, os.path.basename(path) + ".deps.orig"))
            open(path, "w", encoding="utf-8").write(src)
            tmp = "/tmp/lr-chunk-check.mjs"; shutil.copy2(path, tmp)
            res = subprocess.run(["node", "--check", tmp], capture_output=True, text=True)
            if res.returncode != 0:
                raise SystemExit("node --check failed: " + res.stderr[:600])
            print(f"  send deps fixed: {path} (node --check ok)")
    print(f"  backups: {backup_dir}")

if __name__ == "__main__":
    main()
