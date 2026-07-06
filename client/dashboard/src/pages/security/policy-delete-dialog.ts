import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import {
  DETECTION_RULES,
  RULE_CATEGORY_META,
  type RuleCategory,
} from "./policy-data";
import { ruleIdToPresidioEntity } from "./rule-ids";

const DELETE_RULE_LIST_LIMIT = 4;

const PRESIDIO_CATEGORIES: RuleCategory[] = [
  "financial",
  "pii",
  "government_ids",
  "healthcare",
];

const CATEGORY_LEVEL_SOURCE_BY_CATEGORY: Partial<Record<RuleCategory, string>> =
  {
    prompt_injection: "prompt_injection",
    shadow_mcp: "shadow_mcp",
    destructive_tool: "destructive_tool",
    cli_destructive: "cli_destructive",
  };

function presidioCategories(policy: RiskPolicy): RuleCategory[] {
  if (!policy.sources.includes("presidio")) {
    return [];
  }

  const entitySet = new Set(policy.presidioEntities ?? []);
  if (entitySet.size === 0) {
    return PRESIDIO_CATEGORIES;
  }

  return PRESIDIO_CATEGORIES.filter((category) =>
    DETECTION_RULES[category].some((rule) =>
      entitySet.has(ruleIdToPresidioEntity(rule.id)),
    ),
  );
}

export function getPolicyRuleGroupNamesForDeleteDialog(
  policy: RiskPolicy,
): string[] {
  if (policy.policyType === "prompt_based") {
    return [];
  }

  const categories: RuleCategory[] = [];

  if (policy.sources.includes("gitleaks")) {
    categories.push("secrets");
  }

  categories.push(...presidioCategories(policy));

  for (const [category, source] of Object.entries(
    CATEGORY_LEVEL_SOURCE_BY_CATEGORY,
  ) as Array<[RuleCategory, string]>) {
    if (policy.sources.includes(source)) {
      categories.push(category);
    }
  }

  if (policy.customRuleIds?.length) {
    categories.push("custom");
  }

  return categories.map((category) => RULE_CATEGORY_META[category].label);
}

export function getPolicyDeleteRuleActionLabel(policy: RiskPolicy): string {
  return policy.action === "block" ? "block" : "flag";
}

export function getPolicyDeleteRuleListItems(ruleNames: string[]): string[] {
  if (ruleNames.length <= DELETE_RULE_LIST_LIMIT) {
    return ruleNames;
  }

  return [
    ...ruleNames.slice(0, DELETE_RULE_LIST_LIMIT),
    `and ${ruleNames.length - DELETE_RULE_LIST_LIMIT} more`,
  ];
}
