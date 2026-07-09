import type {
  FilterChip,
  ObserveStatusFilterValue,
  ObserveTypeFilterValue,
} from "@/components/observe/ObserveFilterBar";
import type { MultiSelectGroup } from "@/components/ui/multi-select";
import type { useServerNameMappings } from "@/hooks/useServerNameMappings";
import type { ToolUsageHostedServerFilterOption } from "@gram/client/models/components/toolusagehostedserverfilteroption.js";
import type { ToolUsageShadowServerFilterOption } from "@gram/client/models/components/toolusageshadowserverfilteroption.js";
import type { TargetTypes } from "@gram/client/models/components/gettoolusagesummarypayload";
import type { Statuses } from "@gram/client/models/components/listtoolusagetracespayload";
import { normalizeUserEmailFilter } from "./observeUserFilters";

export const SERVER_FILTER_PATH = "gram.tool_call.source";
export const USER_EMAIL_FILTER_PATH = "user.email";
const HOOK_SOURCE_FILTER_PATH = "gram.hook.source";

const HOSTED_SERVER_PREFIX = "hosted:";
const SHADOW_SERVER_PREFIX = "shadow:";

export const TOOL_USAGE_DEFAULT_TYPES: ObserveTypeFilterValue[] = [];
export const TOOL_USAGE_VALID_TYPES: ObserveTypeFilterValue[] = [
  "hosted_mcp_server",
  "tunneled_mcp_server",
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
}> = [
  { label: "Error", value: "error" },
  { label: "Success", value: "success" },
  { label: "Blocked", value: "blocked" },
  { label: "Pending", value: "pending" },
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
  | { type: "hosted"; id: string }
  | { type: "shadow"; id: string };

export function encodeHostedServerFilter(slug: string): string {
  return `${HOSTED_SERVER_PREFIX}${slug}`;
}

export function encodeShadowServerFilter(name: string): string {
  return `${SHADOW_SERVER_PREFIX}${name}`;
}

export function parseTargetFilter(value: string): ParsedTargetFilter {
  if (value.startsWith(HOSTED_SERVER_PREFIX)) {
    return { type: "hosted", id: value.slice(HOSTED_SERVER_PREFIX.length) };
  }
  if (value.startsWith(SHADOW_SERVER_PREFIX)) {
    return { type: "shadow", id: value.slice(SHADOW_SERVER_PREFIX.length) };
  }
  return { type: "shadow", id: value };
}

export function selectedTargetValues(activeFilters: FilterChip[]): string[] {
  return activeFilters
    .filter((f) => f.path === SERVER_FILTER_PATH)
    .flatMap((f) => f.filters)
    .filter(Boolean);
}

export function selectedUserEmails(activeFilters: FilterChip[]): string[] {
  return activeFilters
    .filter((f) => f.path === USER_EMAIL_FILTER_PATH)
    .flatMap((f) => f.filters)
    .map(normalizeUserEmailFilter)
    .filter(Boolean);
}

export function selectedHookSources(activeFilters: FilterChip[]): string[] {
  return activeFilters
    .filter((f) => f.path === HOOK_SOURCE_FILTER_PATH)
    .flatMap((f) => f.filters)
    .filter(Boolean);
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
  activeFilters,
  serverNameMappings,
}: {
  hostedServers: ToolUsageHostedServerFilterOption[];
  shadowServers: ToolUsageShadowServerFilterOption[];
  activeFilters: FilterChip[];
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
}): MultiSelectGroup[] {
  const hosted = new Map<string, { label: string; count: number }>();
  const shadow = new Map<string, { label: string; count: number }>();

  for (const server of hostedServers) {
    hosted.set(encodeHostedServerFilter(server.toolsetSlug), {
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

  for (const value of selectedTargetValues(activeFilters)) {
    const parsed = parseTargetFilter(value);
    if (parsed.type === "hosted" && !hosted.has(value)) {
      hosted.set(value, { label: parsed.id, count: 0 });
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
    { heading: "Shadow MCP", options: toOptions(shadow) },
  ].filter((group) => group.options.length > 0);
}
