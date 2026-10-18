#!/usr/bin/env bash
# ============================================================================
# LAM-Router NPM Multi-Platform Build & Publish Script
# ============================================================================
# Cross-compiles native Go binaries for all platforms, places them into
# npm/platforms/<platform>/bin/, and optionally packages or publishes to NPM.
#
# Usage:
#   ./scripts/build-npm.sh          # Cross-compile binaries into npm/platforms/
#   ./scripts/build-npm.sh --pack   # Compile and npm pack all packages locally
#   ./scripts/build-npm.sh --publish # Compile and publish all packages to NPM
# ============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Synchronize version from git tag (if running in CI release) or use npm/package.json
if [ -n "${GITHUB_REF_NAME:-}" ] && [[ "$GITHUB_REF_NAME" =~ ^v[0-9] ]]; then
  VERSION="${GITHUB_REF_NAME#v}"
  echo "📌 Synchronizing version $VERSION from git tag $GITHUB_REF_NAME..."
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
  VERSION=$(node -e "console.log(require('$ROOT_DIR/npm/package.json').version)")
fi

LDFLAGS="-s -w -X 'lamrouter/internal/updater.CurrentVersion=$VERSION'"

echo "📦 Preparing LAM-Router NPM release v$VERSION..."

# 1. Compile all platform binaries
PLATFORMS=(
  "linux:amd64:linux-x64:lam-router"
  "linux:arm64:linux-arm64:lam-router"
  "darwin:arm64:darwin-arm64:lam-router"
  "darwin:amd64:darwin-x64:lam-router"
  "windows:amd64:win32-x64:lam-router.exe"
)

for entry in "${PLATFORMS[@]}"; do
  IFS=":" read -r GOOS GOARCH DIR BIN_NAME <<< "$entry"
  DEST_DIR="$ROOT_DIR/npm/platforms/$DIR/bin"
  mkdir -p "$DEST_DIR"
  echo "  → Building for $GOOS/$GOARCH ($DIR)..."
  GOOS="$GOOS" GOARCH="$GOARCH" CGO_ENABLED=0 go build -ldflags="$LDFLAGS" -o "$DEST_DIR/$BIN_NAME" "$ROOT_DIR/cmd/lamrouter/"
  chmod 755 "$DEST_DIR/$BIN_NAME"
done

echo "✅ All 5 platform binaries successfully compiled!"

# 2. Handle --pack
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

# 3. Handle --publish
if [[ "${1:-}" == "--publish" ]]; then
  echo ""
  echo "🚀 Publishing packages to NPM..."
  for entry in "${PLATFORMS[@]}"; do
    IFS=":" read -r _ _ DIR _ <<< "$entry"
    echo "  → Publishing lam-router-$DIR..."
    (cd "$ROOT_DIR/npm/platforms/$DIR" && npm publish --access public)
  done

  echo "  → Publishing root package lam-router..."
  (cd "$ROOT_DIR/npm" && npm publish --access public)
  echo "🎉 Successfully published LAM-Router v$VERSION to NPM!"
  exit 0
fi

echo ""
echo "Done! To publish, run: ./scripts/build-npm.sh --publish"
