#!/usr/bin/env node

/**
 * Launcher tests. Run directly: node npm/test/launcher.test.js
 *
 * The download test talks to the real GitHub Release on purpose: the failure
 * mode being fixed is "npm refused this platform package name", and the only
 * thing standing between that and a silently wrong binary is checksum
 * verification against the release's own checksums.txt. Set
 * SKIP_NETWORK=1 to run the pure-unit checks only.
 */

const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');
const crypto = require('crypto');

const cli = require('../bin/cli.js');

let passed = 0;
function check(name, fn) {
  try {
    fn();
    passed += 1;
    console.log(`  ✅ ${name}`);
  } catch (err) {
    console.error(`  ❌ ${name}\n     ${err.message}`);
    process.exitCode = 1;
  }
}

async function checkAsync(name, fn) {
  try {
    await fn();
    passed += 1;
    console.log(`  ✅ ${name}`);
  } catch (err) {
    console.error(`  ❌ ${name}\n     ${err.message}`);
    process.exitCode = 1;
  }
}

console.log(`launcher tests (node ${process.version}, platform ${cli.platformKey})`);

// --- pure unit checks ------------------------------------------------------
check('every supported platform has an npm package and a release asset', () => {
  const keys = Object.keys(cli.PLATFORMS);
  assert.deepStrictEqual(keys.sort(), ['darwin-arm64', 'darwin-x64', 'linux-arm64', 'linux-x64', 'win32-x64']);
  for (const [key, spec] of Object.entries(cli.PLATFORMS)) {
    assert.ok(spec.pkg.startsWith('lam-router-'), `${key}: npm package name`);
    assert.ok(spec.asset.startsWith('lam-router-'), `${key}: release asset name`);
  }
});

check('Go asset names use amd64, npm package names use x64', () => {
  assert.strictEqual(cli.PLATFORMS['linux-x64'].asset, 'lam-router-linux-amd64');
  assert.strictEqual(cli.PLATFORMS['linux-x64'].pkg, 'lam-router-linux-x64');
  assert.strictEqual(cli.PLATFORMS['darwin-x64'].asset, 'lam-router-darwin-amd64');
  assert.strictEqual(cli.PLATFORMS['darwin-x64'].pkg, 'lam-router-darwin-x64');
  assert.strictEqual(cli.PLATFORMS['win32-x64'].asset, 'lam-router-windows-amd64.exe');
});

check('windows binaries keep the .exe suffix, others do not', () => {
  assert.strictEqual(cli.binNameFor('win32-x64'), 'lam-router.exe');
  assert.strictEqual(cli.binNameFor('linux-x64'), 'lam-router');
  assert.strictEqual(cli.binNameFor('darwin-arm64'), 'lam-router');
});

check('assetUrl points at the tagged release asset', () => {
  assert.strictEqual(
    cli.assetUrl('2.0.0', 'lam-router-windows-amd64.exe'),
    'https://github.com/ahlfs/LAM-Router/releases/download/v2.0.0/lam-router-windows-amd64.exe',
  );
});

check('parseChecksum finds an entry and ignores near-misses', () => {
  const text = [
    'e43fc93db4a4ed2fac1340803eb2570767eaa16acf7faf7a5aeb05e2e24b6d1c  lam-router-linux-amd64',
    '722cfca4f5f77206cf950605a3de5d917edd39787915d1030760077349a89453 *lam-router-windows-amd64.exe',
    '',
  ].join('\r\n');
  assert.strictEqual(
    cli.parseChecksum(text, 'lam-router-windows-amd64.exe'),
    '722cfca4f5f77206cf950605a3de5d917edd39787915d1030760077349a89453',
  );
  assert.strictEqual(cli.parseChecksum(text, 'lam-router-windows-arm64.exe'), null);
  assert.strictEqual(cli.parseChecksum('not a checksum line', 'x'), null);
});

check('cache dirs are versioned and the first one lives inside the npm package', () => {
  const dirs = cli.cacheDirs('2.0.0');
  assert.ok(dirs[0].startsWith(path.join(__dirname, '..')), 'first cache dir is package-relative');
  assert.ok(dirs[0].includes(`cache${path.sep}2.0.0`), 'first cache dir is versioned');
  assert.ok(dirs[1].includes(os.homedir()), 'second cache dir is in the home directory');
});

