import {
  type RiskMatchCondition,
  type RiskMatchConfig,
} from "@gram/client/models/components";
import {
  invalidateAllRiskListCustomDetectionRules,
  useRiskCreateCustomDetectionRuleMutation,
  useRiskDeleteCustomDetectionRuleMutation,
  useRiskListCustomDetectionRules,
  useRiskUpdateCustomDetectionRuleMutation,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import {
  DETECTION_RULES,
  type PolicyMessageType,
  type RuleCategory,
} from "./policy-data";

export type { RiskMatchCondition, RiskMatchConfig };

/** Severity levels assigned to a detection rule. Drives how findings show
 *  up in dashboards and (eventually) which actions a policy is allowed to
 *  take. Ordered low to high. */
export const SEVERITY_LEVELS = [
  "info",
  "low",
  "medium",
  "high",
  "critical",
] as const;

export type SeverityLevel = (typeof SEVERITY_LEVELS)[number];

/** Default severity for builtin rules. Driven by category since the
 *  underlying detectors are uniform within a category. Individual rules
 *  can override via the Detection Rules page (stored locally for now). */
const CATEGORY_DEFAULT_SEVERITY: Record<RuleCategory, SeverityLevel> = {
  secrets: "high",
  financial: "medium",
  pii: "medium",
  government_ids: "medium",
  healthcare: "medium",
  prompt_policy: "medium",
  prompt_injection: "medium",
  off_policy: "medium",
  shadow_mcp: "medium",
  destructive_tool: "medium",
  cli_destructive: "medium",
  custom: "medium",
};

/** Generic rule descriptions for categories where rule entries carry only
 *  a title. We don't have per-rule prose for the gitleaks/Presidio catalog
 *  so we surface a category-level explanation. */
const CATEGORY_RULE_DESCRIPTION: Record<RuleCategory, string> = {
  secrets:
    "Regex-backed detector tuned to the issuing service's token format. Flags credentials that match the canonical shape of the underlying provider.",
  financial:
    "Pattern + checksum detector for financial identifiers. Validates the structure (length, check digit, BIN range) before reporting a match.",
  pii: "Pattern detector for personal identifiable information embedded in free-form text. Anchors to the canonical format of the field.",
  government_ids:
    "Pattern + checksum detector for government-issued identifiers, validated against the issuer's format and check-digit rules.",
  healthcare:
    "Pattern detector for healthcare identifiers and clinical references in free-form text.",
  prompt_policy:
    "Natural-language guardrail evaluated by the policy judge against agent activity.",
  prompt_injection:
    "Hybrid detector that combines classifier scoring with regex and keyword heuristics to flag attempts to override the agent's instructions.",
  off_policy:
    "Classifier-backed detector for requests that fall outside the organization's acceptable-use policy.",
  shadow_mcp:
    "Detects MCP tool calls in Cursor and Claude Code that didn't originate from a Speakeasy-issued MCP server. Requires Speakeasy hooks on the agent.",
  destructive_tool:
    "Flags tool calls whose Gram tool definition is annotated as destructive. Requires Speakeasy hooks and Gram-issued tool metadata.",
  cli_destructive:
    "Pattern detector for destructive shell, git, database, and cloud CLI invocations passed through tool arguments.",
  custom:
    "Organization-defined regex pattern. Matches anywhere in the scanned payload.",
};

export type BuiltinRule = {
  id: string;
  title: string;
  description: string;
  category: RuleCategory;
  defaultSeverity: SeverityLevel;
};

/** Synthetic single-rule entries for categories where the category itself
 *  acts as the detector (no granular sub-rules to expose). */
const SYNTHETIC_CATEGORY_RULES: Partial<
  Record<RuleCategory, { id: string; title: string }>
> = {
  prompt_injection: {
    id: "prompt_injection.default",
    title: "Prompt Injection",
  },
  prompt_policy: {
    id: "llm_judge",
    title: "LLM Judge",
  },
  shadow_mcp: {
    id: "shadow_mcp.default",
    title: "Unverified MCP Tool Call",
  },
  destructive_tool: {
    id: "destructive_tool.default",
    title: "Destructive Tool Annotation",
  },
};

/** Flattened, category-keyed view of every builtin rule. Drives the
 *  Detection Rules listing and uniqueness checks for custom rule ids. */
export const BUILTIN_RULES_BY_CATEGORY: Record<RuleCategory, BuiltinRule[]> = (
  Object.keys(DETECTION_RULES) as RuleCategory[]
).reduce(
  (acc, category) => {
    // Hidden rules (deprecated / unreliable upstream scanner) stay in
    // DETECTION_RULES so legacy risk_results keep resolving their title via
    // risk-utils, but they are dropped from the visible Detection Rules
    // catalog so users never see them as a selectable/listed rule.
    const catalog = DETECTION_RULES[category].filter((r) => !r.hidden);
    const description = CATEGORY_RULE_DESCRIPTION[category];
    const severity = CATEGORY_DEFAULT_SEVERITY[category];
    if (catalog.length > 0) {
      acc[category] = catalog.map((r) => ({
        id: r.id,
        title: r.title,
        description,
        category,
        defaultSeverity: severity,
      }));
      return acc;
    }
    const synthetic = SYNTHETIC_CATEGORY_RULES[category];
    if (synthetic) {
      acc[category] = [
        {
          id: synthetic.id,
          title: synthetic.title,
          description,
          category,
          defaultSeverity: severity,
        },
      ];
      return acc;
    }
    acc[category] = [];
    return acc;
  },
  {} as Record<RuleCategory, BuiltinRule[]>,
);

/** All builtin rule ids, used for custom rule id collision checks. Includes
 *  hidden/deprecated rule ids (which BUILTIN_RULES_BY_CATEGORY omits) so a
 *  custom rule can never reuse an id that legacy findings still resolve. */
const BUILTIN_RULE_IDS = new Set<string>([
  ...Object.values(BUILTIN_RULES_BY_CATEGORY).flatMap((rules) =>
    rules.map((r) => r.id),
  ),
  ...Object.values(DETECTION_RULES).flatMap((rules) => rules.map((r) => r.id)),
]);

/** Rule polarity. `deny` flags a finding when matched (the default); `allow`
 *  is an inline allowlist that short-circuits the whole policy for a message. */
export type CustomRuleEffect = "deny" | "allow";

export type CustomDetectionRule = {
  id: string;
  dbId: string;
  title: string;
  description: string;
  /** Legacy single pattern. Surfaced as a content/regex condition by
   *  ruleConditions when matchConfig is absent. */
  regex: string;
  matchConfig: RiskMatchConfig | null;
  severity: SeverityLevel;
  createdAt: string;
  updatedAt: string;
};

/** Fields needed to create or fully edit a custom rule's matcher. */
export type CustomRuleDraft = {
  id: string;
  title: string;
  description: string;
  conditions: RiskMatchCondition[];
  effect: CustomRuleEffect;
  severity: SeverityLevel;
};

function mapCustomDetectionRule(rule: {
  id: string;
  ruleId: string;
  title: string;
  description: string;
  regex: string;
  matchConfig?: RiskMatchConfig | null;
  severity: string;
  createdAt: Date;
  updatedAt: Date;
}): CustomDetectionRule {
  return {
    id: rule.ruleId,
    dbId: rule.id,
    title: rule.title,
    description: rule.description,
    regex: rule.regex,
    matchConfig: rule.matchConfig ?? null,
    severity: rule.severity as SeverityLevel,
    createdAt: rule.createdAt.toISOString(),
    updatedAt: rule.updatedAt.toISOString(),
  };
}

function useDetectionRulesStoreImpl() {
  const queryClient = useQueryClient();
  const rulesQuery = useRiskListCustomDetectionRules();

  const invalidate = () =>
    invalidateAllRiskListCustomDetectionRules(queryClient);

  const createMutation = useRiskCreateCustomDetectionRuleMutation({
    onSuccess: invalidate,
  });

  const updateMutation = useRiskUpdateCustomDetectionRuleMutation({
    onSuccess: invalidate,
  });

  const deleteMutation = useRiskDeleteCustomDetectionRuleMutation({
    onSuccess: invalidate,
  });

  const customRules = rulesQuery.data?.rules.map(mapCustomDetectionRule) ?? [];

  return {
    customRules,
    isLoading: rulesQuery.isLoading,
    error: rulesQuery.error,
    addCustomRule: (rule: CustomRuleDraft) =>
      createMutation.mutate({
        request: {
          createCustomDetectionRuleRequestBody: {
            ruleId: rule.id,
            title: rule.title,
            description: rule.description,
            matchConfig: buildMatchConfig(rule.conditions, rule.effect),
            severity: rule.severity,
          },
        },
      }),
    updateCustomRule: (
      id: string,
      patch: Partial<Omit<CustomRuleDraft, "id">>,
    ) => {
      const rule = customRules.find((r) => r.id === id);
      if (!rule) {
        return Promise.reject(new Error("Custom detection rule not found"));
      }
      const conditions = patch.conditions ?? ruleConditions(rule);
      const effect = patch.effect ?? ruleEffect(rule);
      return updateMutation
        .mutateAsync({
          request: {
            updateCustomDetectionRuleRequestBody: {
              id: rule.dbId,
              title: patch.title ?? rule.title,
              description: patch.description ?? rule.description,
              matchConfig: buildMatchConfig(conditions, effect),
              severity: patch.severity ?? rule.severity,
            },
          },
        })
        .then(() => undefined);
    },
    removeCustomRule: (id: string) => {
      const rule = customRules.find((r) => r.id === id);
      if (!rule) return;
      deleteMutation.mutate({
        request: {
          riskIDRequestBody: {
            id: rule.dbId,
          },
        },
      });
    },
  };
}

export function useDetectionRulesStore(): ReturnType<
  typeof useDetectionRulesStoreImpl
> {
  return useDetectionRulesStoreImpl();
}

/** Validate a proposed custom rule id. Returns an error message if the id
 *  collides with a builtin or an existing custom rule, or is malformed. */
export function validateCustomRuleId(
  id: string,
  existingCustomIds: string[],
): string | null {
  const trimmed = id.trim();
  if (!trimmed) return "Rule ID is required";
  if (!/^custom\.[a-z0-9_]+$/.test(trimmed)) {
    return "Use lowercase letters, digits, or underscores after custom.";
  }
  if (BUILTIN_RULE_IDS.has(trimmed)) {
    return "This ID collides with a built-in rule";
  }
  if (existingCustomIds.includes(trimmed)) {
    return "Another custom rule already uses this ID";
  }
  return null;
}

/** Validate a proposed regex pattern. Tries to compile and surface a human
 *  message if the engine rejects it. */
export function validateRegex(pattern: string): string | null {
  const trimmed = pattern.trim();
  if (!trimmed) return "Regex is required";
  try {
    new RegExp(trimmed);
    return null;
  } catch (err) {
    return err instanceof Error ? err.message : "Invalid regex";
  }
}

/* -------------------------------------------------------------------------- */
/*  Condition (match_config) helpers — shared by the query-builder UI         */
/* -------------------------------------------------------------------------- */

type MatchTarget = RiskMatchCondition["target"];
type MatchOp = RiskMatchCondition["op"];

export const MATCH_TARGETS = [
  "content",
  "user_prompt",
  "assistant_text",
  "tool_result",
  "tool_name",
  "tool_server",
  "tool_function",
  "tool_args",
] as const satisfies readonly MatchTarget[];

export const MATCH_OPS = [
  "regex",
  "equals",
  "not_equals",
  "glob",
  "keyword",
  "exists",
] as const satisfies readonly MatchOp[];

export const TARGET_LABELS: Record<MatchTarget, string> = {
  content: "Message content",
  user_prompt: "User prompt",
  assistant_text: "Assistant text",
  tool_result: "Tool result",
  tool_name: "Tool name",
  tool_server: "Tool server (MCP)",
  tool_function: "Tool function",
  tool_args: "Tool argument",
};

export const OP_LABELS: Record<MatchOp, string> = {
  regex: "matches regex",
  equals: "equals",
  not_equals: "does not equal",
  glob: "matches glob",
  keyword: "contains keyword",
  exists: "is present",
};

/** Message type each target is scoped to (a tool_server condition only matches
 *  tool-request messages, etc). `null` means the target applies to any type.
 *  Drives the PolicyCenter coverage warning. */
export const TARGET_MESSAGE_TYPE: Record<
  MatchTarget,
  PolicyMessageType | null
> = {
  content: null,
  user_prompt: "user_message",
  assistant_text: "assistant_message",
  tool_result: "tool_response",
  tool_name: "tool_request",
  tool_server: "tool_request",
  tool_function: "tool_request",
  tool_args: "tool_request",
};

export function defaultCondition(): RiskMatchCondition {
  return { target: "content", op: "regex", value: "" };
}

export function buildMatchConfig(
  conditions: RiskMatchCondition[],
  effect: CustomRuleEffect,
): RiskMatchConfig {
  return { effect, combine: "and", conditions };
}

/** Effective conditions for a rule — its match_config, or the legacy regex
 *  surfaced as a single content/regex condition. */
export function ruleConditions(
  rule: CustomDetectionRule,
): RiskMatchCondition[] {
  if (rule.matchConfig && rule.matchConfig.conditions.length > 0) {
    return rule.matchConfig.conditions;
  }
  if (rule.regex) {
    return [{ target: "content", op: "regex", value: rule.regex }];
  }
  return [];
}

export function ruleEffect(rule: CustomDetectionRule): CustomRuleEffect {
  return (rule.matchConfig?.effect as CustomRuleEffect) ?? "deny";
}

/** Message types a rule can ever match, inferred from its condition targets.
 *  Empty means "any" (a content-only rule). */
export function ruleRequiredMessageTypes(
  conditions: RiskMatchCondition[],
): PolicyMessageType[] {
  const types = new Set<PolicyMessageType>();
  for (const c of conditions) {
    const t = TARGET_MESSAGE_TYPE[c.target];
    if (t) types.add(t);
  }
  return [...types];
}

export function validateCondition(c: RiskMatchCondition): string | null {
  if (c.target === "tool_args" && !(c.path ?? "").trim()) {
    return "Tool argument conditions need a JSON path";
  }
  switch (c.op) {
    case "regex":
      return validateRegex(c.value ?? "");
    case "glob":
      return (c.value ?? "").trim() ? null : "Glob pattern is required";
    case "keyword":
      return (c.values ?? []).some((v) => v.trim())
        ? null
        : "Add at least one keyword";
    // equals / not_equals / exists — empty value is allowed.
    case "equals":
    case "not_equals":
    case "exists":
      return null;
  }
}

export function validateConditions(
  conditions: RiskMatchCondition[],
): string | null {
  if (conditions.length === 0) return "Add at least one condition";
  for (const c of conditions) {
    const err = validateCondition(c);
    if (err) return err;
  }
  return null;
}

const OP_SYMBOL: Record<MatchOp, string> = {
  regex: "~",
  equals: "=",
  not_equals: "≠",
  glob: "glob",
  keyword: "∋",
  exists: "exists",
};

function summarizeCondition(c: RiskMatchCondition): string {
  const target =
    c.target === "tool_args" && c.path ? `tool_args(${c.path})` : c.target;
  if (c.op === "exists") return `${target} present`;
  if (c.op === "keyword") return `${target} ∋ ${(c.values ?? []).join("/")}`;
  return `${target} ${OP_SYMBOL[c.op]} ${JSON.stringify(c.value ?? "")}`;
}

/** Human-readable one-liner summarizing a rule's conditions, for list rows. */
export function summarizeConditions(conditions: RiskMatchCondition[]): string {
  if (conditions.length === 0) return "No matcher configured";
  return conditions.map(summarizeCondition).join(" AND ");
}

/** List-row summary: an allow prefix plus the condition summary. */
export function ruleSummary(rule: CustomDetectionRule): string {
  const prefix = ruleEffect(rule) === "allow" ? "allow · " : "";
  return prefix + summarizeConditions(ruleConditions(rule));
}
