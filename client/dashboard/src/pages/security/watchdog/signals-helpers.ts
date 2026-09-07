import { ACCENT_RED, SEVERITY, TREND } from "@/components/chart/palette";
import type { RiskSignal } from "@gram/client/models/components/risksignal.js";
import type { SeverityRating } from "../risk-utils";

/**
 * Score-number color per severity band, shared by the signal rows and the
 * drawer header. Theme-stable palette values on purpose: the semantic
 * feedback tokens swap foreground/background roles between themes (dark
 * `--warning-foreground` is a near-white wash that reads grey), so classes
 * like text-warning-foreground can't keep medium amber in dark mode.
 * Critical and high share the accent red — the row's left border is what
 * separates those two bands visually.
 */
export const SCORE_TEXT_COLOR: Record<SeverityRating, string> = {
  low: SEVERITY.low,
  medium: SEVERITY.medium,
  high: ACCENT_RED,
  critical: ACCENT_RED,
};

/**
 * Accent color per severity band, matching the mockup's severity-coded table
 * margin: the darker feedback red (TREND.up) for critical, the brand accent
 * red for high, the amber severity tone for medium, and the neutral grey for
 * low. Shared by the row's left border and the toolbar's severity chips.
 */
export const SEVERITY_ACCENT: Record<SeverityRating, string> = {
  critical: TREND.up,
  high: ACCENT_RED,
  medium: SEVERITY.medium,
  low: SEVERITY.low,
};

/** How the signals list is sectioned. Rows keep the server's risk ranking
 * within every section. */
export type SignalGroupMode =
  | "severity"
  | "category"
  | "team"
  | "app"
  | "principal";

/**
 * Group key for signals whose findings carry no team/app attribution (rows
 * written before the attribution columns existed, or users without a
 * directory profile).
 */
export const UNATTRIBUTED_GROUP_KEY = "";

export const SEVERITY_ORDER = ["critical", "high", "medium", "low"] as const;
export type SignalSeverity = (typeof SEVERITY_ORDER)[number];

export const SEVERITY_GROUP_LABEL: Record<SignalSeverity, string> = {
  critical: "Critical",
  high: "High",
  medium: "Medium",
  low: "Low",
};

function mean(values: number[]): number {
  if (values.length === 0) return 0;
  return values.reduce((a, b) => a + b, 0) / values.length;
}

/**
 * Within-window growth in percent: the last third of the window's sparkline
 * buckets against the first third, so the number moves with the drawn line
 * rather than with a previous window the chart never shows. Segment means
 * (mirroring sparkline-math's trendOf) rather than single endpoint buckets,
 * which are too noisy to anchor a percentage. Null when the window opens with
 * no findings — the UI renders "new" instead of a misleading percentage.
 */
export function trendPercent(sparkline: number[]): number | null {
  const n = sparkline.length;
  if (n < 2) return null;
  const seg = Math.max(1, Math.round(n / 3));
  const first = mean(sparkline.slice(0, seg));
  if (first <= 0) return null;
  const last = mean(sparkline.slice(n - seg));
  return ((last - first) / first) * 100;
}

export type SignalGroup = {
  /** severity band, category key, team, or app the section holds. */
  key: string;
  signals: RiskSignal[];
};

/**
 * The team a signal is filed under when grouping by team: the team of its
 * most-affected user that has one (top users arrive sorted by finding count).
 * Empty when no top user carries a team.
 */
function dominantTeam(signal: RiskSignal): string {
  return signal.topUsers.find((user) => user.team)?.team ?? "";
}

/**
 * The app a signal is filed under when grouping by app: the first of its
 * (sorted) observed apps. Empty when the signal's findings carry no app
 * attribution.
 */
function dominantApp(signal: RiskSignal): string {
  return signal.apps[0] ?? "";
}

/**
 * The placeholder email the server emits for findings it cannot attribute to
 * a user. Treated as no attribution here so those signals land in the
 * unattributed bucket rather than under an "Unknown user" heading.
 */
const UNKNOWN_USER_EMAIL = "Unknown user";

/**
 * The principal (user) a signal is filed under when grouping by principal:
 * the most-affected user by finding count. Returns the user's email for
 * display. Empty when no top user exists or the user is unattributed.
 */
function dominantPrincipal(signal: RiskSignal): string {
  const email = signal.topUsers[0]?.email ?? "";
  return email === UNKNOWN_USER_EMAIL ? "" : email;
}

function groupKeyForMode(signal: RiskSignal, mode: SignalGroupMode): string {
  switch (mode) {
    case "severity":
      return signal.severity;
    case "category":
      return signal.category;
    case "team":
      return dominantTeam(signal);
    case "app":
      return dominantApp(signal);
    case "principal":
      return dominantPrincipal(signal);
  }
}

/**
 * Sections an already risk-ranked signal list by the selected mode without
 * reordering within a section. Severity groups follow the fixed band order;
 * the other modes follow first-appearance order (which tracks the risk
 * ranking, so the most severe group leads), with the unattributed bucket
 * moved last.
 */
export function groupSignals(
  signals: RiskSignal[],
  mode: SignalGroupMode,
): SignalGroup[] {
  const byKey = new Map<string, RiskSignal[]>();
  for (const signal of signals) {
    const key = groupKeyForMode(signal, mode);
    const bucket = byKey.get(key);
    if (bucket) {
      bucket.push(signal);
    } else {
      byKey.set(key, [signal]);
    }
  }

  if (mode === "severity") {
    return SEVERITY_ORDER.filter((severity) => byKey.has(severity)).map(
      (severity) => ({ key: severity, signals: byKey.get(severity)! }),
    );
  }
  const groups = [...byKey.entries()].map(([key, grouped]) => ({
    key,
    signals: grouped,
  }));
  return [
    ...groups.filter((group) => group.key !== UNATTRIBUTED_GROUP_KEY),
    ...groups.filter((group) => group.key === UNATTRIBUTED_GROUP_KEY),
  ];
}

/**
 * Applies the severity chip filter. An empty selection means "no filter".
 */
export function filterSignalsBySeverity(
  signals: RiskSignal[],
  severities: string[],
): RiskSignal[] {
  if (severities.length === 0) return signals;
  const allowed = new Set(severities);
  return signals.filter((signal) => allowed.has(signal.severity));
}

/**
 * Applies the category filter (driven by the exposure bar and the toolbar
 * chip). An empty selection means "no filter".
 */
export function filterSignalsByCategory(
  signals: RiskSignal[],
  categories: string[],
): RiskSignal[] {
  if (categories.length === 0) return signals;
  const allowed = new Set(categories);
  return signals.filter((signal) => allowed.has(signal.category));
}

/** Immutably toggles one value in a multi-select filter selection. */
export function toggleFilterValue(
  selection: string[],
  value: string,
): string[] {
  return selection.includes(value)
    ? selection.filter((entry) => entry !== value)
    : [...selection, value];
}
