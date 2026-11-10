import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import {
  isBlockingShadowMCPPolicy,
  isShadowMCPBlockConfiguration,
} from "./policy-shadow-mcp-setup";

/** How a policy decides what it flags: a built-in detector, an LLM judge,
 *  the per-Shadow AI decisions of the project's Shadow AI policy, or a person
 *  approving each held tool call. */
export type PolicyType = "built_in" | "prompt" | "shadow_ai" | "approval";

export const POLICY_TYPE_LABEL: Record<PolicyType, string> = {
  built_in: "Built-in",
  prompt: "Prompt-based",
  shadow_ai: "Shadow AI",
  approval: "Approval",
};

type PolicyShape = Pick<RiskPolicy, "policyType" | "sources" | "action">;

/** A Shadow AI policy is a blocking Shadow MCP policy with no other detector
 *  attached: the one policy whose decisions say which AI tools and MCP servers
 *  may connect. A blocking policy that also scans for secrets or PII stays a
 *  Built-in policy and keeps the standard editor, even though the server
 *  counts it against the same single blocking slot. */
export function isShadowAIPolicy(policy: PolicyShape): boolean {
  return (
    policy.policyType !== "prompt_based" &&
    policy.sources.length === 1 &&
    isShadowMCPBlockConfiguration(policy.sources, policy.action)
  );
}

export function policyTypeOf(policy: PolicyShape): PolicyType {
  if (policy.action === "require_approval") return "approval";
  if (policy.policyType === "prompt_based") return "prompt";
  if (isShadowAIPolicy(policy)) return "shadow_ai";
  return "built_in";
}

/** The enabled policy holding the project's single blocking Shadow MCP slot.
 *  The server refuses a second one, so anything offering to create a Shadow AI
 *  policy points at this one instead when it exists. */
export function findBlockingShadowMCPPolicy<
  T extends Pick<RiskPolicy, "enabled" | "sources" | "action">,
>(policies: readonly T[]): T | undefined {
  return policies.find((policy) =>
    isBlockingShadowMCPPolicy(policy.enabled, policy.sources, policy.action),
  );
}
