import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { cn } from "@/lib/utils";
import { Icon } from "@speakeasy-api/moonshine";
import {
  ENTRY_TYPE_META,
  FILTERABLE_ENTRY_TYPES,
  type FilterableTraceEntryType,
} from "./traceEntries";

const ENTRY_TYPE_FILTER_STYLES: Record<FilterableTraceEntryType, string> = {
  user: [
    "border-border bg-accent/50 text-foreground",
    "data-[state=on]:bg-accent data-[state=on]:text-foreground",
  ].join(" "),
  assistant: [
    "border-information-default bg-information-softest text-foreground",
    "data-[state=on]:bg-information-softest data-[state=on]:text-foreground",
  ].join(" "),
  tool_call: [
    "border-warning-default bg-warning-softest text-foreground",
    "data-[state=on]:bg-warning-softest data-[state=on]:text-foreground",
  ].join(" "),
  tool_result: [
    "border-success-default bg-success-softest text-foreground",
    "data-[state=on]:bg-success-softest data-[state=on]:text-foreground",
  ].join(" "),
};

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
}: {
  value: FilterableTraceEntryType[];
  counts: Record<FilterableTraceEntryType, number>;
  totalCount: number;
  visibleCount: number;
  onChange: (value: FilterableTraceEntryType[]) => void;
}) {
  return (
    <div className="bg-background px-6 py-3">
      <div className="flex min-w-0 flex-col gap-3">
        <div className="flex items-center justify-between gap-3">
          <div className="text-sm font-medium">Entries</div>
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
          className="border-border grid w-full grid-cols-2 rounded-none border lg:grid-cols-4"
        >
          {FILTERABLE_ENTRY_TYPES.map((entryType) => {
            const meta = ENTRY_TYPE_META[entryType];
            const count = counts[entryType];
            const isSelected = value.includes(entryType);
            const isDisabled = count === 0;

            return (
              <ToggleGroupItem
                key={entryType}
                value={entryType}
                aria-label={`Toggle ${meta.label} entries`}
                disabled={isDisabled}
                className={cn(
                  "h-10 min-w-0 justify-start rounded-none px-3 text-left shadow-none first:rounded-none last:rounded-none disabled:cursor-not-allowed disabled:opacity-45",
                  isSelected && !isDisabled
                    ? ENTRY_TYPE_FILTER_STYLES[entryType]
                    : "border-border bg-background text-muted-foreground hover:border-muted-foreground/40 hover:bg-muted/30 hover:text-foreground",
                )}
              >
                <Icon
                  name={meta.icon}
                  className={cn(
                    "size-4 shrink-0",
                    isSelected && !isDisabled
                      ? meta.iconClassName
                      : "text-muted-foreground",
                  )}
                />
                <span className="min-w-0 flex-1 truncate font-medium">
                  {meta.label}
                </span>
                <span
                  className={cn(
                    "rounded px-1.5 py-0.5 font-mono text-[10px] leading-none",
                    isSelected && !isDisabled
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
    </div>
  );
}
