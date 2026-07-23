import type { RuleCategory } from "./policy-data";
import { ALL_CATEGORIES } from "./policy-form";

export const SHADOW_MCP_DISABLED_REASON =
  "Turn off other built-in rules to select Shadow MCP.";

export const OTHER_BUILT_IN_RULE_DISABLED_REASON =
  "Turn off Shadow MCP to select this rule.";

export function builtInRuleDisabledReason(
  category: RuleCategory,
  selectedCategories: ReadonlySet<RuleCategory>,
): string | undefined {
  if (selectedCategories.has(category)) return undefined;

  if (category === "shadow_mcp") {
    const anotherBuiltInRuleSelected = ALL_CATEGORIES.some(
      (selectedCategory) =>
        selectedCategory !== "shadow_mcp" &&
        selectedCategories.has(selectedCategory),
    );
    return anotherBuiltInRuleSelected ? SHADOW_MCP_DISABLED_REASON : undefined;
  }

  if (category !== "custom" && selectedCategories.has("shadow_mcp")) {
    return OTHER_BUILT_IN_RULE_DISABLED_REASON;
  }

  return undefined;
}
