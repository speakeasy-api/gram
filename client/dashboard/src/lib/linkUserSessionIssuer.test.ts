import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { unrecognized } from "@gram/client/types/unrecognized.js";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { linkToolsetUserSessionIssuer } from "./linkUserSessionIssuer";

const get = vi.fn();
const update = vi.fn();
const client = {
  mcpServers: { get, update },
} as unknown as Parameters<typeof linkToolsetUserSessionIssuer>[0];

const toolset = { id: "toolset-1", slug: "hosted-toolset" };

const canonical: McpServer = {
  id: "toolset-1",
  projectId: "project-1",
  toolsetId: "toolset-1",
  environmentId: "environment-1",
  toolVariationsGroupId: "variation-group-1",
  networkAccessMode: "dual",
  visibility: "public",
  createdAt: new Date(0),
  updatedAt: new Date(0),
};

function notFound(): GramError {
  return new GramError("mcp server not found", {
    response: new Response("{}", { status: 404 }),
    request: new Request("https://example.com"),
    body: "{}",
  });
}

beforeEach(() => {
  get.mockReset();
  update.mockReset().mockResolvedValue(undefined);
});

describe("linkToolsetUserSessionIssuer", () => {
  it("links through mcpServers.update with the full canonical server form", async () => {
    get.mockResolvedValue(canonical);

    await linkToolsetUserSessionIssuer(client, toolset, "issuer-1");

    expect(get).toHaveBeenCalledWith({ id: "toolset-1" }, undefined, undefined);
    expect(update).toHaveBeenCalledWith(
      {
        updateMcpServerForm: {
          id: "toolset-1",
          toolsetId: "toolset-1",
          environmentId: "environment-1",
          toolVariationsGroupId: "variation-group-1",
          visibility: "public",
          userSessionIssuerId: "issuer-1",
        },
      },
      undefined,
      undefined,
    );
    expect(update.mock.calls[0]![0].updateMcpServerForm).not.toHaveProperty(
      "networkAccessMode",
    );
  });

  it("refuses when the server visibility is unknown", async () => {
    get.mockResolvedValue({ ...canonical, visibility: unrecognized("future") });

    await expect(
      linkToolsetUserSessionIssuer(client, toolset, "issuer-1"),
    ).rejects.toThrow("not supported");
    expect(update).not.toHaveBeenCalled();
  });

  it("refuses when no canonical server exists", async () => {
    get.mockRejectedValue(notFound());

    await expect(
      linkToolsetUserSessionIssuer(client, toolset, "issuer-1"),
    ).rejects.toThrow("no hosted MCP server");
    expect(update).not.toHaveBeenCalled();
  });

  it("refuses when the server found is not the toolset's canonical one", async () => {
    get.mockResolvedValue({ ...canonical, toolsetId: "other-toolset" });

    await expect(
      linkToolsetUserSessionIssuer(client, toolset, "issuer-1"),
    ).rejects.toThrow("no hosted MCP server");
    expect(update).not.toHaveBeenCalled();
  });

  it("surfaces lookup failures other than not found", async () => {
    get.mockRejectedValue(new Error("network down"));

    await expect(
      linkToolsetUserSessionIssuer(client, toolset, "issuer-1"),
    ).rejects.toThrow("network down");
    expect(update).not.toHaveBeenCalled();
  });

  it("passes request options through", async () => {
    get.mockResolvedValue(canonical);
    const options = { fetchOptions: { signal: new AbortController().signal } };

    await linkToolsetUserSessionIssuer(client, toolset, "issuer-1", options);

    expect(get).toHaveBeenCalledWith({ id: "toolset-1" }, undefined, options);
    expect(update).toHaveBeenCalledWith(expect.anything(), undefined, options);
  });
});
