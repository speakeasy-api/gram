import type { Plugin } from "@gram/client/models/components/plugin.js";
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import { describe, expect, it } from "vitest";
import { pluginMembership, toolsetFacets } from "./mcp-filter-schema";

describe("toolsetFacets", () => {
  it("counts hosted entries keyed by either the toolset or its canonical server", () => {
    const plugins = [
      { id: "legacy-plugin", servers: [{ toolsetId: "toolset-1" }] },
      { id: "server-plugin", servers: [{ mcpServerId: "toolset-1" }] },
    ] as unknown as Plugin[];
    const toolset = {
      id: "toolset-1",
      mcpEnabled: true,
      mcpIsPublic: false,
    } as unknown as ToolsetEntry;

    expect(toolsetFacets(toolset, pluginMembership(plugins)).pluginIds).toEqual(
      ["legacy-plugin", "server-plugin"],
    );
  });

  it("counts an entry keyed by the toolset's sole non-canonical wrapper", () => {
    const plugins = [
      { id: "wrapper-plugin", servers: [{ mcpServerId: "wrapper-1" }] },
    ] as unknown as Plugin[];
    const toolset = {
      id: "toolset-1",
      mcpEnabled: true,
      mcpIsPublic: false,
    } as unknown as ToolsetEntry;

    expect(
      toolsetFacets(
        toolset,
        pluginMembership(plugins),
        new Map([["toolset-1", "wrapper-1"]]),
      ).pluginIds,
    ).toEqual(["wrapper-plugin"]);
  });
});
