import type {
  App,
  AttemptRow,
  Outcome,
  ProblemRule,
  Source,
  StepGroup,
  SuspectedProblem,
} from "./types";

export const OUTCOME_LABELS: Record<Outcome, string> = {
  connected: "Connected",
  failed: "Failed",
  blocked: "Blocked",
  declined: "Declined",
  didnt_finish: "Didn't finish",
  in_progress: "In progress",
};

/** An outcome in words; one the dashboard does not know yet reads as itself. */
export function outcomeLabel(outcome: string): string {
  return (
    (OUTCOME_LABELS as Record<string, string | undefined>)[outcome] ??
    outcome.replace(/_/g, " ")
  );
}

export const STEP_LABELS: Record<StepGroup, string> = {
  connect: "Connect",
  sign_in: "Sign-in",
  service_sign_in: "Service sign-in",
  sign_in_renewal: "Sign-in renewal",
  service_renewal: "Service renewal",
  listing_tools: "Listing tools",
  request: "Request",
  other: "Other",
};

const STEP_RESULT_LABELS: Record<string, string> = {
  accepted: "Accepted",
  signed_in: "Signed in",
  renewed: "Renewed",
  refreshed: "Renewed",
  listed: "Listed",
  completed: "Done",
  failed: "Failed",
  blocked: "Blocked",
  declined: "Declined",
  didnt_finish: "Didn't finish",
  in_progress: "In progress",
};

/** A raw step result in words; an unknown one reads as itself. */
export function stepResultLabel(result: string): string {
  const label = STEP_RESULT_LABELS[result] ?? result.replace(/_/g, " ");
  return label.charAt(0).toUpperCase() + label.slice(1);
}

export const SOURCE_LABELS: Record<Source, string> = {
  service: "The service",
  server: "The MCP server",
  app: "The app",
  identity_provider: "The identity provider",
  speakeasy_setting: "A Speakeasy setting",
  speakeasy: "Speakeasy",
};

const UNNAMED_SERVER: Record<string, string> = {
  authorization_server: "Shared sign-in",
  gateway: "Removed gateway",
  toolset: "Removed server",
  mcp: "Removed server",
  none: "",
};

/** "Engineering Tools › Linear" for a gateway member, else the server. */
export function serverLabel(
  row: Pick<AttemptRow, "server" | "member">,
): string {
  const server = row.server
    ? (row.server.name ?? UNNAMED_SERVER[row.server.kind] ?? "Removed server")
    : "";
  if (!row.member) return server;
  const member =
    row.member.name ?? UNNAMED_SERVER[row.member.kind] ?? "Removed server";
  return member ? `${server} › ${member}` : server;
}

export function appLabel(app: App): string {
  return app.version ? `${app.name} ${app.version}` : app.name;
}

const APP_KINDS: Record<string, string> = {
  dcr: "Registered itself",
  first_party: "Speakeasy dashboard",
  unverified_client: "Not checked yet",
  other_metadata_url: "Has its own metadata URL",
  workload: "Workload",
  code_mode: "Code mode link",
  recovery_link: "Link given to an assistant",
  background: "Speakeasy background work",
};

/** What tells two unlisted apps apart: their host or kind, when known. */
export function appKeyLabel(app: App): string | undefined {
  if (!app.appKey || app.appKey === "other") return undefined;
  return APP_KINDS[app.appKey] ?? app.appKey;
}

const UNNAMED_PERSON: Record<string, string> = {
  user: "Unnamed person",
  agent: "Unnamed agent",
  apikey: "API key",
  workload: "Workload",
  anonymous: "No known person",
  other: "Unknown caller",
};

export function personLabel(row: Pick<AttemptRow, "person">): string {
  if (!row.person) return "No known person";
  return (
    row.person.name ??
    row.person.email ??
    UNNAMED_PERSON[row.person.kind] ??
    "Unknown caller"
  );
}

const TIME_SECONDS = new Intl.DateTimeFormat(undefined, {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23",
});
const TIME_MINUTES = new Intl.DateTimeFormat(undefined, {
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
});
const DAY_MONTH = new Intl.DateTimeFormat(undefined, {
  day: "numeric",
  month: "short",
});
const WEEKDAY_DAY_MONTH = new Intl.DateTimeFormat(undefined, {
  weekday: "short",
  day: "numeric",
  month: "short",
});
const FULL = new Intl.DateTimeFormat(undefined, {
  day: "numeric",
  month: "short",
  year: "numeric",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23",
});

