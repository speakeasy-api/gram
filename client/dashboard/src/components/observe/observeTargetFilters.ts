import type {
  FilterChip,
  ObserveStatusFilterValue,
  ObserveTypeFilterValue,
} from "@/components/observe/ObserveFilterBar";
import type { MultiSelectGroup } from "@/components/ui/MultiSelect";
import type { useServerNameMappings } from "@/hooks/useServerNameMappings";
import type { ToolUsageGatewayFilterOption } from "@gram/client/models/components/toolusagegatewayfilteroption.js";
import type { ToolUsageHostedServerFilterOption } from "@gram/client/models/components/toolusagehostedserverfilteroption.js";
import type { ToolUsageShadowServerFilterOption } from "@gram/client/models/components/toolusageshadowserverfilteroption.js";
import type { TargetTypes } from "@gram/client/models/components/gettoolusagesummarypayload";
import type { Statuses } from "@gram/client/models/components/gettoolusagesummarypayload";
import { normalizeUserEmailFilter } from "./observeUserFilters";

const SERVER_FILTER_PATH = "gram.tool_call.source";
export const USER_EMAIL_FILTER_PATH = "user.email";
const HOOK_SOURCE_FILTER_PATH = "gram.hook.source";

const HOSTED_SERVER_PREFIX = "hosted:";
const SHADOW_SERVER_PREFIX = "shadow:";
const GATEWAY_SERVER_PREFIX = "gateway:";
const MCP_SERVER_PREFIX = "mcp_server:";

// An agent shares the user filter with people. Its value carries a prefix
// that no email address can start with, so the two never collide.
const AGENT_USER_PREFIX = "agent:";

// Local tools are excluded by default: they dominate event volume and drown
// out the MCP server and skill usage these pages are meant to surface. Users
// can opt back in via the type filter.
export const TOOL_USAGE_DEFAULT_TYPES: ObserveTypeFilterValue[] = [
  "hosted_mcp_server",
  "tunneled_mcp_server",
  "meta_mcp_server",
  "shadow_mcp_server",
  "skill",
];
export const TOOL_USAGE_VALID_TYPES: ObserveTypeFilterValue[] = [
  "hosted_mcp_server",
  "tunneled_mcp_server",
  "meta_mcp_server",
  "shadow_mcp_server",
  "local_tool",
  "skill",
];
export const TOOL_USAGE_TYPE_OPTIONS: Array<{
  label: string;
  value: ObserveTypeFilterValue;
}> = [
  { label: "Hosted MCP Servers", value: "hosted_mcp_server" },
  { label: "Tunneled MCP Servers", value: "tunneled_mcp_server" },
  { label: "Gateways", value: "meta_mcp_server" },
  { label: "Shadow MCP Servers", value: "shadow_mcp_server" },
  { label: "Local Tools", value: "local_tool" },
  { label: "Skills", value: "skill" },
];

export const TOOL_USAGE_VALID_STATUSES: ObserveStatusFilterValue[] = [
  "error",
  "success",
  "blocked",
  "pending",
];
export const TOOL_USAGE_STATUS_OPTIONS: Array<{
  label: string;
  value: ObserveStatusFilterValue;
  /** The dot a row with this status carries, so the two agree on sight. */
  dotClassName: string;
}> = [
  { label: "Error", value: "error", dotClassName: "bg-rose-500" },
  { label: "Success", value: "success", dotClassName: "bg-emerald-500" },
  // Amber, not red: a denial is the policy working, not the call failing, and
  // a reader scanning for breakage should not be pulled to it.
  { label: "Blocked", value: "blocked", dotClassName: "bg-amber-500" },
  {
    label: "Pending",
    value: "pending",
    dotClassName: "bg-muted-foreground/30",
  },
];

export function toStatuses(
  selectedStatuses: ObserveStatusFilterValue[],
): Statuses[] | undefined {
  const mapped = selectedStatuses.filter((status): status is Statuses =>
    TOOL_USAGE_VALID_STATUSES.includes(status),
  );
  return mapped.length > 0 ? mapped : undefined;
}

