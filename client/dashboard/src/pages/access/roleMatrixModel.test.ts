// @vitest-environment node
import { ListScopesResult$inboundSchema } from "@gram/client/models/components/listscopesresult.js";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import type { Selector } from "@gram/client/models/components/selector.js";
import { describe, expect, it } from "vitest";

import servedCatalogue from "./listScopes.fixture.json";
import { grantKeysString } from "./roleDialogState";
import {
  diffGrants,
  grantsFromRole,
  sdkGrantsFromForm,
} from "./roleGrantTransform";
import {
  type McpRow,
  mcpAllows,
  mcpBlocks,
  mcpLevelIncludes,
  mcpToolLimit,
  otherMcpRules,
  platformAreas,
  platformBlock,
  removeOtherMcpRule,
  setMcpAllow,
  setMcpRowBlocked,
  setMcpToolLimit,
  setPlatformAllow,
  setPlatformBlocked,
  setPlatformScopeBlocked,
  splitMcpToolLimit,
  clearMcp,
  excludeMcpRow,
  clearPlatform,
  limitMcpUse,
  mcpBlockedTools,
  mcpRowDenied,
  platformAllows,
  platformAreaBlockable,
  platformAreaDenied,
  platformLevel,
  roleDenies,
  setMcpToolBlocked,
  setPlatformAreaDenied,
  setPlatformLevel,
  unblockPlatformScope,
} from "./roleMatrixModel";
import type { RoleGrant } from "./types";

const def = (
  slug: string,
  resourceType: string,
  exclusionScope?: string,
  extra: Partial<ScopeDefinition> = {},
): ScopeDefinition =>
  ({
    slug,
    label: slug,
    resourceType,
    category: resourceType,
    description: slug,
    agentEligible: true,
    visibility: "user_visible",
    exclusionScope,
    sensitive: false,
    subScopes: [],
    ...extra,
  }) as ScopeDefinition;

const scopes: ScopeDefinition[] = [
  def("mcp:connect", "mcp", "mcp:blocked_connect"),
  def("mcp:read", "mcp", "mcp:blocked_read"),
  def("mcp:write", "mcp", "mcp:blocked_write", { subScopes: ["mcp:read"] }),
  def("org:read", "org", "org:blocked_read"),
  def("org:admin", "org", "org:blocked_admin", { subScopes: ["org:read"] }),
  def("org:hooks_ingest", "org"),
  def("project:read", "project", "project:blocked_read"),
  def("project:write", "project", "project:blocked_write", {
    subScopes: ["project:read"],
  }),
  def("plugin:write", "project", "plugin:blocked_write", {
    category: "plugin" as ScopeDefinition["category"],
  }),
  def("chat:read", "chat", undefined, {
    category: "session" as ScopeDefinition["category"],
  }),
  def("chat:write", "chat", undefined, {
    category: "session" as ScopeDefinition["category"],
    subScopes: ["chat:read"],
  }),
];

const mcp = (resourceId: string, extra: Partial<Selector> = {}): Selector => ({
  resourceKind: "mcp",
  resourceId,
  ...extra,
});

/** Read stored grants the way the role editor does. */
function load(
  stored: { scope: string; selectors?: Selector[] }[],
): Record<string, RoleGrant> {
  return grantsFromRole(
    { grants: stored } as unknown as Parameters<typeof grantsFromRole>[0],
    scopes,
  );
}

function save(grants: Record<string, RoleGrant>) {
  return sdkGrantsFromForm(grants, scopes);
}

const all: McpRow = { kind: "all" };
const project: McpRow = { kind: "project", projectId: "p1" };
const github: McpRow = { kind: "server", serverId: "gh", projectId: "p1" };
const linear: McpRow = { kind: "server", serverId: "lin", projectId: "p1" };

