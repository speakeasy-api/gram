// @vitest-environment node
import { unrecognized } from "@gram/client/types/unrecognized.js";
import {
  canAssignChallengeRole,
  countChallenges,
  hasActiveTemporaryGrant,
  resolutionSummary,
  scopeChallenges,
} from "./challengeHelpers";
import { describe, expect, it } from "vitest";

import type { AuthzChallenge } from "@gram/client/models/components/authzchallenge.js";

describe("canAssignChallengeRole", () => {
  it("allows one concrete user", () => {
    expect(
      canAssignChallengeRole({
        principalType: "user",
        principalUrn: "user:u1",
      }),
    ).toBe(true);
  });

  it.each([
    { principalType: "user" as const, principalUrn: "user:all" },
    { principalType: "api_key" as const, principalUrn: "api_key:key1" },
    { principalType: "assistant" as const, principalUrn: "assistant:a1" },
    { principalType: "agent" as const, principalUrn: "agent:a1" },
  ])("rejects $principalType principal $principalUrn", (principal) => {
    expect(canAssignChallengeRole(principal)).toBe(false);
  });
});

function makeChallenge(
  overrides: Partial<AuthzChallenge> = {},
): AuthzChallenge {
  return {
    id: "ch-1",
    evaluatedGrantCount: 1,
    matchedGrantCount: 0,
    operation: "require",
    organizationId: "org-1",
    outcome: "deny",
    principalType: "user",
    principalUrn: "user:u1",
    reason: "scope_unsatisfied",
    roleSlugs: [],
    scope: "project:read",
    timestamp: new Date("2026-05-01T00:00:00Z"),
    ...overrides,
  };
}

describe("scopeChallenges", () => {
  const challenges = [
    makeChallenge({
      id: "1",
      userEmail: "alice@acme.com",
      scope: "project:read",
    }),
    makeChallenge({
      id: "2",
      userEmail: "bob@acme.com",
      scope: "project:read",
    }),
    makeChallenge({
      id: "3",
      userEmail: "alice@acme.com",
      scope: "org:admin",
    }),
    makeChallenge({
      id: "4",
      userEmail: "bob@acme.com",
      scope: "org:admin",
    }),
  ];

  it("returns all challenges when both filters are 'all'", () => {
    const result = scopeChallenges(challenges, "all", "all");
    expect(result).toHaveLength(4);
  });

  it("filters by principal", () => {
    const result = scopeChallenges(challenges, "alice@acme.com", "all");
    expect(result).toHaveLength(2);
    expect(result.every((c) => c.userEmail === "alice@acme.com")).toBe(true);
  });

  it("filters by scope", () => {
    const result = scopeChallenges(challenges, "all", "org:admin");
    expect(result).toHaveLength(2);
    expect(result.every((c) => c.scope === "org:admin")).toBe(true);
  });

  it("filters by both principal and scope", () => {
    const result = scopeChallenges(challenges, "alice@acme.com", "org:admin");
    expect(result).toHaveLength(1);
    expect(result[0]!.id).toBe("3");
  });

  it("returns empty when no matches", () => {
    const result = scopeChallenges(challenges, "nobody@acme.com", "all");
    expect(result).toHaveLength(0);
  });

  it("falls back to principalUrn when userEmail is undefined", () => {
    const noEmail = [
      makeChallenge({
        id: "5",
        principalUrn: "api_key:k1",
        userEmail: undefined,
      }),
      makeChallenge({
        id: "6",
        principalUrn: "api_key:k2",
        userEmail: undefined,
      }),
    ];
    const result = scopeChallenges(noEmail, "api_key:k1", "all");
    expect(result).toHaveLength(1);
    expect(result[0]!.id).toBe("5");
  });
});

