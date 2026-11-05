import {
  listedMcpServers,
  type ListedMcpServer,
} from "@/components/mcp/mcp-server-kind";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { Plugin } from "@gram/client/models/components/plugin.js";
import { describe, expect, it } from "vitest";
import { mcpServerFacets, pluginMembership } from "./mcp-filter-schema";

function listed(fields: Partial<McpServer>): ListedMcpServer {
  const [row] = listedMcpServers([
    {
      id: "server-1",
      visibility: "private",
      hasEndpoint: true,
      ...fields,
    } as McpServer,
  ]);
  if (!row) throw new Error("fixture has no backend");
  return row;
}

const plugins = [
  { id: "legacy-plugin", servers: [{ toolsetId: "toolset-1" }] },
  { id: "canonical-plugin", servers: [{ mcpServerId: "toolset-1" }] },
  { id: "wrapper-plugin", servers: [{ mcpServerId: "wrapper-1" }] },
] as unknown as Plugin[];

describe("mcpServerFacets", () => {
  it("credits a canonical wrapper with entries keyed by its toolset or its own id", () => {
    const facets = mcpServerFacets(
      listed({ id: "toolset-1", toolsetId: "toolset-1" }),
      pluginMembership(plugins),
    );
    expect(facets.pluginIds.sort()).toEqual([
      "canonical-plugin",
      "legacy-plugin",
    ]);
    expect(facets.serverId).toBe("toolset-1");
  });

  it("credits a non-canonical wrapper only with entries naming it", () => {
    const facets = mcpServerFacets(
      listed({ id: "wrapper-1", toolsetId: "toolset-1" }),
      pluginMembership(plugins),
    );
    expect(facets.pluginIds).toEqual(["wrapper-plugin"]);
    expect(facets.serverId).toBe("wrapper-1");
  });

  it.each([
    [{ toolsetId: "t", toolset: { registrySpecifier: "io/x" } }, "catalog"],
    [{ toolsetId: "t" }, "custom"],
    [{ remoteMcpServerId: "r" }, "remote"],
    [{ tunneledMcpServerId: "t" }, "tunneled"],
    [{ unproxiedMcpServerId: "u", toolsetId: "t" }, "unproxied"],
    [{ builtinBackend: "approvals" }, "built_in"],
  ])("derives the source of %o as %s", (fields, source) => {
    expect(
      mcpServerFacets(
        listed(fields as Partial<McpServer>),
        pluginMembership([]),
      ).source,
    ).toBe(source);
  });

  it("reads status from visibility", () => {
    const membership = pluginMembership([]);
    expect(
      mcpServerFacets(
        listed({ toolsetId: "t", visibility: "public" }),
        membership,
      ).status,
    ).toBe("public");
    expect(
      mcpServerFacets(
        listed({ toolsetId: "t", visibility: "disabled" }),
        membership,
      ).status,
    ).toBe("disabled");
  });
});