describe("MCP cells", () => {
  it("reads each stored row as one cell", () => {
    const grants = load([
      { scope: "mcp:connect", selectors: [mcp("*", { projectId: "p1" })] },
      { scope: "mcp:write", selectors: [mcp("lin")] },
      { scope: "mcp:blocked_read", selectors: [mcp("gh")] },
    ]);
    expect(mcpAllows(grants, project, "use")).toBe(true);
    expect(mcpAllows(grants, project, "view")).toBe(false);
    expect(mcpAllows(grants, linear, "manage")).toBe(true);
    expect(mcpAllows(grants, linear, "use")).toBe(false);
    expect(mcpBlocks(grants, github, "view")).toBe(true);
    expect(mcpBlocks(grants, github, "use")).toBe(false);
  });

  it("reads an unrestricted grant as the All servers row", () => {
    const grants = load([{ scope: "mcp:connect" }]);
    expect(mcpAllows(grants, all, "use")).toBe(true);
    expect(mcpToolLimit(grants, all)).toEqual({ kind: "all" });
  });

  it("reads a server named with its project as that server", () => {
    const grants = load([
      { scope: "mcp:connect", selectors: [mcp("gh", { projectId: "p1" })] },
    ]);
    expect(mcpAllows(grants, github, "use")).toBe(true);
    expect(otherMcpRules(grants, [all, project, github])).toEqual([]);
  });

  it("toggles a cell without touching the rest of the role", () => {
    const before = load([
      {
        scope: "mcp:connect",
        selectors: [mcp("lin"), mcp("*", { tool: "x" })],
      },
    ]);
    const after = setMcpAllow(before, github, "use", true);
    expect(save(after)).toEqual([
      {
        scope: "mcp:connect",
        selectors: [mcp("lin"), mcp("*", { tool: "x" }), mcp("gh")],
      },
    ]);
    const back = setMcpAllow(after, github, "use", false);
    expect(grantKeysString(back)).toBe(grantKeysString(before));
  });

  it("takes a server's tool limit with its Use cell", () => {
    const grants = load([
      {
        scope: "mcp:connect",
        selectors: [mcp("gh", { tool: "a" }), mcp("gh", { tool: "b" })],
      },
    ]);
    expect(mcpAllows(grants, github, "use")).toBe(true);
    expect(mcpToolLimit(grants, github)).toEqual({
      kind: "tools",
      tools: ["a", "b"],
    });
    expect(save(setMcpAllow(grants, github, "use", false))).toEqual([]);
  });

  it("limits Use on All servers by annotation", () => {
    const grants = setMcpToolLimit(load([{ scope: "mcp:connect" }]), all, {
      kind: "annotations",
      dispositions: ["read_only"],
    });
    expect(save(grants)).toEqual([
      {
        scope: "mcp:connect",
        selectors: [mcp("*", { disposition: "read_only" })],
      },
    ]);
    expect(mcpAllows(grants, all, "use")).toBe(true);
  });

  it("blocks every level on a row and takes back what it granted", () => {
    const grants = setMcpRowBlocked(
      load([{ scope: "mcp:write", selectors: [mcp("gh"), mcp("lin")] }]),
      github,
      true,
    );
    expect(save(grants)).toEqual([
      { scope: "mcp:write", selectors: [mcp("lin")] },
      { scope: "mcp:blocked_write", selectors: [mcp("gh")] },
      { scope: "mcp:blocked_connect", selectors: [mcp("gh")] },
      { scope: "mcp:blocked_read", selectors: [mcp("gh")] },
    ]);
    expect(save(setMcpRowBlocked(grants, github, false))).toEqual([
      { scope: "mcp:write", selectors: [mcp("lin")] },
    ]);
  });

  it("splits a broader Use to limit tools on one server", () => {
    const other = { kind: "server", serverId: "x", projectId: "p2" } as const;
    const servers = [github, linear, other];
    const grants = splitMcpToolLimit(
      load([{ scope: "mcp:connect" }]),
      github,
      { kind: "tools", tools: ["a"] },
      servers,
    );
    expect(mcpAllows(grants, all, "use")).toBe(false);
    expect(mcpAllows(grants, { kind: "project", projectId: "p2" }, "use")).toBe(
      true,
    );
    expect(mcpToolLimit(grants, linear)).toEqual({ kind: "all" });
    expect(mcpToolLimit(grants, github)).toEqual({
      kind: "tools",
      tools: ["a"],
    });
  });

  it("carries an annotation limit to each server when splitting", () => {
    const servers = [github, linear];
    const start = setMcpToolLimit(load([{ scope: "mcp:connect" }]), all, {
      kind: "annotations",
      dispositions: ["read_only"],
    });
    const grants = splitMcpToolLimit(
      start,
      github,
      { kind: "tools", tools: ["a"] },
      servers,
    );
    expect(mcpAllows(grants, all, "use")).toBe(false);
    expect(mcpToolLimit(grants, linear)).toEqual({
      kind: "annotations",
      dispositions: ["read_only"],
    });
  });

  it("lists rows it has no cell for, and removes just that row", () => {
    const grants = load([
      {
        scope: "mcp:connect",
        selectors: [
          mcp("*", { tool: "search" }),
          mcp("gone"),
          mcp("gh"),
          mcp("gh", { tool: "x", disposition: "read_only" }),
        ],
      },
      { scope: "mcp:blocked_connect", selectors: [mcp("gh", { tool: "rm" })] },
    ]);
    const other = otherMcpRules(grants, [all, project, github]);
    expect(other.map((r) => [r.level, r.blocked, r.selector])).toEqual([
      ["use", false, mcp("gone")],
      ["use", false, mcp("*", { tool: "search" })],
      ["use", false, mcp("gh", { tool: "x", disposition: "read_only" })],
    ]);
    const after = removeOtherMcpRule(grants, other[0]!);
    expect(
      otherMcpRules(after, [all, project, github]).map((r) => r.selector),
    ).toEqual([
      mcp("*", { tool: "search" }),
      mcp("gh", { tool: "x", disposition: "read_only" }),
    ]);
    expect(mcpAllows(after, github, "use")).toBe(true);
  });

  it("splits a wider grant to leave one row out", () => {
    const other: McpRow = { kind: "project", projectId: "p2" };
    const remote: McpRow = { kind: "server", serverId: "rem", projectId: "p2" };
    const rows = [all, project, github, linear, other, remote];
    const everywhere = load([{ scope: "mcp:read" }]);

    // A project: every other project keeps the level.
    const noProject = excludeMcpRow(everywhere, project, "view", rows);
    expect(mcpAllows(noProject, all, "view")).toBe(false);
    expect(mcpAllows(noProject, project, "view")).toBe(false);
    expect(mcpAllows(noProject, other, "view")).toBe(true);
    expect(mcpAllows(noProject, github, "view")).toBe(false);

    // A server: other projects and its sibling servers keep the level.
    const noServer = excludeMcpRow(everywhere, github, "view", rows);
    expect(mcpAllows(noServer, all, "view")).toBe(false);
    expect(mcpAllows(noServer, project, "view")).toBe(false);
    expect(mcpAllows(noServer, github, "view")).toBe(false);
    expect(mcpAllows(noServer, linear, "view")).toBe(true);
    expect(mcpAllows(noServer, other, "view")).toBe(true);

    // A limited Connect on every server is carried to a server that already
    // has a narrower one of its own, on top of it.
    const limited = load([
      {
        scope: "mcp:connect",
        selectors: [
          mcp("*", { disposition: "read_only" }),
          mcp("rem", { tool: "x" }),
        ],
      },
    ]);
    const carried = excludeMcpRow(limited, project, "use", rows);
    expect(
      sdkGrantsFromForm(carried, scopes).find((g) => g.scope === "mcp:connect")
        ?.selectors,
    ).toEqual(
      expect.arrayContaining([
        mcp("rem", { tool: "x" }),
        mcp("rem", { disposition: "read_only" }),
      ]),
    );
    expect(mcpAllows(carried, github, "use")).toBe(false);

    // Connect on a project, taken off one of its servers.
    const connect = load([
      { scope: "mcp:connect", selectors: [mcp("*", { projectId: "p1" })] },
    ]);
    const noConnect = excludeMcpRow(connect, github, "use", rows);
    expect(mcpAllows(noConnect, project, "use")).toBe(false);
    expect(mcpAllows(noConnect, github, "use")).toBe(false);
    expect(mcpAllows(noConnect, linear, "use")).toBe(true);
  });

  it("denies one tool of a server without touching its grants", () => {
    const grants = load([{ scope: "mcp:connect", selectors: [mcp("gh")] }]);
    const denied = setMcpToolBlocked(
      grants,
      github as McpRow & { kind: "server" },
      "rm",
      true,
    );
    expect(mcpBlockedTools(denied, github)).toEqual(["rm"]);
    expect(mcpAllows(denied, github, "use")).toBe(true);
    expect(mcpRowDenied(denied, github)).toBe(false);
    // The tool has its own row, so it is not an "other rule".
    expect(otherMcpRules(denied, [all, project, github])).toEqual([]);
    expect(
      sdkGrantsFromForm(denied, scopes).find(
        (g) => g.scope === "mcp:blocked_connect",
      )?.selectors,
    ).toEqual([mcp("gh", { tool: "rm" })]);
    const back = setMcpToolBlocked(
      denied,
      github as McpRow & { kind: "server" },
      "rm",
      false,
    );
    expect(mcpBlockedTools(back, github)).toEqual([]);
  });

  it("lists a denied row, a denied tool and a block it has no row for", () => {
    let grants = load([
      { scope: "mcp:blocked_connect", selectors: [mcp("gone")] },
    ]);
    grants = setMcpRowBlocked(grants, linear, true);
    grants = setMcpToolBlocked(
      grants,
      github as McpRow & { kind: "server" },
      "rm",
      true,
    );
    expect(mcpRowDenied(grants, linear)).toBe(true);
    const rows = [all, project, github, linear];
    expect(roleDenies(grants, rows, []).map((d) => d.kind)).toEqual([
      "mcp-tool",
      "mcp-row",
      "mcp-other",
    ]);
    // Until the servers are known nothing can be called missing.
    expect(roleDenies(grants, null, [])).toEqual([]);
  });
});

