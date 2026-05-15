import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { cn } from "@/lib/utils";
import {
  ENTRY_TYPE_META,
  FILTERABLE_ENTRY_TYPES,
  type FilterableTraceEntryType,
} from "./traceEntries";
import { TraceEntryIcon } from "./TraceEntryIcon";

function getFilterableEntryTypes(values: string[]) {
  return FILTERABLE_ENTRY_TYPES.filter((entryType) =>
    values.includes(entryType),
  );
}

export function EntryTypeFilterBar({
  value,
  counts,
  totalCount,
  visibleCount,
  onChange,
  title = "Entries Filter",
}: {
  value: FilterableTraceEntryType[];
  counts: Record<FilterableTraceEntryType, number>;
  totalCount: number;
  visibleCount: number;
  onChange: (value: FilterableTraceEntryType[]) => void;
  title?: string;
}) {
  return (
    <div>
      <div className="flex items-center justify-between gap-3 px-6 py-3">
        <div className="text-sm font-medium">{title}</div>
        <div className="text-muted-foreground shrink-0 text-xs">
          Showing {visibleCount.toLocaleString()} of{" "}
          {totalCount.toLocaleString()} entries
        </div>
      </div>
      <ToggleGroup
        type="multiple"
        value={value}
        onValueChange={(next) => {
          const nextValue = getFilterableEntryTypes(next);
          if (nextValue.length > 0) {
            onChange(nextValue);
          }
        }}
        className="grid w-full grid-cols-2 gap-2 rounded-none p-2 pt-0 lg:grid-cols-4"
      >
        {FILTERABLE_ENTRY_TYPES.map((entryType) => {
          const meta = ENTRY_TYPE_META[entryType];
          const count = counts[entryType];
          const isSelected = value.includes(entryType);
          const isDisabled = count === 0;
          // Defaults select every type, so zero-count items can be both selected and disabled.
          const canShowSelectedState = isSelected && !isDisabled;

          return (
            <ToggleGroupItem
              key={entryType}
              value={entryType}
              aria-label={`Toggle ${meta.label} entries`}
              disabled={isDisabled}
              className={cn(
                "h-10 min-w-0 cursor-pointer px-3 disabled:cursor-not-allowed disabled:opacity-45",
                "text-foreground hover:text-foreground justify-start text-left",
                "bg-muted hover:bg-muted/50 rounded-lg shadow-none inset-shadow-xs transition-all",
                "hover:border-muted-foreground/50 border-muted border",
                canShowSelectedState &&
                  "bg-muted border-muted-foreground hover:border-muted-foreground shadow-muted hover:shadow-sm",
              )}
            >
              <TraceEntryIcon entryType={entryType} disabled={isDisabled} />
              <span className="min-w-0 flex-1 truncate font-medium">
                {meta.label}
              </span>
              <span
                className={cn(
                  "rounded px-1.5 py-0.5 font-mono text-[10px] leading-none",
                  canShowSelectedState
                    ? "bg-background/80 text-foreground"
                    : "bg-muted text-muted-foreground",
                )}
              >
                {count.toLocaleString()}
              </span>
            </ToggleGroupItem>
          );
        })}
      </ToggleGroup>
    </div>
  );
}
