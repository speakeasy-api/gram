import type { ReactNode } from "react";
import type { DetailSidebarNavItem } from "@/components/detail/detail-sidebar-nav";
import type { ReadinessCheck } from "@/components/mcp-server-readiness-bar";

import { SETTINGS_ENVIRONMENT_VARIABLES_ID } from "@/components/detail/settings-groups";

/** Anchors the Environment variables block on a server's Settings tab. */
export const MCP_ENVIRONMENT_VARIABLES_SECTION_ID =
  SETTINGS_ENVIRONMENT_VARIABLES_ID;

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
  /** Every address the server answers on, when it has more than one. */
  addresses?: { url: string; label: string }[];
  /** Whose account the server acts as, for servers that sign in upstream. */
  identity?: { label: string; href: string; warn?: boolean };
  readinessChecks: McpReadinessCheck[];
  /** Opens the Setup modal over Overview, when this server offers Setup. */
  setupHref?: string;
  /** Install pages to hand to teammates. */
  installLinks: { url: string; label: string }[];
  items: DetailSidebarNavItem[];
  /** Overview's first card while a guided provider setup is unfinished. */
  overviewSetup?: ReactNode;
  /** Overview's lead card: how the server reaches the team, with Install. */
  overviewBanner?: ReactNode;
}

/** A header readiness check, with what the chip and popover need on top. */
export type McpReadinessCheck = ReadinessCheck & {
  /**
   * Still loading. The chip names no step while any check is pending, so
   * the order the queries settle in never changes what it says.
   */
  pending?: boolean;
  /**
   * The chip's words when this is the only open item, e.g. "Off".
   * Short, so the header row never wraps; the popover has the full text.
   */
  chipLabel?: string;
  /** The server's own words, shown on hover only, never as the message. */
  detail?: string;
};
