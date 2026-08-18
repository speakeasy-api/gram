import { describe, expect, it } from "vitest";

import { platformMcpCtaLabel } from "./usePlatformMcpCta";

describe("platformMcpCtaLabel", () => {
  it("labels a new setup", () => {
    expect(
      platformMcpCtaLabel({
        connectionAuthState: "not_connected",
        workflowActive: false,
        stage: "not_started",
      }),
    ).toBe("Set up Platform MCP");
  });

  it("labels an active or in-progress setup as resumable", () => {
    expect(
      platformMcpCtaLabel({
        connectionAuthState: "not_connected",
        workflowActive: true,
        stage: "not_started",
      }),
    ).toBe("Continue Platform MCP setup");
    expect(
      platformMcpCtaLabel({
        connectionAuthState: "active",
        workflowActive: false,
        stage: "catalog_selected",
      }),
    ).toBe("Continue Platform MCP setup");
  });

  it("prioritizes reconnect when authorization is required", () => {
    expect(
      platformMcpCtaLabel({
        connectionAuthState: "reauthorization_required",
        workflowActive: true,
        stage: "installed",
      }),
    ).toBe("Reconnect Platform MCP");
  });
});
