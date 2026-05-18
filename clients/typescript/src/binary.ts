// Binary resolution. Priority:
//   1. Constructor option `binary`
//   2. Env BRAINAPI_BIN
//   3. Bundled binary at <package>/bin/brainapi[.exe] (from postinstall)
//   4. `brainapi` on PATH (system install)
//   5. Throw with actionable instructions

import { existsSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';

const isWin = process.platform === 'win32';
const exe = isWin ? '.exe' : '';

function packageRoot(): string {
  // dist/ is at <package>/dist; src/ is at <package>/src.
  // Walk up until we find a package.json.
  let here: string;
  try {
    here = dirname(fileURLToPath(import.meta.url));
  } catch {
    here = __dirname;
  }
  let dir = here;
  for (let i = 0; i < 6; i++) {
    if (existsSync(join(dir, 'package.json'))) return dir;
    const parent = dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return here;
}

export function resolveBinary(explicit?: string): string {
  if (explicit) {
    if (!existsSync(explicit)) {
      throw new Error(`brainapi binary not found at ${explicit}`);
    }
    return explicit;
  }
  const envBin = process.env.BRAINAPI_BIN;
  if (envBin) {
    if (!existsSync(envBin)) {
      throw new Error(`brainapi binary not found at $BRAINAPI_BIN=${envBin}`);
    }
    return envBin;
  }
  const bundled = join(packageRoot(), 'bin', `brainapi${exe}`);
  if (existsSync(bundled)) return bundled;
  // Last resort: PATH lookup.
  try {
    const cmd = isWin ? 'where' : 'which';
    const out = execFileSync(cmd, ['brainapi'], { encoding: 'utf8' });
    const first = out.split(/\r?\n/).find((l) => l.trim().length > 0);
    if (first && existsSync(first.trim())) return first.trim();
  } catch {
    // not on PATH — fall through to error
  }
  throw new Error(
    `brainapi binary not found. Options:\n` +
      `  1. Pass {binary: '/path/to/brainapi'} to the Client constructor\n` +
      `  2. Set BRAINAPI_BIN env var\n` +
      `  3. Reinstall with postinstall enabled (unset BRAINAPI_SKIP_DOWNLOAD)\n` +
      `  4. Build from source: cd brainapi-go && go build -o brainapi ./cmd/brainapi`,
  );
}
