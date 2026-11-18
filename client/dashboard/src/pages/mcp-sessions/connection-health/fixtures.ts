// FIXTURE DATA standing in for the mcpConnectionHealth API until the dashboard
// SDK has it (AIM-1029). Every name, address and identifier here is made up.
// Delete this file when `api.ts` points at the SDK.

import type { ConnectionHealthSource } from "./source";
import type {
  ActiveKillswitch,
  App,
  AttemptDetail,
  AttemptFacets,
  AttemptFilters,
  AttemptRow,
  AttemptStep,
  FacetValue,
  Network,
  Outcome,
  Person,
  ServerRef,
  Source,
  StepGroup,
  SuspectedProblem,
  SuspectedProblemDetail,
} from "./types";

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const RETENTION_DAYS = 30;
const PAGE_SIZE = 50;

/** The fixtures' clock: today at 11:05 local, so the sample day reads as today. */
export function fixtureNow(): Date {
  const now = new Date();
  return new Date(now.getFullYear(), now.getMonth(), now.getDate(), 11, 5, 0);
}

/** A local time `dayOffset` days from the fixture day, "HH:MM[:SS]". */
function at(dayOffset: number, time: string): Date {
  const base = fixtureNow();
  const [h = 0, m = 0, s = 0] = time.split(":").map(Number);
  return new Date(
    base.getFullYear(),
    base.getMonth(),
    base.getDate() + dayOffset,
    h,
    m,
    s,
  );
}

function person(name: string): Person {
  const slug = name.toLowerCase().replace(/\s+/g, ".");
  const userId = `user_${slug.replace(/\./g, "_")}`;
  return {
    subjectUrn: `user:${userId}`,
    kind: "user",
    userId,
    name,
    email: `${slug}@acme.example`,
  };
}

const PEOPLE = {
  priya: person("Priya Nair"),
  mei: person("Mei Tan"),
  sam: person("Sam Ortiz"),
  jonas: person("Jonas Weber"),
  omar: person("Omar Haddad"),
  ana: person("Ana Costa"),
  ken: person("Ken Ito"),
  rosa: person("Rosa Mendes"),
  lena: person("Lena Park"),
  hugo: person("Hugo Lindqvist"),
  marcus: person("Marcus Lee"),
  sofia: person("Sofia Marquez"),
  dana: person("Dana Reyes"),
  noor: person("Noor Aziz"),
  lars: person("Lars Nyberg"),
  ivy: person("Ivy Chen"),
  theo: person("Theo Brandt"),
  amara: person("Amara Obi"),
  felix: person("Felix Grant"),
} as const;

const mcp = (id: string, name: string): ServerRef => ({
  id,
  kind: "mcp",
  name,
});

const SERVERS = {
  linear: mcp("srv_linear", "Linear"),
  github: mcp("srv_github", "GitHub"),
  notion: mcp("srv_notion", "Notion"),
  wiki: mcp("srv_wiki", "Internal Wiki"),
  support: mcp("srv_support", "Support Desk"),
  engTools: { id: "gw_eng_tools", kind: "gateway", name: "Engineering Tools" },
} satisfies Record<string, ServerRef>;

/** The Engineering Tools gateway's Linear member. */
const ENG_LINEAR = mcp("srv_eng_linear", "Linear");

const SERVICE_BY_TARGET: Record<string, string> = {
  srv_linear: "linear.app",
  srv_eng_linear: "linear.app",
  srv_github: "github.com",
  srv_notion: "notion.so",
};

const FAMILY_NAMES: Record<string, string> = {
  chatgpt: "ChatGPT",
  cursor: "Cursor",
  codex: "Codex",
  claude_code: "Claude Code",
  claude: "Claude",
  perplexity: "Perplexity",
};

function app(family: string, version?: string): App {
  return { family, name: FAMILY_NAMES[family] ?? "Unlisted app", version };
}
const UNLISTED: App = { family: "", name: "Unlisted app" };

function network(label: string): Network {
  return { key: `${label}${"0".repeat(28)}`, label };
}

interface Reason {
  code: string;
  message: string;
  source: Source;
}

