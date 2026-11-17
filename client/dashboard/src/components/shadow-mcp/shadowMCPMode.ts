import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { findBlockingShadowMCPPolicy } from "@/pages/security/policy-type";

/**
 * How a project treats an MCP server nobody has decided on. "allowed" is the
 * built-in default and needs no policy: a server connects unless an
 * administrator denies it. "blocked" is a policy that blocks every server
 * nobody approved.
 */
export type ShadowMCPMode = "allowed" | "blocked";

export type ShadowMCPModeState = {
  mode: ShadowMCPMode;
  /** The enabled blocking Shadow MCP policy, when there is one. */
  policy: RiskPolicy | undefined;
  /**
   * A Shadow AI policy from Guardrails holds the project's one blocking
   * Shadow MCP slot and allows servers by default, so "blocked by default"
   * cannot be turned on here without replacing it.
   */
  heldByShadowAI: boolean;
};

export function shadowMCPModeOf(
  policies: readonly RiskPolicy[],
): ShadowMCPModeState {
  const policy = findBlockingShadowMCPPolicy(policies);
  if (!policy)
    return { mode: "allowed", policy: undefined, heldByShadowAI: false };
  if (policy.shadowMcpDisposition === "allow_all") {
    return { mode: "allowed", policy, heldByShadowAI: true };
  }
  return { mode: "blocked", policy, heldByShadowAI: false };
}

/** The managed policy "blocked by default" creates. */
export const SHADOW_MCP_BLOCKED_BY_DEFAULT_POLICY = {
  name: "Shadow MCP",
  enabled: true,
  sources: ["shadow_mcp"],
  action: "block",
  audienceType: "everyone",
  audiencePrincipalUrns: [] as string[],
  autoName: false,
  score: 8,
  shadowMcpDisposition: "block_all",
} as const;

/**
 * Who "blocked by default" applies to, in a few words for the page header:
 * everyone, or the people and roles it was narrowed to.
 */
export function shadowMCPAudienceSummary(
  policy:
    | Pick<RiskPolicy, "audienceType" | "audiencePrincipalUrns">
    | undefined,
  groups: ReadonlyArray<{
    options: ReadonlyArray<{ label: string; value: string }>;
  }>,
): string {
  if (!policy || policy.audienceType !== "targeted") {
    return "Applies to everyone";
  }
  const urns = policy.audiencePrincipalUrns;
  if (urns.length === 0) return "Applies to nobody";
  if (urns.length === 1) {
    const label = groups
      .flatMap((group) => group.options)
      .find((option) => option.value === urns[0])?.label;
    if (label) return `Applies to ${label}`;
  }
  const roles = urns.filter((urn) => urn.startsWith("role:")).length;
  const people = urns.length - roles;
  const parts = [
    roles > 0 ? `${roles} ${roles === 1 ? "role" : "roles"}` : "",
    people > 0 ? `${people} ${people === 1 ? "person" : "people"}` : "",
  ].filter(Boolean);
  return `Applies to ${parts.join(" and ")}`;
}
