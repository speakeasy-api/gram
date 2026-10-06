import { describe, expect, it } from "vitest";
import type { ToolSelectionTool } from "@/components/tool-selection/ToolSelectionPanel";
import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import {
  buildAccessRows,
  complementTools,
  inheritedGrants,
  keptIndividually,
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
    // happened to look at first. The person's own rule covers every server,
    // so it names nothing here that could outrank either block.
    const person = personIn([
      role("R1", { level: "use" }),
      role("R1", { appliesTo: "resource", level: "blocked", tools: ["b"] }),
      role("R2", { appliesTo: "resource", level: "blocked", tools: ["c"] }),
      entry({
        principalUrn: "user:u1",
        level: "use",
        appliesTo: "all_resources",
      }),
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
    // The list is of rules; what someone reached only through a role can do
    // is answered by Check access instead.
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

  it("lets a person's own grant outrank a role's block", () => {
    const person = personIn([
      role("Admin", { level: "use" }),
      role("Admin", {
        appliesTo: "resource",
        level: "blocked",
        memberIds: ["u1"],
      }),
      entry({ principalUrn: "user:u1", level: "use", tools: ["a"] }),
    ]);
    const state = scopeState(person, "use", catalog);

    expect(state.granted).toBe(true);
    expect(state.value).toBe("1 tool");
    expect(state.via).toBeUndefined();
    expect(state.capped).toBe(false);
    // Revoking drops the person's own rule; the role's grant is already
    // cancelled by its block, so nothing needs subtracting.
    expect(state.subtracts).toBe(false);
  });

  it("lets a person's own rule reach past a role's narrowed block", () => {
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

    expect(state.value).toBe("All tools");
    expect(state.capped).toBe(false);
  });

  it("leaves a person reached only through a role at what its block leaves", () => {
    const person = personIn([
      role("Admin", { level: "use" }),
      role("Admin", {
        appliesTo: "resource",
        level: "blocked",
        tools: ["b", "c", "d", "e"],
      }),
      entry({
        principalUrn: "user:u1",
        level: "view",
        appliesTo: "all_resources",
      }),
    ]);
    const state = scopeState(person, "use", catalog);

    expect(state.value).toBe("1 tool");
    // The person's own rule here would outrank the block, so the line is not
    // capped: widening it is written as that rule.
    expect(state.capped).toBe(false);
  });

  it("keeps the person's own block over their own grant", () => {
    const person = personIn([
      entry({ principalUrn: "user:u1", level: "use" }),
      entry({ principalUrn: "user:u1", level: "blocked", tools: ["a"] }),
    ]);

    expect(reachableTools(person.cells.use, catalog)).toEqual([
      "b",
      "c",
      "d",
      "e",
    ]);
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

  it("applies a role block to an agent despite its own rule", () => {
    // An agent's owner can write its rules without being an administrator,
    // so they never outrank a block an administrator put on its role.
    const rows = buildAccessRows([
      agent(),
      agentRole("readers", { level: "blocked" }),
    ]);
    const row = rows.find((r) => r.principalUrn === "agent:a1")!;

    expect(row.cells.use.blocks).toHaveLength(1);
    expect(scopeState(row, "use", catalog).granted).toBe(false);
    expect(scopeState(row, "use", catalog).capped).toBe(true);
  });

  it("applies a role block to an agent's rule covering every server", () => {
    const rows = buildAccessRows([
      entry({
        principalUrn: "agent:a1",
        kind: "agent",
        displayName: "Releaser",
        appliesTo: "all_resources",
      }),
      agentRole("readers", { level: "blocked", appliesTo: "resource" }),
    ]);
    const row = rows.find((r) => r.principalUrn === "agent:a1")!;

    expect(scopeState(row, "use", catalog).granted).toBe(false);
  });

  it("applies a role block to a person's rule covering every server", () => {
    const rows = buildAccessRows([
      entry({ appliesTo: "all_resources" }),
      role("readers", { level: "blocked", appliesTo: "resource" }),
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

describe("keptIndividually", () => {
  const everyone = entry({
    principalUrn: "user:all",
    kind: "everyone",
    displayName: "Everyone",
    appliesTo: "all_resources",
  });

  it("names the people a group reaches whose own rule here is in force", () => {
    const rows = buildAccessRows([
      role("Staff", { memberIds: ["u1", "u2", "u4"], agentIds: ["a1"] }),
      entry({ principalUrn: "user:u1", displayName: "Hana Sato" }),
      entry({
        principalUrn: "user:u2",
        displayName: "Ravi Patel",
        appliesTo: "all_resources",
      }),
      entry({
        principalUrn: "user:u3",
        displayName: "Outside Staff",
      }),
      entry({ principalUrn: "agent:a1", kind: "agent", displayName: "Bot" }),
      entry({
        principalUrn: "user:u4",
        displayName: "Self Blocked",
      }),
      entry({
        principalUrn: "user:u4",
        displayName: "Self Blocked",
        level: "blocked",
      }),
    ]);
    const staff = rows.find((row) => row.principalUrn === "role:Staff")!;

    expect(keptIndividually(staff, rows).map((row) => row.displayName)).toEqual(
      ["Hana Sato"],
    );
  });

  it("leaves out a person whose own block cancels every tool their rule names", () => {
    const rows = buildAccessRows([
      role("Staff", { memberIds: ["u1"] }),
      entry({
        principalUrn: "user:u1",
        displayName: "Hana Sato",
        tools: ["a"],
      }),
      entry({
        principalUrn: "user:u1",
        displayName: "Hana Sato",
        level: "blocked",
        tools: ["a"],
      }),
    ]);
    const staff = rows.find((row) => row.principalUrn === "role:Staff")!;

    expect(keptIndividually(staff, rows, catalog)).toEqual([]);
  });

  it("names everyone holding their own rule when the group is everyone", () => {
    const rows = buildAccessRows([
      everyone,
      entry({ principalUrn: "user:u1", displayName: "Hana Sato" }),
      entry({ principalUrn: "user:u3", displayName: "Outside Staff" }),
    ]);
    const all = rows.find((row) => row.principalUrn === "user:all")!;

    expect(keptIndividually(all, rows).map((row) => row.displayName)).toEqual([
      "Hana Sato",
      "Outside Staff",
    ]);
  });
});
