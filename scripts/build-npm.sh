#!/usr/bin/env bash
# ============================================================================
# LAM-Router NPM Multi-Platform Build & Publish Script
# ============================================================================
# Cross-compiles native Go binaries for all platforms, places them into
# npm/platforms/<platform>/bin/, and optionally packages, publishes or verifies
# the NPM release.
#
# Usage:
#   ./scripts/build-npm.sh            # Cross-compile binaries into npm/platforms/
#   ./scripts/build-npm.sh --pack     # Compile and npm pack all packages locally
#   ./scripts/build-npm.sh --publish  # Compile and publish all packages to NPM
#   ./scripts/build-npm.sh --verify   # Verify the *published* npm channel
#
# Releases must come from an exact git tag: the 1.8.5 npm artifacts were built
# from an untagged working tree and shipped without fixes that were already in
# the repo. Tag first (`git tag -a v2.0.0 -m 'LAM-Router 2.0.0'`), then publish.
# ALLOW_UNTAGGED=1 permits a local dry run (compile/--pack only, never --publish).
# ============================================================================

set -euo pipefail

# ::notice::/::warning::/::error:: lines render as GitHub run annotations, which
# is how a CI problem stays diagnosable from the API without the job log.
# Locally they are plain text on stdout/stderr.
note()      { printf '::notice::%s\n' "$*"; }
warn()      { printf '::warning::%s\n' "$*" >&2; }
fail_note() { printf '::error::%s\n' "$*" >&2; }

