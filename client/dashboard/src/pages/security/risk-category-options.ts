import type { FilterOption } from "@/components/filters";
import { RULE_CATEGORY_META, type RuleCategory } from "./policy-data";

/** One option per finding category, in the order the detector cards use, so
 *  the Risk Events filter and the policy editor read the same list. */
export function riskCategoryFilterOptions(): FilterOption[] {
  return (Object.keys(RULE_CATEGORY_META) as RuleCategory[]).map(
    (category) => ({
      label: RULE_CATEGORY_META[category].label,
      value: category,
    }),
  );
}
