import type { Plugin } from "@gram/client/models/components/plugin.js";
import { describe, expect, it } from "vitest";
import {
  matchesPluginFilters,
  pluginServerFilterOptions,
} from "./plugins-filter-schema";

describe("pluginServerFilterOptions", () => {
  it("lists a hosted server once whichever key its entries use", () => {
    const plugins = [
      {
        id: "legacy-plugin",
        servers: [{ toolsetId: "toolset-1", displayName: "Billing" }],
      },
      {
        id: "server-plugin",
        servers: [{ mcpServerId: "toolset-1", displayName: "Billing" }],
      },
    ] as unknown as Plugin[];

    const options = pluginServerFilterOptions(plugins).servers;
    expect(options).toEqual([
      { value: "mcpServer:toolset-1", label: "Billing" },
    ]);
    for (const plugin of plugins) {
      expect(
        matchesPluginFilters(plugin, { servers: ["mcpServer:toolset-1"] }),
      ).toBe(true);
    }
  });

  it("matches a saved legacy toolset key", () => {
    const plugin = {
      id: "server-plugin",
      servers: [{ mcpServerId: "toolset-1", displayName: "Billing" }],
    } as unknown as Plugin;

    expect(
      matchesPluginFilters(plugin, { servers: ["toolset:toolset-1"] }),
    ).toBe(true);
  });

  it("matches either saved key for an entry that reports both ids", () => {
    const plugin = {
      id: "both-plugin",
      servers: [
        {
          toolsetId: "toolset-1",
          mcpServerId: "toolset-1",
          displayName: "Billing",
        },
      ],
    } as unknown as Plugin;

    for (const saved of ["toolset:toolset-1", "mcpServer:toolset-1"]) {
      expect(matchesPluginFilters(plugin, { servers: [saved] })).toBe(true);
    }
  });
});
