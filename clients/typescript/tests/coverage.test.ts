// Drift-detection: every leaf command emitted by `brainapi describe` must
// have a wrapper method. If brainapi-go adds a new subcommand, this test
// fails until the wrapper is updated. Conversely, removing a wrapper while
// the binary still ships the command is also caught.

import { describe, it, expect, beforeAll } from 'vitest';
import { existsSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

import { Client } from '../src/index.js';

const here = dirname(fileURLToPath(import.meta.url));
const isWin = process.platform === 'win32';
const monorepoBinary = join(here, '..', '..', '..', 'bin', `brainapi${isWin ? '.exe' : ''}`);

// Commands the wrapper provides typed methods for. Update alongside client.ts.
// Format: space-joined cobra path.
const WRAPPED = new Set<string>([
  'auth login',
  'auth probe',
  'auth logout',
  'users self',
  'users competitions',
  'users activities',
  'alphas get',
  'alphas check',
  'alphas submit',
  'alphas pnl',
  'alphas corr',
  'alphas corr-local',
  'alphas list',
  'simulations create',
  'simulations get',
  'simulations wait',
  'schema operators',
  'schema data-fields',
  'register',
  'email reverify',
  'email verify',
  'password forgot',
  'password reset',
  'describe',
  'version',
]);

describe('wrapper covers every brainapi describe command', () => {
  let allCommands: string[];

  beforeAll(async () => {
    const binary =
      process.env.BRAINAPI_BIN && existsSync(process.env.BRAINAPI_BIN)
        ? process.env.BRAINAPI_BIN
        : monorepoBinary;
    const cl = new Client({ binary });
    const spec = await cl.describe();
    allCommands = spec.commands.map((c) => c.path.join(' '));
  });

  it('no commands missing from the wrapper', () => {
    const missing = allCommands.filter((c) => !WRAPPED.has(c));
    expect(missing).toEqual([]);
  });

  it('no wrapper methods for non-existent commands', () => {
    const orphaned = [...WRAPPED].filter((c) => !allCommands.includes(c));
    expect(orphaned).toEqual([]);
  });
});