describe("platform areas", () => {
  it("groups by catalogue category, pairing View with what includes it", () => {
    const areas = platformAreas(scopes);
    expect(
      areas.map((a) => [
        a.label,
        a.view?.slug,
        a.manage?.slug,
        a.extras.map((e) => e.slug),
      ]),
    ).toEqual([
      ["Organization", "org:read", "org:admin", ["org:hooks_ingest"]],
      ["Projects", "project:read", "project:write", []],
      ["Plugins", undefined, undefined, ["plugin:write"]],
      ["Agent sessions", "chat:read", "chat:write", []],
    ]);
  });

  it("names an unknown category after itself", () => {
    const [area] = platformAreas([
      def("widget:read", "widget", undefined, {
        category: "widget_kind" as ScopeDefinition["category"],
      }),
    ]);
    expect(area?.label).toBe("Widget kind");
  });

  it("reads which levels the MCP permissions include from the catalogue", () => {
    // Manage includes View; neither includes Connect.
    expect(mcpLevelIncludes(scopes, "manage")).toEqual(["view"]);
    expect(mcpLevelIncludes(scopes, "view")).toEqual([]);
    expect(mcpLevelIncludes(scopes, "use")).toEqual([]);
  });

  it("blocks a single permission everywhere and takes it back", () => {
    const blocked = setPlatformScopeBlocked(
      load([{ scope: "plugin:write" }]),
      "plugin:write" as Scope,
      true,
    );
    expect(save(blocked)).toEqual([
      { scope: "plugin:blocked_write", selectors: undefined },
    ]);
    expect(platformBlock(blocked, "plugin:write")).toBe("all");
    expect(
      save(setPlatformScopeBlocked(blocked, "plugin:write" as Scope, false)),
    ).toEqual([]);
  });

  it("grants everywhere and keeps a block beside it", () => {
    const grants = load([
      {
        scope: "project:blocked_read",
        selectors: [{ resourceKind: "project", resourceId: "p1" }],
      },
    ]);
    const after = setPlatformAllow(grants, "project:read" as Scope, true);
    expect(save(after)).toEqual([
      { scope: "project:read", selectors: undefined },
      {
        scope: "project:blocked_read",
        selectors: [{ resourceKind: "project", resourceId: "p1" }],
      },
    ]);
    expect(platformBlock(after, "project:read")).toBe("some");
  });

  it("blocks on View alone and clears both on unblock", () => {
    const area = platformAreas(scopes).find((a) => a.label === "Projects")!;
    const blocked = setPlatformBlocked(
      load([{ scope: "project:write" }]),
      area,
      true,
    );
    expect(save(blocked)).toEqual([
      { scope: "project:blocked_read", selectors: undefined },
    ]);
    expect(platformBlock(blocked, "project:read")).toBe("all");
    expect(save(setPlatformBlocked(blocked, area, false))).toEqual([]);
  });
});

