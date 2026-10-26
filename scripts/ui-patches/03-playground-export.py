#!/usr/bin/env python3
"""Step 1: make "Export Code" reproduce the request the Playground actually sends.

The generator built every snippet from {model, messages, stream} only, while the live
request also carries temperature, max_tokens and (with Think on) reasoning_effort — so a
pasted snippet was a different request. v1 adds the three fields to all four languages.

Idempotent: marker LrPgExport.
"""
from pathlib import Path
import argparse
import os
import re
import shutil
import subprocess
import time

OLD_SIG = 'function Za(e,t){const{apiBase:n,modelId:s,activeChatId:i,messages:a}=t;switch(e){'
NEW_SIG = ('function Za(e,t){const{apiBase:n,modelId:s,activeChatId:i,messages:a}=t,'
           'T=typeof t.temperature=="number"?t.temperature:void 0,'
           'M=typeof t.maxTokens=="number"?t.maxTokens:void 0,'
           'G=!!t.thinking,R=G?{reasoning_effort:"high"}:{};switch(e){')

EDITS = [
    # signature + params
    (OLD_SIG, NEW_SIG),
    # curl payload
    ('JSON.stringify({model:s,messages:a,stream:!0},null,2)',
     'JSON.stringify({model:s,messages:a,stream:!0,...(T!==void 0?{temperature:T}:{}),'
     '...(M!==void 0?{max_tokens:M}:{}),...R},null,2)'),
    # typescript body
    ('    messages: ${JSON.stringify(a,null,4)},\n    stream: true,\n  });',
     '    messages: ${JSON.stringify(a,null,4)},\n    stream: true,\n'
     '${T!==void 0?`    temperature: ${T},\\n`:""}${M!==void 0?`    max_tokens: ${M},\\n`:""}'
     '${G?`    reasoning_effort: "high",\\n`:""}  });'),
    # python body
    ('    messages=${JSON.stringify(a,null,4)},\n    stream=True,\n)',
     '    messages=${JSON.stringify(a,null,4)},\n    stream=True,\n'
     '${T!==void 0?`    temperature=${T},\\n`:""}${M!==void 0?`    max_tokens=${M},\\n`:""}'
     '${G?`    extra_body={"reasoning_effort": "high"},\\n`:""})'),
    # fetch body
    ('    messages: ${JSON.stringify(a,null,4)},\n    stream: true,\n  }),',
     '    messages: ${JSON.stringify(a,null,4)},\n    stream: true,\n'
     '${T!==void 0?`    temperature: ${T},\\n`:""}${M!==void 0?`    max_tokens: ${M},\\n`:""}'
     '${G?`    reasoning_effort: "high",\\n`:""}  }),'),
    # pass the values in from the hook
    ('Za(R,{apiBase:Ft,modelId:(W==null?void 0:W.id)??"model-id",activeChatId:i,messages:Bt()}),[W,h,Ft,i])',
     'Za(R,{apiBase:Ft,modelId:(W==null?void 0:W.id)??"model-id",activeChatId:i,messages:Bt(),'
     'temperature:N,maxTokens:D,thinking:g}),[W,h,Ft,i,N,D,g])'),
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
            if 'T=typeof t.temperature=="number"' in src:
                print(f"  skip (already patched): {path}")
                continue
            for old, new in EDITS:
                n = src.count(old)
                if n != 1:
                    raise SystemExit(f"anchor appears {n}x in {path}: {old[:70]!r}")
                src = src.replace(old, new)
            os.makedirs(backup_dir, exist_ok=True)
            shutil.copy2(path, os.path.join(backup_dir, os.path.basename(path) + ".export.orig"))
            open(path, "w", encoding="utf-8").write(src)
            tmp = "/tmp/lr-chunk-check.mjs"
            shutil.copy2(path, tmp)
            res = subprocess.run(["node", "--check", tmp], capture_output=True, text=True)
            if res.returncode != 0:
                raise SystemExit("node --check failed: " + res.stderr[:700])
            print(f"  export parity patched: {path} (node --check ok)")
    print(f"  backups: {backup_dir}")


if __name__ == "__main__":
    main()
