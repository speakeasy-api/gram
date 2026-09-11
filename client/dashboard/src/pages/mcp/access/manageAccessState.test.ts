import { describe, expect, it } from "vitest";
import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import {
  pageCount,
  pageOf,
  withAdded,
  withRule,
  withoutPrincipal,
  withoutRules,
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

  it("adds a rule for a principal that has none at that level", () => {
    expect(withRule(rows, "role:global:9", "blocked")).toEqual([
      { principalUrn: "user:1", level: "use" },
      { principalUrn: "user:2", level: "manage" },
      {
        principalUrn: "role:global:9",
        level: "blocked",
        tools: [],
        dispositions: [],
      },
    ]);
  });

  it("replaces the rule a principal already holds at that level", () => {
    expect(withRule(rows, "user:1", "use", { tools: ["search"] })).toEqual([
      {
        principalUrn: "user:1",
        level: "use",
        tools: ["search"],
        dispositions: [],
      },
      { principalUrn: "user:2", level: "manage" },
    ]);
  });

  it("keeps a principal's other level when one of its rules changes", () => {
    // "manage the server" and "connect to two tools" are separate rules, so
    // editing one must not rewrite the other.
    const mixed = [
      entry({ principalUrn: "user:1", level: "manage" }),
      entry({ principalUrn: "user:1", level: "use", tools: ["search"] }),
    ];
    expect(withRule(mixed, "user:1", "use")).toEqual([
      { principalUrn: "user:1", level: "manage" },
      { principalUrn: "user:1", level: "use", tools: [], dispositions: [] },
    ]);
  });

  it("removes rules", () => {
    expect(withoutRules(rows, ["user:1::use"])).toEqual([
      { principalUrn: "user:2", level: "manage" },
    ]);
  });

  it("removes every rule naming a principal, whatever its level", () => {
    const mixed = [
      entry({ principalUrn: "user:1", level: "manage" }),
      entry({ principalUrn: "user:1", level: "use", tools: ["search"] }),
      entry({ principalUrn: "user:2", level: "view" }),
    ];
    expect(withoutPrincipal(mixed, "user:1")).toEqual([
      { principalUrn: "user:2", level: "view" },
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
