import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import type { BadgeProps } from "@speakeasy-api/moonshine";

export type ShadowMCPPolicyState =
  | "blocking"
  | "flagging"
  | "none"
  | "unavailable";

export type ShadowMCPInventoryStatus = "allowed" | "blocked" | "observed";

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

export function shadowMCPPolicyBadgeVariant(
  state: ShadowMCPPolicyState,
): BadgeProps["variant"] {
  switch (state) {
    case "blocking":
      return "destructive";
    case "flagging":
      return "warning";
    case "none":
    case "unavailable":
      return "neutral";
  }
}

export function shadowMCPInventoryStatus(
  server: ShadowMCPInventoryServer,
  policyState: ShadowMCPPolicyState,
): ShadowMCPInventoryStatus {
  if (server.access === "allowed") return "allowed";
  if (server.access === "blocked") return "blocked";
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
  }
}

export function shadowMCPInventoryStatusDescription(
  server: ShadowMCPInventoryServer,
  policyState: ShadowMCPPolicyState,
): string {
  if (server.access === "allowed") return "Allowed by URL rule";
  if (server.access === "blocked") return "Blocked by policy";
  if (policyState === "blocking") return "Blocked by policy";
  return "Not blocking";
}