describe("the catalogue as the server serves it", () => {
  // listScopes.fixture.json is written by the server's
  // TestListScopes_DashboardFixture, so these run against the real catalogue.
  const served = ListScopesResult$inboundSchema.parse(servedCatalogue).scopes;
  const evaluate = {
    scope: "risk_policy:evaluate" as Scope,
    selectors: [{ resourceKind: "risk_policy", resourceId: "policy-1" }],
  };

  it("renders no row for risk policy permissions, which role writes reject", () => {
    const areas = platformAreas(served);
    expect(areas.map((a) => a.category)).not.toContain("risk_policy");
    const slugs = areas.flatMap((a) =>
      [a.view, a.manage, ...a.extras].flatMap((d) => (d ? [d.slug] : [])),
    );
    expect(slugs.filter((slug) => slug.startsWith("risk_policy:"))).toEqual([]);
    expect(slugs).toContain("log:read");
  });

  it("keeps a role's risk policy grants through Reset and Save", () => {
    const stored = grantsFromRole(
      {
        grants: [evaluate, { scope: "project:read" as Scope }],
      } as unknown as Parameters<typeof grantsFromRole>[0],
      served,
    );
    const reset = clearPlatform(stored, platformAreas(served));
    expect(sdkGrantsFromForm(reset, served)).toEqual([evaluate]);
    expect(
      diffGrants(
        sdkGrantsFromForm(stored, served),
        sdkGrantsFromForm(reset, served),
      ),
    ).toEqual({
      addGrants: [],
      removeGrants: [{ scope: "project:read", selectors: undefined }],
    });
  });
});

