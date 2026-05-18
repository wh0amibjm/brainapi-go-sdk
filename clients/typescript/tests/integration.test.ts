// Integration tests against the actual brainapi binary. Requires either:
//   - BRAINAPI_BIN env pointing at a real binary, OR
//   - The monorepo's local build at ../../bin/brainapi[.exe]
//
// These tests do NOT hit the live BRAIN API — they exercise the local-only
// commands (describe, version) so the wrapper plumbing is verified in CI
// without network or credentials.

import { describe, it, expect, beforeAll } from 'vitest';
import { existsSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

import { Client } from '../src/index.js';

const here = dirname(fileURLToPath(import.meta.url));
const isWin = process.platform === 'win32';
const monorepoBinary = join(here, '..', '..', '..', 'bin', `brainapi${isWin ? '.exe' : ''}`);

function pickBinary(): string {
  if (process.env.BRAINAPI_BIN && existsSync(process.env.BRAINAPI_BIN)) {
    return process.env.BRAINAPI_BIN;
  }
  if (existsSync(monorepoBinary)) return monorepoBinary;
  throw new Error(
    `no brainapi binary found; set BRAINAPI_BIN or build with ` +
      `\`go build -o bin/brainapi ./cmd/brainapi\` in the monorepo root`,
  );
}

describe('Client (integration)', () => {
  let cl: Client;

  beforeAll(() => {
    cl = new Client({ binary: pickBinary() });
  });

  it('version → semver string', async () => {
    const v = await cl.version();
    expect(v).toHaveProperty('version');
    expect(typeof v.version).toBe('string');
  });

  it('describe → spec with envelope/exitCodes/errorKinds/commands/contracts', async () => {
    const spec = await cl.describe();
    expect(spec.envelope.success).toContain('"ok":true');
    expect(spec.envelope.failure).toContain('"ok":false');
    expect(spec.exitCodes.length).toBeGreaterThanOrEqual(9);
    expect(spec.errorKinds.length).toBeGreaterThanOrEqual(12);
    expect(spec.commands.length).toBeGreaterThanOrEqual(20);
    expect(spec.nonObviousContracts.length).toBeGreaterThanOrEqual(8);
  });

  it('long-poll commands carry longPoll=true', async () => {
    const spec = await cl.describe();
    const longPolls = spec.commands.filter((c) => c.longPoll).map((c) => c.path.join(' '));
    expect(longPolls).toEqual(
      expect.arrayContaining(['alphas check', 'alphas submit', 'alphas pnl', 'simulations wait']),
    );
  });

  it('escape hatch: run any subcommand', async () => {
    const v = await cl.run<{ version: string }>(['version']);
    expect(typeof v.version).toBe('string');
  });
});
