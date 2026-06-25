// Policy-eval insights → suggested config refinements (AGE-2704).
//
// Renders, for a COMPLETED run with findings, two blocks driven by the
// `useRiskGetPolicyEvalRunInsights` aggregation:
//
//   1. "Repeated matches" — informational. The top duplicated matched values
//      (already deduped + redacted server-side), grouped by match. Collapses
//      noise like one secret repeated across many sessions. No apply: a single
//      value can't be cleanly CEL-exempted.
//   2. "Suggested refinements" — actionable cards, each with a one-click Apply
//      that mutates the DRAFT config via the shared form. The existing
//      dirty→re-run banner/Evals note then prompts a re-run; we add only a
//      lightweight inline "Applied — re-run to confirm" acknowledgement.
//
// Suggestions:
//   - Unused rules (standard policies only): rules selected in the policy's
//     detection that never appear in `byRule`. Apply → disable them.
//   - Scope concentration (both kinds): one message type dominates the
//     findings (≥ 80%). Apply → scope the policy to that type.
//
// When a block has nothing to show it renders nothing (no empty cards).

import { useState } from "react";
import { Type } from "@/components/ui/type";
import { Heading } from "@/components/ui/heading";
import { Badge, Button, Icon, type IconName } from "@speakeasy-api/moonshine";
import { useRiskGetPolicyEvalRunInsights } from "@gram/client/react-query/index.js";
import type { PolicyEvalMatchCluster } from "@gram/client/models/components/policyevalmatchcluster.js";
import type { PolicyEvalMessageTypeCluster } from "@gram/client/models/components/policyevalmessagetypecluster.js";
import {
  DETECTION_RULES,
  POLICY_MESSAGE_TYPE_META,
  type PolicyMessageType,
  type RuleCategory,
} from "../policy-data";
import { MatchCell, SourceRuleCell } from "../finding-cells";
import { RevealAllProvider, RevealAllToggle } from "../risk-ui";
import type { usePolicyForm } from "../policy-form/use-policy-form";
import { formatCount } from "./format";

type PolicyForm = ReturnType<typeof usePolicyForm>;

// One scanned-message role can dominate the findings; map it onto the form's
// message-type vocabulary so the scope-concentration card can apply it. Roles
// with no clean form analogue (e.g. "system") are intentionally absent — the
// card stays informational for them.
const ROLE_TO_MESSAGE_TYPE: Record<string, PolicyMessageType | undefined> = {
  user: "user_message",
  assistant: "assistant_message",
  tool: "tool_response",
};

// Share at or above which a single message type is treated as "dominant".
const SCOPE_CONCENTRATION_THRESHOLD = 0.8;

// Top N repeated-match clusters to surface; the rest are noise on the noise.
const MAX_REPEATED_MATCHES = 6;

function roleLabel(role: string): string {
  const type = ROLE_TO_MESSAGE_TYPE[role];
  if (type) return POLICY_MESSAGE_TYPE_META[type].label;
  // Fall back to a humanized role for unmapped values (e.g. "system").
  return role.charAt(0).toUpperCase() + role.slice(1);
}

