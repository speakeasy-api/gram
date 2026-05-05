import { TimeRangePicker, type DateRangePreset } from "@gram-ai/elements";
import type { TypesToInclude } from "@gram/client/models/components";
import { useMemo, useState } from "react";
import { ChevronDown, Check } from "lucide-react";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  Icon,
} from "@speakeasy-api/moonshine";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { McpIcon } from "@/components/ui/mcp-icon";
import { MultiSearch } from "@/components/ui/multi-search";
import { MultiSelect } from "@/components/ui/multi-select";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";

export interface FilterChip {
  display: string;
  filters: string[];
  path: string;
}

const SERVER_TYPES: Array<{
  label: string;
  labelShort: string;
  value: TypesToInclude;
}> = [
  {
    label: "MCP Servers",
    labelShort: "Servers",
    value: "mcp",
  },
  {
    label: "Local Tools",
    labelShort: "Local",
    value: "local",
  },
  { label: "Skills", labelShort: "Skills", value: "skill" },
];

export function ObserveTypeFilter({
  selectedTypes,
  onTypesChange,
}: {
  selectedTypes: TypesToInclude[];
  onTypesChange: (types: TypesToInclude[]) => void;
}) {
  const getButtonText = () => {
    if (selectedTypes.length === 3) {
      return "Showing all types";
    }

    if (selectedTypes.length === 0) {
      return "No types selected";
    }

    if (selectedTypes.length === 1) {
      const selected = SERVER_TYPES.find(
        (opt) => opt.value === selectedTypes[0],
      );
      return `Showing ${selected?.labelShort || selectedTypes[0]}`;
    }

    const labels = SERVER_TYPES.filter((opt) =>
      selectedTypes.includes(opt.value),
    ).map((opt) => opt.labelShort);
    return `Showing ${labels.join(" & ")}`;
  };

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          className="h-[42px] w-[200px] shrink-0 justify-between"
        >
          <span className="text-sm">{getButtonText()}</span>
          <Icon name="chevron-down" className="ml-2 size-4" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[200px] p-3" align="start">
        <div className="space-y-2">
          {SERVER_TYPES.map((option) => (
            <div key={option.value} className="flex items-center space-x-2">
              <Checkbox
                id={`observe-type-${option.value}`}
                checked={selectedTypes.includes(option.value)}
                onCheckedChange={(checked) => {
                  if (checked) {
                    onTypesChange([...selectedTypes, option.value]);
                  } else {
                    onTypesChange(
                      selectedTypes.filter((t) => t !== option.value),
                    );
                  }
                }}
              />
              <label
                htmlFor={`observe-type-${option.value}`}
                className="cursor-pointer text-sm leading-none font-medium"
              >
                {option.label}
              </label>
            </div>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
}

export function ObserveFilterBar({
  serverOptions,
  onServerSelectionChange,
  userEmailInput,
  setUserEmailInput,
  onSubmitUserEmailFilter,
  activeFilters,
  removeFilter,
  selectedTypes,
  onTypesChange,
  dateRange,
  customRange,
  customRangeLabel,
  onDateRangeChange,
  onCustomRangeChange,
  onClearCustomRange,
  projectSlug,
  className,
}: {
  serverOptions: string[];
  onServerSelectionChange: (values: string[]) => void;
  userEmailInput: string;
  setUserEmailInput: (value: string) => void;
  onSubmitUserEmailFilter: () => void;
  activeFilters: FilterChip[];
  removeFilter: (path: string, display?: string) => void;
  selectedTypes: TypesToInclude[];
  onTypesChange: (types: TypesToInclude[]) => void;
  dateRange: DateRangePreset;
  customRange: { from: Date; to: Date } | null;
  customRangeLabel: string | null;
  onDateRangeChange: (preset: DateRangePreset) => void;
  onCustomRangeChange: (from: Date, to: Date, label?: string) => void;
  onClearCustomRange: () => void;
  projectSlug?: string;
  className?: string;
}) {
  const selectedServers = useMemo(
    () =>
      activeFilters
        .filter((f) => f.path === "gram.tool_call.source")
        .map((f) => f.filters[0])
        .filter((v): v is string => Boolean(v)),
    [activeFilters],
  );

  return (
    <div
      className={cn("flex shrink-0 flex-wrap items-center gap-2", className)}
    >
      <MultiSelect
        options={serverOptions.map((s) => ({ label: s, value: s }))}
        defaultValue={selectedServers}
        onValueChange={onServerSelectionChange}
        placeholder="Filter by server name"
        hideSelectAll
        className="min-w-[200px] flex-1"
      />
      <MultiSearch
        value={userEmailInput}
        onChange={setUserEmailInput}
        onSubmit={onSubmitUserEmailFilter}
        placeholder="Filter by user email (press Enter to add)"
        className="min-w-[200px] flex-1"
        chips={activeFilters
          .filter((f) => f.path === "user.email")
          .map((f) => ({ display: f.display, value: f.display }))}
        onRemoveChip={(display) => removeFilter("user.email", display)}
      />
      <ObserveTypeFilter
        selectedTypes={selectedTypes}
        onTypesChange={onTypesChange}
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
}) {
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
