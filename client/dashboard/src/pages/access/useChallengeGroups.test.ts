import { describe, expect, it } from "vitest";
import type { AuthzChallenge } from "@gram/client/models/components/authzchallenge.js";
import { challengeGroupKey, groupChallenges } from "./useChallengeGroups";

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

describe("challengeGroupKey", () => {
  it("uses userEmail when available", () => {
    const c = makeChallenge({ userEmail: "alice@acme.com" });
    expect(challengeGroupKey(c)).toBe("alice@acme.com|project:read|deny||");
  });

  it("falls back to principalUrn when no userEmail", () => {
    const c = makeChallenge({ userEmail: undefined });
    expect(challengeGroupKey(c)).toBe("user:u1|project:read|deny||");
  });

  it("includes resourceKind and resourceId", () => {
    const c = makeChallenge({
      userEmail: "alice@acme.com",
      resourceKind: "project",
      resourceId: "proj-1",
    });
    expect(challengeGroupKey(c)).toBe(
      "alice@acme.com|project:read|deny|project|proj-1",
    );
  });

  it("produces different keys for different outcomes", () => {
    const deny = makeChallenge({ userEmail: "a@b.com", outcome: "deny" });
    const allow = makeChallenge({ userEmail: "a@b.com", outcome: "allow" });
    expect(challengeGroupKey(deny)).not.toBe(challengeGroupKey(allow));
  });

  it("produces different keys for different scopes", () => {
    const a = makeChallenge({ userEmail: "a@b.com", scope: "org:read" });
    const b = makeChallenge({ userEmail: "a@b.com", scope: "org:admin" });
    expect(challengeGroupKey(a)).not.toBe(challengeGroupKey(b));
  });
});

describe("groupChallenges", () => {
  it("returns empty results for empty input", () => {
    const result = groupChallenges([]);
    expect(result.grouped).toHaveLength(0);
    expect(result.groupCounts.size).toBe(0);
    expect(result.groupKeys.size).toBe(0);
    expect(result.siblingIds.size).toBe(0);
  });

  it("returns single challenge ungrouped", () => {
    const c = makeChallenge({ id: "1", userEmail: "a@b.com" });
    const result = groupChallenges([c]);
    expect(result.grouped).toEqual([c]);
    expect(result.groupCounts.get("1")).toBe(1);
  });

  it("collapses identical-key challenges into one row", () => {
    const challenges = [
      makeChallenge({ id: "1", userEmail: "a@b.com", scope: "org:read" }),
      makeChallenge({ id: "2", userEmail: "a@b.com", scope: "org:read" }),
      makeChallenge({ id: "3", userEmail: "a@b.com", scope: "org:read" }),
    ];
    const result = groupChallenges(challenges);
    expect(result.grouped).toHaveLength(1);
    expect(result.grouped[0].id).toBe("1");
    expect(result.groupCounts.get("1")).toBe(3);
  });

  it("keeps different-key challenges as separate rows", () => {
    const challenges = [
      makeChallenge({ id: "1", userEmail: "a@b.com", scope: "org:read" }),
      makeChallenge({ id: "2", userEmail: "b@b.com", scope: "org:read" }),
    ];
    const result = groupChallenges(challenges);
    expect(result.grouped).toHaveLength(2);
  });

  it("tracks groupKeys for every member", () => {
    const challenges = [
      makeChallenge({ id: "1", userEmail: "a@b.com", scope: "org:read" }),
      makeChallenge({ id: "2", userEmail: "a@b.com", scope: "org:read" }),
    ];
    const result = groupChallenges(challenges);
    const key1 = result.groupKeys.get("1");
    const key2 = result.groupKeys.get("2");
    expect(key1).toBeDefined();
    expect(key1).toBe(key2);
  });

  it("populates siblingIds for all members in a group", () => {
    const challenges = [
      makeChallenge({ id: "1", userEmail: "a@b.com", scope: "org:read" }),
      makeChallenge({ id: "2", userEmail: "a@b.com", scope: "org:read" }),
      makeChallenge({ id: "3", userEmail: "a@b.com", scope: "org:read" }),
    ];
    const result = groupChallenges(challenges);
    expect(result.siblingIds.get("1")).toEqual(["1", "2", "3"]);
    expect(result.siblingIds.get("2")).toEqual(["1", "2", "3"]);
    expect(result.siblingIds.get("3")).toEqual(["1", "2", "3"]);
  });

  it("expands a group when its key is in expandedGroups", () => {
    const challenges = [
      makeChallenge({ id: "1", userEmail: "a@b.com", scope: "org:read" }),
      makeChallenge({ id: "2", userEmail: "a@b.com", scope: "org:read" }),
      makeChallenge({ id: "3", userEmail: "a@b.com", scope: "org:read" }),
    ];
    const groupKey = challengeGroupKey(challenges[0]);
    const expanded = new Set([groupKey]);
    const result = groupChallenges(challenges, expanded);
    expect(result.grouped).toHaveLength(3);
    expect(result.grouped.map((c) => c.id)).toEqual(["1", "2", "3"]);
  });

  it("only expands matching groups, collapses others", () => {
    const challenges = [
      makeChallenge({ id: "1", userEmail: "a@b.com", scope: "org:read" }),
      makeChallenge({ id: "2", userEmail: "a@b.com", scope: "org:read" }),
      makeChallenge({
        id: "3",
        userEmail: "b@b.com",
        scope: "org:read",
        outcome: "deny",
      }),
      makeChallenge({
        id: "4",
        userEmail: "b@b.com",
        scope: "org:read",
        outcome: "deny",
      }),
    ];
    const expandKey = challengeGroupKey(challenges[0]);
    const expanded = new Set([expandKey]);
    const result = groupChallenges(challenges, expanded);
    // Group A (a@b.com) expanded → 2 rows, Group B (b@b.com) collapsed → 1 row
    expect(result.grouped).toHaveLength(3);
    expect(result.grouped.map((c) => c.id)).toEqual(["1", "2", "3"]);
  });

  it("groups by resource fields", () => {
    const challenges = [
      makeChallenge({
        id: "1",
        userEmail: "a@b.com",
        resourceKind: "project",
        resourceId: "p1",
      }),
      makeChallenge({
        id: "2",
        userEmail: "a@b.com",
        resourceKind: "project",
        resourceId: "p2",
      }),
    ];
    const result = groupChallenges(challenges);
    // Different resourceId → different groups
    expect(result.grouped).toHaveLength(2);
  });

  it("count is only set on first member of each group", () => {
    const challenges = [
      makeChallenge({ id: "1", userEmail: "a@b.com", scope: "org:read" }),
      makeChallenge({ id: "2", userEmail: "a@b.com", scope: "org:read" }),
    ];
    const result = groupChallenges(challenges);
    expect(result.groupCounts.get("1")).toBe(2);
    expect(result.groupCounts.has("2")).toBe(false);
  });
});
