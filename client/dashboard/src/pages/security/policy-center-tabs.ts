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

export function policyCenterTabs(): PageTab[] {
  return POLICY_CENTER_TABS.map((tab) => ({
    value: tab,
    label: TAB_LABEL[tab],
    href: `?tab=${tab}`,
  }));
}
