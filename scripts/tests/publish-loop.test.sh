#!/usr/bin/env bash
# Offline regression test for the publish loop in scripts/build-npm.sh.
#
# Why this exists: the loop used to abort the whole release on the first publish
# failure, so one package name that npm's screening refuses blocked the root
# package and every other platform too. The opposite failure is worse and
# silent: a publish that reports success and never lands must not pass for a
# release, which is why the script reads every published version back from the
# registry - over HTTP, because `npm view` can answer from a stale packument
# cache and report a package that did publish as missing.
#
# The test runs the REAL loop and REAL helper functions, cut out of the script
# with sed, against stub `npm` and `curl` binaries that keep state in a
# directory. No network, no token, no registry writes.

set -uo pipefail

REPO="${REPO:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"

PASS=0
FAIL=0
WORK=""
OUT=""
STATUS=0

# ---------------------------------------------------------------------------
# Stub npm on PATH: `view` answers from a marker directory, `publish` records
# one. STUB_REFUSE_DIR / STUB_FAIL_DIR force specific failures, STUB_SILENT
# makes publish report success without landing (the silent-success case),
# STUB_FLAKY_DIR makes npm's signing service return 502.
# ---------------------------------------------------------------------------
make_stub() {
  mkdir -p "$WORK/bin"
  cat > "$WORK/bin/npm" <<'STUB'
#!/usr/bin/env bash
STATE="${STUB_STATE:?}"
case "${1:-}" in
  view)
    spec="${2:-}"
    name="${spec%%@*}"
    if [ -f "$STATE/$name" ]; then
      # STUB_LAG_VIEWS simulates the npm-view fallback lagging: the version IS
      # published but stays invisible for the first N read-backs.
      if [ "${STUB_LAG_VIEWS:-0}" != "0" ]; then
        n=$(cat "$STATE/.lag-$name" 2>/dev/null || echo 0)
        if [ "$n" -lt "$STUB_LAG_VIEWS" ]; then echo $((n + 1)) > "$STATE/.lag-$name"; exit 1; fi
      fi
      cat "$STATE/$name"; exit 0
    fi
    if [ "${STUB_ALL_PUBLISHED:-0}" = "1" ]; then echo "${spec##*@}"; exit 0; fi
    exit 1 ;;
  config)
    # `npm config get registry`: the publish stage records it so a "did not land"
    # failure names the registry it targeted instead of only saying it is missing.
    [ "${2:-}" = "get" ] && [ "${3:-}" = "registry" ] && { echo "${STUB_REGISTRY:-https://registry.npmjs.org}"; exit 0; }
    exit 0 ;;
  whoami)
    echo "${STUB_USER:-stub-user}"; exit 0 ;;
  publish)
    dir="$(basename "$PWD")"
    if [ "$dir" = "npm" ]; then name="lam-router"; else name="lam-router-$dir"; fi
    # Provenance signing lives in npm's own service: STUB_FLAKY_DIR makes the
    # first attempt fail there (502), STUB_FLAKY_ALWAYS keeps failing.
    if [ -n "${STUB_FLAKY_DIR:-}" ] && [ "$dir" = "$STUB_FLAKY_DIR" ]; then
      if [ "${STUB_FLAKY_ALWAYS:-0}" = "1" ] || [ ! -f "$STATE/.flaky-$dir" ]; then
        mkdir -p "$STATE"; : > "$STATE/.flaky-$dir"
        printf '%s\n' \
          "npm notice Publishing to https://registry.npmjs.org with tag latest and public access" \
          "npm error code CA_CREATE_SIGNING_CERTIFICATE_ERROR" \
          "npm error error creating signing certificate - (502) Bad Gateway" >&2
        exit 1
      fi
    fi
    if [ -n "${STUB_REFUSE_DIR:-}" ] && [[ "$dir" == "$STUB_REFUSE_DIR" ]]; then
      printf '%s\n' \
        "npm error code E403" \
        "npm error 403 403 Forbidden - PUT https://registry.npmjs.org/$name - Package name triggered spam detection; if you believe this is in error, please contact support at https://npmjs.com/support" >&2
      exit 1
    fi
    if [ -n "${STUB_FAIL_DIR:-}" ] && [[ "$dir" == "$STUB_FAIL_DIR" ]]; then
      printf '%s\n' "npm error code E401" "npm error Incorrect or missing password." >&2
      exit 1
    fi
    echo "npm notice stub-published $dir"
    if [ "${STUB_SILENT:-0}" != "1" ]; then
      mkdir -p "$STATE"
      echo "${STUB_VERSION:-2.0.0}" > "$STATE/$name"
    fi
    exit 0 ;;
esac
exit 0
STUB
  chmod +x "$WORK/bin/npm"

  # `curl -s -o /dev/null -w '%{http_code}' <registry>/<pkg>/-/<pkg>-<ver>.tgz`
  # answers 200 only once that version really landed, mirroring the registry's
  # tarball endpoint. STUB_LAG_HTTP delays it, STUB_HTTP_404 blocks HTTP
  # entirely so the npm-view fallback is exercised.
  cat > "$WORK/bin/curl" <<'STUBCURL'
#!/usr/bin/env bash
STATE="${STUB_STATE:?}"
url="${!#}"
file="$(basename "$url")"
name="${file%-${STUB_VERSION:-2.0.0}.tgz}"
if [ "${STUB_HTTP_404:-0}" = "1" ]; then echo 404; exit 0; fi
if [ -f "$STATE/$name" ]; then
  if [ "${STUB_LAG_HTTP:-0}" != "0" ]; then
    n=$(cat "$STATE/.laghttp-$name" 2>/dev/null || echo 0)
    if [ "$n" -lt "$STUB_LAG_HTTP" ]; then echo $((n + 1)) > "$STATE/.laghttp-$name"; echo 404; exit 0; fi
  fi
  echo 200; exit 0