const REASONS = {
  unavailable: {
    code: "provider_unavailable",
    message: "The service is temporarily unavailable.",
    source: "service",
  },
  serverError: {
    code: "provider_server_error",
    message: "The service returned a server error.",
    source: "service",
  },
  signInSystem: {
    code: "remote_session_unavailable",
    message: "The service's sign-in system is temporarily unavailable.",
    source: "service",
  },
  invalidScope: {
    code: "provider_oauth_invalid_scope",
    message: "The service rejected the access we asked for.",
    source: "service",
  },
  metadata: {
    code: "client_metadata_unreadable",
    message: "We could not read this app's public details.",
    source: "app",
  },
  keyRejected: {
    code: "server_key_rejected",
    message: "The server rejected the saved key.",
    source: "service",
  },
  canceled: {
    code: "sign_in_canceled",
    message: "The sign-in was canceled.",
    source: "app",
  },
  staleGrant: {
    code: "refresh_token_unknown",
    message: "The app used an old or unknown sign-in.",
    source: "app",
  },
  noAccess: {
    code: "no_server_access",
    message: "No access to this server.",
    source: "speakeasy_setting",
  },
  clientNotAllowed: {
    code: "client_not_allowed",
    message: "Client access doesn't allow this app.",
    source: "speakeasy_setting",
  },
  appBlocked: {
    code: "client_blocked",
    message: "This AI app is blocked.",
    source: "speakeasy_setting",
  },
  expired: {
    code: "session_expired",
    message: "Sign-in reached its time limit.",
    source: "speakeasy_setting",
  },
} satisfies Record<string, Reason>;

const STORED_STEP: Record<StepGroup, string> = {
  connect: "inbound_authorize",
  sign_in: "inbound_sign_in",
  service_sign_in: "provider_sign_in",
  sign_in_renewal: "sign_in_renewal",
  service_renewal: "provider_refresh",
  listing_tools: "discovery_listing",
  request: "mcp_request",
  other: "other",
};

const SUCCESS_RESULT: Record<StepGroup, string> = {
  connect: "accepted",
  sign_in: "signed_in",
  service_sign_in: "signed_in",
  sign_in_renewal: "renewed",
  service_renewal: "renewed",
  listing_tools: "listed",
  request: "completed",
  other: "completed",
};

interface RowSpec {
  at: Date;
  firstAt?: Date;
  count?: number;
  outcome: Outcome;
  step: StepGroup;
  person?: Person;
  network: string;
  app: App;
  server: ServerRef;
  member?: ServerRef;
  reason?: Reason;
  id?: string;
  steps?: AttemptStep[];
  traceId?: string;
}

let referenceSeed = 1;
function nextReference(): string {
  const hex = (n: number, width: number) =>
    (n >>> 0).toString(16).padStart(width, "0").slice(-width);
  const seed = referenceSeed++;
  const a = Math.imul(seed, 2654435761);
  const b = Math.imul(seed ^ 0x5bd1e995, 1597334677);
  return `${hex(a, 8)}-${hex(b >>> 16, 4)}-4${hex(b, 3)}-a${hex(a >>> 8, 3)}-${hex(a ^ b, 8)}${hex(seed, 4)}`;
}

function row(spec: RowSpec): AttemptDetail {
  const target = spec.member ?? spec.server;
  const service = SERVICE_BY_TARGET[target.id];
  const at = spec.at.toISOString();
  return {
    id: spec.id ?? nextReference(),
    time: at,
    firstStartedAt: (spec.firstAt ?? spec.at).toISOString(),
    count: spec.count ?? 1,
    outcome: spec.outcome,
    step: spec.step,
    reason: spec.reason?.code,
    message: spec.reason?.message,
    source: spec.reason?.source,
    person: spec.person,
    network: network(spec.network),
    app: spec.app,
    server: spec.server,
    member: spec.member,
    service,
    startedAt: at,
    traceId: spec.traceId,
    steps: spec.steps ?? [
      {
        at,
        step: spec.step,
        storedStep: STORED_STEP[spec.step],
        result:
          spec.outcome === "connected"
            ? SUCCESS_RESULT[spec.step]
            : spec.outcome,
        final: spec.outcome !== "in_progress",
        reason: spec.reason?.code,
        message: spec.reason?.message,
        member: spec.member,
        service,
      },
    ],
    context: { retries: 0 },
  };
}

function step(
  when: Date,
  group: StepGroup,
  result: string,
  final: boolean,
  reason?: Reason,
): AttemptStep {
  return {
    at: when.toISOString(),
    step: group,
    storedStep: STORED_STEP[group],
    result,
    final,
    reason: reason?.code,
    message: reason?.message,
  };
}

