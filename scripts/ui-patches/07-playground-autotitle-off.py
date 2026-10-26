#!/usr/bin/env python3
"""Step 3c: stop the auto-title helper request.

It is redundant — a local effect already names a session from its first user message — and
its guard reads a possibly-stale sessions array, so it fired nondeterministically (observed
twice for one send). Disabling the call keeps the endpoint unused instead of billing an extra
request per new session. Idempotent marker: !1&&st&&(st.title==="New Conversation"
"""
from pathlib import Path
import argparse, os, re, shutil, subprocess, time

OLD = 'st&&(st.title==="New Conversation"||!st.title)&&(async()=>{'
NEW = '!1&&st&&(st.title==="New Conversation"||!st.title)&&(async()=>{'

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
            if '!1&&st&&(st.title==="New Conversation"' in src:
                print(f"  skip (already patched): {path}"); continue
            n = src.count(OLD)
            if n != 1:
                raise SystemExit(f"anchor appears {n}x in {path}")
            src = src.replace(OLD, NEW)
            os.makedirs(backup_dir, exist_ok=True)
            shutil.copy2(path, os.path.join(backup_dir, os.path.basename(path) + ".title.orig"))
            open(path, "w", encoding="utf-8").write(src)
            tmp = "/tmp/lr-chunk-check.mjs"; shutil.copy2(path, tmp)
            res = subprocess.run(["node", "--check", tmp], capture_output=True, text=True)
            if res.returncode != 0:
                raise SystemExit("node --check failed: " + res.stderr[:600])
            print(f"  auto-title request disabled: {path} (node --check ok)")
    print(f"  backups: {backup_dir}")

if __name__ == "__main__":
    main()
