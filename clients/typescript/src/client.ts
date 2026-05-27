import { spawn } from 'node:child_process';
import {
  parseEnvelope,
  errorForExitCode,
  TransportError,
  BrainapiError,
} from './envelope.js';
import { resolveBinary } from './binary.js';
import type {
  ActivityKind,
  Alpha,
  AlphasPage,
  DecodedActivityStream,
  DescribeSpec,
  ListAlphasOpts,
  ListMessagesOpts,
  MaxSelfCorrResult,
  MessagesPage,
  ProbeInfo,
  SelfCorrelationBlock,
  SelfCorrLocalInput,
  SimulationRequest,
  SimulationSettings,
  SimulationStatus,
  UserInfo,
  Verdict,
} from './types.js';
import { DEFAULT_SIM_SETTINGS } from './types.js';

export interface ClientOptions {
  /** Override binary path. Default resolution: BRAINAPI_BIN env > bundled > PATH. */
  binary?: string;
  /** BRAIN account email. Falls back to BRAINAPI_USER env. */
  user?: string;
  /** BRAIN account password. Falls back to BRAINAPI_PASS env. */
  pass?: string;
  /** Override base URL (staging / proxy). Default: https://api.worldquantbrain.com */
  baseUrl?: string;
  /** Cookie jar file path. Default: platform-specific cache dir, keyed by email. */
  cookieJar?: string;
  /** Per-invocation subprocess timeout. Default 300s — generous for long-poll endpoints. */
  timeoutMs?: number;
  /** Proxy URL (http/https/socks5). */
  proxy?: string;
  /** TLS impersonation profile (chrome131 etc., or `auto:<email>`). */
  profile?: string;
}

export class Client {
  private readonly binary: string;
  private readonly opts: ClientOptions;

  constructor(opts: ClientOptions = {}) {
    this.opts = opts;
    this.binary = resolveBinary(opts.binary);
  }

  // ---------- auth ----------
  probe = (): Promise<ProbeInfo> => this.run<ProbeInfo>(['auth', 'probe']);
  login = (): Promise<unknown> => this.run<unknown>(['auth', 'login']);
  logout = (): Promise<{ signed_out: boolean }> =>
    this.run<{ signed_out: boolean }>(['auth', 'logout']);

  // ---------- users ----------
  self = (): Promise<UserInfo> => this.run<UserInfo>(['users', 'self']);
  competitions = (): Promise<unknown> => this.run<unknown>(['users', 'competitions']);
  activities = (kind: ActivityKind): Promise<DecodedActivityStream> =>
    this.run<DecodedActivityStream>(['users', 'activities', kind, '--decode']);

  // ---------- alphas ----------
  getAlpha = (id: string): Promise<Alpha> => this.run<Alpha>(['alphas', 'get', id]);
  checkAlpha = (id: string): Promise<unknown> => this.run<unknown>(['alphas', 'check', id]);
  submitAlpha = (id: string): Promise<Verdict> => this.run<Verdict>(['alphas', 'submit', id]);
  alphaPnl = (id: string): Promise<unknown> => this.run<unknown>(['alphas', 'pnl', id]);
  // Server-side self-corr: GET /alphas/{id}/correlations/self. Gate submission
  // on `max < 0.7`.
  alphaCorr = (id: string): Promise<SelfCorrelationBlock> =>
    this.run<SelfCorrelationBlock>(['alphas', 'corr', id]);
  // Offline self-corr: compute locally from supplied candidate + neighbour PnL,
  // no BRAIN call. Works on PnL that isn't yet a main-account alpha.
  alphaCorrLocal = (input: SelfCorrLocalInput): Promise<MaxSelfCorrResult> =>
    this.run<MaxSelfCorrResult>(['alphas', 'corr-local', '--json', '-'], JSON.stringify(input));
  listAlphas = (opts: ListAlphasOpts = {}): Promise<AlphasPage> => {
    const args = ['alphas', 'list'];
    if (opts.status) args.push('--status', opts.status);
    if (opts.order) args.push('--order', opts.order);
    if (typeof opts.limit === 'number') args.push('--limit', String(opts.limit));
    if (typeof opts.offset === 'number') args.push('--offset', String(opts.offset));
    if (opts.all) args.push('--all');
    return this.run<AlphasPage>(args);
  };

