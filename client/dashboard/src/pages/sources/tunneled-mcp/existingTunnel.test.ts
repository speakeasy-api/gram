import { ServiceError } from "@gram/client/models/errors/serviceerror.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { describe, expect, it, vi } from "vitest";
import {
  addMcpServerOnTunnelHref,
  deleteTunnelAndConfirmedServers,
  isDefiniteRejection,
  TunnelDeleteIncompleteError,
  TunnelServersChangedError,
} from "./existingTunnel";

function serviceError(
  status: number,
  overrides: { timeout?: boolean; fault?: boolean } = {},
): ServiceError {
  const body = JSON.stringify({ message: `status ${status}` });
  return new ServiceError(
    {
      name: "test",
      message: `status ${status}`,
      id: "err",
      fault: overrides.fault ?? false,
      temporary: false,
      timeout: overrides.timeout ?? false,
    },
    {
      response: new Response(body, { status }),
      request: new Request("http://localhost/rpc"),
      body,
    },
  );
}

function server(id: string): McpServer {
  return { id, tunneledMcpServerId: "tunnel-1" } as McpServer;
}

function deps(listed: string[]) {
  return {
    listLinked: vi.fn(async () => listed.map(server)),
    deleteMcpServer: vi.fn(async (_id: string) => undefined as unknown),
    deleteTunnel: vi.fn(async () => undefined as unknown),
  };
}

describe("isDefiniteRejection", () => {
  it("treats an answered 4xx as a refusal", () => {
    expect(isDefiniteRejection(serviceError(400))).toBe(true);
    expect(isDefiniteRejection(serviceError(403))).toBe(true);
    expect(isDefiniteRejection(serviceError(409))).toBe(true);
  });

  it("treats timeouts, faults, 5xx and transport errors as uncertain", () => {
    expect(isDefiniteRejection(serviceError(408))).toBe(false);
    expect(isDefiniteRejection(serviceError(400, { timeout: true }))).toBe(
      false,
    );
    expect(isDefiniteRejection(serviceError(500, { fault: true }))).toBe(false);
    expect(isDefiniteRejection(serviceError(502))).toBe(false);
    expect(isDefiniteRejection(new TypeError("Failed to fetch"))).toBe(false);
  });
});

describe("addMcpServerOnTunnelHref", () => {
  it("preselects the tunnel on the create page", () => {
    expect(addMcpServerOnTunnelHref("/o/p/mcp/add/tunneled", "a b")).toBe(
      "/o/p/mcp/add/tunneled?tunnel=a%20b",
    );
  });
});

describe("deleteTunnelAndConfirmedServers", () => {
  it("deletes exactly the confirmed servers, then the tunnel", async () => {
    const d = deps(["a", "b"]);
    await deleteTunnelAndConfirmedServers({ confirmedIds: ["b", "a"], ...d });
    expect(d.deleteMcpServer.mock.calls.map(([id]) => id).sort()).toEqual([
      "a",
      "b",
    ]);
    expect(d.deleteTunnel).toHaveBeenCalledOnce();
  });

  it.each([
    ["a server was added", ["a", "b", "c"]],
    ["a confirmed server left the tunnel", ["a"]],
    ["a server was swapped", ["a", "c"]],
  ])("writes nothing when %s since review", async (_case, listed) => {
    const d = deps(listed);
    await expect(
      deleteTunnelAndConfirmedServers({ confirmedIds: ["a", "b"], ...d }),
    ).rejects.toBeInstanceOf(TunnelServersChangedError);
    expect(d.deleteMcpServer).not.toHaveBeenCalled();
    expect(d.deleteTunnel).not.toHaveBeenCalled();
  });

  it("keeps the tunnel and reports progress when a server delete fails", async () => {
    const d = deps(["a", "b"]);
    d.deleteMcpServer.mockImplementation(async (id: string) => {
      if (id === "b") throw serviceError(403);
    });
    const error = await deleteTunnelAndConfirmedServers({
      confirmedIds: ["a", "b"],
      ...d,
    }).catch((e: unknown) => e);
    expect(error).toBeInstanceOf(TunnelDeleteIncompleteError);
    expect((error as TunnelDeleteIncompleteError).progressed).toBe(true);
    expect((error as Error).message).toContain("Deleted 1 of 2");
    expect(d.deleteTunnel).not.toHaveBeenCalled();
  });

  it("reports no progress when every server delete was refused", async () => {
    const d = deps(["a"]);
    d.deleteMcpServer.mockRejectedValue(serviceError(403));
    const error = await deleteTunnelAndConfirmedServers({
      confirmedIds: ["a"],
      ...d,
    }).catch((e: unknown) => e);
    expect((error as TunnelDeleteIncompleteError).progressed).toBe(false);
  });

  it("counts a lost server delete response as possible progress", async () => {
    const d = deps(["a"]);
    d.deleteMcpServer.mockRejectedValue(new TypeError("Failed to fetch"));
    const error = await deleteTunnelAndConfirmedServers({
      confirmedIds: ["a"],
      ...d,
    }).catch((e: unknown) => e);
    expect((error as TunnelDeleteIncompleteError).progressed).toBe(true);
  });

  it("treats an already deleted server as done", async () => {
    const d = deps(["a"]);
    d.deleteMcpServer.mockRejectedValue(serviceError(404));
    await deleteTunnelAndConfirmedServers({ confirmedIds: ["a"], ...d });
    expect(d.deleteTunnel).toHaveBeenCalledOnce();
  });

  it("keeps a server added after the re-read: the tunnel delete is refused", async () => {
    const d = deps(["a"]);
    d.deleteTunnel.mockRejectedValue(serviceError(409));
    const error = await deleteTunnelAndConfirmedServers({
      confirmedIds: ["a"],
      ...d,
    }).catch((e: unknown) => e);
    expect(error).toBeInstanceOf(TunnelDeleteIncompleteError);
    expect((error as Error).message).toContain(
      "other MCP servers still use it",
    );
    expect(d.deleteMcpServer).toHaveBeenCalledTimes(1);
  });

  it("asks for a retry when a tunnel with no servers could not be confirmed deleted", async () => {
    const d = deps([]);
    d.deleteTunnel.mockRejectedValue(new TypeError("Failed to fetch"));
    const error = await deleteTunnelAndConfirmedServers({
      confirmedIds: [],
      ...d,
    }).catch((e: unknown) => e);
    expect((error as TunnelDeleteIncompleteError).progressed).toBe(false);
    expect((error as Error).message).toContain("Retry to finish");
  });
});