export const formatClock = (iso: string): string =>
  TIME_SECONDS.format(new Date(iso));
export const formatClockMinutes = (iso: string): string =>
  TIME_MINUTES.format(new Date(iso));
export const formatFull = (iso: string): string => FULL.format(new Date(iso));

function sameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}

/** "09:48 today", or "16 Nov, 16:05" on another day. */
export function formatSince(iso: string, now: Date): string {
  const date = new Date(iso);
  if (sameDay(date, now)) return `${TIME_MINUTES.format(date)} today`;
  return `${DAY_MONTH.format(date)}, ${TIME_MINUTES.format(date)}`;
}

/** "Today, 10:18", or "15 Nov". */
export function formatLast(iso: string, now: Date): string {
  const date = new Date(iso);
  if (sameDay(date, now)) return `Today, ${TIME_MINUTES.format(date)}`;
  return DAY_MONTH.format(date);
}

export const formatDayMonth = (iso: string): string =>
  DAY_MONTH.format(new Date(iso));

/** The day header over a group of rows: "Today · Tue 17 Nov". */
export function formatDayHeading(iso: string, now: Date): string {
  const date = new Date(iso);
  const label = WEEKDAY_DAY_MONTH.format(date);
  if (sameDay(date, now)) return `Today · ${label}`;
  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  if (sameDay(date, yesterday)) return `Yesterday · ${label}`;
  return label;
}

export function dayKey(iso: string): string {
  const date = new Date(iso);
  return `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}`;
}

/** "1 h 16 min". */
export function formatDuration(ms: number): string {
  const minutes = Math.max(0, Math.round(ms / 60_000));
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);
  if (days > 0) return `${days} d ${hours % 24} h`;
  if (hours > 0) return `${hours} h ${minutes % 60} min`;
  return `${minutes} min`;
}

export const formatCount = (n: number): string => n.toLocaleString();

export function plural(n: number, one: string, many = `${one}s`): string {
  return `${formatCount(n)} ${n === 1 ? one : many}`;
}

export const PROBLEM_RULES: Record<ProblemRule, string> = {
  three_people_same_service:
    "3 or more people got the same kind of failure from one service and have not connected since.",
  three_people_same_server:
    "3 or more people got the same kind of failure on one server and have not connected since.",
  three_people_same_app_version:
    "3 or more people on one app version got the same kind of failure and have not connected since.",
  one_person_24_hours:
    "One person got the same kind of failure for 24 hours and has not connected since.",
};

/** The headline with when it started, as the panel lists it. */
export function problemHeadline(problem: SuspectedProblem, now: Date): string {
  const since =
    problem.rule === "one_person_24_hours"
      ? formatDayMonth(problem.since)
      : formatSince(problem.since, now);
  return `${problem.headline} since ${since}`;
}

/** A session length in its largest whole unit: "9 days", "5 hours". */
export function formatSessionLength(seconds: number): string {
  const hours = Math.floor(seconds / 3_600);
  if (hours >= 24) {
    const days = Math.floor(hours / 24);
    const rest = hours % 24;
    return rest === 0
      ? plural(days, "day")
      : `${plural(days, "day")} ${plural(rest, "hour")}`;
  }
  if (hours >= 1) return plural(hours, "hour");
  return plural(Math.max(1, Math.round(seconds / 60)), "minute");
}

const WEEKDAY_DAY = new Intl.DateTimeFormat(undefined, {
  weekday: "short",
  day: "numeric",
});

/** "Wed 11": the weekday first, whatever the locale's order. */
export function formatWeekdayDay(ms: number): string {
  const parts = WEEKDAY_DAY.formatToParts(ms);
  const weekday = parts.find((p) => p.type === "weekday")?.value ?? "";
  const day = parts.find((p) => p.type === "day")?.value ?? "";
  return `${weekday} ${day}`.trim();
}

/** Where "now" falls: the bucket it is in and how far through it. */
export function nowPosition(
  timestamps: number[],
  bucketMs: number,
  now: number,
): { index: number; fraction: number } | null {
  const index = timestamps.findIndex((t) => now >= t && now < t + bucketMs);
  if (index === -1) return null;
  return { index, fraction: (now - timestamps[index]!) / bucketMs };
}

/** The page's mono font family, for charts drawn on canvas. */
export function monoFont(): string {
  if (typeof document === "undefined") return "monospace";
  const family = getComputedStyle(document.documentElement)
    .getPropertyValue("--font-mono")
    .trim();
  return family || "monospace";
}
