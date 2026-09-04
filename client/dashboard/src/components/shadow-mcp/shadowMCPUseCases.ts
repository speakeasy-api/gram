import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { shadowMCPInventoryStatus } from "./shadowMCPInventoryStatus";

export function shadowMCPRiskRank(server: ShadowMCPInventoryServer): number {
  const status = shadowMCPInventoryStatus(server);
  const statusWeight =
    status === "pending"
      ? 5
      : status === "blocked"
        ? 4
        : status === "restricted"
          ? 3
          : status === "observed"
            ? 2
            : 1;
  return (
    statusWeight * 1000 +
    server.requestCount * 100 +
    server.observedUseCount * 10 +
    server.userCount
  );
}

export function shadowMCPRiskyServers(
  inventory: ShadowMCPInventoryServer[],
  limit = 8,
): ShadowMCPInventoryServer[] {
  return [...inventory]
    .sort((a, b) => shadowMCPRiskRank(b) - shadowMCPRiskRank(a))
    .filter((server) => {
      const status = shadowMCPInventoryStatus(server);
      return (
        status === "pending" || status === "blocked" || status === "restricted"
      );
    })
    .slice(0, limit);
}

export function shadowMCPOpportunityServers(
  inventory: ShadowMCPInventoryServer[],
  limit = 8,
): ShadowMCPInventoryServer[] {
  return [...inventory]
    .filter((server) => server.observedUseCount > 0 && server.userCount > 0)
    .sort((a, b) => {
      const ratioA = a.observedUseCount / a.userCount;
      const ratioB = b.observedUseCount / b.userCount;
      if (ratioA !== ratioB) return ratioB - ratioA;
      return b.observedUseCount - a.observedUseCount;
    })
    .slice(0, limit);
}

export function shadowMCPPolicyUseCaseMetrics(
  inventory: ShadowMCPInventoryServer[],
): {
  totalPendingRequests: number;
  restrictedOrBlockedCount: number;
} {
  const totalPendingRequests = inventory.reduce(
    (sum, server) => sum + server.requestCount,
    0,
  );
  const restrictedOrBlockedCount = inventory.filter((server) => {
    const status = shadowMCPInventoryStatus(server);
    return status === "blocked" || status === "restricted";
  }).length;
  return { totalPendingRequests, restrictedOrBlockedCount };
}

export function shadowMCPGatewayUseCaseMetrics(
  inventory: ShadowMCPInventoryServer[],
): {
  totalObservedCalls: number;
  concentratedUsageCount: number;
} {
  const totalObservedCalls = inventory.reduce(
    (sum, server) => sum + server.observedUseCount,
    0,
  );
  const concentratedUsageCount = inventory.filter(
    (server) =>
      server.observedUseCount >= 10 &&
      server.userCount > 0 &&
      server.userCount <= 3,
  ).length;
  return { totalObservedCalls, concentratedUsageCount };
}
