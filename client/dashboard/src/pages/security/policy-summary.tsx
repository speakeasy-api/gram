// Shared policy display helpers + summary view — AGE-2704.
//
// Single source of truth for turning a `RiskPolicy`'s wire representation into
// the labels/badges shown in the UI. Both the Policy Center table and the
// Policy Detail "Overview" tab render from these helpers so the two surfaces
// can never drift. This module is a leaf consumer of `policy-data` (the
// category/message-type primitives) and `rule-ids`; keep it free of page state.

import { Type } from "@/components/ui/type";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { Badge, type BadgeProps } from "@speakeasy-api/moonshine";
import {
  DETECTION_RULES,
  POLICY_MESSAGE_TYPE_META,
  RULE_CATEGORY_META,
  type PolicyAction,
  type PolicyMessageType,
  type RuleCategory,
} from "./policy-data";
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

const ACTION_BADGE_CONFIG: Record<
  PolicyAction,
  { label: string; variant: NonNullable<BadgeProps["variant"]> }
> = {
  flag: { label: "Flag", variant: "neutral" },
  block: { label: "Block", variant: "destructive" },
};

export function ActionBadge({ action }: { action: PolicyAction }) {
  const config = ACTION_BADGE_CONFIG[action] ?? ACTION_BADGE_CONFIG.flag;
  return (
    <Badge variant={config.variant}>
      <Badge.Text>{config.label}</Badge.Text>
    </Badge>
  );
}

function SummaryField({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="border-border/60 flex flex-col gap-1.5 border-b py-4 last:border-b-0 sm:flex-row sm:items-start sm:gap-6">
      <Type
        small
        muted
        className="shrink-0 sm:w-40 sm:pt-0.5 sm:text-right uppercase tracking-wide"
      >
        {label}
      </Type>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}

/** Read-only configuration summary for a single policy. Renders the same
 * derived values as the Policy Center table, in a vertical detail layout. */
export function PolicySummary({ policy }: { policy: RiskPolicy }) {
  const isPrompt = policy.policyType === "prompt_based";

  const categories = isPrompt
    ? []
    : sourcesToCategories(policy.sources, policy.presidioEntities);
  if (!isPrompt && policy.customRuleIds?.length) {
    categories.push("custom");
  }

  const messageTypes = policyMessageTypesForDisplay(policy.messageTypes);
  const messageTypeSet = new Set(messageTypes);
  const compactMessageTypes =
    messageTypeSet.size === ALL_POLICY_MESSAGE_TYPES.length ||
    hasOnlyToolCallMessageTypes(messageTypeSet);

  const scopeInclude = policy.scopeInclude?.trim();
  const scopeExempt = policy.scopeExempt?.trim();

  return (
    <div className="rounded-xl border px-6">
      <SummaryField label="Type">
        <Type small>{isPrompt ? "Prompt-based" : "Detection rules"}</Type>
      </SummaryField>

      <SummaryField label="Action">
        <ActionBadge action={(policy.action as PolicyAction) ?? "flag"} />
      </SummaryField>

      {isPrompt ? (
        <SummaryField label="Prompt">
          <Type small className="whitespace-pre-wrap">
            {policy.prompt?.trim() || "—"}
          </Type>
        </SummaryField>
      ) : (
        <SummaryField label="Detectors">
          {categories.length === 0 ? (
            <Type small muted>
              —
            </Type>
          ) : (
            <div className="flex flex-wrap gap-1.5">
              {categories.map((cat) => (
                <Badge key={cat} variant="neutral">
                  <Badge.Text>{RULE_CATEGORY_META[cat].label}</Badge.Text>
                </Badge>
              ))}
            </div>
          )}
        </SummaryField>
      )}

      <SummaryField label="Applies to">
        <Type small>
          {compactMessageTypes
            ? messageTypesSummary(messageTypeSet)
            : messageTypes
                .map((type) => POLICY_MESSAGE_TYPE_META[type].label)
                .join(", ")}
        </Type>
      </SummaryField>

      {(scopeInclude || scopeExempt) && (
        <SummaryField label="Scope (CEL)">
          <div className="flex flex-col gap-2">
            {scopeInclude && (
              <code className="bg-muted/50 block rounded px-2 py-1 text-xs">
                {scopeInclude}
              </code>
            )}
            {scopeExempt && (
              <code className="bg-muted/50 text-muted-foreground block rounded px-2 py-1 text-xs">
                except: {scopeExempt}
              </code>
            )}
          </div>
        </SummaryField>
      )}

      <SummaryField label="Audience">
        <Type small>{policyAudienceSummary(policy)}</Type>
      </SummaryField>
    </div>
  );
}
