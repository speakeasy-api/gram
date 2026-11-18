import type { AuthzChallenge } from "@gram/client/models/components/authzchallenge.js";

/**
 * One scope checked on one resource, with how often it was checked and when
 * last. An agent's gateway checks the same server on every call it makes, so
 * the raw rows repeat; a group says the same thing once.
 */
export type CheckGroup = {
  key: string;
  scope: string;
  resourceKind: string;
  resourceId: string;
  /** The project the newest check named, so the resource can be looked up. */
  projectId?: string;
  /**
   * A remote server's tools/list or describe: one filtering pass over its
   * tools, recorded with no scope and no resource.
   */
  toolListFilter: boolean;
  count: number;
  latest: Date;
};

/**
 * Groups checks by scope and resource, newest group first. The count is the
 * number of rows given, which is the latest page rather than the whole period
 * when the period holds more checks than were fetched.
 */
export function groupChecks(checks: readonly AuthzChallenge[]): CheckGroup[] {
  const groups = new Map<string, CheckGroup>();
  for (const check of checks) {
    const resourceKind = check.resourceKind ?? "";
    const resourceId = check.resourceId ?? "";
    const toolListFilter =
      check.operation === "filter" && !check.scope && !resourceId;
    const key = toolListFilter
      ? "tool-list-filter"
      : `${check.scope}|${resourceKind}|${resourceId}`;
    const group = groups.get(key);
    if (!group) {
      groups.set(key, {
        key,
        scope: check.scope,
        resourceKind,
        resourceId,
        projectId: check.projectId,
        toolListFilter,
        count: 1,
        latest: check.timestamp,
      });
      continue;
    }
    group.count += 1;
    if (check.timestamp <= group.latest) continue;
    group.latest = check.timestamp;
    group.projectId = check.projectId ?? group.projectId;
  }
  return [...groups.values()].sort(
    (a, b) => b.latest.getTime() - a.latest.getTime(),
  );
}

/**
 * "Denied 4 times", "Allowed once". A count from a slice cut off at its page
 * size is only the checks fetched, so it reads as a lower bound.
 */
export function checkCountLabel(
  outcome: "allow" | "deny",
  count: number,
  atLeast = false,
): string {
  const verb = outcome === "deny" ? "Denied" : "Allowed";
  const bound = atLeast ? "at least " : "";
  if (count === 1) return `${verb} ${bound}once`;
  return `${verb} ${bound}${count.toLocaleString()} times`;
}

/**
 * The panel footer, counted only from the slices that loaded. A slice that
 * failed says its count is missing rather than counting as none, and the
 * allowed slice that did load still counts.
 */
export function checksFooter(
  failedTotal: number | undefined,
  connectTotal: number | undefined,
  otherTotal: number | undefined,
): string | undefined {
  const missing = [
    failedTotal === undefined && "failed",
    connectTotal === undefined && "MCP server",
    otherTotal === undefined && "other allowed",
  ].filter((label): label is string => !!label);
  const allowed = [connectTotal, otherTotal].filter(
    (total): total is number => total !== undefined,
  );
  const allowedTotal = allowed.reduce((sum, total) => sum + total, 0);
  const counts = [
    failedTotal !== undefined && `${failedTotal.toLocaleString()} failed`,
    allowed.length > 0 && `${allowedTotal.toLocaleString()} allowed`,
  ].filter((part): part is string => !!part);
  if (counts.length === 0) return undefined;
  if (missing.length === 0) {
    if ((failedTotal ?? 0) + allowedTotal === 0) return undefined;
    return `${counts.join(" and ")} checks in this period, grouped by scope and resource.`;
  }
  const what = missing.length === 1 ? "count" : "counts";
  return `${counts.join(" and ")} checks in this period. The ${missing.join(" and ")} ${what} could not be loaded.`;
}
