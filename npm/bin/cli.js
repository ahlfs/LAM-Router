#!/usr/bin/env node

/**
 * LAM-Router NPM launcher.
 *
 * Resolution order, cheapest first:
 *   1. platform package from optionalDependencies  (Pattern A: zero overhead)
 *   2. npm/platforms/<key>/bin                     (monorepo / direct build)
 *   3. hoisted or sibling node_modules copy        (pnpm, npm hoisting oddities)
 *   4. a system-wide lam-router already on disk     (reported as not npm-managed)
 *   5. download from the GitHub Release             (checksum-verified, cached)
 *
 * Step 5 exists because npm's name screening can refuse a brand-new platform
 * package name — HTTP 403 "Package name triggered spam detection" on
 * lam-router-win32-x64 is a real, npm-side refusal this repo cannot fix from
 * code. The same release still publishes that platform's binary as a GitHub
 * Release asset, so a refused platform package degrades to a verified download
 * instead of a broken install. Set LAM_ROUTER_NO_DOWNLOAD=1 to stay offline.
 */

const os = require('os');
const fs = require('fs');
const path = require('path');
const https = require('https');
const crypto = require('crypto');
const { spawnSync } = require('child_process');

// The release this launcher fetches from, and the two naming schemes npm
// platforms use: the npm package name (os.arch()) and the Go asset name (amd64).
const REPO = 'ahlfs/LAM-Router';
const PLATFORMS = {
  'darwin-arm64': { pkg: 'lam-router-darwin-arm64', asset: 'lam-router-darwin-arm64' },
  'darwin-x64':   { pkg: 'lam-router-darwin-x64',   asset: 'lam-router-darwin-amd64' },
  'linux-arm64':  { pkg: 'lam-router-linux-arm64',  asset: 'lam-router-linux-arm64' },
  'linux-x64':    { pkg: 'lam-router-linux-x64',    asset: 'lam-router-linux-amd64' },
  'win32-x64':    { pkg: 'lam-router-win32-x64',    asset: 'lam-router-windows-amd64.exe' },
};

const platformKey = `${os.platform()}-${os.arch()}`;
const target = PLATFORMS[platformKey] || null;
const binName = os.platform() === 'win32' ? 'lam-router.exe' : 'lam-router';

function binNameFor(key) {
  return key.startsWith('win32') ? 'lam-router.exe' : 'lam-router';
}

function pkgVersion() {
  // The launcher always sits at <pkg>/bin/cli.js, so ../package.json is the
  // version of the package the user installed.
  return JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'package.json'), 'utf8')).version;
}

function assetUrl(version, asset) {
  return `https://github.com/${REPO}/releases/download/v${version}/${asset}`;
}

/**
 * Parse sha256sum output ("<64 hex>  <name>", CRLF and "*" markers tolerated).
 * Returns the lowercase digest for one asset, or null when absent — an absent
 * entry must abort the download, never fall through unverified.
 */
function parseChecksum(text, assetName) {
  for (const rawLine of String(text).split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line) continue;
    const m = /^([0-9a-fA-F]{64})\s+\*?(.+)$/.exec(line);
    if (m && m[2].trim() === assetName) return m[1].toLowerCase();
  }
  return null;
}

function httpsGet(url, redirectsLeft = 5) {
  return new Promise((resolve, reject) => {
    if (redirectsLeft < 0) {
      reject(new Error(`too many redirects for ${url}`));
      return;
    }
    const req = https.get(url, {
      headers: { 'User-Agent': `lam-router-launcher/${pkgVersion()}`, Accept: '*/*' },
    }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        res.resume();
        // GitHub release downloads redirect to a signed CDN host.
        resolve(httpsGet(new URL(res.headers.location, url).toString(), redirectsLeft - 1));
        return;
      }
      if (res.statusCode !== 200) {
        res.resume();
        reject(new Error(`HTTP ${res.statusCode} for ${url}`));
        return;
      }
      resolve(res);
    });
    req.setTimeout(120000, () => req.destroy(new Error(`request timed out: ${url}`)));
    req.on('error', reject);
  });
}

async function fetchText(url) {
  const res = await httpsGet(url);
  const chunks = [];
  for await (const chunk of res) chunks.push(chunk);
  return Buffer.concat(chunks).toString('utf8');
}

function fetchToFile(url, destPath, label) {
  return new Promise((resolve, reject) => {
    const hash = crypto.createHash('sha256');
    httpsGet(url).then((res) => {
      const total = Number(res.headers['content-length'] || 0);
      const out = fs.createWriteStream(destPath);
      const tty = Boolean(process.stderr.isTTY);
      let done = 0;
      let lastPct = -1;

      if (!tty) {
        const size = total ? ` (${(total / 1048576).toFixed(1)} MB)` : '';
        process.stderr.write(`  fetching ${label}${size}...\n`);
      }

      res.on('data', (chunk) => {
        done += chunk.length;
        hash.update(chunk);
        if (tty && total) {
          const pct = Math.floor((done / total) * 100);
          if (pct >= lastPct + 5) {
            lastPct = pct;
            process.stderr.write(`\r  fetching ${label} ${String(pct).padStart(3)}% (${(done / 1048576).toFixed(1)}/${(total / 1048576).toFixed(1)} MB)`);
          }
        }
      });
      res.on('error', reject);
      out.on('error', reject);
      out.on('finish', () => {
        if (tty) process.stderr.write(`\r${' '.repeat(72)}\r`);
        resolve({ bytes: done, digest: hash.digest('hex') });
      });
      res.pipe(out);
    }).catch(reject);
  });
}

