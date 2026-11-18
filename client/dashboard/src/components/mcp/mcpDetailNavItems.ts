import type { DetailSidebarNavItem } from "@/components/detail/detail-sidebar-nav";
import {
  LayoutDashboard,
  Plug,
  ScanSearch,
  Settings as SettingsIcon,
  Terminal,
  Users,
  type LucideIcon,
} from "lucide-react";
import type { ReactNode } from "react";

/** A tab only one kind of MCP detail page has, with its own title and icon. */
export interface McpDetailTabDef<Key extends string = string> {
  key: Key;
  title: string;
  Icon: LucideIcon;
}

/** The tabs servers and gateways share, titled and iconed once. */
const SHARED_TABS = {
  overview: { key: "overview", title: "Overview", Icon: LayoutDashboard },
  inspect: { key: "inspect", title: "Inspect", Icon: ScanSearch },
  skills: { key: "skills", title: "Skills", Icon: Terminal },
  "team-access": { key: "team-access", title: "Team Access", Icon: Users },
  sessions: { key: "sessions", title: "Clients and Sessions", Icon: Plug },
  settings: { key: "settings", title: "Settings", Icon: SettingsIcon },
} satisfies Record<string, McpDetailTabDef>;

export type SharedMcpDetailTab = keyof typeof SHARED_TABS;

/**
 * Builds an MCP detail page's tabs in the order given. Each entry names a
 * shared tab or carries a page-only tab's definition; falsy entries are the
 * tabs this viewer or this kind of server does not get.
 */
export function buildMcpDetailNavItems<Tab extends string>({
  tabs,
  activeTab,
  href,
  titleNodes,
}: {
  // Shared tabs only when the page routes them, and page-only tabs keyed by
  // the page's own tab values, so every key is one `href` accepts.
  tabs: Array<
    (SharedMcpDetailTab & Tab) | McpDetailTabDef<Tab> | false | null | undefined
  >;
  activeTab: Tab | undefined;
  href: (tab: Tab) => string;
  titleNodes?: Partial<Record<Tab, ReactNode>>;
}): DetailSidebarNavItem[] {
  return tabs.flatMap((tab) => {
    if (!tab) return [];
    const def: McpDetailTabDef<Tab> =
      typeof tab === "string" ? { ...SHARED_TABS[tab], key: tab } : tab;
    const titleNode = titleNodes?.[def.key];
    return [
      {
        ...def,
        ...(titleNode !== undefined ? { titleNode } : {}),
        href: href(def.key),
        active: activeTab === def.key,
      },
    ];
  });
}
