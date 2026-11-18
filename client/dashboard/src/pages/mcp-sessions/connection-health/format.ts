import type {
  App,
  AttemptRow,
  Outcome,
  ProblemRule,
  Source,
  StepGroup,
} from "./types";

export const OUTCOME_LABELS: Record<Outcome, string> = {
  connected: "Connected",
  failed: "Failed",
  blocked: "Blocked",
  declined: "Declined",
  didnt_finish: "Didn't finish",
  in_progress: "In progress",
};

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
  return STEP_RESULT_LABELS[result] ?? result.replace(/_/g, " ");
}

export const SOURCE_LABELS: Record<Source, string> = {
  service: "The service",
  app: "The app",
  identity_provider: "The identity provider",
  speakeasy_setting: "A Speakeasy setting",
  speakeasy: "Speakeasy",
};

/** "Engineering Tools › Linear" for a gateway member, else the server. */
export function serverLabel(
  row: Pick<AttemptRow, "server" | "member">,
): string {
  const server = row.server?.name ?? (row.server ? "Removed server" : "");
  return row.member?.name ? `${server} › ${row.member.name}` : server;
}

export function appLabel(app: App): string {
  return app.version ? `${app.name} ${app.version}` : app.name;
}

export function personLabel(row: Pick<AttemptRow, "person">): string {
  if (!row.person) return "No known person";
  return row.person.name ?? row.person.email ?? "Unnamed person";
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
