import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { describe, expect, it } from "vitest";
import { testAccessSummary } from "./shadowMCPInventoryTestFixtures";
import {
  shadowMCPGatewayUseCaseMetrics,
  shadowMCPOpportunityServers,
  shadowMCPPolicyUseCaseMetrics,
  shadowMCPRiskRank,
  shadowMCPRiskyServers,
} from "./shadowMCPUseCases";

function inventoryServer(
  overrides: Partial<ShadowMCPInventoryServer> = {},
): ShadowMCPInventoryServer {
  return {
    access: "none",
    accessSummary: testAccessSummary("none"),
    allowedPolicyIds: [],
    blockedPolicyIds: [],
    canonicalServerUrl: "https://shadow.example/mcp",
    firstSeen: new Date("2026-01-01T00:00:00Z"),
    lastCalled: new Date("2026-01-03T00:00:00Z"),
    lastSeen: new Date("2026-01-03T00:00:00Z"),
    observedUseCount: 12,
    requestCount: 0,
    serverName: "Shadow Example",
    serverSlug: "shadow-example",
    topUsers: [],
    urlHost: "shadow.example",
    userCount: 2,
    ...overrides,
  };
}

describe("shadowMCPUseCases", () => {
  it("ranks pending servers above observed servers", () => {
    const pending = inventoryServer({
      serverSlug: "pending",
      requestCount: 1,
      observedUseCount: 1,
    });
    const observed = inventoryServer({
      serverSlug: "observed",
      requestCount: 0,
      observedUseCount: 100,
    });

    expect(shadowMCPRiskRank(pending)).toBeGreaterThan(
      shadowMCPRiskRank(observed),
    );
  });

  it("selects risky servers by review state", () => {
    const inventory = [
      inventoryServer({ serverSlug: "observed-only", requestCount: 0 }),
      inventoryServer({ serverSlug: "pending", requestCount: 2 }),
      inventoryServer({
        serverSlug: "blocked",
        access: "blocked",
        accessSummary: testAccessSummary("blocked"),
      }),
    ];

    expect(
      shadowMCPRiskyServers(inventory).map((server) => server.serverSlug),
    ).toEqual(["pending", "blocked"]);
  });

  it("selects opportunity servers by calls per user", () => {
    const inventory = [
      inventoryServer({
        serverSlug: "low-leverage",
        observedUseCount: 4,
        userCount: 4,
      }),
      inventoryServer({
        serverSlug: "high-leverage",
        observedUseCount: 40,
        userCount: 2,
      }),
    ];

    expect(
      shadowMCPOpportunityServers(inventory).map((server) => server.serverSlug),
    ).toEqual(["high-leverage", "low-leverage"]);
  });

  it("computes policy and gateway metrics", () => {
    const inventory = [
      inventoryServer({ requestCount: 3, observedUseCount: 1, userCount: 10 }),
      inventoryServer({
        access: "blocked",
        accessSummary: testAccessSummary("blocked"),
        observedUseCount: 12,
        userCount: 2,
      }),
    ];

    expect(shadowMCPPolicyUseCaseMetrics(inventory)).toEqual({
      totalPendingRequests: 3,
      restrictedOrBlockedCount: 1,
    });
    expect(shadowMCPGatewayUseCaseMetrics(inventory)).toEqual({
      totalObservedCalls: 13,
      concentratedUsageCount: 1,
    });
  });
});