/** The sample rows the design draws, newest first. */
function curatedRows(): AttemptDetail[] {
  const today = (t: string) => at(0, t);
  return [
    row({
      at: today("11:04:12"),
      firstAt: today("09:48:03"),
      count: 14,
      outcome: "failed",
      step: "service_sign_in",
      person: PEOPLE.priya,
      network: "51ac",
      app: app("chatgpt"),
      server: SERVERS.linear,
      reason: REASONS.unavailable,
    }),
    row({
      at: today("11:03:51"),
      firstAt: today("10:39:20"),
      count: 3,
      outcome: "failed",
      step: "service_sign_in",
      person: PEOPLE.mei,
      network: "0e7b",
      app: app("codex", "0.62.1"),
      server: SERVERS.engTools,
      member: ENG_LINEAR,
      reason: REASONS.serverError,
    }),
    row({
      at: today("11:02:30"),
      outcome: "connected",
      step: "sign_in_renewal",
      person: PEOPLE.sam,
      network: "3f9d",
      app: app("claude_code", "2.1.4"),
      server: SERVERS.github,
    }),
    row({
      at: today("11:01:17"),
      outcome: "failed",
      step: "service_sign_in",
      person: PEOPLE.jonas,
      network: "3f9d",
      app: app("claude_code", "2.1.4"),
      server: SERVERS.engTools,
      member: ENG_LINEAR,
      reason: REASONS.unavailable,
    }),
    row({
      at: today("10:58:02"),
      firstAt: today("08:05:44"),
      count: 12,
      outcome: "failed",
      step: "connect",
      network: "7c1e",
      app: app("claude"),
      server: SERVERS.support,
      reason: REASONS.metadata,
    }),
    row({
      at: today("10:57:44"),
      outcome: "failed",
      step: "service_sign_in",
      person: PEOPLE.omar,
      network: "3f9d",
      app: app("claude_code"),
      server: SERVERS.linear,
      reason: REASONS.signInSystem,
    }),
    row({
      at: today("10:55:19"),
      outcome: "connected",
      step: "listing_tools",
      person: PEOPLE.ana,
      network: "3f9d",
      app: app("cursor"),
      server: SERVERS.notion,
    }),
    row({
      at: today("10:52:08"),
      firstAt: today("10:41:30"),
      count: 4,
      outcome: "failed",
      step: "listing_tools",
      person: PEOPLE.ken,
      network: "51ac",
      app: app("chatgpt"),
      server: SERVERS.wiki,
      reason: REASONS.keyRejected,
    }),
    row({
      at: today("10:49:33"),
      outcome: "declined",
      step: "sign_in",
      person: PEOPLE.rosa,
      network: "3f9d",
      app: app("claude_code"),
      server: SERVERS.github,
      reason: REASONS.canceled,
    }),
    row({
      at: today("10:41:26"),
      firstAt: today("09:02:10"),
      count: 38,
      outcome: "failed",
      step: "sign_in_renewal",
      person: PEOPLE.lena,
      network: "0e7b",
      app: app("cursor", "2.0.1"),
      server: SERVERS.github,
      reason: REASONS.staleGrant,
    }),
    row({
      at: today("10:39:10"),
      outcome: "blocked",
      step: "connect",
      person: PEOPLE.hugo,
      network: "3f9d",
      app: app("claude"),
      server: SERVERS.linear,
      reason: REASONS.noAccess,
    }),
    row({
      at: today("10:33:15"),
      outcome: "failed",
      step: "service_sign_in",
      person: PEOPLE.marcus,
      network: "3f9d",
      app: app("cursor"),
      server: SERVERS.linear,
      reason: REASONS.invalidScope,
      id: "8e51c6d9-27b0-4f5e-a1c3-9d04b2e6f718",
      traceId: "4bf92f3577b34da6a3ce929d0e0e4736",
      steps: [
        step(today("10:33:02"), "connect", "accepted", false),
        step(today("10:33:09"), "sign_in", "signed_in", false),
        step(
          today("10:33:15"),
          "service_sign_in",
          "failed",
          true,
          REASONS.invalidScope,
        ),
      ],
    }),
    row({
      at: today("10:30:02"),
      firstAt: today("09:55:12"),
      count: 9,
      outcome: "failed",
      step: "service_sign_in",
      person: PEOPLE.sofia,
      network: "0e7b",
      app: app("cursor", "2.0.2"),
      server: SERVERS.linear,
      reason: REASONS.serverError,
    }),
    row({
      at: today("10:22:40"),
      firstAt: today("09:51:31"),
      count: 5,
      outcome: "failed",
      step: "service_sign_in",
      person: PEOPLE.omar,
      network: "3f9d",
      app: app("claude_code"),
      server: SERVERS.linear,
      reason: REASONS.unavailable,
    }),
    row({
      at: today("10:18:44"),
      firstAt: today("08:12:09"),
      count: 14,
      outcome: "blocked",
      step: "connect",
      network: "a94b",
      app: UNLISTED,
      server: SERVERS.github,
      reason: REASONS.clientNotAllowed,
    }),
    row({
      at: today("10:12:05"),
      outcome: "didnt_finish",
      step: "sign_in",
      person: PEOPLE.omar,
      network: "3f9d",
      app: app("claude_code"),
      server: SERVERS.github,
    }),
    row({
      at: today("09:58:21"),
      outcome: "connected",
      step: "request",
      person: PEOPLE.ken,
      network: "51ac",
      app: app("chatgpt"),
      server: SERVERS.notion,
    }),
    ...[PEOPLE.ivy, PEOPLE.theo, PEOPLE.amara].map((p, i) =>
      row({
        at: at(-1, `16:${35 + i * 7}:12`),
        firstAt: at(-1, `16:0${5 + i}:40`),
        count: 3 + i,
        outcome: "failed",
        step: "listing_tools",
        person: p,
        network: "51ac",
        app: app("chatgpt"),
        server: SERVERS.wiki,
        reason: REASONS.keyRejected,
      }),
    ),
    ...[PEOPLE.felix, PEOPLE.lars].map((p, i) =>
      row({
        at: at(-1, `18:1${i}:02`),
        firstAt: at(-2, `10:2${i}:15`),
        count: 22 + i * 5,
        outcome: "failed",
        step: "sign_in_renewal",
        person: p,
        network: "0e7b",
        app: app("cursor", "2.0.1"),
        server: SERVERS.github,
        reason: REASONS.staleGrant,
      }),
    ),
    row({
      at: at(-2, "15:42:19"),
      firstAt: at(-2, "15:40:02"),
      count: 2,
      outcome: "blocked",
      step: "connect",
      network: "c210",
      app: app("perplexity"),
      server: SERVERS.notion,
      reason: REASONS.appBlocked,
    }),
    row({
      at: at(-3, "16:38:05"),
      firstAt: at(-3, "09:12:44"),
      count: 41,
      outcome: "failed",
      step: "sign_in_renewal",
      person: PEOPLE.dana,
      network: "3f9d",
      app: app("claude_code", "2.1.4"),
      server: SERVERS.linear,
      reason: REASONS.expired,
      id: "6f1c2a9e-4b7d-4c1e-9a33-0d5e8b7f21c4",
    }),
  ];
}