check('unsupported platform has no mapping', () => {
  assert.strictEqual(cli.PLATFORMS['linux-ppc64'], undefined);
});

// --- the real release ------------------------------------------------------
if (process.env.SKIP_NETWORK === '1') {
  console.log(`\n${passed} check(s) passed (network checks skipped)`);
} else {
  const version = cli.pkgVersion();
  const tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'lam-launcher-test-'));

  (async () => {
    await checkAsync(`checksums.txt of v${version} lists every platform asset`, async () => {
      const text = await fetchTextForTest(
        `https://github.com/ahlfs/LAM-Router/releases/download/v${version}/checksums.txt`,
      );
      for (const spec of Object.values(cli.PLATFORMS)) {
        assert.ok(cli.parseChecksum(text, spec.asset), `missing checksum for ${spec.asset}`);
      }
    });

    await checkAsync(`downloads and verifies ${cli.PLATFORMS['win32-x64'].asset}`, async () => {
      const destDir = path.join(tmpRoot, 'win32-x64');
      const started = Date.now();
      const binary = await cli.downloadVerified(version, 'win32-x64', destDir);
      const stat = fs.statSync(binary);
      assert.strictEqual(path.basename(binary), 'lam-router.exe');
      assert.ok(stat.size > 10 * 1024 * 1024, `unexpected size ${stat.size}`);
      assert.ok(Date.now() - started < 120000, 'download took longer than the timeout');
      console.log(`     ${(stat.size / 1048576).toFixed(1)} MB in ${((Date.now() - started) / 1000).toFixed(1)}s`);
      // The verified file is a real Windows PE binary, not an HTML error page.
      const header = Buffer.alloc(2);
      const fd = fs.openSync(binary, 'r');
      fs.readSync(fd, header, 0, 2, 0);
      fs.closeSync(fd);
      assert.strictEqual(header.toString('latin1'), 'MZ', 'downloaded file is not a PE executable');
    });

    await checkAsync('the file on disk matches the digest published in checksums.txt', async () => {
      const dest = path.join(tmpRoot, 'win32-x64', 'lam-router.exe');
      const published = cli.parseChecksum(
        await fetchTextForTest(
          `https://github.com/ahlfs/LAM-Router/releases/download/v${version}/checksums.txt`,
        ),
        cli.PLATFORMS['win32-x64'].asset,
      );
      const onDisk = crypto.createHash('sha256').update(fs.readFileSync(dest)).digest('hex');
      assert.strictEqual(onDisk, published, 'the verified artifact drifted from its published digest');
    });

    await checkAsync('an unverifiable download throws instead of landing unverified', async () => {
      const destDir = path.join(tmpRoot, 'noverify');
      await assert.rejects(
        () => cli.downloadVerified('0.0.0-does-not-exist', 'win32-x64', destDir),
        /HTTP 404|no entry|ENOTFOUND/,
      );
      assert.ok(!fs.existsSync(path.join(destDir, 'lam-router.exe')), 'nothing may be written without a checksum');
    });

    fs.rmSync(tmpRoot, { recursive: true, force: true });
    console.log(`\n${passed} check(s) passed`);
  })().catch((err) => {
    console.error(`  ❌ unexpected failure: ${err.stack || err.message}`);
    process.exitCode = 1;
  });
}

// Small local copy so the unit file has no dependency on the launcher internals.
function fetchTextForTest(url) {
  return new Promise((resolve, reject) => {
    const https = require('https');
    https.get(url, { headers: { 'User-Agent': 'lam-router-launcher-test' } }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        res.resume();
        resolve(fetchTextForTest(new URL(res.headers.location, url).toString()));
        return;
      }
      if (res.statusCode !== 200) {
        res.resume();
        reject(new Error(`HTTP ${res.statusCode} for ${url}`));
        return;
      }
      const chunks = [];
      res.on('data', (c) => chunks.push(c));
      res.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
      res.on('error', reject);
    }).on('error', reject);
  });
}
