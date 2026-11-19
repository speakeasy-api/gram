// FIXTURE DATA (made-up names): suspected problems have no detector or API yet.
// Replace with listSuspectedProblems / getSuspectedProblem when they ship.

import type {
  App,
  GetSuspectedProblemResponse,
  ListSuspectedProblemsResponse,
  Person,
  ServerRef,
  Source,
  SuspectedProblem,
  SuspectedProblemDetail,
} from "./types";

const MINUTE = 60_000;

/** The fixtures' clock: today at 11:05 local, so the sample day reads as today. */
function fixtureNow(): Date {
  const now = new Date();
  const today = new Date(
    now.getFullYear(),
    now.getMonth(),
    now.getDate(),
    11,
    5,
  );
  // Before 11:05 the sample day is yesterday, so no sample time is in the future.
  if (today > now) today.setDate(today.getDate() - 1);
  return today;
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
    code: "static_key_rejected",
    message: "The server rejected the saved key.",
    source: "server",
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
    peopleCount: 5,
    rule: "three_people_same_service",
    lastFailedAt: at(0, "11:04:12").toISOString(),
    scope: { kind: "service", id: "linear.app", name: "Linear service" },
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
    otherFailuresOnService: 1,
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
        member: ENG_LINEAR,
        attempts: 3,
        firstFailedAt: at(0, "10:39:20").toISOString(),
      },
      {
        person: PEOPLE.jonas,
        app: app("claude_code", "2.1.4"),
        server: SERVERS.engTools,
        member: ENG_LINEAR,
        attempts: 1,
        firstFailedAt: at(0, "11:01:17").toISOString(),
      },
    ],
    servers: [
      { server: SERVERS.linear, attempts: 29 },
      { server: SERVERS.engTools, member: ENG_LINEAR, attempts: 4 },
    ],
  };
  const wiki: SuspectedProblemDetail = {
    id: "problem_internal_wiki",
    headline: "4 people can't connect to Internal Wiki",
    fact: "The server rejects the saved key.",
    since: at(-1, "16:05").toISOString(),
    peopleCount: 4,
    rule: "three_people_same_server",
    lastFailedAt: at(0, "10:52:08").toISOString(),
    scope: { kind: "server", id: SERVERS.wiki.id, name: "Internal Wiki" },
    filters: {
      from: at(-1, "16:05").toISOString(),
      serverIds: [SERVERS.wiki.id],
      outcomes: ["failed"],
      reasons: [REASONS.keyRejected.code],
    },
    source: "server",
    failedPer10Minutes: tenMinutes(at(-1, "16:00"), [2, 3, 1, 0, 2, 4]),
    otherFailuresOnService: 0,
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
    peopleCount: 3,
    rule: "three_people_same_app_version",
    lastFailedAt: at(0, "10:41:26").toISOString(),
    scope: { kind: "app_version", id: "cursor@2.0.1", name: "Cursor 2.0.1" },
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
    otherFailuresOnService: 0,
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
    peopleCount: 1,
    rule: "one_person_24_hours",
    lastFailedAt: at(-3, "16:38:05").toISOString(),
    scope: { kind: "person", id: PEOPLE.dana.subjectUrn, name: "Dana Reyes" },
    filters: {
      from: at(-3, "00:00").toISOString(),
      people: [PEOPLE.dana.subjectUrn],
      serverIds: [SERVERS.linear.id],
      steps: ["sign_in_renewal"],
      outcomes: ["failed"],
    },
    source: "speakeasy_setting",
    failedPer10Minutes: tenMinutes(at(-3, "16:00"), [5, 7, 6, 8, 5]),
    otherFailuresOnService: 0,
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
    otherFailuresOnService: _other,
    lastConnectedAt: _last,
    messages: _messages,
    people: _people,
    servers: _servers,
    ...summary
  } = problem;
  return { ...summary, peopleCount: problem.people.length };
}

/** Dev-only `?fixture=quiet`: no suspected problems, as on a quiet day. */
function quiet(): boolean {
  if (!import.meta.env.DEV || typeof window === "undefined") return false;
  return new URLSearchParams(window.location.search).get("fixture") === "quiet";
}

function later<T>(value: () => T): Promise<T> {
  return new Promise((resolve) => {
    setTimeout(() => {
      resolve(value());
    }, 120);
  });
}

export function fixtureListSuspectedProblems(_: {
  userSessionIssuerId: string;
  serverId?: string;
}): Promise<ListSuspectedProblemsResponse> {
  return later(() => {
    if (quiet()) return { loggingEnabled: true, problems: [] };
    const list = problems();
    return {
      loggingEnabled: true,
      // Sample problems name made-up servers, so no per-server share is shown.
      problems: list.map(summaryOf),
    };
  });
}

export function fixtureGetSuspectedProblem({
  id,
}: {
  id: string;
}): Promise<GetSuspectedProblemResponse> {
  return later(() => {
    const problem = problems().find((p) => p.id === id);
    return { loggingEnabled: true, found: !!problem, problem };
  });
}
