#!/usr/bin/env python3
"""Neural Pipeline: stop drawing providers that are not connected.

The topology component built its node list as
    p = connected.length > 0 ? connected : allProviders.slice(0, 8)
so an install with no connection at all still painted eight provider clusters taken from the
catalog, reading as "these providers exist but are idle" instead of "nothing is connected".
Now only connected providers are drawn, and an empty state explains what to do.

Idempotent marker: data-lr-empty.
"""
from pathlib import Path
import argparse
import os
import re
import shutil
import subprocess
import time

OLD_FILTER = 'g=M.useMemo(()=>e.filter(y=>{var S,$;return En(y)||(((S=y.status)==null?void 0:S.connectedCount)??0)>0||(($=y.status)==null?void 0:$.state)==="connected"||y.connections&&y.connections.length>0}),[e])'
NEW_FILTER = 'g=M.useMemo(()=>e.filter(y=>En(y)),[e])'
OLD_PICK = 'p=M.useMemo(()=>g.length>0?g:e.slice(0,8),[g,e])'
NEW_PICK = 'p=M.useMemo(()=>g,[g])'

GRAPH_OLD = ('s==="graph"?c.jsxs("div",{className:"h-[520px] w-full rounded-lg border '
             'border-border/60 bg-background/50 overflow-hidden relative",children:[c.jsxs(cg,{')
GRAPH_NEW = ('s==="graph"?c.jsxs("div",{className:"h-[520px] w-full rounded-lg border '
             'border-border/60 bg-background/50 overflow-hidden relative",children:['
             'p.length===0&&c.jsx("div",{"data-lr-empty":"picker",style:{position:"absolute",'
             'inset:0,display:"flex",alignItems:"center",justifyContent:"center",zIndex:5,'
             'pointerEvents:"none",padding:"24px"},children:c.jsxs("div",{style:{textAlign:"center",'
             'maxWidth:"320px"},children:['
             'c.jsx("div",{style:{fontSize:"11.5px",fontWeight:700,color:"var(--foreground)"},'
             'children:"No provider connected"}),'
             'c.jsx("div",{style:{fontSize:"10.5px",lineHeight:1.6,color:"var(--muted-foreground)",'
             'marginTop:"6px"},children:"The pipeline only draws providers with an active connection. '
             'Add one under Providers to see it here."})]})}),c.jsx(cg,{')


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default=str(Path(__file__).resolve().parents[2]))
    args = ap.parse_args()

    backup_dir = f"/tmp/lam-router-ui-backup-{time.strftime('%Y%m%d-%H%M%S')}"
    for dist in ("internal/webdist/dist", "web/dist"):
        assets = os.path.join(args.root, dist, "assets")
        if not os.path.isdir(assets):
            continue
        for name in sorted(os.listdir(assets)):
            path = os.path.join(assets, name)
            if not re.fullmatch(r"index-[A-Za-z0-9_\-]+\.js", name):
                continue
            src = open(path, encoding="utf-8").read()
            if OLD_FILTER not in src and OLD_PICK not in src and "data-lr-empty" not in src:
                continue  # chunk without the topology component
            if OLD_FILTER not in src and "data-lr-empty" in src:
                print(f"  skip (already patched): {path}")
                continue
            for old, new in ((OLD_FILTER, NEW_FILTER), (OLD_PICK, NEW_PICK), (GRAPH_OLD, GRAPH_NEW)):
                if old not in src:
                    if new in src:
                        continue  # this edit is already in place
                    raise SystemExit(f"anchor missing in {path}: {old[:70]!r}")
                n = src.count(old)
                if n != 1:
                    raise SystemExit(f"anchor appears {n}x in {path}: {old[:60]!r}")
                src = src.replace(old, new)
            os.makedirs(backup_dir, exist_ok=True)
            shutil.copy2(path, os.path.join(backup_dir, os.path.basename(path) + ".pipe.orig"))
            open(path, "w", encoding="utf-8").write(src)
            tmp = "/tmp/lr-chunk-check.mjs"
            shutil.copy2(path, tmp)
            res = subprocess.run(["node", "--check", tmp], capture_output=True, text=True)
            if res.returncode != 0:
                raise SystemExit("node --check failed: " + res.stderr[:700])
            print(f"  pipeline fixed: {path} (node --check ok)")
    print(f"  backups: {backup_dir}")


if __name__ == "__main__":
    main()
