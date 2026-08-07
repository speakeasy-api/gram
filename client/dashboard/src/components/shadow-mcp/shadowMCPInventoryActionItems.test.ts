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
    blockedPolicyIds: [],
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

describe("shadowMCPInventoryActions under allow_all", () => {
  const options = {
    canManageAllowRules: true,
    disabled: false,
    disposition: "allow_all" as const,
    onOpenAction: vi.fn(() => {}),
  };

  it("offers Block Server for an unblocked server", () => {
    expect(
      shadowMCPInventoryActions(server({ access: "allowed" }), options),
    ).toEqual([
      expect.objectContaining({ label: "Block Server", destructive: true }),
    ]);
  });

  it("offers Unblock Server for a blocked server", () => {
    expect(
      shadowMCPInventoryActions(server({ access: "blocked" }), options),
    ).toEqual([expect.objectContaining({ label: "Unblock Server" })]);
  });

  it("keeps Review Request first for a server with a pending request", () => {
    const actions = shadowMCPInventoryActions(
      server({ access: "blocked", requestCount: 1 }),
      options,
    );
    expect(actions.map((action) => action.label)).toEqual([
      "Review Request",
      "Unblock Server",
    ]);
  });

  it("never offers allow-rule actions", () => {
    const labels = shadowMCPInventoryActions(
      server({ access: "allowed" }),
      options,
    ).map((action) => action.label);
    expect(labels).not.toContain("Add Allow Rule");
    expect(labels).not.toContain("Edit Rule");
    expect(labels).not.toContain("Delete Rule");
  });
});
