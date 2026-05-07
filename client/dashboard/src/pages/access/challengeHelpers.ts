import type { AuthzChallenge } from "@gram/client/models/components/authzchallenge.js";

type Reason = AuthzChallenge["reason"];

export function getInitials(identifier: string): string {
  const name = identifier.split("@")[0] ?? identifier;
  return name.slice(0, 2).toUpperCase();
}

export function reasonLabel(reason: Reason): string {
  switch (reason) {
    case "grant_matched":
      return "Access granted — a matching role permission was found.";
    case "no_grants":
      return "No permissions configured for this identity.";
    case "scope_unsatisfied":
      return "The identity's roles don't include this permission.";
    case "invalid_check":
      return "The authorization check was malformed or invalid.";
    case "rbac_skipped_apikey":
      return "API keys bypass role checks — access was allowed directly.";
    case "dev_override":
      return "Access allowed via a development override.";
  }
}

/**
 * Filter challenges by principal and scope. When a filter is "all", it is
 * not applied — all challenges pass through.
 */
export function scopeChallenges(
  challenges: AuthzChallenge[],
  principalFilter: string,
  scopeFilter: string,
): AuthzChallenge[] {
  let base = challenges;
  if (principalFilter !== "all") {
    base = base.filter(
      (c) => (c.userEmail ?? c.principalUrn) === principalFilter,
    );
  }
  if (scopeFilter !== "all") {
    base = base.filter((c) => c.scope === scopeFilter);
  }
  return base;
}

export type ChallengeCounts = {
  all: number;
  deny: number;
  allow: number;
  resolved: number;
};

/**
 * Compute pill counts from a list of challenges.
 * Resolved challenges are counted separately regardless of outcome.
 */
export function countChallenges(challenges: AuthzChallenge[]): ChallengeCounts {
  const c: ChallengeCounts = {
    all: challenges.length,
    deny: 0,
    allow: 0,
    resolved: 0,
  };
  for (const ch of challenges) {
    if (ch.resolvedAt) {
      c.resolved++;
    } else if (ch.outcome === "deny") {
      c.deny++;
    } else {
      c.allow++;
    }
  }
  return c;
}
