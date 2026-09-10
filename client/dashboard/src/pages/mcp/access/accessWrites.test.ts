import { describe, expect, it } from "vitest";
import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import type { ToolSelectionTool } from "@/components/tool-selection/ToolSelectionPanel";
import { buildAccessRows } from "./accessRows";
import {
  addPrincipalsWrite,
  allowWrite,
  narrowWrite,
  revokeRowWrite,
  revokeScopeWrite,
} from "./accessWrites";
import { ownRules } from "./serverAudience";

function entry(
  overrides: Partial<ResourceAudienceEntry> & { principalUrn: string },
): ResourceAudienceEntry {
  return {
    kind: "user",
    displayName: "Hana Sato",
    level: "use",
    appliesTo: "resource",
    ...overrides,
  } as ResourceAudienceEntry;
}

/** The list as the page holds it: the rules it owns, and the rows it shows. */
function state(entries: ResourceAudienceEntry[]) {
  return { direct: ownRules(entries), rows: buildAccessRows(entries) };
}

const catalog: ToolSelectionTool[] = [
  { name: "search", annotations: ["read_only"] },
  { name: "fetch", annotations: ["read_only"] },
  { name: "delete", annotations: ["destructive"] },
];

const role = (overrides: Partial<ResourceAudienceEntry> = {}) =>
  entry({
    principalUrn: "role:global:1",
    kind: "role",
    displayName: "Engineering",
    appliesTo: "all_resources",
    ...overrides,
  });

describe("allowWrite", () => {
  it("opens a scope by adding the rule for it", () => {
    const { direct, rows } = state([entry({ principalUrn: "user:1" })]);

    expect(allowWrite(direct, rows[0]!, "manage").entries).toEqual([
      { principalUrn: "user:1", level: "use" },
      {
        principalUrn: "user:1",
        level: "manage",
        tools: [],
        dispositions: [],
      },
    ]);
  });

  it("widens a narrowed rule back to the whole server", () => {
    const { direct, rows } = state([
      entry({ principalUrn: "user:1", tools: ["search"] }),
    ]);

    expect(allowWrite(direct, rows[0]!, "use").entries).toEqual([
      { principalUrn: "user:1", level: "use", tools: [], dispositions: [] },
    ]);
  });

  it("only lifts the block when an organization-wide rule already grants it", () => {
    // A grant alongside a rule covering every server would say nothing new, so
    // the whole edit is dropping the block this page wrote.
    const { direct, rows } = state([
      role(),
      role({ appliesTo: "resource", level: "blocked", tools: ["delete"] }),
    ]);

    expect(allowWrite(direct, rows[0]!, "use").entries).toEqual([]);
  });
});

describe("revokeScopeWrite", () => {
  it("drops only the rule for that scope", () => {
    const { direct, rows } = state([
      entry({ principalUrn: "user:1", level: "use" }),
      entry({ principalUrn: "user:1", level: "manage" }),
    ]);

    expect(revokeScopeWrite(direct, rows[0]!, "manage").entries).toEqual([
      { principalUrn: "user:1", level: "use" },
    ]);
  });

  it("blocks manage alone when an organization-wide rule grants it", () => {
    // The grant belongs to the role editor, so the per-server edit is a
    // block at the level for that one scope. Connect and view are untouched.
    const { direct, rows } = state([role({ level: "manage" })]);

    const write = revokeScopeWrite(direct, rows[0]!, "manage", "Acme Ops");

    expect(write.entries).toEqual([
      {
        principalUrn: "role:global:1",
        level: "blocked_manage",
        tools: [],
        dispositions: [],
      },
    ]);
    expect(write.message).toBe(
      "Engineering can no longer manage Acme Ops. Other servers are unchanged.",
    );
  });

  it("blocks view without touching a block already written for manage", () => {
    // The mcp:blocked_* scopes are independent, so each line's block stands
    // on its own rather than one making another redundant.
    const { direct, rows } = state([
      role({ level: "manage" }),
      role({ appliesTo: "resource", level: "blocked_manage" }),
    ]);

    const write = revokeScopeWrite(direct, rows[0]!, "view", "Acme Ops");

    expect(write.entries).toEqual([
      { principalUrn: "role:global:1", level: "blocked_manage" },
      {
        principalUrn: "role:global:1",
        level: "blocked_view",
        tools: [],
        dispositions: [],
      },
    ]);
    expect(write.message).toBe(
      "Engineering can no longer view Acme Ops. Other servers are unchanged.",
    );
  });

  it("blocks a scope a stronger grant on the same row satisfies", () => {
    // Manage satisfies a connect check, so dropping the connect rule is not
    // enough — the block is what actually closes the line.
    const { direct, rows } = state([
      entry({ principalUrn: "user:1", level: "manage" }),
    ]);

    expect(revokeScopeWrite(direct, rows[0]!, "use").entries).toEqual([
      { principalUrn: "user:1", level: "manage" },
      {
        principalUrn: "user:1",
        level: "blocked",
        tools: [],
        dispositions: [],
      },
    ]);
  });
});

