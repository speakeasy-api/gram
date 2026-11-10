import { ACCENT_RED, SEVERITY } from "@/components/chart/palette";
import type { RuleCategory } from "./policy-data";

// Editorial severity-first ramp from the shared chart palette: the worst
// category (secrets) takes the one red accent, the next tiers take the
// severity oranges, and the rest walk the categorical ramp (hue repeats are
// fine — lines read by legend label, not hue). No category takes a grey: grey
// is reserved for the dimmed/inactive state in the exposure bar. Every index
// here must exist in SERIES; a slot past its end silently drops the line back
// to Chart.js's own default color.
function riskCategoryChartColors(
  series: readonly string[],
): Array<{ category: RuleCategory; color: string }> {
  return [
    { category: "secrets", color: ACCENT_RED },
    { category: "financial", color: SEVERITY.high },
    { category: "pii", color: SEVERITY.medium },
    { category: "government_ids", color: series[0]! },
    { category: "healthcare", color: series[1]! },
    { category: "prompt_policy", color: series[2]! },
    { category: "prompt_injection", color: series[3]! },
    { category: "off_policy", color: series[4]! },
    { category: "shadow_mcp", color: series[5]! },
    { category: "destructive_tool", color: series[1]! },
    { category: "cli_destructive", color: series[2]! },
    { category: "account_identity", color: series[3]! },
    { category: "custom", color: series[6]! },
  ];
}

// Resolves one category's chart color from the theme-resolved series ramp
// (callers pass useSeriesColors()), so the Watchdog exposure bar keeps its
// palette in both themes.
export function getRiskCategoryChartColor(
  category: string,
  series: readonly string[],
): string | undefined {
  return riskCategoryChartColors(series).find(
    (entry) => entry.category === category,
  )?.color;
}
