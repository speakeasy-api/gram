import type { PageTab } from "@/components/page-templates";

// The Shadow AI page has two tabs: the MCP servers people's agents use, and
// the AI tools the device agents look for. `tab` is parsed as a string
// literal union, so an unknown `?tab=` lands on the first tab.
export const SHADOW_AI_TABS = ["mcp-servers", "ai-tools"] as const;

export type ShadowAITab = (typeof SHADOW_AI_TABS)[number];

const TAB_LABEL: Record<ShadowAITab, string> = {
  "mcp-servers": "MCP servers",
  "ai-tools": "AI tools",
};

/** The query string that selects a tab, for links that land on one from
 *  elsewhere: append it to the Shadow AI href. */
export function shadowAITabSearch(tab: ShadowAITab): string {
  return `?tab=${tab}`;
}

export function shadowAITabs(): PageTab[] {
  return SHADOW_AI_TABS.map((tab) => ({
    value: tab,
    label: TAB_LABEL[tab],
    href: shadowAITabSearch(tab),
  }));
}

// The tool and server detail pages sit under the old category segments, so
// their breadcrumbs walk through them. The category segments have no page of
// their own; each one links to the tab that lists its rows.
export const shadowAIBreadcrumbSubstitutions: Record<string, string> = {
  "shadow-ai": "Shadow AI",
  harnesses: TAB_LABEL["ai-tools"],
  assistants: TAB_LABEL["ai-tools"],
  models: TAB_LABEL["ai-tools"],
  mcps: TAB_LABEL["mcp-servers"],
};

export const shadowAIBreadcrumbUrlSubstitutions: Record<string, string> = {
  "/shadow-ai/harnesses": `/shadow-ai${shadowAITabSearch("ai-tools")}`,
  "/shadow-ai/assistants": `/shadow-ai${shadowAITabSearch("ai-tools")}`,
  "/shadow-ai/models": `/shadow-ai${shadowAITabSearch("ai-tools")}`,
  "/shadow-ai/mcps": `/shadow-ai${shadowAITabSearch("mcp-servers")}`,
};
