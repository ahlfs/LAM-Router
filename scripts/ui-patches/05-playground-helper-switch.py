#!/usr/bin/env python3
"""Step 3: expose the hidden helper requests (session title + follow-up suggestions).

Each assistant reply fired a second billed request for follow-up suggestions, and the
first message of a session fired a third one to auto-title it. Neither was visible in the
UI. This adds a switch in Playground settings that gates both and persists the choice.

Idempotent: marker LrPgAuxOn.
"""
from pathlib import Path
import argparse
import os
import re
import shutil
import subprocess
import time

SWITCH = (
    'l.jsxs("div",{style:{display:"flex",alignItems:"center",justifyContent:"space-between",'
    'gap:"8px",padding:"7px 9px",borderRadius:"8px",border:"1px solid var(--line)",'
    'background:"var(--field)"},children:['
    'l.jsxs("div",{style:{minWidth:0},children:['
    'l.jsx("div",{className:"font-mono text-[11px] font-semibold text-[var(--ink)]",children:"AI helper requests"}),'
    'l.jsx("div",{className:"font-mono text-[9.5px] text-[var(--ink-3)]",'
    'children:"auto title + follow-ups \\u00b7 extra billed calls"})]}),'
    'l.jsx("button",{type:"button",role:"switch","aria-checked":AG?"true":"false",'
    'onClick:()=>AGC(!AG),title:"Toggle AI helper requests (auto title + follow-up suggestions)",'
    'style:{position:"relative",width:"32px",height:"18px",borderRadius:"999px",flexShrink:0,'
    'border:"1px solid "+(AG?"rgba(16,185,129,.5)":"var(--line)"),'
    'background:AG?"rgba(16,185,129,.25)":"var(--field)",transition:"background .16s"},'
    'children:l.jsx("span",{style:{position:"absolute",top:"2px",left:AG?"16px":"2px",'
    'width:"12px",height:"12px",borderRadius:"50%",background:AG?"#10b981":"var(--ink-3)",'
    'transition:"left .16s"}})})]}),'
)

EDITS = [
    # 1) state
    ('[g,P]=x.useState(()=>!!LrPgCfg().think)',
     '[g,P]=x.useState(()=>!!LrPgCfg().think),[LrPgAuxOn,LrPgSetAux]=x.useState(()=>LrPgCfg().aux!==!1)'),
    # 2) persist
    ('LrPgSave({prompt:A,temp:N,max:D,think:g})},[A,N,D,g]',
     'LrPgSave({prompt:A,temp:N,max:D,think:g,aux:LrPgAuxOn})},[A,N,D,g,LrPgAuxOn]'),
    # 3) send() must see the current toggle value (its callback deps are stale otherwise)
    ('}},[u,m,W,h,i]),de=x.useCallback',
     '}},[u,m,W,h,i,n,LrPgAuxOn]),de=x.useCallback'),
    # 4) gate the follow-up request
    ('(async()=>{var H,he,re;try{f(I=>I.map(X=>X.id===M?{...X,isGeneratingFollowUps:!0}:X));',
     'LrPgAuxOn&&(async()=>{var H,he,re;try{f(I=>I.map(X=>X.id===M?{...X,isGeneratingFollowUps:!0}:X));'),
    # 5) gate the auto-title request
    ('st&&(st.title==="New Conversation"||!st.title)&&(async()=>{var he,re,fe,I;try{',
     'LrPgAuxOn&&st&&(st.title==="New Conversation"||!st.title)&&(async()=>{var he,re,fe,I;try{'),
    # 6) expose on the hook result
    ('clearMessages:ke,generateCode:tt,handleCopyCode:zt}',
     'clearMessages:ke,auxOn:LrPgAuxOn,setAuxOn:LrPgSetAux,generateCode:tt,handleCopyCode:zt}'),
    # 7) composer component props
    ('maxTokens:j=2048,onMaxTokensChange:T,onOpenCode:b,onClear:g,variant:P="Rounded"}',
     'maxTokens:j=2048,onMaxTokensChange:T,onOpenCode:b,onClear:g,auxOn:AQ=!0,onAuxChange:AQC,variant:P="Rounded"}'),
    # 8) pass them into the settings panel
    ('l.jsx(yf,{systemPrompt:p,onSystemPromptChange:v,temperature:y,onTemperatureChange:w,maxTokens:j,onMaxTokensChange:T,onOpenCode:b,onClear:g,hasMessages:a})',
     'l.jsx(yf,{systemPrompt:p,onSystemPromptChange:v,temperature:y,onTemperatureChange:w,maxTokens:j,onMaxTokensChange:T,onOpenCode:b,onClear:g,hasMessages:a,auxOn:AQ,onAuxChange:AQC})'),
    # 9) page level: hook state -> composer props
    ('onMaxTokensChange:o.setMaxTokens,onOpenCode:',
     'onMaxTokensChange:o.setMaxTokens,auxOn:o.auxOn,onAuxChange:o.setAuxOn,onOpenCode:'),
    # 10) settings panel signature
    ('function yf({systemPrompt:e,onSystemPromptChange:t,temperature:n,onTemperatureChange:s,maxTokens:i,onMaxTokensChange:a,onOpenCode:r,onClear:o,hasMessages:c}){',
     'function yf({systemPrompt:e,onSystemPromptChange:t,temperature:n,onTemperatureChange:s,maxTokens:i,onMaxTokensChange:a,onOpenCode:r,onClear:o,hasMessages:c,auxOn:AG=!0,onAuxChange:AGC}){'),
    # 11) the "modified" dot also reflects an opted-out helper switch
    ('const f=!!e.trim()||n!==.7||i!==2048',
     'const f=!!e.trim()||n!==.7||i!==2048||AG===!1'),
    # 12) render the switch above the action row
    ('l.jsxs("div",{className:"flex items-center gap-1.5 pt-1",children:[l.jsxs("button",{type:"button",onClick:()=>{r(),d(!1)},',
     SWITCH + 'l.jsxs("div",{className:"flex items-center gap-1.5 pt-1",children:[l.jsxs("button",{type:"button",onClick:()=>{r(),d(!1)},'),
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
            if "LrPgAuxOn" in src:
                print(f"  skip (already patched): {path}")
                continue
            for old, new in EDITS:
                n = src.count(old)
                if n != 1:
                    raise SystemExit(f"anchor appears {n}x in {path}: {old[:70]!r}")
                src = src.replace(old, new)
            os.makedirs(backup_dir, exist_ok=True)
            shutil.copy2(path, os.path.join(backup_dir, os.path.basename(path) + ".aux.orig"))
            open(path, "w", encoding="utf-8").write(src)
            tmp = "/tmp/lr-chunk-check.mjs"
            shutil.copy2(path, tmp)
            res = subprocess.run(["node", "--check", tmp], capture_output=True, text=True)
            if res.returncode != 0:
                raise SystemExit("node --check failed: " + res.stderr[:900])
            print(f"  helper-request switch patched: {path} (node --check ok)")
    print(f"  backups: {backup_dir}")


if __name__ == "__main__":
    main()
