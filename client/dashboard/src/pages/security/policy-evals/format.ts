// Shared formatting helpers for the policy-evals surfaces (AGE-2704).
//
// One place for currency / count / duration / latency formatting so the runs
// list, run detail, and the config banner stay consistent. Missing values
// render as an em-dash ("—"); never as a misleading $0.00 or 0.

import { formatDistanceToNow } from "date-fns";
import type { PolicyEvalRun } from "@gram/client/models/components/policyevalrun.js";

const EMPTY = "—";

const usdFormatter = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
});

/** A dollar amount, always "$"-prefixed (not "US$"). Missing -> "—". */
export function formatUsd(n: number | null | undefined): string {
  if (n == null) return EMPTY;
  return usdFormatter.format(n);
}

/** A whole-number count with locale grouping. Missing -> "—". */
export function formatCount(n: number | null | undefined): string {
  if (n == null) return EMPTY;
  return n.toLocaleString();
}

/** A latency in milliseconds. Missing -> "—". */
export function formatMs(n: number | null | undefined): string {
  if (n == null) return EMPTY;
  return `${n.toLocaleString()} ms`;
}

/** A duration between two instants, e.g. "12s" or "3m 4s". Missing -> "—". */
export function formatDuration(
  start: Date | null | undefined,
  end: Date | null | undefined,
): string {
  if (!start || !end) return EMPTY;
  const totalSeconds = Math.max(0, Math.round((+end - +start) / 1000));
  if (totalSeconds < 60) return `${totalSeconds}s`;
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes < 60) {
    return seconds === 0 ? `${minutes}m` : `${minutes}m ${seconds}s`;
  }
  const hours = Math.floor(minutes / 60);
  const remMinutes = minutes % 60;
  return remMinutes === 0 ? `${hours}h` : `${hours}h ${remMinutes}m`;
}

/** A readable run title, e.g. "Eval · 3m ago · policy v4". Falls back to the
 *  creation time alone when the run wasn't pinned to a policy version (draft). */
export function runTitle(run: PolicyEvalRun): string {
  const when = formatDistanceToNow(run.createdAt, { addSuffix: true });
  const version =
    run.riskPolicyVersion != null ? ` · policy v${run.riskPolicyVersion}` : "";
  return `Eval · ${when}${version}`;
}

/** True when a saved run was pinned to a policy version older than the current
 *  one — the config changed since the run, so a re-run is advisable. */
export function isRunStale(
  run: Pick<PolicyEvalRun, "riskPolicyVersion">,
  currentVersion: number | undefined,
): boolean {
  return (
    run.riskPolicyVersion != null &&
    currentVersion != null &&
    run.riskPolicyVersion < currentVersion
  );
}

/** A human description of the sample window, e.g.
 *  "Last 30 days · 5 sessions · max 2,000 messages". */
export function describeSample(sample: PolicyEvalRun["sample"]): string | null {
  if (!sample) return null;
  const parts: string[] = [];
  if (sample.lookbackDays != null) {
    parts.push(`last ${sample.lookbackDays} days`);
  }
  if (sample.lastNSessions != null) {
    parts.push(`${formatCount(sample.lastNSessions)} sessions`);
  }
  parts.push(`max ${formatCount(sample.maxMessages)} messages`);
  return parts.join(" · ");
}