fi
echo 404; exit 0
STUBCURL
  chmod +x "$WORK/bin/curl"
}

# ---------------------------------------------------------------------------
# The harness: the script's own helpers (note/warn/fail_note/
# registry_has_version) plus the surrounding variables it expects, then the
# publish loop itself, all extracted from the real script so they cannot drift.
# ---------------------------------------------------------------------------
make_harness() {
  mkdir -p "$WORK/root/npm" "$WORK/bin"
  for d in linux-x64 linux-arm64 darwin-arm64 darwin-x64 win32-x64; do
    mkdir -p "$WORK/root/npm/platforms/$d/bin"
  done

  {
    echo 'set -uo pipefail'
    # REGISTRY_POLL_DELAY=0 keeps a failing read-back instant; the attempt count
    # stays low so the cases finish quickly.
    echo 'REGISTRY_POLL_DELAY=0'
    echo 'REGISTRY_ATTEMPTS=3'
    sed -n '/^# ::notice::/,/^}/p' "$REPO/scripts/build-npm.sh"
    cat <<'PRELUDE'
ROOT_DIR="$WORK/root"
VERSION=2.0.0
PLATFORMS=(
  "linux:amd64:linux-x64:lam-router"
  "linux:arm64:linux-arm64:lam-router"
  "darwin:arm64:darwin-arm64:lam-router"
  "darwin:amd64:darwin-x64:lam-router"
  "windows:amd64:win32-x64:lam-router.exe"
)
PRELUDE
    sed -n '/^  REFUSED=()/,/^  echo "   Verify the channel/p' "$REPO/scripts/build-npm.sh"
  } > "$WORK/run.sh"
}

setup_work() {
  WORK="$(mktemp -d)"
  export WORK
  export STUB_VERSION=2.0.0
  make_stub
  make_harness
  export STUB_STATE="$WORK/state"
  mkdir -p "$STUB_STATE"
  export PATH="$WORK/bin:$PATH"
}

teardown_work() {
  unset STUB_REFUSE_DIR STUB_FAIL_DIR STUB_SILENT STUB_ALL_PUBLISHED STUB_STATE \
        STUB_FLAKY_DIR STUB_FLAKY_ALWAYS STUB_REGISTRY STUB_USER STUB_VERSION \
        STUB_LAG_VIEWS STUB_LAG_HTTP STUB_HTTP_404 \
        PUBLISH_RETRY_DELAY PUBLISH_ATTEMPTS 2>/dev/null || true
  rm -rf "$WORK"
}

run_loop() {
  OUT="$(bash "$WORK/run.sh" 2>&1)"
  STATUS=$?
}

ok() {  # label expected_status want_text [bad_text]
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
ok "every publish is read back from the registry"           0 "read back from the registry"
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

echo "case 4: a publish that reports success without landing must fail loudly"
setup_work
export STUB_REFUSE_DIR=win32-x64
export STUB_SILENT=1
run_loop
teardown_work
ok "read-back catches the silent publish"                   1 "npm reported success but the registry does not serve it"
ok "the silent publish's own output rides in the annotation" 1 "stub-published linux-x64"
ok "root package is not reported as published"              1 "::error::lam-router-linux-x64@2.0.0" "stub-published npm"

echo "case 5: annotations carry npm's own words, not just a verdict"
setup_work
export STUB_FAIL_DIR=linux-x64
export STUB_REGISTRY=https://registry.npmjs.org/
export STUB_USER=ci-bot
run_loop
teardown_work
ok "npm's error text is in the annotation"                  1 "Incorrect or missing password"
ok "the stage names the registry and identity it targeted"   1 "registry=https://registry.npmjs.org/ user=ci-bot"

echo "case 6: a transient 502 from npm's signing service is retried, not fatal"
setup_work
export STUB_FLAKY_DIR=darwin-arm64
export PUBLISH_RETRY_DELAY=0
run_loop
teardown_work
ok "the release completes after the transient 502"         0 "stub-published npm"
ok "the retry is announced"                                0 "npm's signing service returned 502"
ok "the retried platform is published"                     0 "stub-published darwin-arm64"

echo "case 7: a 502 that never clears still aborts the release"
setup_work
export STUB_FLAKY_DIR=darwin-x64
export STUB_FLAKY_ALWAYS=1
export PUBLISH_RETRY_DELAY=0
export PUBLISH_ATTEMPTS=2
run_loop
teardown_work
ok "aborts instead of retrying forever"                    1 "failed for a reason other than name screening"
ok "the root package is not published"                     1 "Publishing lam-router-darwin-x64" "stub-published npm"

echo "case 8: the HTTP probe carries the read-back when npm-view is stale"
setup_work
export STUB_LAG_HTTP=2
# The fallback must never win here, so success can only have come from the HTTP
# probe - that is the whole point of the read-back asking the registry directly.
export STUB_LAG_VIEWS=99
run_loop
teardown_work
ok "the release completes on the HTTP probe alone"         0 "stub-published npm"
ok "the wait is reported with the HTTP poll count"         0 "became visible after 2 poll(s)"

echo "case 9: when HTTP is unavailable the npm view fallback still waits"
setup_work
export STUB_HTTP_404=1
export STUB_LAG_VIEWS=1
run_loop
teardown_work
ok "the fallback read-back still completes the release"    0 "stub-published npm"
ok "the fallback reports its own poll count"               0 "became visible after 1 poll(s)"

echo
echo "publish-loop: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