export type ParsedTargetFilter =
  | { type: "mcp_server"; id: string }
  | { type: "hosted"; id: string }
  | { type: "shadow"; id: string }
  | { type: "gateway"; id: string };

// Keyed by toolset slug; links made before servers were filtered by id use it.
export function encodeHostedServerFilter(toolsetSlug: string): string {
  return `${HOSTED_SERVER_PREFIX}${toolsetSlug}`;
}

export function encodeMcpServerFilter(mcpServerId: string): string {
  return `${MCP_SERVER_PREFIX}${mcpServerId}`;
}

/** Hosted option value; a selected legacy slug value is kept so it stays selected. */
export function hostedServerFilterValue(
  server: { toolsetSlug: string; mcpServerId?: string | undefined },
  selectedValues: string[],
): string {
  const legacy = encodeHostedServerFilter(server.toolsetSlug);
  if (!server.mcpServerId || selectedValues.includes(legacy)) return legacy;
  return encodeMcpServerFilter(server.mcpServerId);
}

export function encodeShadowServerFilter(name: string): string {
  return `${SHADOW_SERVER_PREFIX}${name}`;
}

// Keyed by meta MCP server id; covers calls on the gateway and its dispatches to members.
export function encodeGatewayServerFilter(metaMcpServerId: string): string {
  return `${GATEWAY_SERVER_PREFIX}${metaMcpServerId}`;
}

export function parseTargetFilter(value: string): ParsedTargetFilter {
  if (value.startsWith(MCP_SERVER_PREFIX)) {
    return { type: "mcp_server", id: value.slice(MCP_SERVER_PREFIX.length) };
  }
  if (value.startsWith(HOSTED_SERVER_PREFIX)) {
    return { type: "hosted", id: value.slice(HOSTED_SERVER_PREFIX.length) };
  }
  if (value.startsWith(SHADOW_SERVER_PREFIX)) {
    return { type: "shadow", id: value.slice(SHADOW_SERVER_PREFIX.length) };
  }
  if (value.startsWith(GATEWAY_SERVER_PREFIX)) {
    return { type: "gateway", id: value.slice(GATEWAY_SERVER_PREFIX.length) };
  }
  return { type: "shadow", id: value };
}

/** Splits server filters into server ids and legacy toolset slugs. */
export function splitHostedTargets(filters: ParsedTargetFilter[]): {
  mcpServerIds: string[];
  hostedToolsetSlugs: string[];
} {
  const mcpServerIds: string[] = [];
  const hostedToolsetSlugs: string[] = [];
  for (const filter of filters) {
    if (filter.type === "mcp_server") mcpServerIds.push(filter.id);
    if (filter.type === "hosted") hostedToolsetSlugs.push(filter.id);
  }
  return { mcpServerIds, hostedToolsetSlugs };
}

export function selectedTargetValues(activeFilters: FilterChip[]): string[] {
  return activeFilters
    .filter((f) => f.path === SERVER_FILTER_PATH)
    .flatMap((f) => f.filters)
    .filter(Boolean);
}

export function encodeAgentUserFilter(agentId: string): string {
  return `${AGENT_USER_PREFIX}${agentId}`;
}

function selectedUserValues(activeFilters: FilterChip[]): string[] {
  return activeFilters
    .filter((f) => f.path === USER_EMAIL_FILTER_PATH)
    .flatMap((f) => f.filters)
    .map((value) => value.trim())
    .filter(Boolean);
}

export function selectedUserEmails(activeFilters: FilterChip[]): string[] {
  return selectedUserValues(activeFilters)
    .filter((value) => !value.startsWith(AGENT_USER_PREFIX))
    .map(normalizeUserEmailFilter);
}

export function selectedUserAgentIds(activeFilters: FilterChip[]): string[] {
  return selectedUserValues(activeFilters)
    .filter((value) => value.startsWith(AGENT_USER_PREFIX))
    .map((value) => value.slice(AGENT_USER_PREFIX.length))
    .filter(Boolean);
}

