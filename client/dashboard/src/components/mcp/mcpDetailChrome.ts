import type { ReactNode } from "react";
import type { DetailSidebarNavItem } from "@/components/detail/detail-sidebar-nav";
import type { ReadinessCheck } from "@/components/mcp-server-readiness-bar";

/** Everything an MCP detail page's header shows: servers and gateways alike. */
export interface McpDetailChrome {
  name: string;
  /** The server's logo, sized by the caller. */
  icon: (className: string) => ReactNode;
  kindLabel: string | undefined;
  /** The enable switch or visibility dropdown, for the server's kind. */
  statusControl: ReactNode;
  /** The URL clients connect to, when there is one. */
  primaryUrl: string | undefined;
  readinessChecks: ReadinessCheck[];
  /** The provider setup guide, for servers whose upstream has one. */
  setupGuide?: ReactNode;
  /** Whose identity the server calls its upstream with, for remote servers. */
  identity?: ReactNode;
  /** Install pages to hand to teammates. */
  installLinks: { url: string; label: string }[];
  items: DetailSidebarNavItem[];
  /** The header's top-right card, e.g. which plugins distribute the server. */
  aside?: ReactNode;
}
