import { DETECTION_RULES, type RuleCategory } from "./policy-data";
import { humanizeRuleId } from "./rule-ids";

// DETECTION_RULES.id is the canonical rule_id the backend writes to
// risk_results, so lookup maps key by it directly.
export const RULE_ID_TO_CATEGORY = new Map<string, RuleCategory>();

export const RULE_ID_TO_TITLE = new Map<string, string>();

for (const [category, rules] of Object.entries(DETECTION_RULES)) {
  for (const rule of rules) {
    RULE_ID_TO_CATEGORY.set(rule.id, category as RuleCategory);
    RULE_ID_TO_TITLE.set(rule.id, rule.title);
  }
}

export function getRuleTitleFallback(ruleId: string | undefined): string {
  if (!ruleId) return "-";
  return RULE_ID_TO_TITLE.get(ruleId) ?? humanizeRuleId(ruleId);
}

export const SOURCE_TO_CATEGORY = new Map<string, RuleCategory>([
  ["destructive_tool", "destructive_tool"],
  ["shadow_mcp", "shadow_mcp"],
  ["prompt_injection", "prompt_injection"],
  ["cli_destructive", "cli_destructive"],
]);

export function getCategoryForFinding(
  source: string | undefined,
  ruleId: string | undefined,
): RuleCategory | null {
  if (ruleId) {
    const byRule = RULE_ID_TO_CATEGORY.get(ruleId);
    if (byRule) return byRule;
  }
  if (source) {
    return SOURCE_TO_CATEGORY.get(source) ?? null;
  }
  return null;
}