# A publish that silently does nothing must never look like a release. Read the
# exact version back from the registry, polling because propagation is not
# instant, instead of trusting npm's exit status alone.
registry_has_version() {
  local pkg="$1" want="$2" got i
  local delay="${REGISTRY_POLL_DELAY:-3}"
  for i in 1 2 3 4 5; do
    got="$(npm view "$pkg@$want" version 2>/dev/null || true)"
    [ "$got" = "$want" ] && return 0
    sleep "$delay"
  done
  return 1
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# ---------------------------------------------------------------------------
# 0. Release provenance guard — runs BEFORE anything is rewritten on disk.
# ---------------------------------------------------------------------------
ALLOW_UNTAGGED="${ALLOW_UNTAGGED:-0}"
PKG_VERSION="$(node -e "console.log(require('$ROOT_DIR/npm/package.json').version)")"

if [ -n "${GITHUB_REF_NAME:-}" ] && [[ "$GITHUB_REF_NAME" =~ ^v[0-9] ]]; then
  # CI: the release workflow only triggers on v* tag pushes.
  TAG="$GITHUB_REF_NAME"
else
  TAG="$(git -C "$ROOT_DIR" describe --tags --exact-match 2>/dev/null || true)"
fi

UNTAGGED=0
# `--verify <version>` targets an already-published release and is read-only, so
# it needs no release provenance: ./scripts/build-npm.sh --verify 1.8.5
VERIFY_TARGET=""
if [[ "${1:-}" == "--verify" && -n "${2:-}" ]]; then
  VERIFY_TARGET="${2#v}"
fi

if [ -n "$VERIFY_TARGET" ]; then
  # Read-only: it reads the registry for an already-published release, so an
  # untagged HEAD or a dirty tree must not block it. Checked first, because a
  # tag sitting on HEAD with uncommitted edits (the normal state right before
  # the next release) otherwise refuses the check that is supposed to help
  # diagnose exactly that situation.
  echo "🔍 --verify v$VERIFY_TARGET: release provenance skipped (read-only registry check)"
elif [ -z "$TAG" ]; then
  if [ "$ALLOW_UNTAGGED" != "1" ]; then
    echo "❌ Refusing to build: HEAD is not on an exact release tag (npm/package.json says $PKG_VERSION)." >&2
    echo "   Tag the release:   git tag -a v$PKG_VERSION -m 'LAM-Router $PKG_VERSION'" >&2
    echo "   Local dry run:     ALLOW_UNTAGGED=1 $0" >&2
    exit 3
  fi
  UNTAGGED=1
  echo "⚠️  ALLOW_UNTAGGED=1: building from untagged HEAD ($(git -C "$ROOT_DIR" rev-parse --short HEAD))."
  echo "    --publish is blocked in this mode; the result is a local dry run only."
  if [[ "${1:-}" == "--publish" ]]; then
    echo "❌ --publish requires a tagged, committed release. Re-run without ALLOW_UNTAGGED." >&2
    exit 3
  fi
else
  if [ "${TAG#v}" != "$PKG_VERSION" ]; then
    echo "❌ Refusing to build: tag $TAG does not match npm/package.json version $PKG_VERSION." >&2
    exit 3
  fi
  # Tracked-file cleanliness decides whether the artifacts match the commit.
  # Untracked files (CI .npmrc, build outputs) do not change the source tree.
  DIRT="$(git -C "$ROOT_DIR" status --porcelain --untracked-files=no)"
  if [ -n "$DIRT" ]; then
    echo "❌ Refusing to build: tracked files are modified; artifacts would not match $TAG." >&2
    echo "$DIRT" >&2
    exit 3
  fi
  LOOSE="$(git -C "$ROOT_DIR" status --porcelain --untracked-files=normal | grep '^??' || true)"
  [ -n "$LOOSE" ] && echo "ℹ️  Untracked files present (ignored for provenance): $(echo "$LOOSE" | wc -l) entry(s)"
  echo "🔒 Provenance OK: $TAG @ $(git -C "$ROOT_DIR" rev-parse --short HEAD), tree clean"
fi

# ---------------------------------------------------------------------------
# 1. Synchronize versions from the release tag
# ---------------------------------------------------------------------------
if [ -n "$VERIFY_TARGET" ]; then
  VERSION="$VERIFY_TARGET"
  echo "🔍 Verifying the already-published v$VERSION (no version sync, no build)."
elif [ "$UNTAGGED" = "0" ]; then
  VERSION="${TAG#v}"
  echo "📌 Synchronizing version $VERSION from git tag $TAG..."
  node -e "
    const fs = require('fs');
    const path = require('path');
    const rootPkgPath = path.join('$ROOT_DIR', 'npm', 'package.json');
    const rootPkg = JSON.parse(fs.readFileSync(rootPkgPath, 'utf8'));
    rootPkg.version = '$VERSION';
    for (const k of Object.keys(rootPkg.optionalDependencies || {})) {
      rootPkg.optionalDependencies[k] = '$VERSION';
    }
    fs.writeFileSync(rootPkgPath, JSON.stringify(rootPkg, null, 2) + '\n');

    const platforms = ['linux-x64', 'linux-arm64', 'darwin-arm64', 'darwin-x64', 'win32-x64'];
    for (const p of platforms) {
      const pPath = path.join('$ROOT_DIR', 'npm', 'platforms', p, 'package.json');
      if (fs.existsSync(pPath)) {
        const pkg = JSON.parse(fs.readFileSync(pPath, 'utf8'));
        pkg.version = '$VERSION';
        fs.writeFileSync(pPath, JSON.stringify(pkg, null, 2) + '\n');
      }
    }
  "
else
  VERSION="$PKG_VERSION"
fi

LDFLAGS="-s -w -X 'lamrouter/internal/updater.CurrentVersion=$VERSION'"

echo "📦 Preparing LAM-Router NPM release v$VERSION..."

# 2. Platform matrix
PLATFORMS=(
  "linux:amd64:linux-x64:lam-router"
  "linux:arm64:linux-arm64:lam-router"
  "darwin:arm64:darwin-arm64:lam-router"
  "darwin:amd64:darwin-x64:lam-router"
  "windows:amd64:win32-x64:lam-router.exe"
)

# Compile all platform binaries. Skipped for --verify, which only reads the
# registry and must not require a Go toolchain.
if [[ "${1:-}" != "--verify" ]]; then
  for entry in "${PLATFORMS[@]}"; do
    IFS=":" read -r GOOS GOARCH DIR BIN_NAME <<< "$entry"
    DEST_DIR="$ROOT_DIR/npm/platforms/$DIR/bin"
    mkdir -p "$DEST_DIR"
    echo "  → Building for $GOOS/$GOARCH ($DIR)..."
    GOOS="$GOOS" GOARCH="$GOARCH" CGO_ENABLED=0 go build -ldflags="$LDFLAGS" -o "$DEST_DIR/$BIN_NAME" "$ROOT_DIR/cmd/lamrouter/"
    chmod 755 "$DEST_DIR/$BIN_NAME"
  done

  echo "✅ All 5 platform binaries successfully compiled!"
fi

# 3. Handle --pack
if [[ "${1:-}" == "--pack" ]]; then
  echo ""
  echo "📦 Packing npm tarballs locally..."
  for entry in "${PLATFORMS[@]}"; do
    IFS=":" read -r _ _ DIR _ <<< "$entry"
    echo "  → Packing npm/platforms/$DIR..."
    (cd "$ROOT_DIR/npm/platforms/$DIR" && npm pack)
  done
  echo "  → Packing npm root..."
  (cd "$ROOT_DIR/npm" && npm pack)
  echo "🎉 All packages packed! You can inspect or install the .tgz files."
  exit 0
fi

# 4. Handle --publish
if [[ "${1:-}" == "--publish" ]]; then
  echo ""
  if [ "$UNTAGGED" = "1" ]; then
    echo "❌ --publish requires a tagged, committed release. Re-run without ALLOW_UNTAGGED." >&2
    exit 3
  fi
  echo "🚀 Publishing packages to NPM..."

  # Ensure npm authentication is visible to sub-package publishes ($HOME/.npmrc
  # is the only config npm reads after `cd npm/platforms/<p>`). Never clobber an
  # operator's existing ~/.npmrc that already holds a token.
  if [ -n "${NODE_AUTH_TOKEN:-}" ]; then
    printf '//registry.npmjs.org/:_authToken=%s\n' "$NODE_AUTH_TOKEN" > "$HOME/.npmrc"
    chmod 600 "$HOME/.npmrc"
  elif [ -f "$ROOT_DIR/.npmrc" ] && ! grep -q '_authToken' "$HOME/.npmrc" 2>/dev/null; then
    cp "$ROOT_DIR/.npmrc" "$HOME/.npmrc"
    chmod 600 "$HOME/.npmrc"
  fi

  if ! npm whoami >/dev/null 2>&1; then
    echo "❌ No usable npm credentials (npm whoami failed)." >&2
    echo "   Local: export NODE_AUTH_TOKEN=<granular access token>, or run 'npm login'." >&2
    echo "   CI:    add the NPM_TOKEN repository secret." >&2
    exit 4
  fi
  echo "🔑 npm auth OK as $(npm whoami)"

  # A package name that has never been published can fail in two ways with the
  # same symptom, and the fixes are not interchangeable: a brand-new unscoped
  # name may need an interactive OTP once, while a name npm's screening dislikes
  # is refused outright (403 "Package name triggered spam detection") and no
  # amount of retrying or correct auth will publish it. Warn up front so neither
  # failure is a mystery mid-release.
  MISSING=()
  for entry in "${PLATFORMS[@]}"; do
    IFS=":" read -r _ _ DIR _ <<< "$entry"
    if ! npm view "lam-router-$DIR" version >/dev/null 2>&1; then
      MISSING+=("$DIR")
    fi
  done
  if [ "${#MISSING[@]}" -gt 0 ]; then
    echo "⚠️  Not on the registry yet: ${MISSING[*]}"
    echo "    Either it was never published (a new unscoped name needs a one-time"
    echo "    interactive \`--otp=<code>\`), or npm's name screening refuses it:"
    for DIR in "${MISSING[@]}"; do
      echo "      (cd npm/platforms/$DIR && npm publish --access public --otp=<6-digit-code>)"
    done
    echo "    A refusal is an npm-side policy decision, not an auth problem; the release"
    echo "    continues without that package and the launcher downloads its binary instead."
  fi

  # Platform packages first, root meta-package last: the root declares them as
  # optionalDependencies, so publishing it first serves 404s to installers.
  # A version that is already on the registry is skipped: npm refuses to
  # overwrite one anyway, and a re-run after a partial failure (or after an
  # interactive --otp seed) must be able to finish the release.
  #
  # A platform package REFUSED by npm's name screening must not fail the whole
  # release: every other platform and the root package are still good, and the
  # launcher downloads the refused platform's binary from the GitHub Release
  # instead. The name stays in optionalDependencies, so if npm ever accepts it
  # those installs switch back to the package with no release of their own.
  # Every failure other than name screening still aborts, so an expired token or
  # a registry outage can never ship a release that silently lacks a platform.
  REFUSED=()
  # Registry identity in the annotation trail: a publish that exits 0 without
  # landing is nearly always the wrong registry or the wrong identity, and
  # neither is visible in a failure message that only says "not served".
  note "publish stage: version=$VERSION registry=$(npm config get registry 2>/dev/null || echo '?') user=$(npm whoami 2>/dev/null || echo '?')"
  for entry in "${PLATFORMS[@]}"; do
    IFS=":" read -r _ _ DIR _ <<< "$entry"
    if [ "$(npm view "lam-router-$DIR@$VERSION" version 2>/dev/null || true)" = "$VERSION" ]; then
      echo "  ⏭️  lam-router-$DIR@$VERSION already on the registry — skipping"
      continue
    fi
    echo "  → Publishing lam-router-$DIR..."
    LOG="$(mktemp)"
    PUBLISHED=0
    if ! (cd "$ROOT_DIR/npm/platforms/$DIR" && npm publish --access public) 2>&1 | tee "$LOG"; then
      if grep -q 'spam detection' "$LOG"; then
        REFUSED+=("$entry")
        warn "npm refuses the name lam-router-$DIR (name screening, not an auth error) — the launcher serves that platform from the GitHub Release"
      else
        fail_note "publishing lam-router-$DIR failed for a reason other than name screening. npm output: $(tr '\n' '|' < "$LOG" | tail -c 700)"
        rm -f "$LOG"
        exit 1
      fi
    else
      PUBLISHED=1
    fi
    if [ "$PUBLISHED" = "1" ]; then
      # The log outlives the read-back on purpose: when npm exits 0 without
      # landing, its own output is the only thing that names the reason, and the
      # annotation must carry it (job logs are not readable anonymously).
      if ! registry_has_version "lam-router-$DIR" "$VERSION"; then
        fail_note "lam-router-$DIR@$VERSION: npm reported success but the registry does not serve it. npm output: $(tr '\n' '|' < "$LOG" | tail -c 700)"
        rm -f "$LOG"
        exit 1
      fi
      note "published lam-router-$DIR@$VERSION (read back from the registry)"
    fi
    rm -f "$LOG"
  done

  # Stage marker: the annotation trail must show whether the release reached the
  # root publish at all, so a missing root package is never guesswork.
  note "root stage reached (platforms refused: ${#REFUSED[@]})"
  if [ "$(npm view "lam-router@$VERSION" version 2>/dev/null || true)" = "$VERSION" ]; then
    echo "  ⏭️  lam-router@$VERSION already on the registry — skipping"
    note "root: already on the registry, nothing to publish"
  else
    echo "  → Publishing root package lam-router..."
    LOG="$(mktemp)"
    if (cd "$ROOT_DIR/npm" && npm publish --access public) 2>&1 | tee "$LOG"; then
      note "root publish output: $(tr '\n' '|' < "$LOG" | cut -c1-500)"
    else
      fail_note "root publish FAILED: $(tr '\n' '|' < "$LOG" | cut -c1-800)"
      rm -f "$LOG"
      exit 1
    fi
    rm -f "$LOG"
    if ! registry_has_version lam-router "$VERSION"; then
      fail_note "root publish exited 0 but the registry does not serve lam-router@$VERSION. npm output: $(tr '\n' '|' < "$LOG" | tail -c 700)"
      exit 1
    fi
    note "published lam-router@$VERSION (read back from the registry)"
  fi
  echo "🎉 Published LAM-Router v$VERSION to NPM."

  if [ "${#REFUSED[@]}" -gt 0 ]; then
    echo ""
    echo "⚠️  npm refused these package names; their binaries are served by the launcher"
    echo "    (bin/cli.js), which downloads and sha256-verifies the GitHub Release asset:"
    for entry in "${REFUSED[@]}"; do
      IFS=":" read -r GOOS GOARCH DIR BIN_NAME <<< "$entry"
      ASSET="lam-router-${GOOS}-${GOARCH}"
      if [[ "$BIN_NAME" == *.exe ]]; then
        ASSET="$ASSET.exe"
      fi
      echo "      lam-router-$DIR  →  https://github.com/ahlfs/LAM-Router/releases/download/v$VERSION/$ASSET"
    done
    echo "    Installs on those platforms still work; the npm PACKAGE channel for them"
    echo "    needs npm to lift the block — https://npmjs.com/support, quoting the"
    echo "    403 'Package name triggered spam detection' message."
  fi

  echo "   Verify the channel with: ./scripts/build-npm.sh --verify"
  exit 0
fi

# 5. Handle --verify — proves the npm channel actually serves the current code
if [[ "${1:-}" == "--verify" ]]; then
  echo ""
  echo "🔍 Verifying published npm artifacts for v$VERSION..."
  FAIL=0

  # In CI this runs seconds after the publish job, and registry propagation is
  # not instant: an artifact that just landed can still read as missing, which
  # turns a good release into a false failure. Wait for the root package before
  # judging anything that depends on it; on a long-published version the first
  # probe succeeds, so this costs nothing.
  if [ "$(npm view "lam-router@$VERSION" version 2>/dev/null || true)" != "$VERSION" ]; then
    echo "  ⏳ lam-router@$VERSION not visible yet — waiting for registry propagation..."
    if ! REGISTRY_POLL_DELAY=6 registry_has_version lam-router "$VERSION"; then
      echo "  ⚠️  still not visible after polling; the checks below will report it" >&2
    fi
  fi

  # npm's name screening can refuse a brand-new unscoped package name (403
  # "Package name triggered spam detection"), and that package then cannot exist
  # on the registry at all. Its absence is an accepted gap rather than a
  # failure, because the launcher fetches the same binary from the GitHub
  # Release — but the version must still match whenever it IS present, so a
  # stale copy cannot slip past verification.
  NAME_REFUSABLE=("win32-x64")
  PLATFORMS_MISSING=()
  for entry in "${PLATFORMS[@]}"; do
    IFS=":" read -r _ _ DIR _ <<< "$entry"
    GOT="$(npm view "lam-router-$DIR@$VERSION" version 2>/dev/null || true)"
    if [ "$GOT" = "$VERSION" ]; then
      echo "  ✅ lam-router-$DIR@$VERSION present"
    elif [[ " ${NAME_REFUSABLE[*]} " == *" $DIR "* ]]; then
      PLATFORMS_MISSING+=("$DIR")
      echo "  ⚠️  lam-router-$DIR@$VERSION absent — npm refuses this package name; the launcher"
      echo "      downloads the same binary from the GitHub Release and verifies its sha256"
    else
      echo "  ❌ lam-router-$DIR@$VERSION missing on the registry (got '${GOT:-nothing}')" >&2
      FAIL=1
    fi
  done

  ROOT_DEPS="$(npm view "lam-router@$VERSION" optionalDependencies --json 2>/dev/null || echo '{}')"
  for entry in "${PLATFORMS[@]}"; do
    IFS=":" read -r _ _ DIR _ <<< "$entry"
    if echo "$ROOT_DEPS" | grep -q "\"lam-router-$DIR\""; then
      echo "  ✅ root optionalDependencies include lam-router-$DIR"
    else
      echo "  ❌ root package is missing optionalDependency lam-router-$DIR — installs would break" >&2
      FAIL=1
    fi
  done

  TMP="$(mktemp -d)"
  trap 'rm -rf "$TMP"' EXIT
  if (cd "$TMP" && npm pack "lam-router-linux-x64@$VERSION" >/dev/null 2>&1 && tar xzf ./*.tgz); then
    if [ -f "$TMP/package/bin/lam-router" ]; then
      if grep -aq 'antigravity/hub/' "$TMP/package/bin/lam-router"; then
        echo "  ✅ linux-x64 binary carries the antigravity hub user-agent (quota fix shipped)"
      else
        echo "  ❌ linux-x64 binary is STALE: no antigravity/hub user-agent (the 1.8.5 regression)" >&2
        FAIL=1
      fi
      echo "     published binary self-report: $("$TMP/package/bin/lam-router" version 2>&1 | head -1)"
    else
      echo "  ❌ tarball for lam-router-linux-x64@$VERSION has no bin/lam-router" >&2
      FAIL=1
    fi
  else
    echo "  ❌ could not fetch/expand lam-router-linux-x64@$VERSION from the registry" >&2
    FAIL=1
  fi

  # Worst case is "npm refuses a platform name AND the shipped launcher has no
  # download fallback": that platform installs a CLI that cannot run at all.
  # Ship both and fail the release if they ever drift apart.
  ROOT_TMP="$(mktemp -d)"
  if (cd "$ROOT_TMP" && npm pack "lam-router@$VERSION" >/dev/null 2>&1 && tar xzf ./*.tgz); then
    LAUNCHER="$ROOT_TMP/package/bin/cli.js"
    if [ -f "$LAUNCHER" ] && grep -q 'checksums.txt' "$LAUNCHER" && grep -q 'releases/download' "$LAUNCHER"; then
      echo "  ✅ root launcher carries the checksum-verified GitHub Release fallback"
    else
      echo "  ❌ root launcher has no download fallback — a name-refused platform would break" >&2
      FAIL=1
    fi
    for DIR in "${PLATFORMS_MISSING[@]}"; do
      if grep -q "$DIR" "$LAUNCHER"; then
        echo "  ✅ launcher knows $DIR, so its download fallback can trigger there"
      else
        echo "  ❌ launcher does not know platform $DIR — its binary would be unreachable" >&2
        FAIL=1
      fi
    done
  else
    echo "  ❌ could not fetch/expand lam-router@$VERSION from the registry" >&2
    FAIL=1
  fi
  rm -rf "$ROOT_TMP"

  if [ "$FAIL" = "0" ]; then
    echo "🎉 npm channel verified: v$VERSION is installable and current."
    exit 0
  fi
  echo "❌ npm channel verification FAILED for v$VERSION." >&2
  exit 5
fi

echo ""
echo "Done! To publish: ./scripts/build-npm.sh --publish   |   To verify: --verify"
