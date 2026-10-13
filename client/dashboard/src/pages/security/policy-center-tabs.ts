import type { PageTab } from "@/components/page-templates";

// Suppressed findings used to be a third tab here; they now live in the
// Watchdog page's Suppressed section. `tab` is parsed as a string literal
// union, so a stale `?tab=dismissed` link falls back to "policies" rather
// than rendering an empty page.
export const POLICY_CENTER_TABS = [
  "policies",
  "detection-rules",
  "shadow-ai-targets",
  "exclusions",
  "quarantines",
] as const;

export type PolicyCenterTab = (typeof POLICY_CENTER_TABS)[number];

const TAB_LABEL: Record<PolicyCenterTab, string> = {
  policies: "Policies",
  "detection-rules": "Detection Rules",
  "shadow-ai-targets": "Shadow AI targets",
  exclusions: "Exclusion Rules",
  quarantines: "Quarantines",
};

/** Which tabs the strip shows. The Shadow AI targets tab arrives with the
 *  Shadow AI fold-in flag; without it the page keeps its four tabs. */
function visibleTabs(shadowAIEnabled: boolean): PolicyCenterTab[] {
  return POLICY_CENTER_TABS.filter(
    (tab) => shadowAIEnabled || tab !== "shadow-ai-targets",
  );
}

export function policyCenterTabs(shadowAIEnabled: boolean): PageTab[] {
  return visibleTabs(shadowAIEnabled).map((tab) => ({
    value: tab,
    label: TAB_LABEL[tab],
    href: `?tab=${tab}`,
  }));
}

/** A `?tab=` value the strip does not show falls back to Policies, so a
 *  Shadow AI targets link opened with the flag off still lands somewhere. */
export function visiblePolicyCenterTab(
  tab: PolicyCenterTab,
  shadowAIEnabled: boolean,
): PolicyCenterTab {
  return visibleTabs(shadowAIEnabled).includes(tab) ? tab : "policies";
}
