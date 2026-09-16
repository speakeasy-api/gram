import { describe, expect, it } from "vitest";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { MetaMcpMember } from "@gram/client/models/components/metamcpmember.js";
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import {
  buildAddCandidates,
  buildMemberRows,
  classifyMemberServer,
  memberBackendKind,
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

  it.each([
    "toolsetId",
    "remoteMcpServerId",
    "tunneledMcpServerId",
    "unproxiedMcpServerId",
  ] as const)("classifies a disabled %s server as disabled", (backend) => {
    expect(
      classifyMemberServer(
        server({ visibility: "disabled", [backend]: "b-1" }),
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

describe("memberBackendKind", () => {
  it("names each backend kind", () => {
    expect(memberBackendKind(server({ toolsetId: "ts-1" }))).toBe("hosted");
    expect(memberBackendKind(server({ remoteMcpServerId: "r-1" }))).toBe(
      "remote",
    );
    expect(memberBackendKind(server({ tunneledMcpServerId: "t-1" }))).toBe(
      "tunneled",
    );
  });

  it("returns undefined for servers with no backend and for missing servers", () => {
    expect(memberBackendKind(server())).toBeUndefined();
    expect(memberBackendKind(undefined)).toBeUndefined();
  });

  it("treats unproxied as kindless even when a backend id is present", () => {
    expect(
      memberBackendKind(
        server({ unproxiedMcpServerId: "u-1", toolsetId: "ts-1" }),
      ),
    ).toBeUndefined();
  });

  it("prefers hosted, then tunneled, over remote when multiple ids are set", () => {
    expect(
      memberBackendKind(
        server({ toolsetId: "ts-1", remoteMcpServerId: "r-1" }),
      ),
    ).toBe("hosted");
    expect(
      memberBackendKind(
        server({ tunneledMcpServerId: "t-1", remoteMcpServerId: "r-1" }),
      ),
    ).toBe("tunneled");
  });
});

describe("buildAddCandidates", () => {
  const toolset = (overrides: Partial<ToolsetEntry> = {}): ToolsetEntry =>
    ({
      id: "ts-1",
      name: "Linear",
      slug: "linear",
      mcpEnabled: true,
      ...overrides,
    }) as ToolsetEntry;

  it("offers toolsets that have no mcp_servers wrapper yet", () => {
    const candidates = buildAddCandidates(
      [server({ id: "s-1", name: "Linear David", slug: "linear-david" })],
      [toolset()],
      new Set(),
      "linear",
    );
    expect(candidates.map((c) => c.kind)).toEqual(["toolset", "server"]);
  });

  it("hides toolsets already represented by a wrapper row", () => {
    const candidates = buildAddCandidates(
      [server({ id: "s-1", name: "Linear", toolsetId: "ts-1" })],
      [toolset()],
      new Set(),
      "",
    );
    expect(candidates).toEqual([
      { kind: "server", server: expect.objectContaining({ id: "s-1" }) },
    ]);
  });

  it("drops servers that are already members and trims the search", () => {
    const candidates = buildAddCandidates(
      [server({ id: "s-1", name: "Linear" })],
      [toolset(), toolset({ id: "ts-2", name: "Other", slug: "other" })],
      new Set(["s-1"]),
      "  LINEAR ",
    );
    expect(candidates).toEqual([
      { kind: "toolset", toolset: expect.objectContaining({ id: "ts-1" }) },
    ]);
  });

  it("interleaves kinds by display name, treating a nameless server as empty", () => {
    const candidates = buildAddCandidates(
      [
        server({ id: "s-1", name: "Zulu" }),
        server({ id: "s-2", name: undefined, slug: "nameless" }),
      ],
      [
        toolset({ id: "ts-1", name: "Alpha" }),
        toolset({ id: "ts-2", name: "Mike" }),
      ],
      new Set(),
      "",
    );
    expect(
      candidates.map((c) =>
        c.kind === "server"
          ? `server:${c.server.id}`
          : `toolset:${c.toolset.id}`,
      ),
    ).toEqual(["server:s-2", "toolset:ts-1", "toolset:ts-2", "server:s-1"]);
  });
});

describe("addCandidateBatch", () => {
  it("attaches distinct existing wrappers sharing a toolset", async () => {
    const { addCandidateBatch, candidateKey } = await import("./memberRows");
    const candidates = ["first", "second"].map((id) => ({
      kind: "server" as const,
      server: server({ id, toolsetId: "tools" }),
    }));
    const state = {
      wrappers: new Map([["toolset-tools", "batch-wrapper"]]),
      orders: new Map<string, number>(),
      completed: new Set<string>(),
    };
    const calls: [string, number][] = [];
    const results = await addCandidateBatch(candidates, 0, state, {
      createWrapper: async () => {
        throw new Error("Unexpected creation");
      },
      attach: async (id, order) => {
        calls.push([id, order]);
      },
    });
    expect(
      candidates.map((candidate) => candidateKey(candidate, state.wrappers)),
    ).toEqual(["server-first", "server-second"]);
    expect(calls).toEqual([
      ["first", 0],
      ["second", 1],
    ]);
    expect(results.map((result) => result.error)).toEqual([
      undefined,
      undefined,
    ]);
  });

  it("continues after failures and retries without reminting wrappers or readding successes", async () => {
    const { addCandidateBatch, candidateKey, reconcileCompletedMembers } =
      await import("./memberRows");
    const hosted = {
      kind: "toolset" as const,
      toolset: { id: "tools", name: "Hosted" } as ToolsetEntry,
    };
    const remote = {
      kind: "server" as const,
      server: server({ id: "remote", name: "Remote" }),
    };
    const state = {
      wrappers: new Map<string, string>(),
      orders: new Map<string, number>(),
      completed: new Set<string>(),
    };
    let creates = 0;
    let fail = true;
    const calls: [string, number][] = [];
    const dependencies = {
      createWrapper: async () => {
        creates++;
        return "wrapper";
      },
      attach: async (id: string, order: number) => {
        calls.push([id, order]);
        if (id === "wrapper" && fail) throw new Error("Try again");
      },
    };
    const first = await addCandidateBatch(
      [hosted, remote],
      4,
      state,
      dependencies,
    );
    expect(first.map((r) => r.error)).toEqual(["Try again", undefined]);
    reconcileCompletedMembers(state, new Set(["remote"]));
    fail = false;
    const refreshed = {
      kind: "server" as const,
      server: server({ id: "wrapper", toolsetId: "tools" }),
    };
    await addCandidateBatch([refreshed, remote], 6, state, dependencies);
    expect(creates).toBe(1);
    expect(calls).toEqual([
      ["wrapper", 4],
      ["remote", 5],
      ["wrapper", 4],
    ]);
    expect(state.completed.has(candidateKey(hosted))).toBe(true);
    expect(candidateKey(refreshed, state.wrappers)).toBe(candidateKey(hosted));
  });

  it("retries wrapper creation only when creation failed", async () => {
    const { addCandidateBatch } = await import("./memberRows");
    const candidate = {
      kind: "toolset" as const,
      toolset: { id: "tools", name: "Hosted" } as ToolsetEntry,
    };
    const state = {
      wrappers: new Map<string, string>(),
      orders: new Map<string, number>(),
      completed: new Set<string>(),
    };
    let creates = 0;
    const dependencies = {
      createWrapper: async () => {
        if (++creates === 1) throw new Error("Creation failed");
        return "wrapper";
      },
      attach: async () => {},
    };
    expect(
      (await addCandidateBatch([candidate], 0, state, dependencies))[0]?.error,
    ).toBe("Creation failed");
    expect(
      (await addCandidateBatch([candidate], 0, state, dependencies))[0]?.error,
    ).toBeUndefined();
    expect(creates).toBe(2);
  });
});

describe("completed membership reconciliation", () => {
  it.each([false, true])(
    "reattaches an externally removed member (hosted: %s)",
    async (hosted) => {
      const { addCandidateBatch, reconcileCompletedMembers } =
        await import("./memberRows");
      const candidate = hosted
        ? {
            kind: "toolset" as const,
            toolset: { id: "tools", name: "Hosted" } as ToolsetEntry,
          }
        : { kind: "server" as const, server: server({ id: "remote" }) };
      const state = {
        wrappers: new Map<string, string>(),
        orders: new Map<string, number>(),
        completed: new Set<string>(),
      };
      let creates = 0;
      const calls: [string, number][] = [];
      const dependencies = {
        createWrapper: async () => {
          creates++;
          return "wrapper";
        },
        attach: async (id: string, order: number) => {
          calls.push([id, order]);
        },
      };
      const id = hosted ? "wrapper" : "remote";
      await addCandidateBatch([candidate], 0, state, dependencies);
      // No fresh membership yet: the old response must not undo protection.
      await addCandidateBatch([candidate], 0, state, dependencies);
      reconcileCompletedMembers(state, new Set([id]));
      await addCandidateBatch([candidate], 0, state, dependencies);
      expect(calls).toEqual([[id, 0]]);
      // Another tab removes it; the next successful response is authoritative.
      reconcileCompletedMembers(state, new Set());
      const refreshed = { kind: "server" as const, server: server({ id }) };
      const results = await addCandidateBatch(
        [refreshed],
        7,
        state,
        dependencies,
      );
      expect(results[0]?.error).toBeUndefined();
      expect(calls).toEqual([
        [id, 0],
        [id, 7],
      ]);
      expect(creates).toBe(hosted ? 1 : 0);
    },
  );
});
