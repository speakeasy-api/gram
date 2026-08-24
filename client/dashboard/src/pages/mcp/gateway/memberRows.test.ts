import { describe, expect, it } from "vitest";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { MetaMcpMember } from "@gram/client/models/components/metamcpmember.js";
import {
  buildMemberRows,
  classifyMemberServer,
  nextSortOrder,
  planReorder,
} from "./memberRows";

function server(overrides: Partial<McpServer> = {}): McpServer {
  return {
    id: "server-1",
    slug: "server-one",
    visibility: "private",
    ...overrides,
  } as McpServer;
}

function member(overrides: Partial<MetaMcpMember> = {}): MetaMcpMember {
  return {
    id: "member-1",
    mcpServerId: "server-1",
    sortOrder: 0,
    ...overrides,
  };
}

describe("classifyMemberServer", () => {
  it("classifies toolset-backed servers as hosted", () => {
    expect(classifyMemberServer(server({ toolsetId: "ts-1" }))).toBe("hosted");
  });

  it("classifies remote and tunneled servers as proxied", () => {
    expect(classifyMemberServer(server({ remoteMcpServerId: "r-1" }))).toBe(
      "proxied",
    );
    expect(classifyMemberServer(server({ tunneledMcpServerId: "t-1" }))).toBe(
      "proxied",
    );
  });

  it("classifies disabled servers as disabled whatever their backend", () => {
    expect(
      classifyMemberServer(
        server({ visibility: "disabled", toolsetId: "ts-1" }),
      ),
    ).toBe("disabled");
    expect(
      classifyMemberServer(
        server({ visibility: "disabled", remoteMcpServerId: "r-1" }),
      ),
    ).toBe("disabled");
  });

  it("classifies unproxied servers as unproxied even when toolset-backed", () => {
    expect(
      classifyMemberServer(
        server({ unproxiedMcpServerId: "u-1", toolsetId: "ts-1" }),
      ),
    ).toBe("unproxied");
  });

  it("classifies slugless servers as slugless — the runtime excludes them", () => {
    expect(
      classifyMemberServer(server({ slug: undefined, toolsetId: "ts-1" })),
    ).toBe("slugless");
  });

  it("returns unknown for a missing server or an unrecognised backend", () => {
    expect(classifyMemberServer(undefined)).toBe("unknown");
    expect(classifyMemberServer(server())).toBe("unknown");
  });
});

describe("buildMemberRows", () => {
  it("preserves the API's order and joins the backing server", () => {
    const rows = buildMemberRows(
      [
        member({ id: "a", mcpServerId: "s1", sortOrder: 0 }),
        member({ id: "b", mcpServerId: "s2", sortOrder: 1 }),
      ],
      [
        server({ id: "s1", toolsetId: "ts-1" }),
        server({ id: "s2", remoteMcpServerId: "r-1" }),
      ],
    );
    expect(rows.map((row) => row.member.id)).toEqual(["a", "b"]);
    expect(rows.map((row) => row.classification)).toEqual([
      "hosted",
      "proxied",
    ]);
  });

  // The runtime orders by (sort_order, created_at, id) and the API returns
  // that order; re-sorting on sortOrder alone would reshuffle ties into an
  // order list_servers never serves.
  it("does not reshuffle members that share a sortOrder", () => {
    const rows = buildMemberRows(
      [
        member({ id: "b", mcpServerId: "s2", sortOrder: 0 }),
        member({ id: "a", mcpServerId: "s1", sortOrder: 0 }),
      ],
      [],
    );
    expect(rows.map((row) => row.member.mcpServerId)).toEqual(["s2", "s1"]);
  });

  it("keeps a member whose server is missing, classified unknown", () => {
    const rows = buildMemberRows([member({ mcpServerId: "gone" })], []);
    expect(rows).toHaveLength(1);
    expect(rows[0]!.server).toBeUndefined();
    expect(rows[0]!.classification).toBe("unknown");
  });
});

describe("planReorder", () => {
  const members = [
    member({ id: "a", sortOrder: 0 }),
    member({ id: "b", sortOrder: 1 }),
    member({ id: "c", sortOrder: 2 }),
  ];

  it("writes only the rows whose position actually changed", () => {
    expect(planReorder(members, 2, 1)).toEqual([
      { id: "c", sortOrder: 1 },
      { id: "b", sortOrder: 2 },
    ]);
  });

  it("renumbers every row when the list starts with duplicate sortOrders", () => {
    const flat = [
      member({ id: "a", sortOrder: 0 }),
      member({ id: "b", sortOrder: 0 }),
      member({ id: "c", sortOrder: 0 }),
    ];
    expect(planReorder(flat, 0, 2)).toEqual([
      { id: "c", sortOrder: 1 },
      { id: "a", sortOrder: 2 },
    ]);
  });

  it("is a no-op for a move that goes nowhere or off the ends", () => {
    expect(planReorder(members, 1, 1)).toEqual([]);
    expect(planReorder(members, 0, -1)).toEqual([]);
    expect(planReorder(members, 2, 3)).toEqual([]);
  });
});

describe("nextSortOrder", () => {
  it("places a new member after the last one", () => {
    expect(
      nextSortOrder([member({ sortOrder: 0 }), member({ sortOrder: 4 })]),
    ).toBe(5);
  });

  it("starts at 0 for an empty gateway", () => {
    expect(nextSortOrder([])).toBe(0);
  });
});
