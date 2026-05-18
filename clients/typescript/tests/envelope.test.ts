import { describe, it, expect } from 'vitest';
import {
  parseEnvelope,
  errorForExitCode,
  APIError,
  RateLimitError,
  BannedError,
  DRFValidationError,
  BudgetExhaustedError,
  NetworkError,
  PersonaInquiryError,
  UsageError,
  BrainapiError,
} from '../src/envelope.js';

describe('parseEnvelope', () => {
  it('decodes an ok envelope', () => {
    const r = parseEnvelope<{ count: number }>('{"ok":true,"data":{"count":42}}', '', 0);
    expect(r.ok).toBe(true);
    if (r.ok) expect(r.data.count).toBe(42);
  });

  it('decodes a structured error envelope', () => {
    const r = parseEnvelope(
      '{"ok":false,"error":{"kind":"api","message":"boom","details":{"status":500}}}',
      '',
      6,
    );
    expect(r.ok).toBe(false);
    if (!r.ok) {
      expect(r.error.kind).toBe('api');
      expect(r.error.message).toBe('boom');
      expect((r.error.details as { status: number }).status).toBe(500);
    }
  });

  it('synthesises no_output when stdout is empty', () => {
    const r = parseEnvelope('', 'cobra: unknown flag\n', 2);
    expect(r.ok).toBe(false);
    if (!r.ok) {
      expect(r.error.kind).toBe('no_output');
      expect(r.error.message).toContain('cobra');
    }
  });

  it('synthesises parse_failure when stdout is not JSON', () => {
    const r = parseEnvelope('panic: nil pointer\n', '', 8);
    expect(r.ok).toBe(false);
    if (!r.ok) {
      expect(r.error.kind).toBe('parse_failure');
      expect(r.error.message).toContain('non-JSON');
    }
  });
});

describe('errorForExitCode', () => {
  it.each([
    [2, UsageError],
    [3, RateLimitError],
    [4, BannedError],
    [5, DRFValidationError],
    [6, APIError],
    [7, BudgetExhaustedError],
    [8, NetworkError],
    [10, PersonaInquiryError],
  ])('exit %i -> %s', (code, ctor) => {
    expect(errorForExitCode(code)).toBe(ctor);
  });

  it('unknown exit -> BrainapiError', () => {
    expect(errorForExitCode(99)).toBe(BrainapiError);
  });
});