/**
 * Fetch one platform binary from the GitHub Release and verify it against the
 * release's own checksums.txt before it is allowed to run. Exported so the
 * launcher test can exercise the real download path.
 */
async function downloadVerified(version, key, destDir) {
  const spec = PLATFORMS[key];
  if (!spec) throw new Error(`unsupported platform '${key}'`);

  const expected = parseChecksum(await fetchText(assetUrl(version, 'checksums.txt')), spec.asset);
  if (!expected) throw new Error(`v${version} checksums.txt has no entry for ${spec.asset}`);

  fs.mkdirSync(destDir, { recursive: true });
  const dest = path.join(destDir, binNameFor(key));
  const tmp = `${dest}.partial`;
  const { bytes, digest } = await fetchToFile(assetUrl(version, spec.asset), tmp, spec.asset);

  if (digest !== expected) {
    fs.rmSync(tmp, { force: true });
    throw new Error(`checksum mismatch for ${spec.asset}: expected ${expected}, got ${digest}`);
  }
  if (bytes === 0) {
    fs.rmSync(tmp, { force: true });
    throw new Error(`empty download for ${spec.asset}`);
  }
  fs.chmodSync(tmp, 0o755);
  fs.renameSync(tmp, dest);
  return dest;
}

/** Step 1-3: anything npm or a local build already put on disk. */
function findPackagedBinary() {
  if (!target) return null;

  const candidates = [];
  try {
    const pkgPath = require.resolve(`${target.pkg}/package.json`);
    candidates.push(path.join(path.dirname(pkgPath), 'bin', binName));
  } catch {}
  candidates.push(path.join(__dirname, '..', 'platforms', platformKey, 'bin', binName));
  candidates.push(
    path.join(__dirname, '..', 'node_modules', target.pkg, 'bin', binName),
    path.join(__dirname, '..', '..', target.pkg, 'bin', binName),
    path.join(__dirname, '..', '..', '..', 'node_modules', target.pkg, 'bin', binName),
  );
  for (const c of candidates) {
    if (fs.existsSync(c)) return c;
  }
  return null;
}

/** Step 4: a binary someone else installed, which npm does not manage. */
function findSystemBinary() {
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
        // Say so instead of silently running something other than the package
        // the user just installed.
        console.error(`⚠️  Platform package ${target ? target.pkg : platformKey} is not installed; falling back to ${p} (not npm-managed).`);
        return p;
      } catch {}
    }
  }
  return null;
}

/** Step 5: launcher-managed cache, filled from the GitHub Release. */
function findCachedBinary(version) {
  for (const dir of cacheDirs(version)) {
    const candidate = path.join(dir, binName);
    if (fs.existsSync(candidate)) return candidate;
  }
  return null;
}

function cacheDirs(version) {
  return [
    // Inside the npm package: npm owns this tree, so the binary keeps
    // reporting "Install channel: npm" and is cleaned up on reinstall.
    path.join(__dirname, '..', 'cache', version),
    // Used only when node_modules is not writable (read-only global prefix).
    path.join(os.homedir(), '.lam-router', 'bin', version),
  ];
}

async function resolveBinary() {
  const packaged = findPackagedBinary();
  if (packaged) return packaged;

  const system = findSystemBinary();
  if (system) return system;

  if (target && process.env.LAM_ROUTER_NO_DOWNLOAD !== '1') {
    const version = pkgVersion();
    const cached = findCachedBinary(version);
    if (cached) return cached;

    console.error(`ℹ️  Platform package ${target.pkg} is not available; fetching the official v${version} binary for ${platformKey} from GitHub Releases.`);
    let lastError = null;
    for (const dir of cacheDirs(version)) {
      try {
        const downloaded = await downloadVerified(version, platformKey, dir);
        console.error(`✅ Verified ${target.asset} (sha256) → ${downloaded}`);
        return downloaded;
      } catch (err) {
        lastError = err;
        console.error(`⚠️  ${dir} is unusable: ${err.message}`);
      }
    }
    console.error(`❌ Could not fetch a verified binary: ${lastError ? lastError.message : 'unknown error'}`);
    console.error(`   Download it manually: ${assetUrl(version, target.asset)}`);
  }

  return null;
}

async function main() {
  const binaryPath = await resolveBinary();

  if (!binaryPath) {
    console.error(`\n❌ LAM-Router binary for platform '${platformKey}' was not found.`);
    if (target) {
      console.error(`Expected package '${target.pkg}' to provide '${binName}'.\n`);
      console.error(`Quick fixes:`);
      console.error(`  1. Reinstall globally:   npm install -g lam-router`);
      console.error(`  2. Install binary package directly: npm install -g ${target.pkg}`);
    } else {
      console.error(`Supported platforms: ${Object.keys(PLATFORMS).join(', ')}`);
    }
    console.error(`  3. Or compile from source: git clone https://github.com/${REPO} && cd LAM-Router && make install\n`);
    process.exit(1);
  }

  // Forward all CLI arguments, standard I/O, and environment variables.
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
}

if (require.main === module) {
  main().catch((err) => {
    console.error(`❌ ${err.message}`);
    process.exit(1);
  });
}

module.exports = {
  REPO,
  PLATFORMS,
  assetUrl,
  binNameFor,
  cacheDirs,
  downloadVerified,
  parseChecksum,
  pkgVersion,
  platformKey,
  target,
};