export function RunInsights({
  runId,
  policyType,
  form,
}: {
  runId: string;
  policyType?: "standard" | "prompt_based";
  /** The shared policy form; apply actions mutate its draft state. */
  form: PolicyForm;
}): JSX.Element | null {
  const { data: insights } = useRiskGetPolicyEvalRunInsights(
    { runId },
    undefined,
    { enabled: !!runId },
  );

  if (!insights) return null;

  const repeated = insights.byMatch.slice(0, MAX_REPEATED_MATCHES);
  const unusedRules =
    policyType === "standard" ? computeUnusedRules(insights.byRule, form) : [];
  const concentration = computeScopeConcentration(insights.byMessageType);

  const hasSuggestions = unusedRules.length > 0 || concentration != null;
  if (repeated.length === 0 && !hasSuggestions) return null;

  return (
    <div className="space-y-6">
      {hasSuggestions && (
        <SuggestionsBlock
          unusedRules={unusedRules}
          concentration={concentration}
          form={form}
        />
      )}
      {repeated.length > 0 && <RepeatedMatchesBlock clusters={repeated} />}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Repeated matches (informational)
// ---------------------------------------------------------------------------

function RepeatedMatchesBlock({
  clusters,
}: {
  clusters: PolicyEvalMatchCluster[];
}) {
  return (
    <RevealAllProvider>
      <div className="space-y-3">
        <div className="flex items-center justify-between gap-2">
          <div className="min-w-0 space-y-1">
            <Heading variant="h4">Repeated matches</Heading>
            <Type small muted className="font-normal">
              The same value flagged more than once. Often a single secret or
              identifier echoed across sessions — review it once.
            </Type>
          </div>
          <RevealAllToggle />
        </div>
        <div className="divide-border overflow-hidden rounded-lg border divide-y">
          {clusters.map((c) => (
            <div
              key={c.matchHash}
              className="flex items-center justify-between gap-4 px-4 py-2.5"
            >
              <div className="flex min-w-0 flex-1 items-center gap-3">
                <div className="min-w-0 flex-1">
                  <MatchCell match={c.matchRedacted} source={c.source} />
                </div>
                <div className="shrink-0">
                  <SourceRuleCell source={c.source} ruleId={c.ruleId} />
                </div>
              </div>
              <Type small muted className="shrink-0 font-normal">
                {formatCount(c.count)}× across {formatCount(c.distinctSessions)}{" "}
                {c.distinctSessions === 1 ? "session" : "sessions"}
              </Type>
            </div>
          ))}
        </div>
      </div>
    </RevealAllProvider>
  );
}

// ---------------------------------------------------------------------------
// Suggested refinements (actionable)
// ---------------------------------------------------------------------------

type UnusedRule = { id: string; title: string };

type ScopeConcentration = {
  role: string;
  messageType: PolicyMessageType;
  share: number;
};

// Rules the policy selected (non-hidden, not already disabled) that never fired
// in this sample — candidates to disable to cut noise/cost.
function computeUnusedRules(
  byRule: { ruleId?: string }[],
  form: PolicyForm,
): UnusedRule[] {
  const matched = new Set(
    byRule.map((r) => r.ruleId).filter((id): id is string => !!id),
  );
  const { selectedCategories, disabledRules } = form.state;
  const unused: UnusedRule[] = [];
  for (const category of selectedCategories) {
    for (const rule of DETECTION_RULES[category as RuleCategory] ?? []) {
      if (rule.hidden) continue;
      if (disabledRules.has(rule.id)) continue;
      if (matched.has(rule.id)) continue;
      unused.push({ id: rule.id, title: rule.title });
    }
  }
  return unused;
}

// The single message type that carries a dominant share of findings, if any.
// Only returned when it maps onto a form message type (so Apply is honest).
function computeScopeConcentration(
  byMessageType: PolicyEvalMessageTypeCluster[],
): ScopeConcentration | null {
  const total = byMessageType.reduce((sum, c) => sum + c.count, 0);
  if (total === 0) return null;
  let top: PolicyEvalMessageTypeCluster | null = null;
  for (const c of byMessageType) {
    if (!top || c.count > top.count) top = c;
  }
  if (!top) return null;
  const share = top.count / total;
  if (share < SCOPE_CONCENTRATION_THRESHOLD) return null;
  const messageType = ROLE_TO_MESSAGE_TYPE[top.role];
  if (!messageType) return null;
  return { role: top.role, messageType, share };
}

function SuggestionsBlock({
  unusedRules,
  concentration,
  form,
}: {
  unusedRules: UnusedRule[];
  concentration: ScopeConcentration | null;
  form: PolicyForm;
}) {
  return (
    <div className="space-y-3">
      <Heading variant="h4">Suggested refinements</Heading>
      <div className="space-y-3">
        {unusedRules.length > 0 && (
          <UnusedRulesCard rules={unusedRules} form={form} />
        )}
        {concentration && (
          <ScopeConcentrationCard concentration={concentration} form={form} />
        )}
      </div>
    </div>
  );
}

function SuggestionCard({
  icon,
  title,
  body,
  applyLabel,
  applied,
  onApply,
}: {
  icon: IconName;
  title: string;
  body: string;
  applyLabel: string;
  applied: boolean;
  onApply: () => void;
}) {
  return (
    <div className="bg-muted/20 flex items-start justify-between gap-4 rounded-lg border p-4">
      <div className="flex min-w-0 items-start gap-3">
        <Icon
          name={icon}
          className="text-muted-foreground mt-0.5 h-5 w-5 shrink-0"
        />
        <div className="min-w-0">
          <div className="text-sm font-medium">{title}</div>
          <Type small muted className="font-normal">
            {body}
          </Type>
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {applied ? (
          <Badge variant="success">
            <Badge.Text>Applied — re-run to confirm</Badge.Text>
          </Badge>
        ) : (
          <Button variant="secondary" onClick={onApply}>
            <Button.Text>{applyLabel}</Button.Text>
          </Button>
        )}
      </div>
    </div>
  );
}

function UnusedRulesCard({
  rules,
  form,
}: {
  rules: UnusedRule[];
  form: PolicyForm;
}) {
  const [applied, setApplied] = useState(false);
  const handleApply = () => {
    form.setters.setDisabledRules((prev) => {
      const next = new Set(prev);
      for (const r of rules) next.add(r.id);
      return next;
    });
    setApplied(true);
  };
  const preview = rules
    .slice(0, 3)
    .map((r) => r.title)
    .join(", ");
  const extra = rules.length > 3 ? `, +${rules.length - 3} more` : "";
  return (
    <SuggestionCard
      icon="filter"
      title={`${rules.length} detection ${
        rules.length === 1 ? "rule" : "rules"
      } never matched in this sample`}
      body={`Remove them to cut noise and cost: ${preview}${extra}.`}
      applyLabel={`Disable ${rules.length} ${
        rules.length === 1 ? "rule" : "rules"
      }`}
      applied={applied}
      onApply={handleApply}
    />
  );
}

function ScopeConcentrationCard({
  concentration,
  form,
}: {
  concentration: ScopeConcentration;
  form: PolicyForm;
}) {
  const [applied, setApplied] = useState(false);
  const pct = Math.round(concentration.share * 100);
  const label = roleLabel(concentration.role);
  const handleApply = () => {
    form.setters.setScopeMode("messageTypes");
    form.setters.setSelectedMessageTypes(new Set([concentration.messageType]));
    setApplied(true);
  };
  return (
    <SuggestionCard
      icon="crosshair"
      title={`${pct}% of findings are in ${label}`}
      body={`Scope the policy to ${label} to skip scanning the rest and cut cost.`}
      applyLabel={`Scope to ${label}`}
      applied={applied}
      onApply={handleApply}
    />
  );
}
