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

if [ -z "$TAG" ] && [ -z "$VERIFY_TARGET" ]; then
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
elif [ -n "$TAG" ]; then
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
else
  echo "🔍 --verify v$VERIFY_TARGET: release provenance skipped (read-only registry check)"
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

  # A brand-new unscoped package needs an interactive OTP exactly once. Warn
  # before the loop so the failure is not a mystery EOTP mid-release.
  MISSING=()
  for entry in "${PLATFORMS[@]}"; do
    IFS=":" read -r _ _ DIR _ <<< "$entry"
    if ! npm view "lam-router-$DIR" version >/dev/null 2>&1; then
      MISSING+=("$DIR")
    fi
  done
  if [ "${#MISSING[@]}" -gt 0 ]; then
    echo "⚠️  Not on the registry yet (first publish of a new unscoped package needs an"
    echo "    interactive \`--otp=<code>\`): ${MISSING[*]}"
    echo "    Seed it once from a tagged, clean tree; this script then skips it and"
    echo "    continues with the remaining packages:"
    for DIR in "${MISSING[@]}"; do
      echo "      (cd npm/platforms/$DIR && npm publish --access public --otp=<6-digit-code>)"
    done
  fi

  # Platform packages first, root meta-package last: the root declares them as
  # optionalDependencies, so publishing it first serves 404s to installers.
  # A version that is already on the registry is skipped: npm refuses to
  # overwrite one anyway, and a re-run after a partial failure (or after an
  # interactive --otp seed) must be able to finish the release.
  for entry in "${PLATFORMS[@]}"; do
    IFS=":" read -r _ _ DIR _ <<< "$entry"
    if [ "$(npm view "lam-router-$DIR@$VERSION" version 2>/dev/null || true)" = "$VERSION" ]; then
      echo "  ⏭️  lam-router-$DIR@$VERSION already on the registry — skipping"
      continue
    fi
    echo "  → Publishing lam-router-$DIR..."
    (cd "$ROOT_DIR/npm/platforms/$DIR" && npm publish --access public)
  done

  if [ "$(npm view "lam-router@$VERSION" version 2>/dev/null || true)" = "$VERSION" ]; then
    echo "  ⏭️  lam-router@$VERSION already on the registry — skipping"
  else
    echo "  → Publishing root package lam-router..."
    (cd "$ROOT_DIR/npm" && npm publish --access public)
  fi
  echo "🎉 Published LAM-Router v$VERSION to NPM."
  echo "   Verify the channel with: ./scripts/build-npm.sh --verify"
  exit 0
fi

# 5. Handle --verify — proves the npm channel actually serves the current code
if [[ "${1:-}" == "--verify" ]]; then
  echo ""
  echo "🔍 Verifying published npm artifacts for v$VERSION..."
  FAIL=0

  for entry in "${PLATFORMS[@]}"; do
    IFS=":" read -r _ _ DIR _ <<< "$entry"
    GOT="$(npm view "lam-router-$DIR@$VERSION" version 2>/dev/null || true)"
    if [ "$GOT" != "$VERSION" ]; then
      echo "  ❌ lam-router-$DIR@$VERSION missing on the registry (got '${GOT:-nothing}')" >&2
      FAIL=1
    else
      echo "  ✅ lam-router-$DIR@$VERSION present"
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

  if [ "$FAIL" = "0" ]; then
    echo "🎉 npm channel verified: v$VERSION is installable and current."
    exit 0
  fi
  echo "❌ npm channel verification FAILED for v$VERSION." >&2
  exit 5
fi

echo ""
echo "Done! To publish: ./scripts/build-npm.sh --publish   |   To verify: --verify"