/**
 * Every selected user filter value, people and agents, normalized the way the
 * filter options carry them: emails trimmed and lowercased, agents as `agent:<id>`.
 */
export function selectedUserFilterValues(
  activeFilters: FilterChip[],
): string[] {
  return [
    ...selectedUserEmails(activeFilters),
    ...selectedUserAgentIds(activeFilters).map(encodeAgentUserFilter),
  ];
}

export function selectedHookSources(activeFilters: FilterChip[]): string[] {
  return activeFilters
    .filter((f) => f.path === HOOK_SOURCE_FILTER_PATH)
    .flatMap((f) => f.filters)
    .filter(Boolean);
}

export function isDefaultToolUsageTypeSelection(
  selectedTypes: ObserveTypeFilterValue[],
): boolean {
  return (
    selectedTypes.length === TOOL_USAGE_DEFAULT_TYPES.length &&
    TOOL_USAGE_DEFAULT_TYPES.every((type) => selectedTypes.includes(type))
  );
}

export function toTargetTypes(
  selectedTypes: ObserveTypeFilterValue[],
): TargetTypes[] | undefined {
  const mapped = selectedTypes.filter((type): type is TargetTypes =>
    TOOL_USAGE_VALID_TYPES.includes(type),
  );
  return mapped.length > 0 ? mapped : undefined;
}

export function buildServerOptionGroups({
  hostedServers,
  shadowServers,
  gateways = [],
  activeFilters,
  serverNameMappings,
}: {
  hostedServers: ToolUsageHostedServerFilterOption[];
  shadowServers: ToolUsageShadowServerFilterOption[];
  gateways?: ToolUsageGatewayFilterOption[];
  activeFilters: FilterChip[];
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
}): MultiSelectGroup[] {
  const hosted = new Map<string, { label: string; count: number }>();
  const shadow = new Map<string, { label: string; count: number }>();
  const gateway = new Map<string, { label: string; count: number }>();

  const selectedValues = selectedTargetValues(activeFilters);
  for (const server of hostedServers) {
    hosted.set(hostedServerFilterValue(server, selectedValues), {
      label: server.toolsetName || server.toolsetSlug,
      count: server.eventCount,
    });
  }

  for (const server of shadowServers) {
    shadow.set(encodeShadowServerFilter(server.serverName), {
      label:
        serverNameMappings.rawToDisplay.get(server.serverName) ??
        server.serverName,
      count: server.eventCount,
    });
  }

  for (const entry of gateways) {
    gateway.set(encodeGatewayServerFilter(entry.metaMcpServerId), {
      label: entry.name || entry.metaMcpServerId,
      count: entry.eventCount,
    });
  }

  for (const value of selectedValues) {
    const parsed = parseTargetFilter(value);
    if (
      (parsed.type === "hosted" || parsed.type === "mcp_server") &&
      !hosted.has(value)
    ) {
      hosted.set(value, { label: parsed.id, count: 0 });
    }
    if (parsed.type === "gateway" && !gateway.has(value)) {
      gateway.set(value, { label: parsed.id, count: 0 });
    }
    const encodedShadow = encodeShadowServerFilter(parsed.id);
    if (parsed.type === "shadow" && !shadow.has(encodedShadow)) {
      shadow.set(encodedShadow, {
        label: serverNameMappings.rawToDisplay.get(parsed.id) ?? parsed.id,
        count: 0,
      });
    }
  }

  const toOptions = (entries: Map<string, { label: string; count: number }>) =>
    Array.from(entries.entries())
      .sort(
        (a, b) =>
          b[1].count - a[1].count || a[1].label.localeCompare(b[1].label),
      )
      .map(([value, option]) => ({
        value,
        label:
          option.count > 0 ? `${option.label} (${option.count})` : option.label,
      }));

  return [
    { heading: "Hosted MCP", options: toOptions(hosted) },
    { heading: "Gateways", options: toOptions(gateway) },
    { heading: "Shadow MCP", options: toOptions(shadow) },
  ].filter((group) => group.options.length > 0);
}