describe("countChallenges", () => {
  it("counts empty list", () => {
    expect(countChallenges([])).toEqual({
      all: 0,
      deny: 0,
      allow: 0,
      resolved: 0,
    });
  });

  it("counts denied challenges", () => {
    const challenges = [
      makeChallenge({ outcome: "deny" }),
      makeChallenge({ outcome: "deny" }),
    ];
    expect(countChallenges(challenges)).toEqual({
      all: 2,
      deny: 2,
      allow: 0,
      resolved: 0,
    });
  });

  it("counts allowed challenges", () => {
    const challenges = [
      makeChallenge({ outcome: "allow" }),
      makeChallenge({ outcome: "allow" }),
    ];
    expect(countChallenges(challenges)).toEqual({
      all: 2,
      deny: 0,
      allow: 2,
      resolved: 0,
    });
  });

  it("counts resolved challenges regardless of outcome", () => {
    const challenges = [
      makeChallenge({
        outcome: "deny",
        resolvedAt: new Date("2026-05-02T00:00:00Z"),
      }),
      makeChallenge({
        outcome: "allow",
        resolvedAt: new Date("2026-05-02T00:00:00Z"),
      }),
    ];
    expect(countChallenges(challenges)).toEqual({
      all: 2,
      deny: 0,
      allow: 0,
      resolved: 2,
    });
  });

  it("does not count an error outcome as allowed", () => {
    const challenges = [makeChallenge({ outcome: "error" })];
    expect(countChallenges(challenges)).toEqual({
      all: 1,
      deny: 0,
      allow: 0,
      resolved: 0,
    });
  });

  it("handles mixed outcomes", () => {
    const challenges = [
      makeChallenge({ id: "1", outcome: "deny" }),
      makeChallenge({ id: "2", outcome: "allow" }),
      makeChallenge({ id: "3", outcome: "deny", resolvedAt: new Date() }),
      makeChallenge({ id: "4", outcome: "error" }),
      makeChallenge({ id: "5", outcome: "deny" }),
    ];
    expect(countChallenges(challenges)).toEqual({
      all: 5,
      deny: 2,
      allow: 1,
      resolved: 1,
    });
  });
});

it("never counts unknown or error challenge outcomes as allowed", () => {
  const challenges = [
    makeChallenge({ outcome: unrecognized("future_outcome") }),
    makeChallenge({ outcome: "error" }),
    makeChallenge({ outcome: "deny" }),
    makeChallenge({ outcome: "allow" }),
  ];
  expect(countChallenges(challenges)).toEqual({
    all: 4,
    allow: 1,
    deny: 1,
    resolved: 0,
  });
});

describe("resolutionSummary", () => {
  const now = new Date("2026-11-12T12:00:00Z");
  const later = new Date("2026-11-12T14:00:00Z");
  const earlier = new Date("2026-11-12T11:00:00Z");
  const format = (d: Date) => d.toISOString();
  const resolved = {
    resolvedAt: earlier,
    resolutionRoleSlug: undefined,
    resolutionTemporaryGrant: undefined,
  };

  it("is null for an unresolved row", () => {
    expect(
      resolutionSummary(
        { ...resolved, resolvedAt: undefined, resolutionType: undefined },
        now,
        format,
      ),
    ).toBeNull();
  });

  it("names the role assigned, or a dismissal", () => {
    expect(
      resolutionSummary(
        {
          ...resolved,
          resolutionType: "role_assigned",
          resolutionRoleSlug: "auditor",
        },
        now,
        format,
      ),
    ).toBe("Role assigned: auditor");
    expect(
      resolutionSummary(
        { ...resolved, resolutionType: "dismissed" },
        now,
        format,
      ),
    ).toBe("Dismissed");
  });

  it("says when temporary access ends, or that it ended", () => {
    const withGrant = (
      status: "active" | "expired" | "revoked",
      expiresAt?: Date,
    ) => ({
      ...resolved,
      resolutionType: "temporary_grant" as const,
      resolutionTemporaryGrant: { id: "g", status, expiresAt },
    });
    expect(resolutionSummary(withGrant("active", later), now, format)).toBe(
      `Temporary access · ends ${later.toISOString()}`,
    );
    expect(resolutionSummary(withGrant("expired", earlier), now, format)).toBe(
      "Temporary access, expired",
    );
    expect(resolutionSummary(withGrant("revoked"), now, format)).toBe(
      "Temporary access, revoked",
    );
    // Fetched as active, but the deadline has passed since.
    expect(resolutionSummary(withGrant("active", earlier), now, format)).toBe(
      "Temporary access, expired",
    );
  });
});

describe("hasActiveTemporaryGrant", () => {
  const now = new Date("2026-11-12T12:00:00Z");
  const row = (status: "active" | "expired" | "revoked", expiresAt?: Date) => ({
    resolutionType: "temporary_grant" as const,
    resolutionTemporaryGrant: { id: "g", status, expiresAt },
  });

  it("holds only until the deadline", () => {
    expect(
      hasActiveTemporaryGrant(
        row("active", new Date("2026-11-12T13:00:00Z")),
        now,
      ),
    ).toBe(true);
    expect(
      hasActiveTemporaryGrant(
        row("active", new Date("2026-11-12T11:59:00Z")),
        now,
      ),
    ).toBe(false);
    expect(hasActiveTemporaryGrant(row("revoked"), now)).toBe(false);
    expect(
      hasActiveTemporaryGrant(
        { resolutionType: "dismissed", resolutionTemporaryGrant: undefined },
        now,
      ),
    ).toBe(false);
  });
});
