import type { ReactNode } from "react";
import type { DetailSidebarNavItem } from "@/components/detail/detail-sidebar-nav";
import type { ReadinessCheck } from "@/components/mcp-server-readiness-bar";

/** Anchors the Environment variables block on a server's Settings tab. */
export const MCP_ENVIRONMENT_VARIABLES_SECTION_ID =
  "settings-environment-variables";

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
  readinessChecks: McpReadinessCheck[];
  /** The provider setup guide, for servers whose upstream has one. */
  setupGuide?: ReactNode;
  /** Install pages to hand to teammates. */
  installLinks: { url: string; label: string }[];
  items: DetailSidebarNavItem[];
  /** The header's top-right card, e.g. which plugins distribute the server. */
  aside?: ReactNode;
}

/** A header readiness check, with what the chip and popover need on top. */
export type McpReadinessCheck = ReadinessCheck & {
  /**
   * Still loading. The chip names no step while any check is pending, so
   * the order the queries settle in never changes what it says.
   */
  pending?: boolean;
  /**
   * The chip's words when this is the only open item, e.g. "Turned off".
   * Short, so the header row never wraps; the popover has the full text.
   */
  chipLabel?: string;
  /** The server's own words, shown on hover only, never as the message. */
  detail?: string;
};
