import { type DateRangePreset } from "@gram-ai/elements";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { type ReactNode, useMemo } from "react";
import { MultiSelect } from "@/components/ui/multi-select";
import { cn } from "@/lib/utils";
import type { useServerNameMappings } from "@/hooks/useServerNameMappings";
import { HookSourceIcon } from "@/pages/hooks/HookSourceIcon";
import type {
  MultiSelectGroup,
  MultiSelectOption,
} from "@/components/ui/multi-select";

const HOOK_SOURCE_FILTER_PATH = "gram.hook.source";

export interface FilterChip {
  display: string;
  filters: string[];
  path: string;
}

export type ObserveTypeFilterValue =
  | "mcp"
  | "local"
  | "skill"
  | "hosted_mcp_server"
  | "shadow_mcp_server"
  | "local_tool";

const SERVER_TYPES: Array<{ label: string; value: ObserveTypeFilterValue }> = [
  { label: "Shadow MCP Servers", value: "mcp" },
  { label: "Local Tools", value: "local" },
  { label: "Skills", value: "skill" },
];

export function ObserveFilterBar({
  serverOptions,
  serverOptionGroups,
  onServerSelectionChange,
  userEmailOptions,
  onUserEmailSelectionChange,
  sourceOptions,
  onSourceSelectionChange,
  roleOptions,
  selectedRoleIds,
  onRoleSelectionChange,
  activeFilters,
  selectedTypes,
  onTypesChange,
  typeOptions = SERVER_TYPES,
  dateRange,
  customRange,
  customRangeLabel,
  onDateRangeChange,
  onCustomRangeChange,
  onClearCustomRange,
  projectSlug,
  className,
  serverNameMappings,
  attributeSearchControl,
}: {
  serverOptions: string[];
  serverOptionGroups?: MultiSelectGroup[];
  onServerSelectionChange: (values: string[]) => void;
  userEmailOptions: string[];
  onUserEmailSelectionChange: (values: string[]) => void;
  sourceOptions: string[];
  onSourceSelectionChange: (values: string[]) => void;
  roleOptions: Array<{ id: string; name: string }>;
  selectedRoleIds: string[];
  onRoleSelectionChange: (values: string[]) => void;
  activeFilters: FilterChip[];
  selectedTypes: ObserveTypeFilterValue[];
  onTypesChange: (types: ObserveTypeFilterValue[]) => void;
  typeOptions?: MultiSelectOption[];
  dateRange: DateRangePreset;
  customRange: { from: Date; to: Date } | null;
  customRangeLabel: string | null;
  onDateRangeChange: (preset: DateRangePreset) => void;
  onCustomRangeChange: (from: Date, to: Date, label?: string) => void;
  onClearCustomRange: () => void;
  projectSlug?: string;
  className?: string;
  serverNameMappings?: ReturnType<typeof useServerNameMappings>;
  attributeSearchControl?: ReactNode;
}): JSX.Element {
  const selectedServers = useMemo(
    () =>
      activeFilters
        .filter((f) => f.path === "gram.tool_call.source")
        .flatMap((f) => f.filters)
        .filter(Boolean),
    [activeFilters],
  );

  const selectedEmails = useMemo(
    () =>
      activeFilters
        .filter((f) => f.path === "user.email")
        .flatMap((f) => f.filters)
        .filter(Boolean),
    [activeFilters],
  );

  const selectedSources = useMemo(
    () =>
      activeFilters
        .filter((f) => f.path === HOOK_SOURCE_FILTER_PATH)
        .flatMap((f) => f.filters)
        .filter(Boolean),
    [activeFilters],
  );

  const serverOptionsWithDisplayNames = useMemo(() => {
    if (serverOptionGroups) return serverOptionGroups;
    const rawToDisplay = serverNameMappings?.rawToDisplay;
    return serverOptions.map((rawName) => ({
      label: rawToDisplay?.get(rawName) ?? rawName,
      value: rawName,
    }));
  }, [serverOptionGroups, serverOptions, serverNameMappings?.rawToDisplay]);

  // The raw hook_source value (e.g. "claude-code") is shown verbatim — matching
  // the table row — and paired with its brand icon. HookSourceIcon needs a
  // `source` prop, so bind it per option to satisfy MultiSelect's
  // `icon: ComponentType<{ className }>` contract.
  const sourceOptionsWithIcons = useMemo(
    () =>
      sourceOptions.map((source) => ({
        label: source,
        value: source,
        icon: ({ className }: { className?: string }) => (
          <HookSourceIcon source={source} className={className} />
        ),
      })),
    [sourceOptions],
  );

  return (
    <div className={cn("flex flex-col gap-2", className)}>
      <div className="flex shrink-0 flex-wrap items-center gap-2">
        <MultiSelect
          options={serverOptionsWithDisplayNames}
          defaultValue={selectedServers}
          onValueChange={onServerSelectionChange}
          placeholder="Filter by server name"
          className="min-w-16 flex-1"
          hideSelectAll
          singleLine
        />
        <MultiSelect
          options={userEmailOptions.map((e) => ({ label: e, value: e }))}
          defaultValue={selectedEmails}
          onValueChange={onUserEmailSelectionChange}
          placeholder="Filter by user email"
          className="min-w-16 flex-1"
          hideSelectAll
          singleLine
        />
        <MultiSelect
          options={roleOptions.map((r) => ({ label: r.name, value: r.id }))}
          defaultValue={selectedRoleIds}
          onValueChange={onRoleSelectionChange}
          placeholder="Filter by role"
          className="min-w-16 flex-1"
          hideSelectAll
          singleLine
        />
        <MultiSelect
          options={sourceOptionsWithIcons}
          defaultValue={selectedSources}
          onValueChange={onSourceSelectionChange}
          placeholder="Filter by agent"
          className="min-w-16 flex-1"
          hideSelectAll
          singleLine
        />
        <MultiSelect
          options={typeOptions}
          defaultValue={selectedTypes}
          onValueChange={(values) =>
            onTypesChange(values as ObserveTypeFilterValue[])
          }
          placeholder="Filter by type"
          className="min-w-16 flex-1"
          searchable={false}
          autoSize
          hideSelectAll
          singleLine
        />
        <TimeRangePicker
          preset={customRange ? null : dateRange}
          customRange={customRange}
          customRangeLabel={customRangeLabel}
          onPresetChange={onDateRangeChange}
          onCustomRangeChange={onCustomRangeChange}
          onClearCustomRange={onClearCustomRange}
          projectSlug={projectSlug}
          className="ml-auto min-w-24 flex-1"
        />
      </div>
      {attributeSearchControl}
    </div>
  );
}
