import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { cn } from "@/lib/utils";
import { Switch } from "@speakeasy-api/moonshine";
import {
  ENTRY_TYPE_META,
  FILTERABLE_ENTRY_TYPES,
  type FilterableTraceEntryType,
  type RuleCount,
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
  riskOnly = false,
  riskCount = 0,
  ruleCounts = [],
  onRiskOnlyChange,
  title = "Entries Filter",
}: {
  value: FilterableTraceEntryType[];
  counts: Record<FilterableTraceEntryType, number>;
  totalCount: number;
  visibleCount: number;
  onChange: (value: FilterableTraceEntryType[]) => void;
  riskOnly?: boolean;
  riskCount?: number;
  ruleCounts?: ReadonlyArray<RuleCount>;
  onRiskOnlyChange?: (value: boolean) => void;
  title?: string;
}) {
  const riskOnlyDisabled = !riskOnly && riskCount === 0;

  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-3 px-6 py-3">
        <div className="flex shrink-0 items-baseline gap-4">
          <div className="text-sm font-medium">{title}</div>
          <div className="text-muted-foreground text-xs">
            Showing {visibleCount.toLocaleString()} of{" "}
            {totalCount.toLocaleString()} entries
          </div>
        </div>
        {onRiskOnlyChange && (
          <div
            className={cn(
              "inline-flex min-h-8 flex-wrap items-center justify-end gap-2 text-xs",
              riskOnlyDisabled && "opacity-50",
            )}
          >
            {ruleCounts.length > 0 ? (
              ruleCounts.map(({ ruleId, count }) => (
                <span
                  key={ruleId}
                  className="bg-muted/40 text-muted-foreground inline-flex items-center gap-1 rounded-sm px-1.5 py-0.5 font-mono text-[10px] leading-none"
                  title={`${count} ${ruleId}`}
                >
                  <span className="text-foreground">
                    {count.toLocaleString()}
                  </span>
                  <span>{ruleId}</span>
                </span>
              ))
            ) : (
              <span className="font-mono text-[10px] leading-none">
                {riskCount.toLocaleString()}
              </span>
            )}
            <span>Risk only</span>
            <Switch
              checked={riskOnly}
              disabled={riskOnlyDisabled}
              onCheckedChange={onRiskOnlyChange}
              aria-label="Show risk entries only"
            />
          </div>
        )}
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
        className="grid w-full grid-cols-2 gap-2 rounded-none px-3 pt-0 pb-2 lg:grid-cols-4"
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
                "text-muted-foreground hover:text-foreground justify-start text-left",
                "bg-background hover:bg-muted/40 rounded-lg shadow-none inset-shadow-xs transition-all",
                "hover:border-muted-foreground/50 border-muted border",
                canShowSelectedState &&
                  "bg-muted/80 border-muted-foreground hover:border-muted-foreground shadow-muted hover:shadow-sm",
              )}
            >
              <TraceEntryIcon entryType={entryType} disabled={isDisabled} />
              <span className="min-w-0 flex-1 truncate font-medium">
                {meta.label}
              </span>
              <span
                className={cn(
                  "rounded-sm px-1.5 py-0.5 font-mono text-[10px] leading-none",
                  canShowSelectedState
                    ? "bg-background text-foreground"
                    : "bg-muted/40 text-muted-foreground",
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
