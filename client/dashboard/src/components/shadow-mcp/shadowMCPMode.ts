import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import {
  findBlockingShadowMCPPolicy,
  isShadowAIPolicy,
} from "@/pages/security/policy-type";

/**
 * How a project treats an MCP server nobody has decided on. "allowed" is the
 * built-in default and needs no policy: a server connects unless an
 * administrator denies it. "blocked" is a policy that blocks every server
 * nobody approved.
 */
export type ShadowMCPMode = "allowed" | "blocked";

/**
 * Who sets the mode when this page cannot change it:
 * - "shadow_ai": a Shadow AI policy from Guardrails holds the project's one
 *   blocking Shadow MCP slot and allows servers by default, so "blocked by
 *   default" cannot be turned on here without replacing it.
 * - "guardrails": a Guardrails policy that also scans for other risks, such
 *   as secrets or PII, holds the slot. Its disposition sets the mode, and
 *   switching here would delete or rewrite that policy, so it is changed in
 *   Guardrails only.
 */
export type ShadowMCPModeHolder = "shadow_ai" | "guardrails";

export type ShadowMCPModeState = {
  mode: ShadowMCPMode;
  /** The enabled blocking Shadow MCP policy, when there is one. */
  policy: RiskPolicy | undefined;
  /** Set when a Guardrails policy decides the mode instead of this page. */
  heldBy: ShadowMCPModeHolder | null;
};

export function shadowMCPModeOf(
  policies: readonly RiskPolicy[],
): ShadowMCPModeState {
  const policy = findBlockingShadowMCPPolicy(policies);
  if (!policy) return { mode: "allowed", policy: undefined, heldBy: null };
  const allowsByDefault = policy.shadowMcpDisposition === "allow_all";
  if (!isShadowAIPolicy(policy)) {
    return {
      mode: allowsByDefault ? "allowed" : "blocked",
      policy,
      heldBy: "guardrails",
    };
  }
  if (allowsByDefault) return { mode: "allowed", policy, heldBy: "shadow_ai" };
  return { mode: "blocked", policy, heldBy: null };
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
 * everyone, or the people and roles it was narrowed to. A narrowed rule says
 * that everyone else is still allowed by default.
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
  return `Applies only to ${chosenAudience(urns, groups)}. Everyone else is allowed by default.`;
}

/** The people and roles a narrowed rule names: one by name, more counted. */
function chosenAudience(
  urns: readonly string[],
  groups: ReadonlyArray<{
    options: ReadonlyArray<{ label: string; value: string }>;
  }>,
): string {
  if (urns.length === 1) {
    const label = groups
      .flatMap((group) => group.options)
      .find((option) => option.value === urns[0])?.label;
    if (label) return label;
  }
  const roles = urns.filter((urn) => urn.startsWith("role:")).length;
  const people = urns.length - roles;
  return [
    roles > 0 ? `${roles} ${roles === 1 ? "role" : "roles"}` : "",
    people > 0 ? `${people} ${people === 1 ? "person" : "people"}` : "",
  ]
    .filter(Boolean)
    .join(" and ");
}
