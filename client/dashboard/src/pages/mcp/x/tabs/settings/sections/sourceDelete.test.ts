import { describe, expect, it } from "vitest";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import type { UnproxiedMcpServer } from "@gram/client/models/components/unproxiedmcpserver.js";
import {
  linkedMcpServersFilter,
  serversBackedBySameSource,
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
