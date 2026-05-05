import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { MultiSearch } from "@/components/ui/multi-search";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { TimeRangePicker, type DateRangePreset } from "@gram-ai/elements";
import { cn } from "@/lib/utils";
import type { TypesToInclude } from "@gram/client/models/components";
import { Icon } from "@speakeasy-api/moonshine";

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
  serverInput,
  setServerInput,
  onSubmitServerFilter,
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
  serverInput: string;
  setServerInput: (value: string) => void;
  onSubmitServerFilter: () => void;
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
  return (
    <div
      className={cn("flex shrink-0 flex-wrap items-center gap-2", className)}
    >
      <MultiSearch
        value={serverInput}
        onChange={setServerInput}
        onSubmit={onSubmitServerFilter}
        placeholder="Filter by server name (press Enter to add)"
        className="min-w-[200px] flex-1"
        chips={activeFilters
          .filter((f) => f.path === "gram.tool_call.source")
          .map((f) => ({ display: f.display, value: f.display }))}
        onRemoveChip={(display) =>
          removeFilter("gram.tool_call.source", display)
        }
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