  // ---------- messages ----------
  // GET /users/self/messages: notification feed (announcements, dataset
  // updates, ...). Pass `type` to filter; `all` drains every page.
  listMessages = (opts: ListMessagesOpts = {}): Promise<MessagesPage> => {
    const args = ['messages', 'list'];
    if (opts.type) args.push('--type', opts.type);
    if (opts.order) args.push('--order', opts.order);
    if (typeof opts.limit === 'number') args.push('--limit', String(opts.limit));
    if (typeof opts.offset === 'number') args.push('--offset', String(opts.offset));
    if (opts.all) args.push('--all');
    return this.run<MessagesPage>(args);
  };

  // ---------- simulations ----------
  createSimulation = (req: SimulationRequest): Promise<{ id: string }> =>
    this.run<{ id: string }>(['simulations', 'create', '--json', '-'], JSON.stringify(req));
  getSimulation = (id: string): Promise<SimulationStatus> =>
    this.run<SimulationStatus>(['simulations', 'get', id]);
  waitSimulation = (id: string): Promise<SimulationStatus> =>
    this.run<SimulationStatus>(['simulations', 'wait', id]);

  // ---------- schema ----------
  operators = (): Promise<unknown[]> => this.run<unknown[]>(['schema', 'operators']);
  dataFields = (params: {
    region: string;
    universe: string;
    delay: 0 | 1;
    instrumentType?: string;
    dataset?: string;
    search?: string;
    all?: boolean;
  }): Promise<{ count: number; results: unknown[] }> => {
    const args = ['schema', 'data-fields'];
    args.push('--region', params.region);
    args.push('--universe', params.universe);
    args.push('--delay', String(params.delay));
    if (params.instrumentType) args.push('--instrument-type', params.instrumentType);
    if (params.dataset) args.push('--dataset', params.dataset);
    if (params.search) args.push('--search', params.search);
    if (params.all) args.push('--all');
    return this.run<{ count: number; results: unknown[] }>(args);
  };

  // ---------- registration / email / password ----------
  // These are rarely needed by integrators driving an existing account; provided
  // as raw passthroughs. See docs/sdk-protocol.md for the request body shapes.
  register = (body: unknown): Promise<unknown> =>
    this.run<unknown>(['register', '--json', '-'], JSON.stringify(body));
  emailReverify = (email: string, recaptcha?: string): Promise<unknown> => {
    const args = ['email', 'reverify', '--email', email];
    if (recaptcha) args.push('--recaptcha', recaptcha);
    return this.run<unknown>(args);
  };
  emailVerify = (jwt: string): Promise<unknown> =>
    this.run<unknown>(['email', 'verify', '--jwt', jwt]);
  passwordForgot = (email: string, recaptcha?: string): Promise<unknown> => {
    const args = ['password', 'forgot', '--email', email];
    if (recaptcha) args.push('--recaptcha', recaptcha);
    return this.run<unknown>(args);
  };
  passwordReset = (jwt: string, password: string): Promise<unknown> =>
    this.run<unknown>(['password', 'reset', '--jwt', jwt, '--password', password]);

  // ---------- meta ----------
  describe = (): Promise<DescribeSpec> => this.run<DescribeSpec>(['describe']);
  version = (): Promise<{ version: string; commit?: string; date?: string }> =>
    this.run<{ version: string; commit?: string; date?: string }>(['version']);

  // ---------- composite ----------
  /**
   * Backtest = createSimulation -> waitSimulation -> (if alpha) getAlpha.
   * Returns the terminal sim status and the alpha record if one was produced.
   * Terminates on sim.alpha !== '' (covers COMPLETE / WARNING-with-alpha cases),
   * matching brainapi-go's WaitForSimulation contract.
   */
  backtest = async (
    expression: string,
    overrides: Partial<SimulationSettings> = {},
    type: 'REGULAR' | 'SUPER' = 'REGULAR',
  ): Promise<{ simulation: SimulationStatus; alpha: Alpha | null }> => {
    const settings: SimulationSettings = { ...DEFAULT_SIM_SETTINGS, ...overrides };
    const req: SimulationRequest = {
      type,
      settings,
      ...(type === 'REGULAR' ? { regular: expression } : { super: expression }),
    };
    const created = await this.createSimulation(req);
    const sim = await this.waitSimulation(created.id);
    let alpha: Alpha | null = null;
    if (sim.alpha) {
      alpha = await this.getAlpha(sim.alpha);
    }
    return { simulation: sim, alpha };
  };

