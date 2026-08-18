import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { type BadgeProps } from "@/components/ui/Badge";

export type ShadowMCPPolicyState =
  | "blocking"
  | "warning"
  | "flagging"
  | "none"
  | "unavailable";

export type ShadowMCPInventoryStatus =
  | "allowed"
  | "blocked"
  | "restricted"
  | "observed"
  | "pending"
  | "unavailable";

export type ShadowMCPPolicyDisposition = "block_all" | "allow_all";

export type ShadowMCPPolicy = Pick<
  RiskPolicy,
  | "audienceType"
  | "audiencePrincipalUrns"
  | "id"
  | "name"
  | "shadowMcpDisposition"
>;

/**
 * The effective disposition of the project's enabled blocking shadow MCP
 * policies, or null when none exist. With legacy multi-policy data,
 * deny-by-default wins: allow_all only applies when every blocking policy
 * declares it.
 */
export function shadowMCPBlockingPolicyDisposition(
  policies: Pick<RiskPolicy, "shadowMcpDisposition">[],
): ShadowMCPPolicyDisposition | null {
  if (policies.length === 0) return null;
  return policies.every((policy) => policy.shadowMcpDisposition === "allow_all")
    ? "allow_all"
    : "block_all";
}

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

  if (shadowPolicies.some((policy) => policy.action === "warn")) {
    return "warning";
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
  if (server.access === "restricted") return "restricted";
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
    case "restricted":
      return "Restricted";
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
    case "restricted":
      return "warning";
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
  disposition: ShadowMCPPolicyDisposition | null = null,
): string {
  if (server.requestCount > 0) {
    return `${server.requestCount} access ${server.requestCount === 1 ? "request" : "requests"} pending`;
  }
  if (server.access === "allowed") {
    return disposition === "allow_all"
      ? "Allowed by default"
      : "Allowed by URL rule";
  }
  if (server.access === "blocked") {
    // A denied review is a distinct reason from the policy default. Under
    // allow_all the block *is* the review's doing; under block_all the policy
    // already blocks it and the deny compounds that.
    if (server.approvalRequest?.status === "denied") {
      return disposition === "allow_all"
        ? "Blocked by review"
        : "Blocked by policy & review";
    }
    return disposition === "allow_all"
      ? "Blocked by rule"
      : "Blocked by policy";
  }
  if (server.access === "restricted") {
    // A targeted policy blocks the server for its audience only, so it is not
    // blocked for the whole project.
    return "Blocked for some users";
  }
  if (policyState === "unavailable") return "Policy status unavailable";
  if (policyState === "blocking") {
    return server.approvalRequest?.status === "denied"
      ? "Blocked by policy & review"
      : "Blocked by policy";
  }
  return "Not blocking";
}
