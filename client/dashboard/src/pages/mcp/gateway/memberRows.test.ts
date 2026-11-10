// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { MetaMcpMember } from "@gram/client/models/components/metamcpmember.js";
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
  it("never offers a hosted server, wrapper or not", () => {
    const candidates = buildAddCandidates(
      [
        server({ id: "ts-1", name: "Own hosted", toolsetId: "ts-1" }),
        server({ id: "s-1", name: "Wrapper", toolsetId: "ts-1" }),
        server({ id: "s-2", name: "Remote", remoteMcpServerId: "r-1" }),
      ],
      new Set(),
      "",
    );
    expect(candidates.map((c) => c.id)).toEqual(["s-2"]);
  });

  it("drops servers that are already members and trims the search", () => {
    const candidates = buildAddCandidates(
      [
        server({ id: "s-1", name: "Linear" }),
        server({ id: "s-2", name: "Linear Two", slug: "linear-two" }),
        server({ id: "s-3", name: "Other", slug: "other" }),
      ],
      new Set(["s-1"]),
      "  LINEAR ",
    );
    expect(candidates.map((c) => c.id)).toEqual(["s-2"]);
  });

  it("sorts by display name, treating a nameless server as empty", () => {
    const candidates = buildAddCandidates(
      [
        server({ id: "s-1", name: "Zulu" }),
        server({ id: "s-2", name: undefined, slug: "nameless" }),
        server({ id: "s-3", name: "Alpha" }),
      ],
      new Set(),
      "",
    );
    expect(candidates.map((c) => c.id)).toEqual(["s-2", "s-3", "s-1"]);
  });
});
import {
  addCandidateBatch,
  candidateKey,
  reconcileCompletedMembers,
} from "./memberRows";

describe("addCandidateBatch", () => {
  it("attaches each candidate in order", async () => {
    const candidates = ["first", "second"].map((id) => server({ id }));
    const state = {
      orders: new Map<string, number>(),
      completed: new Set<string>(),
    };
    const calls: [string, number][] = [];
    const results = await addCandidateBatch(candidates, 0, state, {
      attach: async (id, order) => {
        calls.push([id, order]);
      },
    });
    expect(candidates.map((candidate) => candidateKey(candidate))).toEqual([
      "server-first",
      "server-second",
    ]);
    expect(calls).toEqual([
      ["first", 0],
      ["second", 1],
    ]);
    expect(results.map((result) => result.error)).toEqual([
      undefined,
      undefined,
    ]);
  });

  it("continues after failures and retries without readding successes", async () => {
    const flaky = server({ id: "flaky", name: "Flaky" });
    const remote = server({ id: "remote", name: "Remote" });
    const state = {
      orders: new Map<string, number>(),
      completed: new Set<string>(),
    };
    let fail = true;
    const calls: [string, number][] = [];
    const dependencies = {
      attach: async (id: string, order: number) => {
        calls.push([id, order]);
        if (id === "flaky" && fail) throw new Error("Try again");
      },
    };
    const first = await addCandidateBatch(
      [flaky, remote],
      4,
      state,
      dependencies,
    );
    expect(first.map((r) => r.error)).toEqual(["Try again", undefined]);
    reconcileCompletedMembers(state, new Set(["remote"]));
    fail = false;
    await addCandidateBatch([flaky, remote], 6, state, dependencies);
    expect(calls).toEqual([
      ["flaky", 4],
      ["remote", 5],
      ["flaky", 4],
    ]);
    expect(state.completed.has(candidateKey(flaky))).toBe(true);
  });
});

describe("completed membership reconciliation", () => {
  it("reattaches an externally removed member", async () => {
    const candidate = server({ id: "remote" });
    const state = {
      orders: new Map<string, number>(),
      completed: new Set<string>(),
    };
    const calls: [string, number][] = [];
    const dependencies = {
      attach: async (id: string, order: number) => {
        calls.push([id, order]);
      },
    };
    await addCandidateBatch([candidate], 0, state, dependencies);
    // No fresh membership yet: the old response must not undo protection.
    await addCandidateBatch([candidate], 0, state, dependencies);
    reconcileCompletedMembers(state, new Set(["remote"]));
    await addCandidateBatch([candidate], 0, state, dependencies);
    expect(calls).toEqual([["remote", 0]]);
    // Another tab removes it; the next successful response is authoritative.
    reconcileCompletedMembers(state, new Set());
    const results = await addCandidateBatch(
      [candidate],
      7,
      state,
      dependencies,
    );
    expect(results[0]?.error).toBeUndefined();
    expect(calls).toEqual([
      ["remote", 0],
      ["remote", 7],
    ]);
  });
});
