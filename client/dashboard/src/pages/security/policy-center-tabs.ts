import type { PageTab } from "@/components/page-templates";

// Suppressed findings used to be a third tab here; they now live in the
// Watchdog page's Suppressed section. `tab` is parsed as a string literal
// union, so a stale `?tab=dismissed` link falls back to "policies" rather
// than rendering an empty page.
export const POLICY_CENTER_TABS = [
  "policies",
  "detection-rules",
  "shadow-ai-targets",
  "mcp-approvals",
  "tool-approvals",
  "exclusions",
  "quarantines",
] as const;

export type PolicyCenterTab = (typeof POLICY_CENTER_TABS)[number];

const TAB_LABEL: Record<PolicyCenterTab, string> = {
  policies: "Policies",
  "detection-rules": "Detection Rules",
  "shadow-ai-targets": "Shadow AI targets",
  "mcp-approvals": "MCP approvals",
  "tool-approvals": "Tool approvals",
  exclusions: "Exclusion Rules",
  quarantines: "Quarantines",
};

export function policyCenterTabLabel(tab: PolicyCenterTab): string {
  return TAB_LABEL[tab];
}

/** The query string that selects a tab, for links that land on one from
 *  elsewhere: append it to the Guardrails href. */
export function policyCenterTabSearch(tab: PolicyCenterTab): string {
  return `?tab=${tab}`;
}

/** The tabs to show. Tool approvals is behind its rollout flag. */
export function policyCenterTabs({
  toolApprovals = false,
}: { toolApprovals?: boolean } = {}): PageTab[] {
  return POLICY_CENTER_TABS.filter(
    (tab) => toolApprovals || tab !== "tool-approvals",
  ).map((tab) => ({
    value: tab,
    label: TAB_LABEL[tab],
    href: policyCenterTabSearch(tab),
  }));
}
