// Types mirror brainapi-go-sdk's response structs (see pkg/brainapi/types.go).
// Fields BRAIN may reshape are typed `unknown` here — see
// docs/sdk-protocol.md "Forward-compat json.RawMessage fields" for the list.

export interface ProbeInfo {
  user: { id: string };
  token: { expiry: number };
  permissions: string[];
}

export interface UserInfo {
  id: string;
  email: string;
  firstName?: string;
  lastName?: string;
  fullName?: string;
  gender?: string;
  telephone?: string | null;
  dateCreated?: string;
  dateVerified?: string;
  dateApproved?: string;
  verified: boolean;
  approved: boolean;
  address?: unknown;
  education?: unknown;
  employment?: unknown;
  recruitment?: unknown;
  resume?: unknown;
  image?: unknown;
  settings?: unknown;
  onboarding?: unknown;
  geniusLevel?: unknown;
}

export interface AlphaCheck {
  name: string;
  result: string; // PASS | WARNING | FAIL | PENDING | ERROR
  value?: number;
  limit?: number;
  message?: string; // human-readable note on WARNING / non-numeric checks (e.g. REVERSION_COMPONENT)
}

export interface Alpha {
  id: string;
  type?: string;
  author?: string;
  regular?: unknown;
  settings?: unknown;
  is?: {
    sharpe?: number;
    fitness?: number;
    turnover?: number;
    checks?: AlphaCheck[];
    [k: string]: unknown;
  };
  status?: string;
  dateCreated?: string;
  dateSubmitted?: string | null;
  team?: unknown;
  color?: unknown;
  category?: unknown;
}

export interface AlphasPage {
  count: number;
  next?: string | null;
  previous?: string | null;
  results: Alpha[];
}

export interface SimulationSettings {
  instrumentType: string;
  region: string;
  universe: string;
  delay: number;
  decay: number;
  neutralization: string;
  truncation: number;
  pasteurization: string;
  unitHandling: string;
  nanHandling: string;
  language: string;
  visualization: boolean;
}

// BRAIN web UI defaults. Override per call.
export const DEFAULT_SIM_SETTINGS: SimulationSettings = {
  instrumentType: 'EQUITY',
  region: 'USA',
  universe: 'TOP3000',
  delay: 1,
  decay: 4,
  neutralization: 'SUBINDUSTRY',
  truncation: 0.08,
  pasteurization: 'ON',
  unitHandling: 'VERIFY',
  nanHandling: 'OFF',
  language: 'FASTEXPR',
  visualization: false,
};

export interface SimulationRequest {
  type: 'REGULAR' | 'SUPER' | 'COMBO';
  regular?: string;
  super?: string;
  combo?: unknown;
  settings: SimulationSettings;
}

export interface SimulationStatus {
  id?: string;
  type?: string;
  status?: '' | 'COMPLETE' | 'FAIL' | 'ERROR' | 'WARNING';
  alpha?: string;
  message?: string;
  progress?: number;
  settings?: unknown;
  regular?: unknown;
}

export type VerdictStatus =
  | 'verified'
  | 'corr_rejected'
  | 'submit_failed'
  | 'pending_corr'
  | (string & {});

export interface Verdict {
  status: VerdictStatus;
  reason?: string;
  body?: Alpha;
  checks?: AlphaCheck[];
  http?: number;
}

// ---------- self-correlation ----------

// Server-side GET /alphas/{id}/correlations/self (`alphas corr`). Embeds the
// record-set block; min/max are null for fresh accounts with no submitted peers.
export interface SelfCorrelationBlock {
  schema?: unknown;
  records?: unknown[];
  min?: number | null;
  max?: number | null;
}

// One alpha's id + cumulative-PnL series for `alphas corr-local`. records are
// [date, cumulativePnl] tuples — the same shape `alphas pnl` emits.
export interface AlphaPnLInput {
  id: string;
  records: Array<[string, number]>;
}

// Input body for offline self-correlation (`alphas corr-local`).
export interface SelfCorrLocalInput {
  candidate: AlphaPnLInput;
  neighbours: AlphaPnLInput[];
}

export interface CorrNeighbour {
  id: string;
  corr: number;
  overlap: number;
}

// Result of `alphas corr-local`. corrMax is signed, ranked by |corr|; skipped
// counts neighbours dropped for < 30-day date overlap.
export interface MaxSelfCorrResult {
  corrMax: number;
  neighbours: CorrNeighbour[];
  considered: number;
  skipped: number;
}

export interface ListAlphasOpts {
  status?: 'ACTIVE' | 'UNSUBMITTED' | 'DECOMMISSIONED';
  order?: string;
  limit?: number;
  offset?: number;
  all?: boolean;
}

// One item of GET /users/self/messages — the notification-center feed.
// `type` is "ANNOUNCEMENT" (platform announcements, incl. new-dataset notices)
// or "NOTIFICATION" (per-user events, e.g. achievements) — live-confirmed full
// set. Dataset releases arrive as ANNOUNCEMENT messages identified by `title`
// (e.g. "📢 Launching a new dataset …"); filter on title client-side.
// `description` is rendered HTML that may embed large base64 data-URI images;
// strip it before feeding to size-sensitive sinks.
export interface Message {
  id: string;
  type: string;
  title: string;
  description: string;
  dateCreated: string;
  tags: string[];
  read: boolean;
}

export interface MessagesPage {
  count: number;
  next?: string | null;
  previous?: string | null;
  results: Message[];
}

export interface ListMessagesOpts {
  type?: string;
  order?: string;
  limit?: number;
  offset?: number;
  all?: boolean;
}

export type ActivityKind = 'base-payment' | 'other-payment' | 'simulations' | 'submissions';

export interface ActivityPeriod {
  start: string;
  end: string;
  value: number;
}

export type ActivityRecord = Record<string, number | string | boolean | null>;

export interface DecodedActivityStream {
  type: 'DAILY' | 'LIST';
  currency?: string;
  yesterday?: ActivityPeriod;
  current?: ActivityPeriod;
  previous?: ActivityPeriod;
  ytd?: ActivityPeriod;
  total?: ActivityPeriod;
  records: ActivityRecord[];
}

// Output of `brainapi describe`. Stable contract per docs/sdk-protocol.md.
export interface DescribeSpec {
  version: string;
  envelope: { success: string; failure: string };
  exitCodes: Array<{ code: number; name: string; kinds?: string[] }>;
  errorKinds: Array<{ kind: string; exitCode: number; detailsShape?: string }>;
  commands: Array<{
    path: string[];
    short: string;
    args?: string;
    flags?: Array<{
      name: string;
      shorthand?: string;
      type: string;
      default?: string;
      usage?: string;
    }>;
    longPoll?: boolean;
  }>;
  nonObviousContracts: Array<{ id: string; topic: string; summary: string; ref: string }>;
}
