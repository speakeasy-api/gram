import { describe, expect, it } from "vitest";
import type { ToolSelectionTool } from "@/components/tool-selection/ToolSelectionPanel";
import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import {
  buildAccessRows,
  complementTools,
  inheritedGrants,
  reachableTools,
  scopeState,
} from "./accessRows";

const catalog: ToolSelectionTool[] = [
  { name: "a", annotations: ["read_only"] },
  { name: "b", annotations: ["read_only"] },
  { name: "c", annotations: ["destructive"] },
  { name: "d", annotations: [] },
  { name: "e", annotations: [] },
];

function entry(
  overrides: Partial<ResourceAudienceEntry> = {},
): ResourceAudienceEntry {
  return {
    principalUrn: "user:u1",
    kind: "user",
    displayName: "Hana Sato",
    level: "use",
    appliesTo: "resource",
    ...overrides,
  } as ResourceAudienceEntry;
}

/** A rule on a role that reaches u1, which is how a person inherits access. */
function role(
  name: string,
  overrides: Partial<ResourceAudienceEntry> = {},
): ResourceAudienceEntry {
  return entry({
    principalUrn: `role:${name}`,
    kind: "role",
    displayName: name,
    appliesTo: "all_resources",
    memberIds: ["u1"],
    ...overrides,
  });
}

function rowFor(entries: ResourceAudienceEntry[]) {
  return buildAccessRows(entries)[0]!;
}

function personIn(entries: ResourceAudienceEntry[]) {
  return buildAccessRows(entries).find(
    (row) => row.principalUrn === "user:u1",
  )!;
}

describe("grouping", () => {
  it("puts every rule for one principal on a single row", () => {
    const rows = buildAccessRows([
      entry({ principalUrn: "user:1", level: "use", tools: ["a"] }),
      entry({ principalUrn: "user:1", level: "manage" }),
      entry({ principalUrn: "user:2", level: "view" }),
    ]);

    expect(rows).toHaveLength(2);
    expect(rows[0]!.cells.use.own?.tools).toEqual(["a"]);
    expect(rows[0]!.cells.manage.own).toBeTruthy();
    expect(rows[1]!.principalUrn).toBe("user:2");
  });

  it("tells a rule this page owns from one it does not", () => {
    const row = rowFor([role("Engineering")]);

    expect(row.inheritedOnly).toBe(true);
    expect(row.cells.use.grants).toHaveLength(1);
    expect(row.cells.use.own).toBeUndefined();
    expect(inheritedGrants(row)).toHaveLength(1);
  });

  it("keeps only a block naming this server as the row's own", () => {
    const inherited = rowFor([role("Engineering", { level: "blocked" })]);
    expect(inherited.cells.use.blocks).toHaveLength(1);
    expect(inherited.cells.use.ownBlock).toBeUndefined();

    const own = rowFor([entry({ level: "blocked" })]);
    expect(own.cells.use.ownBlock).toBeTruthy();
  });
});

