#!/usr/bin/env node

/**
 * LAM-Router NPM Launcher
 * Resolves platform-specific native Go binary from optionalDependencies with zero overhead.
 */

const os = require('os');
const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');

const PLATFORMS = {
  'darwin-arm64': 'lam-router-darwin-arm64',
  'darwin-x64':   'lam-router-darwin-x64',
  'linux-arm64':  'lam-router-linux-arm64',
  'linux-x64':    'lam-router-linux-x64',
  'win32-x64':    'lam-router-win32-x64',
};

const platformKey = `${os.platform()}-${os.arch()}`;
const targetPkg = PLATFORMS[platformKey];
const binName = os.platform() === 'win32' ? 'lam-router.exe' : 'lam-router';

function findBinary() {
  if (targetPkg) {
    // 1. Resolve via standard Node module resolution (installed optionalDependencies)
    try {
      const pkgPath = require.resolve(`${targetPkg}/package.json`);
      const candidate = path.join(path.dirname(pkgPath), 'bin', binName);
      if (fs.existsSync(candidate)) return candidate;
    } catch {}

    // 2. Check local platforms folder (for monorepo / direct build testing)
    const localPlatformBin = path.join(__dirname, '..', 'platforms', platformKey, 'bin', binName);
    if (fs.existsSync(localPlatformBin)) return localPlatformBin;

    // 3. Check hoisted or sibling node_modules paths
    const siblingCandidates = [
      path.join(__dirname, '..', 'node_modules', targetPkg, 'bin', binName),
      path.join(__dirname, '..', '..', targetPkg, 'bin', binName),
      path.join(__dirname, '..', '..', '..', 'node_modules', targetPkg, 'bin', binName),
    ];
    for (const c of siblingCandidates) {
      if (fs.existsSync(c)) return c;
    }
  }

  // 4. Fallback: Check user / system local installations
  const homeDir = os.homedir();
  const ext = os.platform() === 'win32' ? '.exe' : '';
  const systemCandidates = [
    path.join(homeDir, '.lam-router', 'bin', `lamrouter${ext}`),
    path.join(homeDir, '.local', 'bin', `lam-router${ext}`),
    path.join(homeDir, '.local', 'bin', `lamrouter${ext}`),
    path.join('/usr', 'local', 'bin', `lam-router${ext}`),
    path.join('/usr', 'local', 'bin', `lamrouter${ext}`),
  ];

  for (const p of systemCandidates) {
    if (fs.existsSync(p)) {
      try {
        fs.accessSync(p, fs.constants.X_OK);
        // The npm-installed platform package is missing, so this binary is not
        // npm-managed and may be an older build: say so instead of silently
        // running something other than the package the user just installed.
        console.error(`⚠️  Platform package ${targetPkg || platformKey} is not installed; falling back to ${p} (not npm-managed).`);
        return p;
      } catch {}
    }
  }

  return null;
}

const binaryPath = findBinary();

if (!binaryPath) {
  console.error(`\n❌ LAM-Router binary for platform '${platformKey}' was not found.`);
  if (targetPkg) {
    console.error(`Expected package '${targetPkg}' to provide '${binName}'.\n`);
    console.error(`Quick fixes:`);
    console.error(`  1. Reinstall globally:   npm install -g lam-router`);
    console.error(`  2. Install binary package directly: npm install -g ${targetPkg}`);
  } else {
    console.error(`Supported platforms: ${Object.keys(PLATFORMS).join(', ')}`);
  }
  console.error(`  3. Or compile from source: git clone https://github.com/ahlfs/LAM-Router && cd LAM-Router && make install\n`);
  process.exit(1);
}

// Forward all CLI arguments, standard I/O, and environment variables
const result = spawnSync(binaryPath, process.argv.slice(2), {
  stdio: 'inherit',
  env: process.env,
});

if (result.error) {
  console.error(`❌ Execution error: ${result.error.message}`);
  process.exit(1);
}

if (result.signal) {
  process.kill(process.pid, result.signal);
} else {
  process.exit(result.status !== null ? result.status : 0);
}
