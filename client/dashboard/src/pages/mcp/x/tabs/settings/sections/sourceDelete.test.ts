import { describe, expect, it, vi } from "vitest";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import type { UnproxiedMcpServer } from "@gram/client/models/components/unproxiedmcpserver.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import {
  deleteSourceCascade,
  failedLinkedDeletes,
  linkedMcpServersFilter,
  serversBackedBySameSource,
  serversMatchingFilter,
  sourceDeleteSpec,
} from "./sourceDelete";

const now = new Date("2026-09-16T00:00:00Z");

function mcpServer(overrides: Partial<McpServer>): McpServer {
  return {
    id: "srv-1",
    projectId: "proj",
    createdAt: now,
    updatedAt: now,
    networkAccessMode: "public",
    visibility: "private",
    ...overrides,
  } as McpServer;
}

const remote: RemoteMcpServer = {
  id: "remote-1",
  projectId: "proj",
  url: "https://example.com/mcp",
  transportType: "streamable-http",
  createdAt: now,
  updatedAt: now,
};

const unproxied: UnproxiedMcpServer = {
  id: "unproxied-1",
  projectId: "proj",
  url: "https://vendor.example.com/mcp",
  createdAt: now,
  updatedAt: now,
};

const tunneled = {
  id: "tunnel-1",
  projectId: "proj",
  name: "  Internal tools  ",
  keyPrefix: "tk_abc",
  allowPublic: false,
  connectionStatus: "never_connected",
  status: "created",
  activeConnectionCount: 0,
  activeConsumerSessionCount: 0,
  effectivePublicRequestBurst: 100,
  effectivePublicRequestRatePerSecond: 50,
  createdAt: now,
  updatedAt: now,
} as TunneledMcpServer;

describe("sourceDeleteSpec", () => {
  it("keys remote and unproxied confirmation on the URL", () => {
    expect(sourceDeleteSpec({ kind: "remote", source: remote })).toMatchObject({
      confirmLabel: "the server URL",
      confirmValue: remote.url,
    });
    expect(
      sourceDeleteSpec({ kind: "unproxied", source: unproxied }),
    ).toMatchObject({
      confirmLabel: "the server URL",
      confirmValue: unproxied.url,
    });
  });

  it("keys tunneled confirmation on the trimmed display name", () => {
    expect(
      sourceDeleteSpec({ kind: "tunneled", source: tunneled }),
    ).toMatchObject({
      confirmLabel: "the source name",
      confirmValue: "Internal tools",
    });
  });
});

describe("linkedMcpServersFilter", () => {
  it("returns null for hosted and gateway servers", () => {
    expect(linkedMcpServersFilter(mcpServer({ toolsetId: "ts" }))).toBeNull();
  });

  it("picks the filter matching the source kind", () => {
    expect(
      linkedMcpServersFilter(mcpServer({ remoteMcpServerId: "remote-1" })),
    ).toEqual({ remoteMcpServerId: "remote-1" });
    expect(
      linkedMcpServersFilter(mcpServer({ tunneledMcpServerId: "tunnel-1" })),
    ).toEqual({ tunneledMcpServerId: "tunnel-1" });
    expect(
      linkedMcpServersFilter(
        mcpServer({ unproxiedMcpServerId: "unproxied-1" }),
      ),
    ).toEqual({ unproxiedMcpServerId: "unproxied-1" });
  });
});

describe("serversBackedBySameSource", () => {
  const candidates = [
    mcpServer({ id: "a", remoteMcpServerId: "remote-1" }),
    mcpServer({ id: "b", remoteMcpServerId: "remote-1" }),
    mcpServer({ id: "c", remoteMcpServerId: "remote-2" }),
    mcpServer({ id: "d", tunneledMcpServerId: "tunnel-1" }),
  ];

  it("keeps only servers sharing the source, including the current one", () => {
    expect(
      serversBackedBySameSource(
        mcpServer({ id: "a", remoteMcpServerId: "remote-1" }),
        candidates,
      ).map((server) => server.id),
    ).toEqual(["a", "b"]);
  });

  it("drops candidates a stale unfiltered cache might include", () => {
    expect(
      serversBackedBySameSource(
        mcpServer({ id: "d", tunneledMcpServerId: "tunnel-1" }),
        candidates,
      ).map((server) => server.id),
    ).toEqual(["d"]);
  });

  it("is empty for servers without a source row", () => {
    expect(
      serversBackedBySameSource(mcpServer({ toolsetId: "ts" }), candidates),
    ).toEqual([]);
  });
});