describe("clearing a table", () => {
  it("empties the MCP grid and leaves the platform alone", () => {
    const grants = load([
      { scope: "mcp:connect", selectors: [mcp("gh", { tool: "a" })] },
      { scope: "mcp:blocked_read", selectors: [mcp("lin")] },
      { scope: "org:read" },
    ]);
    expect(save(clearMcp(grants))).toEqual([
      { scope: "org:read", selectors: undefined },
    ]);
  });

  it("empties the platform grid, extras included", () => {
    const grants = load([
      { scope: "project:read" },
      { scope: "org:blocked_read" },
      { scope: "org:hooks_ingest" },
      { scope: "mcp:connect" },
    ]);
    expect(
      save(clearPlatform(grants, platformAreas(scopes))).map((g) => g.scope),
    ).toEqual(["mcp:connect"]);
  });
});

describe("editing without losing rules", () => {
  const scoped = { resourceKind: "project", resourceId: "p2" } as Selector;

  it("keeps annotation limits beside tool limits on one server", () => {
    const grants = load([
      {
        scope: "mcp:connect",
        selectors: [
          mcp("gh", { tool: "a" }),
          mcp("gh", { disposition: "read_only" }),
        ],
      },
    ]);
    expect(mcpToolLimit(grants, github)).toEqual({
      kind: "tools",
      tools: ["a"],
    });
    expect(otherMcpRules(grants, [all, project, github])).toHaveLength(1);
    const after = setMcpToolLimit(grants, github, {
      kind: "tools",
      tools: ["b"],
    });
    expect(save(after)).toEqual([
      {
        scope: "mcp:connect",
        selectors: [
          mcp("gh", { disposition: "read_only" }),
          mcp("gh", { tool: "b" }),
        ],
      },
    ]);
  });

  it("splits a broader Use even when the server has its own", () => {
    const grants = load([
      {
        scope: "mcp:connect",
        selectors: [mcp("*", { projectId: "p1" }), mcp("gh")],
      },
    ]);
    const after = limitMcpUse(grants, github, { kind: "tools", tools: ["a"] }, [
      github,
      linear,
    ]);
    expect(mcpAllows(after, project, "use")).toBe(false);
    expect(mcpAllows(after, linear, "use")).toBe(true);
    expect(mcpToolLimit(after, github)).toEqual({
      kind: "tools",
      tools: ["a"],
    });
  });

  it("keeps blocks on some projects through the Blocked column", () => {
    const projects = platformAreas(scopes).find(
      (a) => a.resourceType === "project",
    )!;
    const grants = load([
      { scope: "project:read" },
      { scope: "project:blocked_read", selectors: [scoped] },
    ]);
    const blocked = setPlatformBlocked(grants, projects, true);
    expect(platformBlock(blocked, "project:read")).toBe("all");
    const unblocked = setPlatformBlocked(blocked, projects, false);
    expect(platformBlock(unblocked, "project:read")).toBe("some");
  });

  it("moves an area between levels, keeping where it applies", () => {
    const projects = platformAreas(scopes).find(
      (a) => a.resourceType === "project",
    )!;
    const grants = load([{ scope: "project:read", selectors: [scoped] }]);
    expect(platformLevel(grants, projects)).toBe("view");
    const managed = setPlatformLevel(grants, projects, "manage");
    expect(platformLevel(managed, projects)).toBe("manage");
    expect(managed["project:read"]).toBeUndefined();
    expect(managed["project:write"]?.rules.map((r) => r.selectors)).toEqual([
      [scoped],
    ]);
    const none = setPlatformLevel(managed, projects, "none");
    expect(platformLevel(none, projects)).toBe("none");
    expect(none["project:write"]).toBeUndefined();
    expect(
      setPlatformLevel(none, projects, "view")["project:read"]?.rules.map(
        (r) => r.selectors,
      ),
    ).toEqual([null]);
  });

  it("denies an area as a whole and lists it once", () => {
    const areas = platformAreas(scopes);
    const projects = areas.find((a) => a.resourceType === "project")!;
    const plugins = areas.find((a) => a.category === "plugin")!;
    let grants = load([{ scope: "project:write" }, { scope: "plugin:write" }]);
    expect(platformAreaBlockable(projects)).toBe(true);
    grants = setPlatformAreaDenied(grants, projects, true);
    grants = setPlatformAreaDenied(grants, plugins, true);
    expect(platformAreaDenied(grants, projects)).toBe(true);
    expect(platformAreaDenied(grants, plugins)).toBe(true);
    expect(platformLevel(grants, projects)).toBe("none");
    expect(roleDenies(grants, [], areas).map((d) => [d.kind, d.key])).toEqual([
      ["area", "area:project"],
      ["area", "area:plugin"],
    ]);
    grants = setPlatformAreaDenied(grants, projects, false);
    expect(platformAreaDenied(grants, projects)).toBe(false);
  });

  it("denies an area's single permissions along with its levels", () => {
    const [area] = platformAreas([
      def("skill:read", "skill", "skill:blocked_read"),
      def("skill:run", "skill", "skill:blocked_run"),
      def("skill:share", "skill"),
    ]);
    let grants: Record<string, RoleGrant> = {};
    for (const scope of ["skill:read", "skill:run", "skill:share"]) {
      grants = setPlatformAllow(grants, scope as Scope, true);
    }
    const denied = setPlatformAreaDenied(grants, area!, true);
    expect(platformAreaDenied(denied, area!)).toBe(true);
    // What can be blocked is blocked and no longer granted.
    expect(platformBlock(denied, "skill:run")).toBe("all");
    expect(platformAllows(denied, "skill:run")).toBe(false);
    // What cannot be blocked is left as it was.
    expect(platformAllows(denied, "skill:share")).toBe(true);
    expect(roleDenies(denied, [], [area!]).map((d) => d.kind)).toEqual([
      "area",
    ]);
    const back = setPlatformAreaDenied(denied, area!, false);
    expect(platformBlock(back, "skill:run")).toBeNull();
  });

  it("lists a block on some projects as its own deny", () => {
    const areas = platformAreas(scopes);
    const grants = load([
      { scope: "project:read" },
      { scope: "project:blocked_read", selectors: [scoped] },
    ]);
    expect(roleDenies(grants, [], areas).map((d) => d.kind)).toEqual(["rule"]);
  });

  it("unblocks one level without touching the other", () => {
    const grants = load([
      { scope: "project:blocked_read", selectors: [scoped] },
      { scope: "project:blocked_write" },
    ]);
    const after = unblockPlatformScope(grants, "project:write" as Scope);
    expect(platformBlock(after, "project:write")).toBeNull();
    expect(platformBlock(after, "project:read")).toBe("some");
  });
});