  // ---------- escape hatch ----------
  /**
   * Run any brainapi subcommand. Use this when the wrapper hasn't added a
   * typed method yet (e.g. after a brainapi-go release adds a new command).
   * Stdin is optional and consumed verbatim; the binary always emits a
   * JSON envelope which this method unwraps and returns as T.
   */
  run = async <T>(args: string[], stdin?: string): Promise<T> => {
    const env: NodeJS.ProcessEnv = { ...process.env };
    if (this.opts.user) env.BRAINAPI_USER = this.opts.user;
    if (this.opts.pass) env.BRAINAPI_PASS = this.opts.pass;

    const fullArgs: string[] = [];
    if (this.opts.cookieJar) fullArgs.push('--cookie-jar', this.opts.cookieJar);
    if (this.opts.baseUrl) fullArgs.push('--base-url', this.opts.baseUrl);
    if (this.opts.proxy) fullArgs.push('--proxy', this.opts.proxy);
    if (this.opts.profile) fullArgs.push('--profile', this.opts.profile);
    fullArgs.push(...args);

    const result = await spawnCapture(this.binary, fullArgs, {
      env,
      stdin,
      timeoutMs: this.opts.timeoutMs ?? 300_000,
    });
    const parsed = parseEnvelope<T>(result.stdout, result.stderr, result.exitCode);
    if (parsed.ok) return parsed.data;
    const Ctor = errorForExitCode(result.exitCode);
    throw new Ctor(parsed.error.message, result.exitCode, parsed.error.kind, parsed.error.details);
  };
}

interface SpawnResult {
  stdout: string;
  stderr: string;
  exitCode: number;
}

function spawnCapture(
  binary: string,
  args: string[],
  opts: { env: NodeJS.ProcessEnv; stdin?: string; timeoutMs: number },
): Promise<SpawnResult> {
  return new Promise<SpawnResult>((resolve, reject) => {
    const child = spawn(binary, args, {
      env: opts.env,
      stdio: ['pipe', 'pipe', 'pipe'],
      windowsHide: true,
    });
    const outChunks: Buffer[] = [];
    const errChunks: Buffer[] = [];
    let timer: NodeJS.Timeout | null = null;
    let timedOut = false;
    child.stdout.on('data', (c: Buffer) => outChunks.push(c));
    child.stderr.on('data', (c: Buffer) => errChunks.push(c));
    child.on('error', (e) => {
      if (timer) clearTimeout(timer);
      reject(new TransportError(`spawn ${binary}: ${e.message}`, -1));
    });
    child.on('close', (code, signal) => {
      if (timer) clearTimeout(timer);
      const stdout = Buffer.concat(outChunks).toString('utf8');
      const stderr = Buffer.concat(errChunks).toString('utf8');
      if (timedOut) {
        reject(
          new TransportError(
            `brainapi timed out after ${opts.timeoutMs}ms (signal=${signal ?? 'none'})`,
            -1,
          ),
        );
        return;
      }
      resolve({ stdout, stderr, exitCode: code ?? -1 });
    });
    if (opts.stdin !== undefined) {
      child.stdin.end(opts.stdin);
    } else {
      child.stdin.end();
    }
    if (opts.timeoutMs > 0) {
      timer = setTimeout(() => {
        timedOut = true;
        child.kill('SIGTERM');
        // Hard-kill after 2s grace
        setTimeout(() => child.kill('SIGKILL'), 2000);
      }, opts.timeoutMs);
    }
  });
}

// Re-exports for callers that want to import only from this file.
export { BrainapiError } from './envelope.js';
