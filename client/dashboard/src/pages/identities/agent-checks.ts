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
    const key = `${check.scope}|${resourceKind}|${resourceId}`;
    const group = groups.get(key);
    if (!group) {
      groups.set(key, {
        key,
        scope: check.scope,
        resourceKind,
        resourceId,
        count: 1,
        latest: check.timestamp,
      });
      continue;
    }
    group.count += 1;
    if (check.timestamp > group.latest) group.latest = check.timestamp;
  }
  return [...groups.values()].sort(
    (a, b) => b.latest.getTime() - a.latest.getTime(),
  );
}

/**
 * The resource a check was made on, in a form a reader can match against the
 * Roles & Permissions page: its kind and id. The id stays whole: ids minted
 * close together share their first characters, so a shortened id can name two
 * resources the same.
 */
export function checkResourceLabel(group: CheckGroup): string {
  if (!group.resourceId) return group.resourceKind || "No resource";
  if (!group.resourceKind) return group.resourceId;
  return `${group.resourceKind} ${group.resourceId}`;
}

/** "Denied 4 times", "Allowed once". */
export function checkCountLabel(outcome: "allow" | "deny", count: number) {
  const verb = outcome === "deny" ? "Denied" : "Allowed";
  if (count === 1) return `${verb} once`;
  return `${verb} ${count.toLocaleString()} times`;
}