describe("lifting a block", () => {
  it("removes only the blocks reaching that scope", () => {
    const { direct, rows } = state([
      role({ level: "manage" }),
      role({ appliesTo: "resource", level: "blocked_manage" }),
    ]);

    // Manage is granted on every server, so lifting the block is the whole
    // edit — no grant naming this server is added.
    expect(allowWrite(direct, rows[0]!, "manage").entries).toEqual([]);
  });

  it("keeps another line's block, which takes nothing from this scope", () => {
    const { direct, rows } = state([
      role({ level: "manage" }),
      role({ appliesTo: "resource", level: "blocked_view" }),
    ]);

    expect(allowWrite(direct, rows[0]!, "manage").entries).toEqual([
      { principalUrn: "role:global:1", level: "blocked_view" },
    ]);
  });

  it("keeps a stronger block, which takes nothing from this scope", () => {
    const { direct, rows } = state([
      role({ level: "use" }),
      role({ appliesTo: "resource", level: "blocked_manage" }),
    ]);

    expect(allowWrite(direct, rows[0]!, "use").entries).toEqual([
      {
        principalUrn: "role:global:1",
        level: "blocked_manage",
      },
    ]);
  });
});

describe("narrowWrite", () => {
  it("rewrites a rule naming this server narrower", () => {
    const { direct, rows } = state([entry({ principalUrn: "user:1" })]);

    expect(
      narrowWrite(
        direct,
        rows[0]!,
        { tools: ["search"], dispositions: [] },
        catalog,
      ).entries,
    ).toEqual([
      {
        principalUrn: "user:1",
        level: "use",
        tools: ["search"],
        dispositions: [],
      },
    ]);
  });

  it("subtracts from an organization-wide rule by blocking what is left out", () => {
    const { direct, rows } = state([role()]);

    const write = narrowWrite(
      direct,
      rows[0]!,
      { tools: ["search"], dispositions: [] },
      catalog,
      "Acme Ops",
    );

    expect(write.entries).toEqual([
      {
        principalUrn: "role:global:1",
        level: "blocked",
        tools: ["fetch", "delete"],
        dispositions: [],
      },
    ]);
    expect(write.message).toContain("Acme Ops");
  });

  it("lifts the block rather than writing one that blocks nothing", () => {
    // An unnarrowed block takes the whole server away, so a choice covering
    // the whole catalogue has to clear the block instead.
    const { direct, rows } = state([
      role(),
      role({ appliesTo: "resource", level: "blocked", tools: ["delete"] }),
    ]);

    expect(
      narrowWrite(
        direct,
        rows[0]!,
        { tools: ["search", "fetch", "delete"], dispositions: [] },
        catalog,
      ).entries,
    ).toEqual([]);
  });
});

describe("revokeRowWrite", () => {
  it("removes the rules naming this server", () => {
    const { direct, rows } = state([
      entry({ principalUrn: "user:1", level: "use" }),
      entry({ principalUrn: "user:1", level: "manage" }),
      entry({ principalUrn: "user:2", displayName: "Jonas" }),
    ]);

    expect(revokeRowWrite(direct, rows[0]!).entries).toEqual([
      { principalUrn: "user:2", level: "use" },
    ]);
  });

  it("blocks every scope for a principal an organization-wide rule reaches", () => {
    // Blocks are independent, so "no access at all" has to say so three
    // times rather than leaning on one block to cover the rest.
    const { direct, rows } = state([role()]);

    const write = revokeRowWrite(direct, rows[0]!, "Acme Ops");

    expect(
      write.entries
        .map((entry) => entry.level)
        .sort((a, b) => a.localeCompare(b)),
    ).toEqual(["blocked", "blocked_manage", "blocked_view"]);
    expect(write.message).toContain("Other servers are unchanged");
  });
});

