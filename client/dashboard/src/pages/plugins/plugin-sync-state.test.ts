import type { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";
import { describe, expect, it } from "vitest";
import { pluginUpToDate } from "./plugin-sync-state";

function status(overrides: Partial<PublishStatusResult>): PublishStatusResult {
  return { configured: true, connected: true, ...overrides };
}

describe("pluginUpToDate", () => {
  it("is unknown for a project that is not connected", () => {
    expect(
      pluginUpToDate(status({ connected: false, upToDate: true }), "tools"),
    ).toBeUndefined();
  });

  it("reads the plugin's own entry", () => {
    const publishStatus = status({
      upToDate: false,
      plugins: [
        { slug: "tools", upToDate: true, awaitingManualSync: false },
        { slug: "held", upToDate: false, awaitingManualSync: true },
      ],
    });
    expect(pluginUpToDate(publishStatus, "tools")).toBe(true);
    expect(pluginUpToDate(publishStatus, "held")).toBe(false);
    expect(pluginUpToDate(publishStatus, "missing")).toBeUndefined();
  });

  it("falls back to the project-wide answer without per-plugin entries", () => {
    for (const plugins of [undefined, []]) {
      expect(
        pluginUpToDate(status({ upToDate: false, plugins }), "tools"),
      ).toBe(false);
    }
  });
});
