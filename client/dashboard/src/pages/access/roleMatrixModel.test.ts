import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import type { Selector } from "@gram/client/models/components/selector.js";
import { describe, expect, it } from "vitest";

import { grantKeysString } from "./roleDialogState";
import { grantsFromRole, sdkGrantsFromForm } from "./roleGrantTransform";
import {
  type McpRow,
  mcpAllows,
  mcpBlocks,
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
  splitMcpToolLimit,
  clearMcp,
  clearPlatform,
  limitMcpUse,
  unblockPlatformScope,
} from "./roleMatrixModel";
import type { RoleGrant } from "./types";

const def = (
  slug: string,
  resourceType: string,
  exclusionScope?: string,
): ScopeDefinition =>
  ({
    slug,
    resourceType,
    description: slug,
    agentEligible: true,
    visibility: "user_visible",
    exclusionScope,
  }) as ScopeDefinition;

const scopes: ScopeDefinition[] = [
  def("mcp:connect", "mcp", "mcp:blocked_connect"),
  def("mcp:read", "mcp", "mcp:blocked_read"),
  def("mcp:write", "mcp", "mcp:blocked_write"),
  def("org:read", "org", "org:blocked_read"),
  def("org:admin", "org", "org:blocked_admin"),
  def("org:hooks_ingest", "org"),
  def("project:read", "project", "project:blocked_read"),
  def("project:write", "project", "project:blocked_write"),
  def("plugin:write", "project", "plugin:blocked_write"),
  def("chat:read", "chat"),
  def("chat:write", "chat"),
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
      ["use", true, mcp("gh", { tool: "rm" })],
    ]);
    const after = removeOtherMcpRule(grants, other[3]!);
    expect(
      otherMcpRules(after, [all, project, github]).map((r) => r.blocked),
    ).toEqual([false, false, false]);
    expect(mcpAllows(after, github, "use")).toBe(true);
  });
});

describe("platform areas", () => {
  it("pairs read and write per area, and keeps the rest as extras", () => {
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
      ["Projects", "project:read", "project:write", ["plugin:write"]],
      ["Agent sessions", "chat:read", "chat:write", []],
    ]);
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
