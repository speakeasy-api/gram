const EMPTY_RULE_ID = "__none";

export function riskRuleKey(source: string, ruleId: string): string {
  return `${source}\u0000${ruleId ? `value:${ruleId}` : `empty:${EMPTY_RULE_ID}`}`;
}
