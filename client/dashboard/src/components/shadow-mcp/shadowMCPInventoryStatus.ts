import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import type { BadgeProps } from "@speakeasy-api/moonshine";

export type ShadowMCPPolicyState =
  | "blocking"
  | "flagging"
  | "none"
  | "unavailable";

export type ShadowMCPInventoryStatus =
  | "allowed"
  | "blocked"
  | "observed"
  | "pending"
  | "unavailable";

export function eligibleShadowMCPAllowRulePolicies(
  policies: RiskPolicy[] | undefined,
): RiskPolicy[] {
  return (
    policies?.filter(
      (policy) =>
        policy.enabled &&
        policy.action === "block" &&
        policy.sources.includes("shadow_mcp"),
    ) ?? []
  );
}

export function shadowMCPPolicyState(
  policies: RiskPolicy[] | undefined,
): ShadowMCPPolicyState {
  if (!policies) return "unavailable";

  const shadowPolicies = policies.filter(
    (policy) => policy.enabled && policy.sources.includes("shadow_mcp"),
  );

  if (shadowPolicies.some((policy) => policy.action === "block")) {
    return "blocking";
  }

  if (shadowPolicies.some((policy) => policy.action === "flag")) {
    return "flagging";
  }

  return "none";
}

export function shadowMCPInventoryStatus(
  server: ShadowMCPInventoryServer,
  policyState: ShadowMCPPolicyState,
): ShadowMCPInventoryStatus {
  if (server.requestCount > 0) return "pending";
  if (server.access === "allowed") return "allowed";
  if (server.access === "blocked") return "blocked";
  if (policyState === "unavailable") return "unavailable";
  if (policyState === "blocking") return "blocked";
  return "observed";
}

export function shadowMCPInventoryStatusLabel(
  status: ShadowMCPInventoryStatus,
): string {
  switch (status) {
    case "allowed":
      return "Allowed";
    case "blocked":
      return "Blocked";
    case "observed":
      return "Observed";
    case "pending":
      return "Pending";
    case "unavailable":
      return "Unknown";
  }
}

export function shadowMCPInventoryStatusBadgeVariant(
  status: ShadowMCPInventoryStatus,
): BadgeProps["variant"] {
  switch (status) {
    case "allowed":
      return "success";
    case "blocked":
      return "destructive";
    case "observed":
      return "neutral";
    case "pending":
      return "warning";
    case "unavailable":
      return "neutral";
  }
}

export function shadowMCPInventoryStatusDescription(
  server: ShadowMCPInventoryServer,
  policyState: ShadowMCPPolicyState,
): string {
  if (server.requestCount > 0) {
    return `${server.requestCount} access ${server.requestCount === 1 ? "request" : "requests"} pending`;
  }
  if (server.access === "allowed") return "Allowed by URL rule";
  if (server.access === "blocked") return "Blocked by policy";
  if (policyState === "unavailable") return "Policy status unavailable";
  if (policyState === "blocking") return "Blocked by policy";
  return "Not blocking";
}
