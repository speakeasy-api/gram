import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import type { Selector } from "@gram/client/models/components/selector.js";
import { describe, expect, it } from "vitest";

import {
  adminCoverage,
  convertToolLimit,
  fuzzyMatch,
  parseMcpConnectGrant,
  serializeMcpConnectAccess,
  toolLimitBadges,
  withoutServer,
} from "./mcpAccessModel";
import { grantKeysString } from "./roleDialogState";
import { sdkGrantsFromForm } from "./roleGrantTransform";
import type { ServerGroup, ServerTool } from "./serverMerge";
import type { RoleGrant } from "./types";

const scopes = [
  {
    slug: "mcp:connect",
    resourceType: "mcp",
    description: "",
    agentEligible: true,
    visibility: "user_visible",
    exclusionScope: "mcp:blocked_connect",
  },
] as ScopeDefinition[];

function grant(allow: Selector[] | null, deny?: Selector[]): RoleGrant {
  const rules: RoleGrant["rules"] = [
    { id: "a", effect: "allow", selectors: allow },
  ];
  if (deny) rules.push({ id: "d", effect: "deny", selectors: deny });
  return { scope: "mcp:connect" as Scope, rules };
}

const server = (resourceId: string, extra: Partial<Selector> = {}) =>
  ({ resourceKind: "mcp", resourceId, ...extra }) as Selector;

function stored(g: RoleGrant | undefined) {
  return g ? sdkGrantsFromForm({ "mcp:connect": g }, scopes) : [];
}

describe("parseMcpConnectGrant", () => {
  it("reads full, tool and annotation limits per server", () => {
    const access = parseMcpConnectGrant(
      grant([
        server("a"),
        server("b", { tool: "search" }),
        server("b", { tool: "get" }),
        server("c", { disposition: "read_only" }),
      ]),
    );
    expect(access.allServers).toBeNull();
    expect(access.servers).toEqual({
      a: { kind: "all" },
      b: { kind: "tools", tools: ["search", "get"] },
      c: { kind: "annotations", dispositions: ["read_only"] },
    });
    expect(access.preservedAllow).toEqual([]);
  });

  it("reads an unrestricted grant as every server", () => {
    expect(parseMcpConnectGrant(grant(null)).allServers).toEqual({
      kind: "all",
    });
    expect(parseMcpConnectGrant(grant([server("*")])).allServers).toEqual({
      kind: "all",
    });
  });

  it("reads wildcard annotation rows as an annotation-limited all servers", () => {
    const access = parseMcpConnectGrant(
      grant([server("*", { disposition: "read_only" })]),
    );
    expect(access.allServers).toEqual({
      kind: "annotations",
      dispositions: ["read_only"],
    });
  });

  it("reads server-level exceptions as forbidden and keeps narrower ones", () => {
    const access = parseMcpConnectGrant(
      grant(null, [server("x"), server("y", { tool: "drop" })]),
    );
    expect(access.forbidden).toEqual(["x"]);
    expect(access.preservedDeny).toEqual([server("y", { tool: "drop" })]);
  });

  it("keeps rows it cannot show", () => {
    const projectWide = server("*", { projectId: "p1" });
    const toolAndDisposition = server("a", {
      tool: "t",
      disposition: "read_only",
    });
    const access = parseMcpConnectGrant(
      grant([projectWide, toolAndDisposition]),
    );
    expect(access.preservedAllow).toEqual([projectWide, toolAndDisposition]);
    expect(access.servers).toEqual({});
  });
});

describe("serializeMcpConnectAccess", () => {
  const roundTrips: [string, RoleGrant][] = [
    ["unrestricted", grant(null)],
    [
      "per-server limits",
      grant([
        server("a"),
        server("b", { tool: "search" }),
        server("c", { disposition: "read_only" }),
      ]),
    ],
    [
      "annotation-limited all servers",
      grant([server("*", { disposition: "read_only" })]),
    ],
    [
      "rows the view cannot show",
      grant(
        [server("*", { projectId: "p1" }), server("a")],
        [server("x"), server("y", { tool: "drop" })],
      ),
    ],
    [
      "a full row beside narrower rows for the same server",
      grant([
        server("c"),
        server("c", { disposition: "read_only" }),
        server("c", { tool: "search" }),
      ]),
    ],
    [
      "all servers plus a named server",
      grant([server("*", { disposition: "read_only" }), server("a")]),
    ],
  ];

  it.each(roundTrips)("stores %s exactly as read", (_, original) => {
    const after = serializeMcpConnectAccess(parseMcpConnectGrant(original));
    const sort = (grants: ReturnType<typeof stored>) =>
      grants.map((g) => ({
        ...g,
        selectors: g.selectors
          ? [...g.selectors].sort((x, y) =>
              JSON.stringify(x).localeCompare(JSON.stringify(y)),
            )
          : g.selectors,
      }));
    expect(sort(stored(after))).toEqual(sort(stored(original)));
    expect(grantKeysString({ "mcp:connect": after! })).toBe(
      grantKeysString({ "mcp:connect": original }),
    );
  });

  it("removes the scope when nothing is granted or forbidden", () => {
    expect(
      serializeMcpConnectAccess(parseMcpConnectGrant(undefined)),
    ).toBeUndefined();
  });

  it("keeps a forbidden-only grant as an exception", () => {
    const access = { ...parseMcpConnectGrant(undefined), forbidden: ["x"] };
    expect(stored(serializeMcpConnectAccess(access))).toEqual([
      { scope: "mcp:blocked_connect", selectors: [server("x")] },
    ]);
  });

  it("does not save named servers while all servers is chosen", () => {
    const access = {
      ...parseMcpConnectGrant(grant([server("a")])),
      allServers: { kind: "all" } as const,
    };
    expect(stored(serializeMcpConnectAccess(access))).toEqual([
      { scope: "mcp:connect", selectors: undefined },
    ]);
  });
});

