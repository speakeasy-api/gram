import type { Mark } from "../kit/types";

/** How Speakeasy reaches the server. A gateway fronts other servers. */
export type McpServerSource =
  | "hosted"
  | "remote"
  | "tunneled"
  | "unproxied"
  | "gateway";

export type McpServerAvailability = "disabled" | "private" | "public";

/** One line in the MCP servers list. */
export interface McpServerRow {
  /**
   * The address of the record: the server's slug when it has one (readable
   * in the surface's address bar), else its id; `gateway:<id>` for gateways.
   */
  id: string;
  /** The backend id that writes are made against. */
  serverId: string;
  name: string;
  source: McpServerSource;
  /** One line of context: what it is built from. */
  summary: string;
  availability: McpServerAvailability;
  /** Unknown for remote servers until someone connects. */
  toolCount?: number;
  mark: Mark;
  updatedAt: Date;
}

export interface McpServerRecord extends McpServerRow {
  url?: string;
  /** Tools, or for a gateway the servers behind it. */
  tools: { name: string; description?: string }[];
  members: { id: string; name: string }[];
  /** Included in at least one plugin, so the team gets it automatically. */
  distributed: boolean;
  /** Someone has already called a tool through it. */
  used: boolean;
  createdAt: Date;
}

export const SOURCE_LABEL: Record<McpServerSource, string> = {
  hosted: "Hosted",
  remote: "Remote",
  tunneled: "Tunneled",
  unproxied: "Unproxied",
  gateway: "Gateway",
};

export const AVAILABILITY_LABEL: Record<McpServerAvailability, string> = {
  disabled: "Off",
  private: "On",
  public: "Public",
};

export const GATEWAY_PREFIX = "gateway:";

export function isGatewayId(id: string): boolean {
  return id.startsWith(GATEWAY_PREFIX);
}
