import { describe, expect, it } from "vitest";
import type { ToolSelectionTool } from "@/components/tool-selection/ToolSelectionPanel";
import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import {
  buildAccessRows,
  complementTools,
  inheritedGrants,
  scopeState,
} from "./accessRows";

function entry(
  overrides: Partial<ResourceAudienceEntry> = {},
): ResourceAudienceEntry {
  return {
    principalUrn: "user:1",
    kind: "user",
    displayName: "Hana Sato",
    level: "use",
    appliesTo: "resource",
    ...overrides,
  } as ResourceAudienceEntry;
}

function rowFor(entries: ResourceAudienceEntry[]) {
  return buildAccessRows(entries)[0]!;
}

describe("grouping", () => {
  it("puts every rule for one principal on a single row", () => {
    const rows = buildAccessRows([
      entry({ principalUrn: "user:1", level: "use", tools: ["search"] }),
      entry({ principalUrn: "user:1", level: "manage" }),
      entry({ principalUrn: "user:2", level: "view" }),
    ]);

    expect(rows).toHaveLength(2);
    expect(rows[0]!.cells.use.direct?.tools).toEqual(["search"]);
    expect(rows[0]!.cells.manage.direct).toBeTruthy();
    expect(rows[1]!.principalUrn).toBe("user:2");
  });

  it("tells a rule naming this server from one covering every server", () => {
    const row = rowFor([
      entry({
        principalUrn: "role:global:1",
        kind: "role",
        displayName: "Engineering",
        appliesTo: "all_resources",
      }),
    ]);

    expect(row.inheritedOnly).toBe(true);
    expect(row.cells.use.inherited).toBeTruthy();
    expect(row.cells.use.direct).toBeUndefined();
    expect(inheritedGrants(row)).toHaveLength(1);
  });

  it("keeps only a block naming this server, since the other is the role editor's", () => {
    const row = rowFor([
      entry({ level: "blocked", appliesTo: "all_resources" }),
    ]);
    expect(row.blocks.use).toBeUndefined();

    const own = rowFor([entry({ level: "blocked" })]);
    expect(own.blocks.use).toBeTruthy();
  });
});

describe("scope implication", () => {
  it("reads a weaker scope as granted by the stronger one that satisfies it", () => {
    const row = rowFor([entry({ level: "manage" })]);

    expect(row.cells.use.impliedBy).toBe("manage");
    expect(row.cells.view.impliedBy).toBe("manage");
    expect(row.cells.manage.impliedBy).toBeUndefined();
    expect(scopeState(row, "use")).toMatchObject({
      value: "All tools",
      granted: true,
    });
  });

  it("offers to turn off a line a stronger scope grants, since a block can say so", () => {
    const row = rowFor([entry({ level: "manage" })]);
    expect(scopeState(row, "use").canRevoke).toBe(true);
    expect(scopeState(row, "manage").canRevoke).toBe(true);
  });

  it("reads a rule covering every server as what it gives on this one", () => {
    // The page is about one server: a line says what the principal has here,
    // not which rule said so.
    const row = rowFor([entry({ appliesTo: "all_resources" })]);
    const state = scopeState(row, "use");

    expect(state).toMatchObject({
      value: "All tools",
      // Grants only add, so narrowing this one has to be a subtraction.
      subtracts: true,
      // Taking it away for this server alone is a block, which is a write
      // this page can make.
      canRevoke: true,
    });
  });

  it("reads a narrowed rule and the block that trims it", () => {
    const row = rowFor([
      entry({ tools: ["search", "fetch"] }),
      entry({ level: "blocked", tools: ["fetch"] }),
    ]);

    expect(scopeState(row, "use").value).toBe("2 tools except fetch");
  });

  it("takes away only the scope a block names", () => {
    // The mcp:blocked_* scopes are independent, so closing connect leaves
    // view and manage exactly as the role left them.
    const row = rowFor([
      entry({ level: "manage", appliesTo: "all_resources" }),
      entry({ level: "blocked" }),
    ]);

    expect(scopeState(row, "use").granted).toBe(false);
    expect(scopeState(row, "view").granted).toBe(true);
    expect(scopeState(row, "manage").granted).toBe(true);
  });

  it("reads no access at every scope only when all three are blocked", () => {
    const row = rowFor([
      entry({ level: "manage", appliesTo: "all_resources" }),
      entry({ level: "blocked" }),
      entry({ level: "blocked_view" }),
      entry({ level: "blocked_manage" }),
    ]);

    for (const scope of ["use", "view", "manage"] as const) {
      expect(scopeState(row, scope).value).toBe("No access");
      expect(scopeState(row, scope).granted).toBe(false);
    }
  });

  it("reads a scope nothing grants as no access", () => {
    const row = rowFor([entry({ level: "use" })]);
    expect(scopeState(row, "manage").granted).toBe(false);
  });
});

describe("complementTools", () => {
  const catalog: ToolSelectionTool[] = [
    { name: "search", annotations: ["read_only"] },
    { name: "fetch", annotations: ["read_only"] },
    { name: "delete", annotations: ["destructive"] },
  ];

  it("names every tool the choice leaves out, which is what a block writes", () => {
    expect(
      complementTools(catalog, { tools: ["search"], dispositions: [] }),
    ).toEqual(["fetch", "delete"]);
  });

  it("keeps a tool an annotation picked, not only one named outright", () => {
    expect(
      complementTools(catalog, { tools: [], dispositions: ["read_only"] }),
    ).toEqual(["delete"]);
  });

  it("returns nothing when the choice is the whole catalogue", () => {
    expect(
      complementTools(catalog, {
        tools: ["search", "fetch", "delete"],
        dispositions: [],
      }),
    ).toEqual([]);
  });
});

describe("what connect reaches", () => {
  const catalog: ToolSelectionTool[] = [
    { name: "a", annotations: ["read_only"] },
    { name: "b", annotations: ["read_only"] },
    { name: "c", annotations: ["destructive"] },
    { name: "d", annotations: [] },
    { name: "e", annotations: [] },
  ];

  it("counts what is left rather than naming the subtraction", () => {
    // "5 tools except 4 tools" is not an answer anyone can read.
    const row = rowFor([
      entry({ level: "use" }),
      entry({ level: "blocked", tools: ["b", "c", "d", "e"] }),
    ]);

    expect(scopeState(row, "use", catalog).value).toBe("1 tool");
  });

  it("says all tools when the block takes nothing back", () => {
    const row = rowFor([entry({ level: "use" })]);
    expect(scopeState(row, "use", catalog).value).toBe("All tools");
  });

  it("resolves an annotation rule against the catalogue", () => {
    const row = rowFor([entry({ level: "use", dispositions: ["read_only"] })]);
    expect(scopeState(row, "use", catalog).value).toBe("2 tools");
  });

  it("falls back to naming the rule when the server publishes no catalogue", () => {
    const row = rowFor([
      entry({ level: "use" }),
      entry({ level: "blocked", tools: ["b", "c"] }),
    ]);

    expect(scopeState(row, "use").value).toBe("All tools except 2 tools");
  });
});
