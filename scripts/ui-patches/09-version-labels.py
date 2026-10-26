#!/usr/bin/env python3
"""Keep the version labels baked into the bundle in sync with npm/package.json.

The dashboard prints its version in two places that live inside the built assets, so a build
that is produced before the release bump ships a stale string: settings shows
`Ce="2.3.1"` and the login screen `"v2.3.1 Live"` while the package is 2.3.2, which
TestEmbeddedBundleVersionLabelsMatchPackage catches. Idempotent: rewrites whatever version is
there to the package version.
"""
import argparse
import json
import os
import re
import shutil
import subprocess
import time
from pathlib import Path

SETTINGS_RE = re.compile(r'Ce="(\d+\.\d+\.\d+[^"]*)"')
LOGIN_RE = re.compile(r'"v(\d+\.\d+\.\d+[^"]*) Live"')


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default=str(Path(__file__).resolve().parents[2]))
    args = ap.parse_args()

    pkg = json.load(open(os.path.join(args.root, "npm", "package.json"), encoding="utf-8"))
    version = pkg["version"]
    prints = {
        "settings-": (SETTINGS_RE, lambda v: f'Ce="{v}"'),
        "index-": (LOGIN_RE, lambda v: f'"v{v} Live"'),
    }

    backup_dir = f"/tmp/lam-router-ui-backup-{time.strftime('%Y%m%d-%H%M%S')}"
    for dist in ("internal/webdist/dist/assets", "web/dist/assets"):
        assets = os.path.join(args.root, dist)
        if not os.path.isdir(assets):
            continue
        for name in sorted(os.listdir(assets)):
            if not name.endswith(".js"):
                continue
            key = next((k for k in prints if name.startswith(k)), None)
            if not key:
                continue
            pattern, render = prints[key]
            path = os.path.join(assets, name)
            src = open(path, encoding="utf-8").read()
            if version in src and render(version) in src:
                print(f"  skip (already {version}): {path}")
                continue
            new, n = pattern.subn(render(version), src, count=1)
            if n == 0:
                continue
            os.makedirs(backup_dir, exist_ok=True)
            shutil.copy2(path, os.path.join(backup_dir, name + ".ver.orig"))
            open(path, "w", encoding="utf-8").write(new)
            tmp = "/tmp/lam-router-ver-check.mjs"
            shutil.copy2(path, tmp)
            res = subprocess.run(["node", "--check", tmp], capture_output=True, text=True)
            if res.returncode != 0:
                raise SystemExit("node --check failed: " + res.stderr[:600])
            print(f"  version label -> {version}: {path}")
    print(f"  backups: {backup_dir}")


if __name__ == "__main__":
    main()
