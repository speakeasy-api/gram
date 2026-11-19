import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { FilterControl } from "@/components/filters/FilterControl";
import {
  calendarDateRangeValue,
  chipLabel,
  defaultValueForDimension,
  type DateRangeValue,
  type FilterDimension,
  type FilterValue,
  type OptionsById,
} from "@/components/filters/filter-schema";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { cn } from "@/lib/utils";
import { useEffect, useState, type ReactNode } from "react";
import {
  DEFAULT_LOG_RANGE,
  LOG_FILTERS,
  presetValue,
  TIME_RANGE_PRESETS,
} from "./filters";
import { formatCount, plural } from "./format";
import type { AttemptFacets } from "./types";

/** Multiselects listed with their counts rather than in a dropdown. */
const LISTED: Partial<Record<string, keyof AttemptFacets>> = {
  outcome: "outcomes",
  step: "steps",
};

/** Dimensions that share a row with the one before them. */
const PAIRED = new Set(["appVersion"]);

/** Connection Logs' More filters: every filter, with what each would match. */
export function LogFilterSheet({
  open,
  onOpenChange,
  accents,
  schema,
  values,
  optionsById,
  facets,
  total,
  onChange,
  onReset,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  accents: Record<string, string>;
  schema: readonly FilterDimension[];
  values: Record<string, FilterValue>;
  optionsById: OptionsById;
  facets: AttemptFacets | undefined;
  total: number | undefined;
  onChange: (id: string, value: FilterValue) => void;
  onReset: () => void;
}): JSX.Element {
  const keepOpenOnPopover = (
    event: { detail?: { originalEvent?: Event } } & Event,
  ) => {
    const target = (event.detail?.originalEvent?.target ??
      event.target) as HTMLElement | null;
    if (target?.closest?.("[data-radix-popper-content-wrapper]")) {
      event.preventDefault();
    }
  };
  const control = (dim: FilterDimension): ReactNode => {
    const value = values[dim.id] ?? defaultValueForDimension(dim);
    if (dim.kind === "daterange") {
      return (
        <TimeRanges
          value={value as DateRangeValue}
          onChange={(next) => onChange(dim.id, next)}
        />
      );
    }
    const facet = LISTED[dim.id];
    if (facet && dim.kind === "multiselect") {
      return (
        <CheckList
          label={dim.label}
          options={(optionsById[dim.id] ?? []).flatMap((o) =>
            "value" in o ? [o] : [],
          )}
          counts={facets?.[facet]}
          value={value as string[]}
          onChange={(next) => onChange(dim.id, next)}
        />
      );
    }
    return (
      <FilterControl
        dim={dim}
        value={value}
        onChange={(next) => onChange(dim.id, next)}
        options={optionsById[dim.id]}
        className="w-full"
      />
    );
  };
  const field = (dim: FilterDimension) => (
    <div key={dim.id} className="flex min-w-0 flex-col gap-2">
      <span className="flex items-center gap-2 text-sm">
        <span
          aria-hidden
          className="size-2 shrink-0"
          style={{ backgroundColor: accents[dim.id] }}
        />
        {dim.label}
      </span>
      {control(dim)}
    </div>
  );
  return (
    <Sheet open={open} onOpenChange={onOpenChange} modal={false}>
      <SheetContent
        className="bg-card w-full gap-0 overflow-y-auto p-0 sm:max-w-[440px]"
        onInteractOutside={keepOpenOnPopover}
        onPointerDownOutside={keepOpenOnPopover}
        onOpenAutoFocus={(e) => e.preventDefault()}
      >
        <SheetHeader className="border-b px-6 py-5 pr-12">
          <SheetTitle className="font-mono text-base font-normal">
            Filters
          </SheetTitle>
          <SheetDescription>
            Every filter is kept in the link, so the view can be shared.
          </SheetDescription>
        </SheetHeader>
        <div className="flex flex-1 flex-col gap-5 px-6 py-5">
          {schema.map((dim, index) => {
            if (PAIRED.has(dim.id)) return null;
            const next = schema[index + 1];
            const pair = next && PAIRED.has(next.id) ? next : undefined;
            const notes = [dim.description, pair?.description].filter(Boolean);
            return (
              <div key={dim.id} className="flex flex-col gap-2">
                {pair ? (
                  <div className="grid grid-cols-2 gap-4">
                    {field(dim)}
                    {field(pair)}
                  </div>
                ) : (
                  field(dim)
                )}
                {notes.map((note) => (
                  <p key={note} className="text-muted-foreground text-sm">
                    {note}
                  </p>
                ))}
              </div>
            );
          })}
        </div>
        <SheetFooter className="bg-card sticky bottom-0 flex-row justify-between border-t px-6 py-4">
          <Button variant="secondary" onClick={onReset}>
            Reset filters
          </Button>
          <Button onClick={() => onOpenChange(false)}>
            {total === undefined
              ? "Show attempts"
              : `Show ${plural(total, "attempt")}`}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

function TimeRanges({
  value,
  onChange,
}: {
  value: DateRangeValue;
  onChange: (value: DateRangeValue) => void;
}): JSX.Element {
  const [custom, setCustom] = useState(!!value.customRange);
  // A preset chosen anywhere (Reset, the picker) leaves Custom.
  useEffect(() => {
    if (!value.customRange) setCustom(false);
  }, [value.preset, value.customRange]);
  const selected = value.customRange || custom ? "custom" : value.preset;
  const option = (key: string, label: string, onClick: () => void) => (
    <button
      key={key}
      type="button"
      aria-pressed={selected === key}
      onClick={onClick}
      className={cn(
        "h-9 border px-3 transition-colors",
        selected === key
          ? "bg-primary text-primary-foreground border-primary"
          : "text-muted-foreground hover:text-foreground",
      )}
    >
      <span className="text-eyebrow" style={{ color: "inherit" }}>
        {label}
      </span>
    </button>
  );
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap gap-2">
        {TIME_RANGE_PRESETS.map((preset) =>
          option(preset, chipLabel(LOG_FILTERS[0], presetValue(preset)), () => {
            setCustom(false);
            onChange(presetValue(preset));
          }),
        )}
        {option("custom", "Custom", () => setCustom(true))}
      </div>
      {selected === "custom" ? (
        <TimeRangePicker
          availablePresets={[...TIME_RANGE_PRESETS]}
          preset={null}
          customRange={value.customRange}
          customRangeLabel={value.customLabel}
          onPresetChange={(preset) => {
            setCustom(false);
            onChange(presetValue(preset));
          }}
          onCalendarRangeChange={(from, to) =>
            onChange(calendarDateRangeValue(from, to))
          }
          onCustomRangeChange={(from, to, label) =>
            onChange({
              preset: null,
              customRange: { from, to },
              customLabel: label ?? null,
            })
          }
          onClearCustomRange={() => {
            setCustom(false);
            onChange(presetValue(DEFAULT_LOG_RANGE));
          }}
          className="w-full"
        />
      ) : null}
    </div>
  );
}

function CheckList({
  label,
  options,
  counts,
  value,
  onChange,
}: {
  label: string;
  options: { value: string; label: string }[];
  counts: { value: string; attempts: number }[] | undefined;
  value: string[];
  onChange: (value: string[]) => void;
}): JSX.Element {
  return (
    <ul aria-label={label} className="divide-y border">
      {options.map((option) => {
        const checked = value.includes(option.value);
        const count = counts?.find((c) => c.value === option.value)?.attempts;
        // Attempts still under way are listed only while there are some.
        if (option.value === "in_progress" && !count && !checked) return null;
        const id = `log-filter-${label}-${option.value}`;
        return (
          <li key={option.value}>
            <label
              htmlFor={id}
              className="hover:bg-muted/40 flex cursor-pointer items-center gap-3 px-3 py-2.5 text-sm"
            >
              <Checkbox
                id={id}
                checked={checked}
                onCheckedChange={(next) =>
                  onChange(
                    next
                      ? [...value, option.value]
                      : value.filter((v) => v !== option.value),
                  )
                }
              />
              <span className="flex-1">{option.label}</span>
              <span className="text-muted-foreground font-mono text-xs">
                {counts ? formatCount(count ?? 0) : null}
              </span>
            </label>
          </li>
        );
      })}
    </ul>
  );
}
