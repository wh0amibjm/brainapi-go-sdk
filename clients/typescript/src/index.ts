// Public API barrel.

export { Client, type ClientOptions } from './client.js';

export {
  parseEnvelope,
  errorForExitCode,
  BrainapiError,
  UsageError,
  RateLimitError,
  BannedError,
  DRFValidationError,
  APIError,
  BudgetExhaustedError,
  NetworkError,
  PersonaInquiryError,
  TransportError,
  type Envelope,
  type ErrorEnvelope,
} from './envelope.js';

export { resolveBinary } from './binary.js';

export {
  DEFAULT_SIM_SETTINGS,
  type ActivityKind,
  type ActivityPeriod,
  type ActivityRecord,
  type Alpha,
  type AlphaCheck,
  type AlphasPage,
  type DecodedActivityStream,
  type DescribeSpec,
  type ListAlphasOpts,
  type ProbeInfo,
  type SimulationRequest,
  type SimulationSettings,
  type SimulationStatus,
  type UserInfo,
  type Verdict,
  type VerdictStatus,
} from './types.js';
