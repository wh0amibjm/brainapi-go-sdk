// Types mirror brainapi-go's response structs (see pkg/brainapi/types.go).
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
  result: string;
  value?: number;
  limit?: number;
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

export interface ListAlphasOpts {
  status?: 'ACTIVE' | 'UNSUBMITTED' | 'DECOMMISSIONED';
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
