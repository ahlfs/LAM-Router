#!/usr/bin/env python3
"""Step 2: persist Playground settings (system prompt, temperature, max tokens, Think).

The model selection was already remembered while every other control reset to the app
defaults on reload. Values now live in localStorage["lamrouter_playground_last"]; the app
defaults (lamrouter_app_settings) stay as the seed when nothing was used yet.

Idempotent: marker LrPgCfg / LrPgSave.
"""
from pathlib import Path
import argparse
import os
import re
import shutil
import subprocess
import time

HELPERS = (
    'function LrPgCfg(){try{const q=JSON.parse(localStorage.getItem("lamrouter_playground_last")||"{}");'
    'return q&&typeof q==="object"?q:{}}catch{return {}}}'
    'function LrPgSave(q){try{localStorage.setItem("lamrouter_playground_last",JSON.stringify(q))}catch{}}'
)

APP = 'JSON.parse(localStorage.getItem("lamrouter_app_settings")||"{}")'

EDITS = [
    # helper functions, inserted at a top-level statement boundary
    ('function sl({open:e,onOpenChange:t,exportLanguage:n,onExportLanguageChange:s,generateCode:i,canCopy:a,copied:r,onCopy:o,selectedModel:c}){',
     HELPERS + 'function sl({open:e,onOpenChange:t,exportLanguage:n,onExportLanguageChange:s,generateCode:i,canCopy:a,copied:r,onCopy:o,selectedModel:c}){'),

    # system prompt: last-used wins, app default is the seed
    ('[A,k]=x.useState(()=>{try{const q=' + APP + ';return typeof q.systemPromptDefault==="string"?q.systemPromptDefault:""}catch{return ""}})',
     '[A,k]=x.useState(()=>{const Pq=LrPgCfg();if(typeof Pq.prompt==="string")return Pq.prompt;try{const q=' + APP + ';return typeof q.systemPromptDefault==="string"?q.systemPromptDefault:""}catch{return ""}})'),

    # temperature
    ('[N,C]=x.useState(()=>{try{const q=' + APP + ';return typeof q.defaultTemperature==="number"?q.defaultTemperature:.7}catch{return .7}})',
     '[N,C]=x.useState(()=>{const Pq=LrPgCfg();if(typeof Pq.temp==="number")return Pq.temp;try{const q=' + APP + ';return typeof q.defaultTemperature==="number"?q.defaultTemperature:.7}catch{return .7}})'),

    # max tokens
    ('[D,O]=x.useState(()=>{try{const q=' + APP + ';return typeof q.defaultMaxTokens==="number"?q.defaultMaxTokens:2048}catch{return 2048}})',
     '[D,O]=x.useState(()=>{const Pq=LrPgCfg();if(typeof Pq.max==="number")return Pq.max;try{const q=' + APP + ';return typeof q.defaultMaxTokens==="number"?q.defaultMaxTokens:2048}catch{return 2048}})'),

    # thinking toggle
    ('[g,P]=x.useState(!1)',
     '[g,P]=x.useState(()=>!!LrPgCfg().think)'),

    # write-back effect, placed right after the last ref/derived declaration
    ('Ft=va();x.useEffect(()=>()=>{',
     'Ft=va();x.useEffect(()=>{LrPgSave({prompt:A,temp:N,max:D,think:g})},[A,N,D,g]);x.useEffect(()=>()=>{'),
]


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
            if not re.fullmatch(r"playground-[A-Za-z0-9_\-]+\.js", name):
                continue
            path = os.path.join(assets, name)
            src = open(path, encoding="utf-8").read()
            if "LrPgCfg" in src:
                print(f"  skip (already patched): {path}")
                continue
            for old, new in EDITS:
                n = src.count(old)
                if n != 1:
                    raise SystemExit(f"anchor appears {n}x in {path}: {old[:70]!r}")
                src = src.replace(old, new)
            os.makedirs(backup_dir, exist_ok=True)
            shutil.copy2(path, os.path.join(backup_dir, os.path.basename(path) + ".persist.orig"))
            open(path, "w", encoding="utf-8").write(src)
            tmp = "/tmp/lr-chunk-check.mjs"
            shutil.copy2(path, tmp)
            res = subprocess.run(["node", "--check", tmp], capture_output=True, text=True)
            if res.returncode != 0:
                raise SystemExit("node --check failed: " + res.stderr[:700])
            print(f"  persistence patched: {path} (node --check ok)")
    print(f"  backups: {backup_dir}")


if __name__ == "__main__":
    main()
