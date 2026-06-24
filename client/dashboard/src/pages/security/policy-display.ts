// Pure policy display helpers — AGE-2704.
//
// Single source of truth for turning a `RiskPolicy`'s wire representation into
// the labels/categories shown in the UI. Both the Policy Center table and the
// Policy Detail summary/wizard derive from these so the surfaces can never
// drift. This module is pure (no React) and a leaf consumer of `policy-data`
// and `rule-ids`; keep it free of page state. Presentational components that
// use these helpers live in `policy-summary.tsx`.

import {
  DETECTION_RULES,
  POLICY_MESSAGE_TYPE_META,
  type PolicyMessageType,
  type RuleCategory,
} from "./policy-data";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { ruleIdToPresidioEntity } from "./rule-ids";

/** Presidio-backed categories. */
export const PRESIDIO_CATEGORIES: RuleCategory[] = [
  "financial",
  "pii",
  "government_ids",
  "healthcare",
];

export const ALL_POLICY_MESSAGE_TYPES = Object.keys(
  POLICY_MESSAGE_TYPE_META,
) as Array<PolicyMessageType>;

const TOOL_CALL_MESSAGE_TYPES = new Set<PolicyMessageType>([
  "tool_request",
  "tool_response",
]);

/** Derive selected categories from a policy's sources + presidioEntities.
 *
 * DETECTION_RULES.id is the canonical `pii.<snake_case>` form; the wire format
 * stored on the policy is the UPPER_SNAKE entity name Presidio speaks. We
 * translate at this boundary so callers never see the wire format. */
export function policyToCategories(
  sources: string[],
  presidioEntities?: string[],
): Set<RuleCategory> {
  const cats = new Set<RuleCategory>();
  if (sources.includes("gitleaks")) cats.add("secrets");
  if (sources.includes("shadow_mcp")) cats.add("shadow_mcp");
  if (sources.includes("destructive_tool")) cats.add("destructive_tool");
  if (sources.includes("cli_destructive")) cats.add("cli_destructive");
  if (sources.includes("prompt_injection")) cats.add("prompt_injection");
  for (const cat of PRESIDIO_CATEGORIES) {
    const wireEntities = DETECTION_RULES[cat].map((r) =>
      ruleIdToPresidioEntity(r.id),
    );
    if (wireEntities.some((id) => presidioEntities?.includes(id))) {
      cats.add(cat);
    }
  }
  return cats;
}

/** Map sources to display categories for the table row badges. */
export function sourcesToCategories(
  sources: string[],
  presidioEntities?: string[],
): RuleCategory[] {
  return [...policyToCategories(sources, presidioEntities)];
}

export function policyMessageTypesForForm(
  messageTypes?: string[],
): Set<PolicyMessageType> {
  if (!messageTypes?.length) {
    return new Set(ALL_POLICY_MESSAGE_TYPES);
  }

  return new Set(
    messageTypes.filter((type): type is PolicyMessageType =>
      ALL_POLICY_MESSAGE_TYPES.includes(type as PolicyMessageType),
    ),
  );
}

export function policyMessageTypesForDisplay(
  messageTypes?: string[],
): PolicyMessageType[] {
  return [...policyMessageTypesForForm(messageTypes)];
}

export function hasOnlyToolCallMessageTypes(
  types: Set<PolicyMessageType>,
): boolean {
  return (
    types.size === TOOL_CALL_MESSAGE_TYPES.size &&
    [...types].every((type) => TOOL_CALL_MESSAGE_TYPES.has(type))
  );
}

export function messageTypesSummary(
  selectedMessageTypes: Set<PolicyMessageType>,
): string {
  if (selectedMessageTypes.size === ALL_POLICY_MESSAGE_TYPES.length) {
    return "All types";
  }

  if (hasOnlyToolCallMessageTypes(selectedMessageTypes)) {
    return "Tool Calls";
  }

  if (
    selectedMessageTypes.size === 1 &&
    selectedMessageTypes.has("tool_request")
  ) {
    return "Tool Requests";
  }

  return `${selectedMessageTypes.size} of ${ALL_POLICY_MESSAGE_TYPES.length} types selected`;
}

export function truncatePrompt(prompt: string, maxLength = 60): string {
  const singleLine = prompt.trim().replace(/\s+/g, " ");
  if (singleLine.length <= maxLength) {
    return singleLine;
  }
  return `${singleLine.slice(0, maxLength - 1)}…`;
}

/** Human summary of who a policy applies to. Prompt-based policies always
 * apply to everyone; risk policies may target specific principals. */
export function policyAudienceSummary(policy: RiskPolicy): string {
  if (policy.policyType === "prompt_based") {
    return "Everyone";
  }
  if (policy.audienceType !== "targeted") {
    return "Everyone";
  }

  const count = policy.audiencePrincipalUrns.length;
  if (count === 1) {
    return "1 target";
  }
  return `${count} targets`;
}
