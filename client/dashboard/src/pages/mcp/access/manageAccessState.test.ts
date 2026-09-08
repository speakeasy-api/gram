import { describe, expect, it } from "vitest";
import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import {
  filterAudience,
  EMPTY_FILTERS,
  pageCount,
  pageOf,
  withAdded,
  withLevel,
  withoutPrincipals,
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

describe("filterAudience", () => {
  const rows = [
    entry({ principalUrn: "user:1", displayName: "Hana Sato", kind: "user" }),
    entry({
      principalUrn: "directory_group:abc",
      displayName: "Infra",
      kind: "directory_group",
      level: "manage",
    }),
    entry({
      principalUrn: "role:global:1",
      displayName: "Admin",
      kind: "role",
      level: "blocked",
    }),
  ];

  it("keeps everything by default", () => {
    expect(filterAudience(rows, EMPTY_FILTERS)).toHaveLength(3);
  });

  it("filters by type", () => {
    const result = filterAudience(rows, { ...EMPTY_FILTERS, type: "role" });
    expect(result.map((r) => r.displayName)).toEqual(["Admin"]);
  });

  it("filters by access level, including blocks", () => {
    const result = filterAudience(rows, { ...EMPTY_FILTERS, level: "blocked" });
    expect(result.map((r) => r.displayName)).toEqual(["Admin"]);
  });

  it("searches name and description, case-insensitively", () => {
    expect(
      filterAudience(rows, { ...EMPTY_FILTERS, search: "hana" }),
    ).toHaveLength(1);
    expect(
      filterAudience(rows, { ...EMPTY_FILTERS, search: "nobody" }),
    ).toHaveLength(0);
  });
});

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
    expect(withLevel(rows, "user:1", "manage")).toEqual([
      { principalUrn: "user:1", level: "manage" },
      { principalUrn: "user:2", level: "manage" },
    ]);
  });

  it("adds a principal that has no rule yet, which is how a block on an inherited rule is written", () => {
    expect(withLevel(rows, "role:global:9", "blocked")).toEqual([
      { principalUrn: "user:1", level: "use" },
      { principalUrn: "user:2", level: "manage" },
      { principalUrn: "role:global:9", level: "blocked" },
    ]);
  });

  it("removes principals", () => {
    expect(withoutPrincipals(rows, ["user:1"])).toEqual([
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