/** Deterministic PRNG so background traffic is the same on every load. */
function mulberry32(seed: number): () => number {
  let a = seed;
  return () => {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const BACKGROUND_PEOPLE = Object.values(PEOPLE);
const BACKGROUND_SERVERS = [
  SERVERS.linear,
  SERVERS.github,
  SERVERS.notion,
  SERVERS.wiki,
  SERVERS.support,
];
const BACKGROUND_APPS = [
  app("claude_code", "2.1.4"),
  app("claude_code", "2.1.3"),
  app("cursor", "2.0.2"),
  app("chatgpt"),
  app("codex", "0.62.1"),
  app("claude"),
];
const NETWORKS = ["3f9d", "51ac", "0e7b", "c210", "9a04"];

/** Hourly traffic for the retention window, up to 09:40 today. */
function backgroundRows(): AttemptDetail[] {
  const random = mulberry32(17);
  const pick = <T>(items: readonly T[]): T =>
    items[Math.floor(random() * items.length)]!;
  const rows: AttemptDetail[] = [];
  const end = at(0, "09:40").getTime();
  const start = end - RETENTION_DAYS * DAY;
  for (let hour = start; hour < end; hour += HOUR) {
    const date = new Date(hour);
    const weekend = date.getDay() === 0 || date.getDay() === 6;
    const workHours = date.getHours() >= 8 && date.getHours() < 18;
    const perHour = weekend ? 3 : workHours ? 26 : 4;
    const count = Math.round(perHour * (0.6 + random() * 0.8));
    for (let i = 0; i < count; i++) {
      const roll = random();
      const group: StepGroup =
        roll < 0.5
          ? "sign_in_renewal"
          : roll < 0.7
            ? "service_renewal"
            : roll < 0.8
              ? "sign_in"
              : roll < 0.9
                ? "listing_tools"
                : "request";
      const outcomeRoll = random();
      const outcome: Outcome =
        group === "sign_in" && outcomeRoll < 0.12
          ? "didnt_finish"
          : group === "sign_in" && outcomeRoll < 0.15
            ? "declined"
            : "connected";
      rows.push(
        row({
          at: new Date(hour + Math.floor(random() * HOUR)),
          outcome,
          step: group,
          person: pick(BACKGROUND_PEOPLE),
          network: pick(NETWORKS),
          app: pick(BACKGROUND_APPS),
          server: pick(BACKGROUND_SERVERS),
          reason: outcome === "declined" ? REASONS.canceled : undefined,
        }),
      );
    }
  }
  return rows;
}

let allRowsCache: AttemptDetail[] | null = null;
function allRows(): AttemptDetail[] {
  allRowsCache ??= [...curatedRows(), ...backgroundRows()].sort(
    (a, b) => Date.parse(b.time) - Date.parse(a.time),
  );
  return allRowsCache;
}

type FacetKey = keyof AttemptFacets;

interface FacetSpec {
  filter: keyof AttemptFilters;
  values: (r: AttemptDetail) => { value: string; label?: string }[];
}

const FACETS: Record<FacetKey, FacetSpec> = {
  outcomes: { filter: "outcomes", values: (r) => [{ value: r.outcome }] },
  steps: { filter: "steps", values: (r) => [{ value: r.step }] },
  servers: {
    filter: "serverIds",
    values: (r) =>
      r.server ? [{ value: r.server.id, label: r.server.name }] : [],
  },
  members: {
    filter: "memberIds",
    values: (r) =>
      r.member ? [{ value: r.member.id, label: r.member.name }] : [],
  },
  people: {
    filter: "people",
    values: (r) =>
      r.person ? [{ value: r.person.subjectUrn, label: r.person.name }] : [],
  },
  networks: {
    filter: "networks",
    values: (r) =>
      !r.person && r.network
        ? [{ value: r.network.key, label: r.network.label }]
        : [],
  },
  apps: {
    filter: "apps",
    values: (r) => [{ value: r.app.family, label: r.app.name }],
  },
  appVersions: {
    filter: "appVersions",
    values: (r) =>
      r.app.version
        ? [
            {
              value: `${r.app.family}@${r.app.version}`,
              label: `${r.app.name} ${r.app.version}`,
            },
          ]
        : [],
  },
  reasons: {
    filter: "reasons",
    values: (r) => (r.reason ? [{ value: r.reason, label: r.message }] : []),
  },
  services: {
    filter: "services",
    values: (r) => (r.service ? [{ value: r.service }] : []),
  },
};

function matchesFilters(
  r: AttemptDetail,
  filters: AttemptFilters | undefined,
  skip?: FacetKey,
): boolean {
  if (!filters) return true;
  for (const [key, spec] of Object.entries(FACETS) as [FacetKey, FacetSpec][]) {
    if (key === skip) continue;
    const wanted = filters[spec.filter] as string[] | undefined;
    if (!wanted || wanted.length === 0) continue;
    const have = spec.values(r).map((v) => v.value);
    // An unlisted app has family "", which `apps: [""]` selects.
    if (have.length === 0 && !(spec.filter === "apps" && wanted.includes(""))) {
      return false;
    }
    if (!have.some((v) => wanted.includes(v))) return false;
  }
  return true;
}

interface RangeRequest {
  serverId?: string;
  from: string;
  to: string;
  filters?: AttemptFilters;
}

const FIXTURE_SERVER_IDS = new Set<string>(
  [...Object.values(SERVERS), ENG_LINEAR].map((s) => s.id),
);

/** A real server id has no sample attempts; the fixtures stand in Linear's. */
function fixtureServerId(serverId: string): string {
  return FIXTURE_SERVER_IDS.has(serverId) ? serverId : SERVERS.linear.id;
}

function rowsInRange(request: RangeRequest, skip?: FacetKey): AttemptDetail[] {
  const from = Date.parse(request.from);
  const to = Date.parse(request.to);
  return allRows().filter((r) => {
    const t = Date.parse(r.time);
    if (t < from || t >= to) return false;
    if (request.serverId && r.server?.id !== fixtureServerId(request.serverId))
      return false;
    return matchesFilters(r, request.filters, skip);
  });
}

function facetValues(request: RangeRequest, key: FacetKey): FacetValue[] {
  const counts = new Map<string, FacetValue>();
  for (const r of rowsInRange(request, key)) {
    for (const v of FACETS[key].values(r)) {
      const entry = counts.get(v.value) ?? {
        value: v.value,
        label: v.label,
        attempts: 0,
      };
      entry.attempts += r.count;
      counts.set(v.value, entry);
    }
  }
  return [...counts.values()]
    .sort((a, b) => b.attempts - a.attempts)
    .slice(0, 100);
}

const stripEmail = (r: AttemptDetail): AttemptRow => {
  const {
    startedAt: _startedAt,
    userSessionIssuerId: _issuer,
    traceId: _traceId,
    steps: _steps,
    context: _context,
    ...rest
  } = r;
  if (!rest.person) return rest;
  const { email: _email, ...personWithoutEmail } = rest.person;
  return { ...rest, person: personWithoutEmail };
};

/** Dev-only `?fixture=` scenarios, for looking at each state. */
type Scenario = "quiet" | "logging-off" | "error" | "loading" | "empty" | null;
function scenario(): Scenario {
  if (!import.meta.env.DEV || typeof window === "undefined") return null;
  return new URLSearchParams(window.location.search).get("fixture") as Scenario;
}

function respond<T>(value: () => T): Promise<T> {
  const current = scenario();
  if (current === "loading") return new Promise<T>(() => {});
  if (current === "error") {
    return Promise.reject(new Error("Fixture scenario: error"));
  }
  return new Promise((resolve) => {
    setTimeout(() => {
      resolve(value());
    }, 120);
  });
}

const loggingOn = () => scenario() !== "logging-off";
const VIEWER = { viewerScope: "all" as const };

function problems(): SuspectedProblemDetail[] {
  const tenMinutes = (start: Date, counts: number[]) =>
    counts.map((attempts, i) => ({
      start: new Date(start.getTime() + i * 10 * MINUTE).toISOString(),
      attempts,
    }));
  const linear: SuspectedProblemDetail = {
    id: "problem_linear_service",
    headline: "5 people can't connect to the Linear service",
    fact: "Linear is returning errors.",
    since: at(0, "09:48").toISOString(),
    rule: "three_people_same_service",
    filters: {
      from: at(0, "09:48").toISOString(),
      services: ["linear.app"],
      outcomes: ["failed"],
      reasons: [
        REASONS.unavailable.code,
        REASONS.serverError.code,
        REASONS.signInSystem.code,
      ],
    },
    source: "service",
    failedPer10Minutes: tenMinutes(at(0, "09:40"), [1, 6, 4, 5, 5, 3, 2, 4, 3]),
    lastConnectedAt: at(0, "09:41").toISOString(),
    messages: [
      {
        reason: REASONS.unavailable.code,
        message: REASONS.unavailable.message,
        attempts: 20,
      },
      {
        reason: REASONS.serverError.code,
        message: REASONS.serverError.message,
        attempts: 12,
      },
      {
        reason: REASONS.signInSystem.code,
        message: REASONS.signInSystem.message,
        attempts: 1,
      },
    ],
    people: [
      {
        person: PEOPLE.priya,
        app: app("chatgpt"),
        server: SERVERS.linear,
        attempts: 14,
        firstFailedAt: at(0, "09:48:03").toISOString(),
      },
      {
        person: PEOPLE.omar,
        app: app("claude_code"),
        server: SERVERS.linear,
        attempts: 6,
        firstFailedAt: at(0, "09:51:31").toISOString(),
      },
      {
        person: PEOPLE.sofia,
        app: app("cursor", "2.0.2"),
        server: SERVERS.linear,
        attempts: 9,
        firstFailedAt: at(0, "09:55:12").toISOString(),
      },
      {
        person: PEOPLE.mei,
        app: app("codex", "0.62.1"),
        server: SERVERS.engTools,
        attempts: 3,
        firstFailedAt: at(0, "10:39:20").toISOString(),
      },
      {
        person: PEOPLE.jonas,
        app: app("claude_code", "2.1.4"),
        server: SERVERS.engTools,
        attempts: 1,
        firstFailedAt: at(0, "11:01:17").toISOString(),
      },
    ],
    servers: [
      { server: SERVERS.linear, attempts: 29 },
      { server: SERVERS.engTools, attempts: 4 },
    ],
  };
  const wiki: SuspectedProblemDetail = {
    id: "problem_internal_wiki",
    headline: "4 people can't connect to Internal Wiki",
    fact: "The server rejects the saved key.",
    since: at(-1, "16:05").toISOString(),
    rule: "three_people_same_server",
    filters: {
      from: at(-1, "16:05").toISOString(),
      serverIds: [SERVERS.wiki.id],
      outcomes: ["failed"],
      reasons: [REASONS.keyRejected.code],
    },
    source: "service",
    failedPer10Minutes: tenMinutes(at(-1, "16:00"), [2, 3, 1, 0, 2, 4]),
    lastConnectedAt: at(-1, "15:58").toISOString(),
    messages: [
      {
        reason: REASONS.keyRejected.code,
        message: REASONS.keyRejected.message,
        attempts: 16,
      },
    ],
    people: [PEOPLE.ken, PEOPLE.ivy, PEOPLE.theo, PEOPLE.amara].map((p, i) => ({
      person: p,
      app: app("chatgpt"),
      server: SERVERS.wiki,
      attempts: [4, 3, 4, 5][i]!,
      firstFailedAt: (i === 0
        ? at(0, "10:41:30")
        : at(-1, `16:0${4 + i}:40`)
      ).toISOString(),
    })),
    servers: [{ server: SERVERS.wiki, attempts: 16 }],
  };
  const cursor: SuspectedProblemDetail = {
    id: "problem_cursor_github",
    headline: "3 people on Cursor 2.0.1 can't renew their sign-in to GitHub",
    fact: "Other apps renewed on GitHub in this time.",
    since: at(-2, "10:20").toISOString(),
    rule: "three_people_same_app_version",
    filters: {
      from: at(-2, "10:20").toISOString(),
      serverIds: [SERVERS.github.id],
      apps: ["cursor"],
      appVersions: ["cursor@2.0.1"],
      steps: ["sign_in_renewal"],
      outcomes: ["failed"],
      reasons: [REASONS.staleGrant.code],
    },
    source: "app",
    failedPer10Minutes: tenMinutes(at(0, "09:30"), [3, 5, 6, 4, 6, 7, 5, 2]),
    lastConnectedAt: at(-2, "10:12").toISOString(),
    messages: [
      {
        reason: REASONS.staleGrant.code,
        message: REASONS.staleGrant.message,
        attempts: 87,
      },
    ],
    people: [PEOPLE.lena, PEOPLE.felix, PEOPLE.lars].map((p, i) => ({
      person: p,
      app: app("cursor", "2.0.1"),
      server: SERVERS.github,
      attempts: [38, 22, 27][i]!,
      firstFailedAt: (i === 0
        ? at(0, "09:02:10")
        : at(-2, `10:2${i - 1}:15`)
      ).toISOString(),
    })),
    servers: [{ server: SERVERS.github, attempts: 87 }],
  };
  const dana: SuspectedProblemDetail = {
    id: "problem_dana_linear",
    headline: "Dana Reyes can't renew a sign-in to Linear",
    fact: "Sign-in reached its time limit.",
    since: at(-3, "09:12").toISOString(),
    rule: "one_person_24_hours",
    filters: {
      from: at(-3, "00:00").toISOString(),
      people: [PEOPLE.dana.subjectUrn],
      serverIds: [SERVERS.linear.id],
      steps: ["sign_in_renewal"],
      outcomes: ["failed"],
    },
    source: "speakeasy_setting",
    failedPer10Minutes: tenMinutes(at(-3, "16:00"), [5, 7, 6, 8, 5]),
    lastConnectedAt: at(-4, "17:20").toISOString(),
    messages: [
      {
        reason: REASONS.expired.code,
        message: REASONS.expired.message,
        attempts: 41,
      },
    ],
    people: [
      {
        person: PEOPLE.dana,
        app: app("claude_code", "2.1.4"),
        server: SERVERS.linear,
        attempts: 41,
        firstFailedAt: at(-3, "09:12:44").toISOString(),
      },
    ],
    servers: [{ server: SERVERS.linear, attempts: 41 }],
  };
  return [linear, wiki, cursor, dana];
}

function summaryOf(problem: SuspectedProblemDetail): SuspectedProblem {
  const {
    source: _source,
    failedPer10Minutes: _buckets,
    lastConnectedAt: _last,
    messages: _messages,
    people: _people,
    servers: _servers,
    ...summary
  } = problem;
  return { ...summary, people: problem.people.length };
}

export const fixtureConnectionHealthSource: ConnectionHealthSource = {
  listAttempts: (request) =>
    respond(() => {
      if (!loggingOn()) return { loggingEnabled: false, ...VIEWER };
      const rows = scenario() === "empty" ? [] : rowsInRange(request);
      const offset = request.cursor ? Number(request.cursor) : 0;
      const limit = request.limit ?? PAGE_SIZE;
      return {
        loggingEnabled: true,
        ...VIEWER,
        attempts: rows.slice(offset, offset + limit).map(stripEmail),
        nextCursor:
          offset + limit < rows.length ? String(offset + limit) : undefined,
      };
    }),

  getAttemptCounts: (request) =>
    respond(() => {
      if (!loggingOn()) return { loggingEnabled: false, ...VIEWER };
      const empty = scenario() === "empty";
      const facets = Object.fromEntries(
        (Object.keys(FACETS) as FacetKey[]).map((key) => [
          key,
          empty ? [] : facetValues(request, key),
        ]),
      ) as unknown as AttemptFacets;
      return {
        loggingEnabled: true,
        ...VIEWER,
        totalAttempts: empty
          ? 0
          : rowsInRange(request).reduce((sum, r) => sum + r.count, 0),
        facets,
      };
    }),

  getAttempt: ({ reference }) =>
    respond(() => {
      if (!loggingOn()) return { loggingEnabled: false, ...VIEWER };
      const found = allRows().find(
        (r) => r.id.toLowerCase() === reference.trim().toLowerCase(),
      );
      return {
        loggingEnabled: true,
        ...VIEWER,
        found: !!found,
        attempt: found,
      };
    }),

  getSignInSummary: ({ from, to, bucket, serverId }) =>
    respond(() => {
      if (!loggingOn()) return { loggingEnabled: false, ...VIEWER };
      const fromMs = Date.parse(from);
      const toMs = Date.parse(to);
      const size: "hour" | "day" =
        bucket ?? (toMs - fromMs <= 48 * HOUR ? "hour" : "day");
      const step = size === "hour" ? HOUR : DAY;
      const align = (t: number) => {
        const d = new Date(t);
        if (size === "hour") d.setMinutes(0, 0, 0);
        else d.setHours(0, 0, 0, 0);
        return d.getTime();
      };
      const buckets = new Map<
        number,
        {
          signIns: { succeeded: number; didNot: number };
          renewals: { succeeded: number; didNot: number };
        }
      >();
      for (let t = align(fromMs); t < toMs; t += step) {
        buckets.set(t, {
          signIns: { succeeded: 0, didNot: 0 },
          renewals: { succeeded: 0, didNot: 0 },
        });
      }
      for (const r of rowsInRange({ from, to, serverId })) {
        if (r.outcome === "in_progress") continue;
        const kind =
          r.step === "sign_in" || r.step === "service_sign_in"
            ? "signIns"
            : r.step === "sign_in_renewal" || r.step === "service_renewal"
              ? "renewals"
              : null;
        if (!kind) continue;
        const entry = buckets.get(align(Date.parse(r.time)));
        if (!entry) continue;
        if (r.outcome === "connected") entry[kind].succeeded += r.count;
        else entry[kind].didNot += r.count;
      }
      return {
        loggingEnabled: true,
        ...VIEWER,
        bucket: size,
        buckets: [...buckets.entries()].map(([start, b]) => ({
          start: new Date(start).toISOString(),
          ...b,
        })),
      };
    }),

  getSessionActivity: ({ from, to }) =>
    respond(() => {
      const days = Math.max(
        1,
        Math.round((Date.parse(to) - Date.parse(from)) / DAY),
      );
      return {
        days: Array.from({ length: days }, (_, i) => {
          const day = at(i - days + 1, "00:00");
          const trend = 118 + (33 * i) / Math.max(1, days - 1);
          const wobble = i === days - 1 ? 0 : Math.sin(i * 1.7) * 2;
          return {
            day: day.toISOString().slice(0, 10),
            activeSessions: Math.round(trend + wobble),
          };
        }),
        activeNow: 151,
        averageSessionSeconds: 9 * 86_400,
        configuredSessionSeconds: 14 * 86_400,
      };
    }),

  listDeniedClients: ({ from, to }) =>
    respond(() => {
      if (!loggingOn()) return { loggingEnabled: false, ...VIEWER };
      if (scenario() === "quiet") {
        return { loggingEnabled: true, ...VIEWER, clients: [] };
      }
      const byApp = new Map<
        string,
        {
          app: App;
          reason: string;
          message: string;
          attempts: number;
          people: Set<string>;
          networks: Set<string>;
          lastAt: string;
        }
      >();
      for (const r of rowsInRange({ from, to })) {
        if (r.outcome !== "blocked") continue;
        if (!["connect", "sign_in", "sign_in_renewal"].includes(r.step))
          continue;
        if (r.person) continue;
        const key = `${r.app.family}|${r.reason}`;
        const entry = byApp.get(key) ?? {
          app: r.app,
          reason: r.reason ?? "",
          message: r.message ?? "",
          attempts: 0,
          people: new Set<string>(),
          networks: new Set<string>(),
          lastAt: r.time,
        };
        entry.attempts += r.count;
        if (r.network) {
          entry.networks.add(r.network.key);
          // The sample's repeats came from more networks than one row records.
          if (r.count > 10) entry.networks.add("extra-a").add("extra-b");
        }
        byApp.set(key, entry);
      }
      return {
        loggingEnabled: true,
        ...VIEWER,
        clients: [...byApp.values()].map((c) => ({
          ...c,
          people: c.people.size,
          networks: c.networks.size,
        })),
      };
    }),

  listSuspectedProblems: ({ serverId }) =>
    respond(() => {
      if (!loggingOn()) return { loggingEnabled: false };
      if (scenario() === "quiet") return { loggingEnabled: true, problems: [] };
      const list = problems();
      const target = serverId ? fixtureServerId(serverId) : undefined;
      return {
        loggingEnabled: true,
        problems: target
          ? list
              .filter((p) => p.servers.some((s) => s.server.id === target))
              .map((p) => ({
                ...summaryOf(p),
                peopleOnServer: p.people.filter((x) => x.server?.id === target)
                  .length,
              }))
          : list.map(summaryOf),
      };
    }),

  getSuspectedProblem: ({ id }) =>
    respond(() => {
      const problem = problems().find((p) => p.id === id);
      return { loggingEnabled: loggingOn(), found: !!problem, problem };
    }),

  listActiveKillswitches: () =>
    respond<ActiveKillswitch[]>(() =>
      scenario() === "quiet"
        ? []
        : [
            {
              id: "ks_release_bot",
              subject: { kind: "agent", name: "Release Bot" },
              servers: ["GitHub"],
              until: null,
            },
            {
              id: "ks_noor",
              subject: { kind: "person", name: PEOPLE.noor.name! },
              servers: ["Linear"],
              until: at(2, "00:00").toISOString(),
            },
          ],
    ),
};
