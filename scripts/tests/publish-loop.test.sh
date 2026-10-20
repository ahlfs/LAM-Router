#!/usr/bin/env bash
# Offline regression test for the publish loop in scripts/build-npm.sh.
#
# Why this exists: the loop used to abort the whole release on the first publish
# failure, so one package name that npm's screening refuses (403 "Package name
# triggered spam detection") blocked the root package and every other platform.
# The loop must keep going ONLY for that refusal and still abort on real errors
# (expired token, registry outage) before the root package is published.
#
# It runs the real fragment extracted from build-npm.sh against a stub `npm`, so
# no token and no network are needed.

set -uo pipefail

REPO="${REPO:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
PLATFORMS=(
  "linux:amd64:linux-x64:lam-router"
  "linux:arm64:linux-arm64:lam-router"
  "darwin:arm64:darwin-arm64:lam-router"
  "darwin:amd64:darwin-x64:lam-router"
  "windows:amd64:win32-x64:lam-router.exe"
)

PASS=0
FAIL=0
STATUS=0
OUT=""

setup_work() {
  WORK="$(mktemp -d)"
  ROOT_DIR="$WORK/root"
  VERSION=2.0.0
  for entry in "${PLATFORMS[@]}"; do
    IFS=":" read -r _ _ dir _ <<< "$entry"
    mkdir -p "$ROOT_DIR/npm/platforms/$dir"
  done
  mkdir -p "$WORK/bin"
  cat > "$WORK/bin/npm" <<'STUB'
#!/usr/bin/env bash
case "${1:-}" in
  view)
    if [ "${STUB_ALL_PUBLISHED:-0}" = "1" ]; then echo "2.0.0"; exit 0; fi
    exit 1
    ;;
  publish)
    if [ -n "${STUB_REFUSE_DIR:-}" ] && [[ "$PWD" == *"/$STUB_REFUSE_DIR" ]]; then
      printf '%s\n' \
        "npm error code E403" \
        "npm error 403 403 Forbidden - PUT https://registry.npmjs.org/lam-router-${STUB_REFUSE_DIR} - Package name triggered spam detection; if you believe this is in error, please contact support at https://npmjs.com/support" >&2
      exit 1
    fi
    if [ -n "${STUB_FAIL_DIR:-}" ] && [[ "$PWD" == *"/$STUB_FAIL_DIR" ]]; then
      printf '%s\n' "npm error code E401" "npm error Incorrect or missing password." >&2
      exit 1
    fi
    echo "npm notice stub-published $(basename "$PWD")"
    exit 0
    ;;
esac
exit 0
STUB
  chmod +x "$WORK/bin/npm"
  export PATH="$WORK/bin:$PATH"

  # Prelude + the real loop, extracted so the test cannot drift from the script.
  {
    echo 'set -euo pipefail'
    echo "ROOT_DIR='$ROOT_DIR'"
    echo "VERSION='$VERSION'"
    printf 'PLATFORMS=(%s)\n' "$(printf '"%s" ' "${PLATFORMS[@]}")"
    sed -n '/^  REFUSED=()/,/^  echo "   Verify the channel/p' "$REPO/scripts/build-npm.sh"
  } > "$WORK/run.sh"
}

teardown_work() {
  rm -rf "$WORK"
  unset STUB_REFUSE_DIR STUB_FAIL_DIR STUB_ALL_PUBLISHED
}

run_loop() {
  OUT="$(bash "$WORK/run.sh" 2>&1)"
  STATUS=$?
}

# ok <label> <expected exit> <must contain> [<must not contain>]
ok() {
  local label="$1" want_status="$2" want_text="$3" bad_text="${4:-}"
  local why=""
  [ "$STATUS" = "$want_status" ] || why="exit $STATUS, expected $want_status"
  grep -qF -- "$want_text" <<< "$OUT" || why="${why:+$why; }missing: $want_text"
  if [ -n "$bad_text" ] && grep -qF -- "$bad_text" <<< "$OUT"; then
    why="${why:+$why; }unexpected: $bad_text"
  fi
  if [ -z "$why" ]; then
    echo "  ✅ $label"
    PASS=$((PASS + 1))
  else
    echo "  ❌ $label — $why"
    echo "$OUT" | sed 's/^/     | /'
    FAIL=$((FAIL + 1))
  fi
}

# ---------------------------------------------------------------------------
echo "case 1: npm refuses the win32-x64 name, everything else publishes"
setup_work
export STUB_REFUSE_DIR=win32-x64
run_loop
teardown_work
ok "release continues past the refused name"                0 "refuses the name lam-router-win32-x64"
ok "each other platform was published"                      0 "stub-published darwin-x64"
ok "root package still gets published"                      0 "stub-published npm"
ok "summary maps the refused name to its release asset"     0 "releases/download/v2.0.0/lam-router-windows-amd64.exe"
ok "summary points at npm support"                          0 "https://npmjs.com/support"

echo "case 2: a real publish failure (401) must abort before the root ships"
setup_work
export STUB_FAIL_DIR=darwin-arm64
run_loop
teardown_work
ok "aborts with a non-zero status"                          1 "failed for a reason other than name screening"
ok "root package is NOT published"                          1 "Publishing lam-router-darwin-arm64" "stub-published npm"

echo "case 3: re-running a finished release publishes nothing"
setup_work
export STUB_ALL_PUBLISHED=1
run_loop
teardown_work
ok "all five platforms are skipped"                         0 "lam-router-darwin-x64@2.0.0 already on the registry"
ok "root package is skipped too"                            0 "lam-router@2.0.0 already on the registry"
ok "nothing was published"                                  0 "Verify the channel with" "stub-published"

echo
echo "publish-loop: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
