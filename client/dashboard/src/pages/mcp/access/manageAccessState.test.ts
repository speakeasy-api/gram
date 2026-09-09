import { describe, expect, it } from "vitest";
import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import {
  pageCount,
  pageOf,
  withAdded,
  withLevel,
  withoutRules,
  ruleId,
} from "./manageAccessState";

function entry(
  overrides: Partial<ResourceAudienceEntry> & { principalUrn: string },
): ResourceAudienceEntry {
  return {
    kind: "user",
    displayName: "Someone",
    level: "use",
    appliesTo: "resource",
    ...overrides,
  } as ResourceAudienceEntry;
}

describe("paging", () => {
  const rows = Array.from({ length: 23 }, (_, i) => i);

  it("counts pages and slices them", () => {
    expect(pageCount(23)).toBe(3);
    expect(pageOf(rows, 0)).toHaveLength(10);
    expect(pageOf(rows, 2)).toEqual([20, 21, 22]);
  });

  it("clamps a page that no longer exists rather than blanking", () => {
    expect(pageOf(rows, 99)).toEqual([20, 21, 22]);
    expect(pageOf([], 3)).toEqual([]);
    expect(pageCount(0)).toBe(1);
  });
});

describe("audience edits", () => {
  const rows = [
    entry({ principalUrn: "user:1", level: "use" }),
    entry({ principalUrn: "user:2", level: "manage" }),
  ];

  it("changes one level and keeps the rest of the list intact", () => {
    expect(withLevel(rows, "user:1::use", "manage")).toEqual([
      { principalUrn: "user:1", level: "manage" },
      { principalUrn: "user:2", level: "manage" },
    ]);
  });

  it("adds a principal that has no rule yet, which is how a block on an inherited rule is written", () => {
    expect(withLevel(rows, "role:global:9::use", "blocked")).toEqual([
      { principalUrn: "user:1", level: "use" },
      { principalUrn: "user:2", level: "manage" },
      { principalUrn: "role:global:9", level: "blocked" },
    ]);
  });

  it("keeps a principal's other level when one of its rules changes", () => {
    // "manage the server" and "connect to two tools" are separate rules, so
    // editing one must not rewrite the other.
    const mixed = [
      entry({ principalUrn: "user:1", level: "manage" }),
      entry({ principalUrn: "user:1", level: "use", tools: ["search"] }),
    ];
    expect(
      withLevel(
        mixed,
        ruleId({ principalUrn: "user:1", level: "use" }),
        "view",
      ),
    ).toEqual([
      { principalUrn: "user:1", level: "manage" },
      { principalUrn: "user:1", level: "view", tools: ["search"] },
    ]);
  });

  it("writes to the right principal when its URN contains a delimiter", () => {
    // The id is principal + level, and a principal URN carries colons of its
    // own, so the level is read from the last delimiter rather than the first.
    expect(
      withLevel(
        [],
        ruleId({ principalUrn: "role:global:9", level: "use" }),
        "blocked",
      ),
    ).toEqual([{ principalUrn: "role:global:9", level: "blocked" }]);
  });

  it("removes rules", () => {
    expect(withoutRules(rows, ["user:1::use"])).toEqual([
      { principalUrn: "user:2", level: "manage" },
    ]);
  });

  it("adds new principals at the default level and never duplicates one", () => {
    expect(withAdded(rows, ["user:3", "user:1"])).toEqual([
      { principalUrn: "user:1", level: "use" },
      { principalUrn: "user:2", level: "manage" },
      { principalUrn: "user:3", level: "use" },
    ]);
  });
});