describe("serversMatchingFilter", () => {
  it("matches on the field the filter names", () => {
    const candidates = [
      mcpServer({ id: "a", unproxiedMcpServerId: "unproxied-1" }),
      mcpServer({ id: "b", remoteMcpServerId: "remote-1" }),
    ];
    expect(
      serversMatchingFilter(
        { unproxiedMcpServerId: "unproxied-1" },
        candidates,
      ).map((server) => server.id),
    ).toEqual(["a"]);
  });
});

function httpError(statusCode: number): GramError {
  return new GramError(`status ${statusCode}`, {
    response: new Response(null, { status: statusCode }),
    request: new Request("https://gram.test/rpc"),
    body: "",
  });
}

describe("failedLinkedDeletes", () => {
  it("keeps rejections other than not-found, in id order", () => {
    const failures = failedLinkedDeletes(
      ["a", "b", "c", "d"],
      [
        { status: "fulfilled", value: undefined },
        { status: "rejected", reason: httpError(404) },
        { status: "rejected", reason: httpError(500) },
        { status: "rejected", reason: new Error("network") },
      ],
    );
    expect(failures.map((failure) => failure.id)).toEqual(["c", "d"]);
  });
});

describe("deleteSourceCascade", () => {
  const linked = [
    mcpServer({ id: "a", remoteMcpServerId: "remote-1" }),
    mcpServer({ id: "b", remoteMcpServerId: "remote-1" }),
  ];

  it("deletes the current linked set, then the source", async () => {
    const deleteMcpServer = vi.fn().mockResolvedValue(undefined);
    const deleteSource = vi.fn().mockResolvedValue(undefined);
    await deleteSourceCascade({
      listLinked: () => Promise.resolve(linked),
      deleteMcpServer,
      deleteSource,
      sourceLabel: "remote MCP source",
    });
    expect(deleteMcpServer.mock.calls.map(([id]) => id)).toEqual(["a", "b"]);
    expect(deleteSource).toHaveBeenCalledTimes(1);
  });

  it("treats an already-deleted wrapper as done", async () => {
    const deleteSource = vi.fn().mockResolvedValue(undefined);
    await deleteSourceCascade({
      listLinked: () => Promise.resolve(linked),
      deleteMcpServer: (id) =>
        id === "a" ? Promise.reject(httpError(404)) : Promise.resolve(),
      deleteSource,
      sourceLabel: "remote MCP source",
    });
    expect(deleteSource).toHaveBeenCalledTimes(1);
  });

  it("leaves the source in place and says so when a wrapper delete fails", async () => {
    const deleteSource = vi.fn().mockResolvedValue(undefined);
    await expect(
      deleteSourceCascade({
        listLinked: () => Promise.resolve(linked),
        deleteMcpServer: (id) =>
          id === "b" ? Promise.reject(new Error("boom")) : Promise.resolve(),
        deleteSource,
        sourceLabel: "remote MCP source",
      }),
    ).rejects.toThrow(
      "Deleted 1 of 2 linked MCP servers; 1 could not be deleted (boom). The remote MCP source was left in place. Retry to delete what remains.",
    );
    expect(deleteSource).not.toHaveBeenCalled();
  });

  it("names the source step when only the source delete fails", async () => {
    await expect(
      deleteSourceCascade({
        listLinked: () => Promise.resolve(linked),
        deleteMcpServer: () => Promise.resolve(),
        deleteSource: () => Promise.reject(new Error("forbidden")),
        sourceLabel: "unproxied MCP source",
      }),
    ).rejects.toThrow(
      "Every linked MCP server was deleted, but the unproxied MCP source was not: forbidden. Retry to finish.",
    );
  });
});
