import {
  invalidateAllRiskListCustomDetectionRules,
  useRiskCreateCustomDetectionRuleMutation,
  useRiskDeleteCustomDetectionRuleMutation,
  useRiskListCustomDetectionRules,
  useRiskUpdateCustomDetectionRuleMutation,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { DETECTION_RULES, type RuleCategory } from "./policy-data";

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

export type CustomDetectionRule = {
  id: string;
  dbId: string;
  title: string;
  description: string;
  regex: string;
  severity: SeverityLevel;
  createdAt: string;
  updatedAt: string;
};

function mapCustomDetectionRule(rule: {
  id: string;
  ruleId: string;
  title: string;
  description: string;
  regex: string;
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
    severity: rule.severity as SeverityLevel,
    createdAt: rule.createdAt.toISOString(),
    updatedAt: rule.updatedAt.toISOString(),
  };
}

export function useDetectionRulesStore() {
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
    addCustomRule: (
      rule: Omit<CustomDetectionRule, "dbId" | "createdAt" | "updatedAt">,
    ) =>
      createMutation.mutate({
        request: {
          createCustomDetectionRuleRequestBody: {
            ruleId: rule.id,
            title: rule.title,
            description: rule.description,
            regex: rule.regex,
            severity: rule.severity,
          },
        },
      }),
    updateCustomRule: (
      id: string,
      patch: Partial<Omit<CustomDetectionRule, "id" | "dbId" | "createdAt">>,
    ) => {
      const rule = customRules.find((r) => r.id === id);
      if (!rule) {
        return Promise.reject(new Error("Custom detection rule not found"));
      }
      return updateMutation
        .mutateAsync({
          request: {
            updateCustomDetectionRuleRequestBody: {
              id: rule.dbId,
              title: patch.title ?? rule.title,
              description: patch.description ?? rule.description,
              regex: patch.regex ?? rule.regex,
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
