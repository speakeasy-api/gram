import { type DateRangePreset } from "@gram-ai/elements";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { useMemo, useState } from "react";
import { ChevronDown, Check } from "lucide-react";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@speakeasy-api/moonshine";
import { McpIcon } from "@/components/ui/mcp-icon";
import { MultiSelect } from "@/components/ui/multi-select";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
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
    <div
      className={cn("flex shrink-0 flex-wrap items-center gap-2", className)}
    >
      <MultiSelect
        options={serverOptionsWithDisplayNames}
        defaultValue={selectedServers}
        onValueChange={onServerSelectionChange}
        placeholder="Filter by server name"
        className="min-w-[200px] flex-1"
        hideSelectAll
        singleLine
      />
      <MultiSelect
        options={userEmailOptions.map((e) => ({ label: e, value: e }))}
        defaultValue={selectedEmails}
        onValueChange={onUserEmailSelectionChange}
        placeholder="Filter by user email"
        className="min-w-[200px] flex-1"
        hideSelectAll
        singleLine
      />
      <MultiSelect
        options={roleOptions.map((r) => ({ label: r.name, value: r.id }))}
        defaultValue={selectedRoleIds}
        onValueChange={onRoleSelectionChange}
        placeholder="Filter by role"
        className="min-w-[160px] flex-1"
        hideSelectAll
        singleLine
      />
      <MultiSelect
        options={sourceOptionsWithIcons}
        defaultValue={selectedSources}
        onValueChange={onSourceSelectionChange}
        placeholder="Filter by source"
        className="min-w-[160px] flex-1"
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
        className="min-w-[96px]"
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
        className="ml-auto"
      />
    </div>
  );
}

export function MCPServerFilter({
  selectedServer,
  onServerChange,
  toolsets,
  isLoading,
  disabled,
}: {
  selectedServer: string | null;
  onServerChange: (serverId: string | null) => void;
  toolsets: Array<{ slug: string; name: string }>;
  isLoading?: boolean;
  disabled?: boolean;
}): JSX.Element {
  const [open, setOpen] = useState(false);

  const selectedToolset = toolsets.find((t) => t.slug === selectedServer);
  const displayLabel = selectedToolset?.name ?? "All Servers";

  return (
    <div
      className={`flex items-center gap-2 ${disabled ? "pointer-events-none opacity-50" : ""}`}
    >
      <span className="text-muted-foreground hidden text-sm font-medium 2xl:inline">
        Filter by
      </span>
      <div className="border-border flex h-[42px] items-center rounded-md border p-1">
        <div className="flex h-8 items-center gap-1.5 px-3">
          <McpIcon className="text-muted-foreground size-3.5" />
          <span className="text-foreground text-sm font-medium">Server</span>
        </div>
        <div className="bg-border/50 mx-1 h-6 w-px" />
        <Popover open={!disabled && open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <button
              disabled={disabled || isLoading}
              className={`flex h-8 min-w-[140px] items-center justify-between gap-2 rounded px-2 text-sm transition-colors ${
                disabled || isLoading
                  ? "cursor-not-allowed opacity-40"
                  : "hover:bg-muted/50"
              }`}
            >
              <span className="max-w-[120px] truncate">
                {isLoading ? "Loading..." : displayLabel}
              </span>
              <ChevronDown className="text-muted-foreground size-3.5 shrink-0" />
            </button>
          </PopoverTrigger>
          <PopoverContent className="w-[220px] p-0" align="end">
            <Command>
              <CommandInput placeholder="Search servers..." className="h-9" />
              <CommandList>
                <CommandEmpty>No servers found.</CommandEmpty>
                <CommandGroup>
                  <CommandItem
                    value="__all__"
                    onSelect={() => {
                      onServerChange(null);
                      setOpen(false);
                    }}
                    className="cursor-pointer"
                  >
                    <Check
                      className={`mr-2 size-4 ${selectedServer === null ? "opacity-100" : "opacity-0"}`}
                    />
                    <span>All Servers</span>
                  </CommandItem>
                  {toolsets.map((toolset) => (
                    <CommandItem
                      key={toolset.slug}
                      value={toolset.name}
                      onSelect={() => {
                        onServerChange(toolset.slug);
                        setOpen(false);
                      }}
                      className="cursor-pointer"
                    >
                      <Check
                        className={`mr-2 size-4 ${selectedServer === toolset.slug ? "opacity-100" : "opacity-0"}`}
                      />
                      <span className="truncate">{toolset.name}</span>
                    </CommandItem>
                  ))}
                </CommandGroup>
              </CommandList>
            </Command>
          </PopoverContent>
        </Popover>
      </div>
    </div>
  );
}
