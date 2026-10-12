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
});
