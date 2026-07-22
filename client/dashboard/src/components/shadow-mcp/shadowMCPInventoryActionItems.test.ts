import { describe, expect, it, vi } from "vitest";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import {
  ALLOW_RULE_POLICY_REQUIRED,
  shadowMCPInventoryActions,
} from "./shadowMCPInventoryActionItems";

function server(
  overrides: Partial<ShadowMCPInventoryServer> = {},
): ShadowMCPInventoryServer {
  return {
    access: "none",
    allowedPolicyIds: [],
    canonicalServerUrl: "https://example.com/mcp",
    firstSeen: new Date("2026-01-01T00:00:00Z"),
    lastCalled: new Date("2026-01-01T00:00:00Z"),
    lastSeen: new Date("2026-01-01T00:00:00Z"),
    observedUseCount: 1,
    requestCount: 0,
    serverName: "Example MCP",
    serverSlug: "example-mcp",
    topUsers: [],
    urlHost: "example.com",
    userCount: 1,
    ...overrides,
  };
}

describe("shadowMCPInventoryActions", () => {
  it("disables add and edit with a reason when no policy is eligible", () => {
    const options = {
      canManageAllowRules: false,
      disabled: false,
      onOpenAction: vi.fn(() => {}),
    };

    expect(shadowMCPInventoryActions(server(), options)).toEqual([
      expect.objectContaining({
        label: "Add Allow Rule",
        disabled: true,
        description: ALLOW_RULE_POLICY_REQUIRED,
      }),
    ]);
    expect(
      shadowMCPInventoryActions(server({ access: "allowed" }), options),
    ).toEqual([
      expect.objectContaining({
        label: "Edit Rule",
        disabled: true,
        description: ALLOW_RULE_POLICY_REQUIRED,
      }),
      expect.objectContaining({ label: "Delete Rule", disabled: false }),
    ]);
  });

  it("keeps review available so a pending request can be denied", () => {
    const actions = shadowMCPInventoryActions(
      server({ access: "blocked", requestCount: 1 }),
      {
        canManageAllowRules: false,
        disabled: false,
        onOpenAction: vi.fn(() => {}),
      },
    );

    expect(actions).toEqual([
      expect.objectContaining({ label: "Review Request", disabled: false }),
    ]);
  });
});