describe("scope resolution", () => {
  it("counts a stronger scope as granting the weaker ones it satisfies", () => {
    const row = rowFor([entry({ level: "manage" })]);

    for (const scope of ["use", "view", "manage"] as const) {
      expect(scopeState(row, scope, catalog).granted).toBe(true);
    }
  });

  it("takes away only the scope a block names", () => {
    // The mcp:blocked_* scopes are independent, so closing connect leaves
    // view and manage exactly as the role left them.
    const row = rowFor([
      role("Engineering", { level: "manage" }),
      role("Engineering", { appliesTo: "resource", level: "blocked" }),
    ]);

    expect(scopeState(row, "use", catalog).granted).toBe(false);
    expect(scopeState(row, "view", catalog).granted).toBe(true);
    expect(scopeState(row, "manage", catalog).granted).toBe(true);
  });

  it("honours a block that covers every server, which this page cannot lift", () => {
    const row = rowFor([
      role("Engineering", { level: "use" }),
      role("Engineering", { level: "blocked" }),
    ]);
    const state = scopeState(row, "use", catalog);

    expect(state.granted).toBe(false);
    expect(state.capped).toBe(true);
  });

  it("unions the tools two grants open", () => {
    const row = rowFor([
      entry({ level: "use", tools: ["a"] }),
      entry({ principalUrn: "user:u1", level: "view", tools: ["b"] }),
    ]);

    expect(reachableTools(row.cells.use, catalog)).toEqual(["a", "b"]);
  });

  it("unions the tools two blocks take away", () => {
    // Two roles removing different tools remove both, not whichever the row
    // happened to look at first.
    const person = personIn([
      role("R1", { level: "use" }),
      role("R1", { appliesTo: "resource", level: "blocked", tools: ["b"] }),
      role("R2", { appliesTo: "resource", level: "blocked", tools: ["c"] }),
      entry({ principalUrn: "user:u1", level: "use" }),
    ]);

    expect(reachableTools(person.cells.use, catalog)).toEqual(["a", "d", "e"]);
  });

  it("resolves an annotation rule against the catalogue", () => {
    const row = rowFor([entry({ level: "use", dispositions: ["read_only"] })]);
    expect(scopeState(row, "use", catalog).value).toBe("2 tools");
  });

  it("counts what is left rather than naming the subtraction", () => {
    // "5 tools except 4 tools" is not an answer anyone can read.
    const row = rowFor([
      entry({ level: "use" }),
      entry({ level: "blocked", tools: ["b", "c", "d", "e"] }),
    ]);

    expect(scopeState(row, "use", catalog).value).toBe("1 tool");
  });

  it("does not claim every tool for a narrowed stronger grant", () => {
    // Manage over two tools satisfies a connect check for those two only.
    const row = rowFor([entry({ level: "manage", tools: ["a", "b"] })]);

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

describe("a person and the roles they are in", () => {
  it("gives a person no row until a rule names them here", () => {
    // The list is of rules; someone reached only through a role shows up
    // under "People this reaches" instead.
    expect(
      buildAccessRows([role("Admin", { level: "manage" })]).map(
        (row) => row.principalUrn,
      ),
    ).toEqual(["role:Admin"]);
  });

  it("reads a scope the person only has through a role", () => {
    const person = personIn([
      role("Admin", { level: "manage" }),
      entry({ principalUrn: "user:u1", level: "use" }),
    ]);
    const state = scopeState(person, "view", catalog);

    expect(state.granted).toBe(true);
    expect(state.via).toBe("Admin");
    // The grant is not this page's to rewrite, so closing it is a block.
    expect(state.subtracts).toBe(true);
  });

  it("says nothing extra when a row's own rule for every server decides it", () => {
    // "via Collaborator" on the Collaborator row says nothing, and this page
    // answers for one server, so where else the rule reaches is not the note.
    const row = rowFor([role("Collaborator", { level: "view" })]);
    const state = scopeState(row, "view", catalog);

    expect(state.value).toBe("Allowed");
    expect(state.via).toBeUndefined();
    expect(state.note).toBeUndefined();
  });

  it("says which scope on this server a weaker line is included in", () => {
    const row = rowFor([
      role("Collaborator", { appliesTo: "resource", level: "manage" }),
    ]);

    expect(scopeState(row, "view", catalog).note).toBe("included in Manage");
  });

  it("reads a row's own block for every server as plain no access", () => {
    const row = rowFor([role("Collaborator", { level: "blocked_manage" })]);
    const state = scopeState(row, "manage", catalog);

    expect(state.value).toBe("No access");
    expect(state.granted).toBe(false);
    expect(state.via).toBeUndefined();
    expect(state.note).toBeUndefined();
    expect(state.capped).toBe(true);
  });

  it("names another principal over the row's own wider rule", () => {
    const person = personIn([
      role("Admin", { level: "manage" }),
      entry({ principalUrn: "user:u1", appliesTo: "all_resources" }),
    ]);
    const state = scopeState(person, "use", catalog);

    expect(state.via).toBe("Admin");
    expect(state.note).toBeUndefined();
  });

  it("still resolves a role's grant when the person has a rule of their own", () => {
    // The person's own connect rule must not hide the role's manage grant:
    // the write paths need it to know a block is required.
    const person = personIn([
      role("Admin", { level: "manage" }),
      entry({ principalUrn: "user:u1", level: "use" }),
    ]);

    expect(scopeState(person, "view", catalog).granted).toBe(true);
    expect(scopeState(person, "use", catalog).subtracts).toBe(true);
  });

  it("says a role's block cancels the person's own grant", () => {
    const person = personIn([
      role("Admin", {
        appliesTo: "resource",
        level: "blocked",
        memberIds: ["u1"],
      }),
      entry({ principalUrn: "user:u1", level: "use", tools: ["a"] }),
    ]);
    const state = scopeState(person, "use", catalog);

    expect(state.granted).toBe(false);
    expect(state.via).toBe("Admin");
    expect(state.capped).toBe(true);
  });

  it("caps a person at what a role's narrowed block leaves", () => {
    const person = personIn([
      role("Admin", { level: "use" }),
      role("Admin", {
        appliesTo: "resource",
        level: "blocked",
        tools: ["b", "c", "d", "e"],
      }),
      entry({ principalUrn: "user:u1", level: "use" }),
    ]);
    const state = scopeState(person, "use", catalog);

    expect(state.value).toBe("1 tool");
    expect(state.capped).toBe(true);
  });

  it("does not let a role's rule reach another role", () => {
    const rows = buildAccessRows([
      role("Admin", { level: "manage" }),
      entry({
        principalUrn: "role:other",
        kind: "role",
        displayName: "Other",
        appliesTo: "all_resources",
        level: "use",
        memberIds: ["u1"],
      }),
    ]);
    const other = rows.find((row) => row.principalUrn === "role:other")!;

    expect(scopeState(other, "manage", catalog).granted).toBe(false);
  });
});

describe("complementTools", () => {
  it("names every tool the choice leaves out, which is what a block writes", () => {
    expect(
      complementTools(catalog, { tools: ["a"], dispositions: [] }),
    ).toEqual(["b", "c", "d", "e"]);
  });

  it("keeps a tool an annotation picked, not only one named outright", () => {
    expect(
      complementTools(catalog, { tools: [], dispositions: ["read_only"] }),
    ).toEqual(["c", "d", "e"]);
  });

  it("returns nothing when the choice is the whole catalogue", () => {
    expect(
      complementTools(catalog, {
        tools: ["a", "b", "c", "d", "e"],
        dispositions: [],
      }),
    ).toEqual([]);
  });
});

describe("blocks against an agent", () => {
  /** A rule on a role that reaches agent a1. */
  function agentRole(
    name: string,
    overrides: Partial<ResourceAudienceEntry> = {},
  ): ResourceAudienceEntry {
    return entry({
      principalUrn: `role:${name}`,
      kind: "role",
      displayName: name,
      appliesTo: "all_resources",
      agentIds: ["a1"],
      ...overrides,
    });
  }

  const agent = () =>
    entry({ principalUrn: "agent:a1", kind: "agent", displayName: "Releaser" });

  it("leaves an agent connected when a role it holds is blocked", () => {
    // The blocked_* scopes are not agent-runtime-safe, so this block is
    // dropped when the agent's policy loads. Reporting it would tell an
    // administrator the agent has no access while it can still connect.
    const rows = buildAccessRows([
      agent(),
      agentRole("readers", { level: "blocked" }),
    ]);
    const row = rows.find((r) => r.principalUrn === "agent:a1")!;

    expect(row.cells.use.blocks).toEqual([]);
    expect(scopeState(row, "use", catalog).granted).toBe(true);
    expect(scopeState(row, "use", catalog).capped).toBe(false);
  });

  it("still applies a role block to a person", () => {
    const rows = buildAccessRows([
      entry(),
      role("readers", { level: "blocked" }),
    ]);
    const row = rows.find((r) => r.principalUrn === "user:u1")!;

    expect(row.cells.use.blocks).toHaveLength(1);
    expect(scopeState(row, "use", catalog).granted).toBe(false);
  });

  it("still gives an agent the access a role grants it", () => {
    const rows = buildAccessRows([agentRole("readers")]);
    const agentRow = rows.find((r) => r.principalUrn === "role:readers")!;

    expect(agentRow).toBeTruthy();
    expect(
      buildAccessRows([agent(), agentRole("readers")]).find(
        (r) => r.principalUrn === "agent:a1",
      )!.cells.use.grants.length,
    ).toBe(2);
  });
});