describe("withoutServer", () => {
  it("drops the server's stored rows along with its limit", () => {
    const access = parseMcpConnectGrant(
      grant([
        server("c"),
        server("c", { disposition: "read_only" }),
        server("d"),
      ]),
    );
    const after = serializeMcpConnectAccess(withoutServer(access, "c"));
    expect(stored(after)).toEqual([
      { scope: "mcp:connect", selectors: [server("d")] },
    ]);
  });
});

describe("grantKeysString", () => {
  it("marks swapping one server for another as a change", () => {
    expect(grantKeysString({ "mcp:connect": grant([server("a")]) })).not.toBe(
      grantKeysString({ "mcp:connect": grant([server("b")]) }),
    );
  });
});

describe("adminCoverage", () => {
  const groups: ServerGroup[] = [
    {
      projectId: "p1",
      projectName: "Default",
      servers: [
        {
          id: "a",
          name: "A",
          slug: "a",
          tools: [],
          dynamicTools: false,
          remoteBacked: false,
        },
        {
          id: "b",
          name: "B",
          slug: "b",
          tools: [],
          dynamicTools: false,
          remoteBacked: false,
        },
      ],
    },
    {
      projectId: "p2",
      projectName: "Other",
      servers: [
        {
          id: "c",
          name: "C",
          slug: "c",
          tools: [],
          dynamicTools: false,
          remoteBacked: false,
        },
      ],
    },
  ];
  const adminGrant = (scope: string, selectors: Selector[] | null) => ({
    scope: scope as Scope,
    rules: [{ id: scope, effect: "allow" as const, selectors }],
  });

  it("covers named servers, project-wide rows and unrestricted grants", () => {
    expect(
      adminCoverage(
        {
          "mcp:read": adminGrant("mcp:read", [server("a")]),
          "mcp:write": adminGrant("mcp:write", [
            server("*", { projectId: "p2" }),
          ]),
        },
        groups,
      ),
    ).toEqual(
      new Map([
        ["a", "mcp:read"],
        ["c", "mcp:write"],
      ]),
    );
    expect(
      adminCoverage({ "mcp:read": adminGrant("mcp:read", null) }, groups).size,
    ).toBe(3);
  });
});

describe("tool limits", () => {
  const tools: ServerTool[] = [
    {
      id: "1",
      name: "search",
      type: "http",
      annotations: { readOnlyHint: true },
    },
    {
      id: "2",
      name: "delete",
      type: "http",
      annotations: { destructiveHint: true },
    },
    { id: "3", name: "plain", type: "http" },
  ];

  it("carries access across kinds where the tools allow", () => {
    expect(
      convertToolLimit(
        { kind: "annotations", dispositions: ["read_only"] },
        "tools",
        tools,
      ),
    ).toEqual({ kind: "tools", tools: ["search"] });
    expect(
      convertToolLimit(
        { kind: "tools", tools: ["delete"] },
        "annotations",
        tools,
      ),
    ).toEqual({ kind: "annotations", dispositions: ["destructive"] });
    expect(convertToolLimit({ kind: "all" }, "tools", tools)).toEqual({
      kind: "tools",
      tools: ["search", "delete", "plain"],
    });
  });

  it("summarizes limits as badges", () => {
    expect(toolLimitBadges({ kind: "all" })).toEqual([]);
    expect(toolLimitBadges({ kind: "tools", tools: ["a", "b", "c"] })).toEqual([
      "3 Tools",
    ]);
    expect(
      toolLimitBadges({
        kind: "annotations",
        dispositions: ["open_world", "read_only"],
      }),
    ).toEqual(["Read-Only Tools", "Open World Tools"]);
    expect(toolLimitBadges({ kind: "tools", tools: [] })).toEqual(["No Tools"]);
  });
});

describe("fuzzyMatch", () => {
  it("matches subsequences, ignoring case and spaces", () => {
    expect(fuzzyMatch("gdrv", "Google Drive")).toBe(true);
    expect(fuzzyMatch("GOOG dr", "Google Drive")).toBe(true);
    expect(fuzzyMatch("xyz", "Google Drive")).toBe(false);
    expect(fuzzyMatch("", "anything")).toBe(true);
  });
});
