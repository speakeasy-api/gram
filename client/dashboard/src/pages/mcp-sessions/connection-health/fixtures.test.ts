// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  fixtureConnectionHealthSource as source,
  fixtureNow,
} from "./fixtures";

const providerId = "provider";

function window(hours: number) {
  const now = fixtureNow();
  return {
    from: new Date(now.getTime() - hours * 3_600_000).toISOString(),
    to: now.toISOString(),
  };
}

describe("fixture connection health source", () => {
  it("lists the problem's attempts with the filters it hands to Connection Logs", async () => {
    const problems = await source.listSuspectedProblems({
      userSessionIssuerId: providerId,
    });
    const linear = problems.problems!.find(
      (p) => p.id === "problem_linear_service",
    )!;
    const { from, ...filters } = linear.filters;
    const counts = await source.getAttemptCounts({
      userSessionIssuerId: providerId,
      from,
      to: fixtureNow().toISOString(),
      filters,
    });
    // The detail sheet's 33 failed attempts are the rows Connection Logs lists.
    expect(counts.totalAttempts).toBe(33);
  });

  it("finds a folded row by reference outside the time range", async () => {
    const found = await source.getAttempt({
      reference: "6f1c2a9e-4b7d-4c1e-9a33-0d5e8b7f21c4",
    });
    expect(found.found).toBe(true);
    expect(found.attempt?.count).toBe(41);
  });

  it("counts each facet under every filter but its own", async () => {
    const counts = await source.getAttemptCounts({
      userSessionIssuerId: providerId,
      ...window(24),
      filters: { outcomes: ["failed"] },
    });
    const outcomes = counts.facets!.outcomes.map((f) => f.value);
    expect(outcomes).toContain("connected");
    expect(outcomes).toContain("failed");
  });

  it("pages the list with an opaque cursor", async () => {
    const first = await source.listAttempts({
      userSessionIssuerId: providerId,
      ...window(24 * 7),
      limit: 10,
    });
    expect(first.attempts).toHaveLength(10);
    expect(first.nextCursor).toBeDefined();
    const second = await source.listAttempts({
      userSessionIssuerId: providerId,
      ...window(24 * 7),
      limit: 10,
      cursor: first.nextCursor,
    });
    expect(second.attempts![0]!.id).not.toBe(first.attempts![0]!.id);
    // Lists never carry email; only getAttempt does.
    expect(first.attempts!.every((a) => !a.person?.email)).toBe(true);
  });
});
