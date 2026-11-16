import type { AuthzChallenge } from "@gram/client/models/components/authzchallenge.js";
import type { TemporaryGrant } from "@gram/client/models/components/temporarygrant.js";
import { invalidateAllChallengeBuckets } from "@gram/client/react-query/challengeBuckets.js";
import { invalidateAllChallenges } from "@gram/client/react-query/challenges.js";
import { invalidateAllGrants } from "@gram/client/react-query/grants.js";
import { invalidateAllIdentityAccess } from "@gram/client/react-query/identityAccess.js";
import { invalidateAllTemporaryGrants } from "@gram/client/react-query/temporaryGrants.js";
import type { QueryClient } from "@tanstack/react-query";

/** The durations an administrator can give themselves access for. */
export type TemporaryAccessDuration = "30m" | "2h" | "4h" | "eod" | "24h";

export const DEFAULT_TEMPORARY_ACCESS_DURATION: TemporaryAccessDuration = "2h";

export const DURATION_LABELS: Record<TemporaryAccessDuration, string> = {
  "30m": "30 min",
  "2h": "2 hours",
  "4h": "4 hours",
  eod: "End of day",
  "24h": "24 hours",
};

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
// The server refuses a deadline under five minutes or over 24 hours away; the
// margins keep a choice made now valid by the time the request lands.
const MIN_LEAD = 10 * MINUTE;
const MAX_LEAD = 24 * HOUR - MINUTE;
// Recent denials worth resolving together with the grant. Older ones have long
// stopped meaning someone is waiting on this access.
export const TEMPORARY_ACCESS_CHALLENGE_LOOKBACK_MS = 24 * HOUR;

/**
 * When a grant for `duration` chosen at `now` ends, or null when the choice
 * is not available (end of day is too close). End of day is the last minute of
 * the local day, capped at the 24-hour maximum.
 */
export function temporaryAccessDeadline(
  duration: TemporaryAccessDuration,
  now: Date,
): Date | null {
  const at = (ms: number) => new Date(now.getTime() + ms);
  switch (duration) {
    case "30m":
      return at(30 * MINUTE);
    case "2h":
      return at(2 * HOUR);
    case "4h":
      return at(4 * HOUR);
    case "24h":
      return at(MAX_LEAD);
    case "eod": {
      const endOfDay = new Date(now);
      endOfDay.setHours(23, 59, 0, 0);
      if (endOfDay.getTime() - now.getTime() < MIN_LEAD) return null;
      return endOfDay;
    }
  }
}

/** "16:42" today, otherwise "Fri 16:42". */
export function formatDeadline(deadline: Date, now: Date): string {
  const time = deadline.toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
  });
  if (deadline.toDateString() === now.toDateString()) return `${time} today`;
  const day = deadline.toLocaleDateString(undefined, { weekday: "short" });
  return `${day} ${time}`;
}

/**
 * The denied challenges a self-grant answers: this person's unresolved
 * denials of the scope, on a resource the grant covers. Rows without a
 * recorded selector cannot be resolved by a grant, so they are left out
 * rather than failing the whole request.
 */
export function challengesAnsweredBy(
  challenges: readonly AuthzChallenge[],
  grant: {
    principalUrn: string;
    scope: string;
    resourceKind: string;
    resourceId: string;
  },
): string[] {
  return challenges
    .filter(
      (c) =>
        c.outcome === "deny" &&
        !c.resolutionType &&
        c.principalUrn === grant.principalUrn &&
        c.scope === grant.scope &&
        c.resourceKind === grant.resourceKind &&
        c.selector !== undefined &&
        Object.keys(c.selector).length > 0 &&
        (grant.resourceId === "*" || c.resourceId === grant.resourceId),
    )
    .map((c) => c.id);
}

/** Whether a temporary grant gives the scope on this resource. */
export function temporaryGrantCovers(
  grant: TemporaryGrant,
  scope: string,
  resourceId: string | undefined,
): boolean {
  if (grant.status !== "active" || grant.scope !== scope) return false;
  const granted = grant.selector.resourceId;
  return granted === "*" || granted === (resourceId ?? "*");
}

/**
 * Refreshes everything that reads the viewer's access after a temporary grant
 * starts, ends or is revoked: their grants (every permission gate), their
 * temporary grants, identity access views, and the challenges a grant resolves.
 */
export function invalidateTemporaryAccess(queryClient: QueryClient): void {
  void invalidateAllGrants(queryClient);
  void invalidateAllTemporaryGrants(queryClient);
  void invalidateAllIdentityAccess(queryClient);
  void invalidateAllChallenges(queryClient);
  void invalidateAllChallengeBuckets(queryClient);
}

/** "1 h 12 min", "45 min", "under a minute" until `deadline`. */
export function formatTimeLeft(deadline: Date, now: Date): string {
  const minutes = Math.floor((deadline.getTime() - now.getTime()) / MINUTE);
  if (minutes < 1) return "under a minute";
  if (minutes < 60) return `${minutes} min`;
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  return rest === 0 ? `${hours} h` : `${hours} h ${rest} min`;
}

/**
 * What a temporary grant reaches, in words: every resource of its kind, or one
 * resource named by a short id ("Session 0f1e2d3c").
 */
export function temporaryGrantTarget(grant: TemporaryGrant): string {
  const { resourceKind, resourceId } = grant.selector;
  const noun = RESOURCE_NOUNS[resourceKind] ?? {
    one: "Resource",
    all: "All resources",
  };
  if (resourceId === "*") return noun.all;
  return `${noun.one} ${resourceId.slice(0, 8)}`;
}

const RESOURCE_NOUNS: Record<string, { one: string; all: string }> = {
  chat: { one: "Session", all: "All sessions" },
  project: { one: "Project", all: "All projects" },
  mcp: { one: "Server", all: "All servers" },
  environment: { one: "Environment", all: "All environments" },
  skill: { one: "Skill", all: "All skills" },
  assistant: { one: "Assistant", all: "All assistants" },
  agent: { one: "Agent", all: "All agents" },
  workload: { one: "Workload", all: "All workloads" },
  org: { one: "Organization", all: "Organization-wide" },
};

/** Active grants, soonest deadline first, as the panel lists them. */
export function activeTemporaryGrants(
  grants: readonly TemporaryGrant[],
  now: Date,
): TemporaryGrant[] {
  return grants
    .filter((g) => g.status === "active" && g.expiresAt > now)
    .sort((a, b) => a.expiresAt.getTime() - b.expiresAt.getTime());
}
