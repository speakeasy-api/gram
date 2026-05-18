import { DETECTION_RULES, type RuleCategory } from "./policy-data";
import { humanizeRuleId } from "./rule-ids";

const SOURCE_TO_CATEGORY: ReadonlyMap<string, RuleCategory> = new Map<
  string,
  RuleCategory
>([
  ["destructive_tool", "destructive_tool"],
  ["shadow_mcp", "shadow_mcp"],
  ["prompt_injection", "prompt_injection"],
  ["cli_destructive", "cli_destructive"],
]);

const ruleIdToCategory = new Map<string, RuleCategory>();
const ruleIdToTitle = new Map<string, string>();

for (const [category, rules] of Object.entries(DETECTION_RULES)) {
  for (const rule of rules) {
    ruleIdToCategory.set(rule.id, category as RuleCategory);
    ruleIdToTitle.set(rule.id, rule.title);
  }
}

// DETECTION_RULES.id is the canonical rule_id the backend writes to
// risk_results, so lookup maps key by it directly.
const RULE_ID_TO_CATEGORY: ReadonlyMap<string, RuleCategory> = ruleIdToCategory;
const RULE_ID_TO_TITLE: ReadonlyMap<string, string> = ruleIdToTitle;

export function getRuleTitleFallback(ruleId: string | undefined): string {
  if (!ruleId) return "-";
  return RULE_ID_TO_TITLE.get(ruleId) ?? humanizeRuleId(ruleId);
}

export function getCategoryForFinding(
  source?: string,
  ruleId?: string,
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
