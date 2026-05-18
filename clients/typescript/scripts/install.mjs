#!/usr/bin/env node
// Postinstall: download the platform-specific brainapi binary from the
// matching GitHub release, verify SHA256, and place it at <package>/bin/.
//
// Skip via BRAINAPI_SKIP_DOWNLOAD=1 (useful for CI and monorepo dev where
// the binary lives elsewhere). Failure NEVER aborts the install — we log
// instructions and exit 0 so `npm install` succeeds; the Client constructor
// throws with the same instructions if the binary is missing at runtime.

import { createHash } from 'node:crypto';
import { createWriteStream, mkdirSync, chmodSync, readFileSync, existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { pipeline } from 'node:stream/promises';

const here = dirname(fileURLToPath(import.meta.url));
const pkgRoot = join(here, '..');
const pkg = JSON.parse(readFileSync(join(pkgRoot, 'package.json'), 'utf8'));

function bail(msg, ...extra) {
  console.error(`[brainapi-postinstall] ${msg}`);
  for (const line of extra) console.error(`  ${line}`);
  console.error(
    `  Set BRAINAPI_BIN at runtime to a local binary, OR rebuild from source:`,
    `cd brainapi-go && go build -o brainapi ./cmd/brainapi`,
  );
  process.exit(0);
}

if (process.env.BRAINAPI_SKIP_DOWNLOAD === '1') {
  console.log('[brainapi-postinstall] BRAINAPI_SKIP_DOWNLOAD=1 — skipping');
  process.exit(0);
}

const osMap = { linux: 'linux', darwin: 'darwin', win32: 'windows' };
const archMap = { x64: 'amd64', arm64: 'arm64' };
const goos = osMap[process.platform];
const goarch = archMap[process.arch];
if (!goos || !goarch) bail(`unsupported platform ${process.platform}/${process.arch}`);
if (goos === 'windows' && goarch === 'arm64') {
  bail('windows/arm64 is not built by the release pipeline');
}

const ext = goos === 'windows' ? '.exe' : '';
const version = `v${pkg.version}`;
const filename = `brainapi-${version}-${goos}-${goarch}${ext}`;
const base = `https://github.com/wh0amibjm/brainapi-go-sdk/releases/download/${version}`;
const binUrl = `${base}/${filename}`;
const sumsUrl = `${base}/SHA256SUMS.txt`;

const binDir = join(pkgRoot, 'bin');
mkdirSync(binDir, { recursive: true });
const outPath = join(binDir, `brainapi${ext}`);

if (existsSync(outPath)) {
  console.log(`[brainapi-postinstall] ${outPath} already present — skipping`);
  process.exit(0);
}

console.log(`[brainapi-postinstall] fetching ${filename}`);

try {
  const sumsResp = await fetch(sumsUrl);
  if (!sumsResp.ok) bail(`SHA256SUMS.txt fetch HTTP ${sumsResp.status}`, `URL: ${sumsUrl}`);
  const sums = await sumsResp.text();
  const expected = sums
    .split('\n')
    .map((l) => l.match(/^([0-9a-f]{64})\s+\*?(\S+)$/))
    .filter(Boolean)
    .find((m) => m[2] === filename);
  if (!expected) bail(`${filename} not listed in SHA256SUMS.txt`);

  const binResp = await fetch(binUrl);
  if (!binResp.ok) bail(`binary fetch HTTP ${binResp.status}`, `URL: ${binUrl}`);
  if (!binResp.body) bail('binary response had no body');

  const hasher = createHash('sha256');
  await pipeline(
    binResp.body,
    async function* (source) {
      for await (const chunk of source) {
        hasher.update(chunk);
        yield chunk;
      }
    },
    createWriteStream(outPath),
  );
  const got = hasher.digest('hex');
  if (got !== expected[1]) {
    bail(`SHA mismatch — expected ${expected[1]}, got ${got}`);
  }
  if (goos !== 'windows') chmodSync(outPath, 0o755);
  console.log(`[brainapi-postinstall] installed ${outPath} (sha256 verified)`);
} catch (e) {
  bail(`download failed: ${e.message}`);
}
