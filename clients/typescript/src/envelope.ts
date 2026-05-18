// Envelope parser + typed exception hierarchy.
// Mirrors brainapi's stable contract: see docs/sdk-protocol.md.

export type Envelope<T> =
  | { ok: true; data: T }
  | { ok: false; error: ErrorEnvelope };

export interface ErrorEnvelope {
  kind: string;
  message: string;
  details?: unknown;
}

// Exit code → typed error class. Stable per the SDK protocol spec; any
// new kind that exits with one of these codes falls through to the
// generic class for that code.
const EXIT = {
  OK: 0,
  USAGE: 2,
  RATE_LIMIT: 3,
  BANNED: 4,
  DRF_VALIDATION: 5,
  API: 6,
  BUDGET_EXHAUSTED: 7,
  NETWORK: 8,
  PERSONA_INQUIRY: 10,
} as const;

export class BrainapiError extends Error {
  constructor(
    message: string,
    public readonly exitCode: number,
    public readonly kind?: string,
    public readonly details?: unknown,
  ) {
    super(message);
    this.name = new.target.name;
  }
}
export class UsageError extends BrainapiError {}
export class RateLimitError extends BrainapiError {}
export class BannedError extends BrainapiError {}
export class DRFValidationError extends BrainapiError {}
export class APIError extends BrainapiError {}
export class BudgetExhaustedError extends BrainapiError {}
export class NetworkError extends BrainapiError {}
export class PersonaInquiryError extends BrainapiError {}
export class TransportError extends BrainapiError {}

const EXIT_TO_CTOR: Record<
  number,
  new (message: string, exitCode: number, kind?: string, details?: unknown) => BrainapiError
> = {
  [EXIT.USAGE]: UsageError,
  [EXIT.RATE_LIMIT]: RateLimitError,
  [EXIT.BANNED]: BannedError,
  [EXIT.DRF_VALIDATION]: DRFValidationError,
  [EXIT.API]: APIError,
  [EXIT.BUDGET_EXHAUSTED]: BudgetExhaustedError,
  [EXIT.NETWORK]: NetworkError,
  [EXIT.PERSONA_INQUIRY]: PersonaInquiryError,
};

export function errorForExitCode(code: number) {
  return EXIT_TO_CTOR[code] ?? BrainapiError;
}

// parseEnvelope decodes brainapi's stdout. If stdout is empty (cobra parse
// errors exit 2 with no body), we synthesise an error envelope from stderr.
export function parseEnvelope<T>(
  stdout: string,
  stderr: string,
  exitCode: number,
): Envelope<T> {
  const trimmed = stdout.trim();
  if (!trimmed) {
    return {
      ok: false,
      error: {
        kind: 'no_output',
        message: stderr.trim() || `brainapi exited ${exitCode} with no stdout`,
      },
    };
  }
  try {
    return JSON.parse(trimmed) as Envelope<T>;
  } catch (e) {
    return {
      ok: false,
      error: {
        kind: 'parse_failure',
        message: `non-JSON stdout (exit=${exitCode}): ${(e as Error).message}`,
        details: trimmed.slice(0, 500),
      },
    };
  }
}