describe("addPrincipalsWrite", () => {
  it("adds people at connect and counts them in the message", () => {
    const { direct } = state([entry({ principalUrn: "user:1" })]);

    const write = addPrincipalsWrite(direct, ["user:2", "user:3"]);

    expect(write.entries).toEqual([
      { principalUrn: "user:1", level: "use" },
      { principalUrn: "user:2", level: "use" },
      { principalUrn: "user:3", level: "use" },
    ]);
    expect(write.message).toBe("2 people can now connect to this server.");
  });
});

describe("access a role gives a person", () => {
  // A person's row shows what they can do here, not only the rules that name
  // them: a role they are in reaches them too, and the row would otherwise
  // say "No access" about someone who plainly has it.
  const withRole = () =>
    state([
      role({ level: "view", memberIds: ["u1"] }),
      role({ level: "manage", memberIds: ["u1"] }),
      entry({ principalUrn: "user:u1", tools: ["search"], memberIds: ["u1"] }),
    ]);

  function personRow() {
    const { rows } = withRole();
    return rows.find((row) => row.principalUrn === "user:u1")!;
  }

  it("closes it with a block, since the grant is the role's", () => {
    const { direct } = withRole();

    const write = revokeScopeWrite(direct, personRow(), "view", "Acme Ops");

    expect(write.entries).toEqual([
      { principalUrn: "user:u1", level: "use", tools: ["search"] },
      {
        principalUrn: "user:u1",
        level: "blocked_view",
        tools: [],
        dispositions: [],
      },
    ]);
  });

  it("adds no grant of its own when the role already opens it whole", () => {
    const { direct } = withRole();

    expect(allowWrite(direct, personRow(), "view", "Acme Ops").entries).toEqual(
      [{ principalUrn: "user:u1", level: "use", tools: ["search"] }],
    );
  });
});

describe("a role the page has already trimmed", () => {
  // Admin reaches every server, but a block on this one leaves it one tool.
  // A person in Admin who is also granted access here has their own rule,
  // and that rule must survive: the role no longer opens the whole server,
  // so treating it as though it did would delete the grant and write
  // nothing in its place.
  const trimmed = () =>
    state([
      role({ level: "use" }),
      role({
        appliesTo: "resource",
        level: "blocked",
        tools: ["b", "c", "d", "e"],
      }),
      entry({ principalUrn: "user:u1", level: "use", memberIds: ["u1"] }),
    ]);

  function personRow() {
    const { rows } = trimmed();
    return rows.find((row) => row.principalUrn === "user:u1")!;
  }

  it("keeps the person's own rule when widening it to every tool", () => {
    // The row vanished when this dropped the rule and wrote nothing: the
    // role looked like it opened the whole server, so the grant seemed
    // redundant. The block on the role means it does not.
    const { direct } = trimmed();

    expect(allowWrite(direct, personRow(), "use", "Acme Ops").entries).toEqual([
      {
        principalUrn: "role:global:1",
        level: "blocked",
        tools: ["b", "c", "d", "e"],
      },
      { principalUrn: "user:u1", level: "use", tools: [], dispositions: [] },
    ]);
  });

  it("narrows the person's own rule rather than subtracting from the role", () => {
    const { direct } = trimmed();

    expect(
      narrowWrite(
        direct,
        personRow(),
        { tools: ["a"], dispositions: [] },
        catalog,
        "Acme Ops",
      ).entries,
    ).toEqual([
      {
        principalUrn: "role:global:1",
        level: "blocked",
        tools: ["b", "c", "d", "e"],
      },
      {
        principalUrn: "user:u1",
        level: "use",
        tools: ["a"],
        dispositions: [],
      },
    ]);
  });
});

describe("a stronger scope cannot outrun a trim", () => {
  // Manage satisfies a connect check, so a role granting it looks like it
  // opens every tool. A block trimming that role's connect on this server
  // subtracts from that too, so the person's own rule still has work to do.
  it("keeps the person's rule when the role's other scopes are unblocked", () => {
    const { direct, rows } = state([
      role({ level: "use" }),
      role({ level: "manage" }),
      role({
        appliesTo: "resource",
        level: "blocked",
        tools: ["b", "c", "d", "e"],
      }),
      entry({ principalUrn: "user:u1", level: "use", memberIds: ["u1"] }),
    ]);
    const person = rows.find((row) => row.principalUrn === "user:u1")!;

    expect(
      allowWrite(direct, person, "use", "Acme Ops").entries.some(
        (written) =>
          written.principalUrn === "user:u1" && written.level === "use",
      ),
    ).toBe(true);
  });
});
