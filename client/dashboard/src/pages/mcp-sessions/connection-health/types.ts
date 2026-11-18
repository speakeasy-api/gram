// mcpConnectionHealth API shapes (AIM-1029 contract, 2026-11-18), SDK camelCase.

export const ATTEMPT_OUTCOMES = [
  "connected",
  "failed",
  "blocked",
  "declined",
  "didnt_finish",
  "in_progress",
] as const;
export type Outcome = (typeof ATTEMPT_OUTCOMES)[number];

/** The five outcomes the design offers as filters. */
export const FILTER_OUTCOMES: readonly Outcome[] = [
  "connected",
  "failed",
  "blocked",
  "declined",
  "didnt_finish",
];

export const STEP_GROUPS = [
  "connect",
  "sign_in",
  "service_sign_in",
  "sign_in_renewal",
  "service_renewal",
  "listing_tools",
  "request",
  "other",
] as const;
export type StepGroup = (typeof STEP_GROUPS)[number];

/** The steps the design offers as filters. */
export const FILTER_STEPS: readonly StepGroup[] = STEP_GROUPS.filter(
  (step) => step !== "other",
);

/** Sign-in activity lists attempts that ended at these steps. */
export const SIGN_IN_STEPS: readonly StepGroup[] = [
  "sign_in",
  "service_sign_in",
  "sign_in_renewal",
  "service_renewal",
];

/** The party that returned a failure. */
export type Source =
  | "service"
  | "app"
  | "identity_provider"
  | "speakeasy_setting"
  | "speakeasy";

export type ServerKind =
  | "mcp"
  | "gateway"
  | "toolset"
  | "authorization_server"
  | "none";

export type ViewerScope = "all" | "own";

export interface Person {
  subjectUrn: string;
  kind: "user" | "agent" | "apikey" | "workload" | "anonymous" | "other";
  userId?: string;
  name?: string;
  /** getAttempt only. */
  email?: string;
}

export interface App {
  /** "" when the app is not on the stored list. */
  family: string;
  /** "Unlisted app" when the family is unknown. */
  name: string;
  version?: string;
  appKey?: string;
}

export interface ServerRef {
  id: string;
  kind: ServerKind;
  name?: string;
}

export interface Network {
  /** Opaque; the filter value. */
  key: string;
  /** First 4 hex characters, as shown. */
  label: string;
}

export interface AttemptFilters {
  outcomes?: Outcome[];
  steps?: StepGroup[];
  serverIds?: string[];
  memberIds?: string[];
  /** Subject URNs. */
  people?: string[];
  networks?: string[];
  /** App families; "" selects unlisted apps. */
  apps?: string[];
  /** "<family>@<version>". */
  appVersions?: string[];
  /** Reason codes. */
  reasons?: string[];
  /** Service hosts. */
  services?: string[];
}

export interface AttemptRangeRequest {
  userSessionIssuerId: string;
  serverId?: string;
  from: string;
  to: string;
  filters?: AttemptFilters;
}

export interface ListAttemptsRequest extends AttemptRangeRequest {
  cursor?: string;
  limit?: number;
}

/** One Connection Logs row; failed and blocked repeats fold into one (`count` > 1). */
export interface AttemptRow {
  /** Reference of the newest attempt in the row. */
  id: string;
  time: string;
  firstStartedAt: string;
  count: number;
  outcome: Outcome;
  step: StepGroup;
  reason?: string;
  message?: string;
  source?: Source;
  /** Absent: no known person. */
  person?: Person;
  network?: Network;
  app: App;
  server?: ServerRef;
  /** Gateway member on the ending step. */
  member?: ServerRef;
  service?: string;
}

export interface ListAttemptsResponse {
  loggingEnabled: boolean;
  viewerScope: ViewerScope;
  attempts?: AttemptRow[];
  nextCursor?: string;
}

export interface FacetValue {
  value: string;
  label?: string;
  attempts: number;
}

export interface AttemptFacets {
  outcomes: FacetValue[];
  steps: FacetValue[];
  servers: FacetValue[];
  members: FacetValue[];
  people: FacetValue[];
  networks: FacetValue[];
  apps: FacetValue[];
  appVersions: FacetValue[];
  reasons: FacetValue[];
  services: FacetValue[];
}

export interface AttemptCountsResponse {
  loggingEnabled: boolean;
  viewerScope: ViewerScope;
  totalAttempts?: number;
  facets?: AttemptFacets;
  truncated?: boolean;
}

export interface AttemptStep {
  at: string;
  step: StepGroup;
  storedStep: string;
  /** Raw step result, e.g. "signed_in", "failed". */
  result: string;
  final: boolean;
  reason?: string;
  message?: string;
  member?: ServerRef;
  service?: string;
}

export interface AttemptDetail extends AttemptRow {
  startedAt: string;
  userSessionIssuerId?: string;
  traceId?: string;
  steps: AttemptStep[];
  context: { grantType?: string; subReason?: string; retries: number };
}

export interface GetAttemptResponse {
  loggingEnabled: boolean;
  viewerScope: ViewerScope;
  found?: boolean;
  attempt?: AttemptDetail;
}

export type SummaryBucket = "hour" | "day";

export interface SignInSummaryResponse {
  loggingEnabled: boolean;
  viewerScope: ViewerScope;
  bucket?: SummaryBucket;
  buckets?: {
    start: string;
    signIns: { succeeded: number; didNot: number };
    renewals: { succeeded: number; didNot: number };
  }[];
}

export interface SessionActivityResponse {
  days: { day: string; activeSessions: number }[];
  activeNow: number;
  averageSessionSeconds?: number;
  configuredSessionSeconds: number;
}

export interface DeniedClient {
  app: App;
  reason: string;
  message: string;
  attempts: number;
  people: number;
  networks: number;
  lastAt: string;
}

export interface DeniedClientsResponse {
  loggingEnabled: boolean;
  viewerScope: ViewerScope;
  clients?: DeniedClient[];
}

export type ProblemRule =
  | "three_people_same_server"
  | "three_people_same_service"
  | "three_people_same_app_version"
  | "one_person_24_hours";

export interface SuspectedProblem {
  id: string;
  /** "5 people can't connect to the Linear service". */
  headline: string;
  /** "Linear is returning errors." */
  fact: string;
  since: string;
  people: number;
  /** Only when listed for one server. */
  peopleOnServer?: number;
  rule: ProblemRule;
  /** Opens Connection Logs with exactly this item's rows. */
  filters: { from: string } & AttemptFilters;
}

export interface ListSuspectedProblemsResponse {
  loggingEnabled: boolean;
  problems?: SuspectedProblem[];
}

/** Contract `people` collides (count vs list); the list count is `people.length`. */
export interface SuspectedProblemDetail extends Omit<
  SuspectedProblem,
  "people"
> {
  source?: Source;
  failedPer10Minutes: { start: string; attempts: number }[];
  lastConnectedAt?: string;
  messages: { reason: string; message: string; attempts: number }[];
  people: {
    person?: Person;
    network?: Network;
    app: App;
    server?: ServerRef;
    attempts: number;
    firstFailedAt: string;
  }[];
  servers: { server: ServerRef; attempts: number }[];
}

export interface GetSuspectedProblemResponse {
  loggingEnabled: boolean;
  found: boolean;
  problem?: SuspectedProblemDetail;
}

/** Not in the contract; Killswitches in force stays on fixtures. */
export interface ActiveKillswitch {
  id: string;
  subject: { kind: "agent" | "person"; name: string };
  servers: string[];
  until: string | null;
}
