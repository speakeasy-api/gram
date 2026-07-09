import { useEffect, useId, useMemo, useState } from "react";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { Checkbox } from "@/components/ui/checkbox";
import { MultiSelect } from "@/components/ui/multi-select";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Icon } from "@/components/ui/moonshine";
import {
  allLabelFor,
  flattenOptions,
  isGroupedOptions,
  type DateRangeValue,
  type FilterDimension,
  type FilterOptions,
  type FilterValue,
} from "./filter-schema";

const SELECT_ALL_VALUE = "__all__";

interface FilterControlProps {
  dim: FilterDimension;
  value: FilterValue;
  onChange: (value: FilterValue) => void;
  options?: FilterOptions;
  projectSlug?: string;
  className?: string;
}

/**
 * Renders the input control for a single filter dimension. Shared by the bar
 * (pinned dimensions) and the sheet (every dimension), so the two surfaces can
 * never drift. Dispatches on `dim.kind`; the value/onChange contract is the
 * dimension's value type from {@link FilterValue}.
 */
export function FilterControl({
  dim,
  value,
  onChange,
  options,
  projectSlug,
  className,
}: FilterControlProps): JSX.Element {
  switch (dim.kind) {
    case "multiselect":
      return (
        <MultiSelect
          options={
            options && isGroupedOptions(options)
              ? options
              : flattenOptions(options).map((o) => ({
                  label: o.label,
                  value: o.value,
                  icon: o.icon,
                }))
          }
          defaultValue={value as string[]}
          onValueChange={(values) => onChange(values)}
          placeholder={
            dim.placeholder ?? `Filter by ${dim.label.toLowerCase()}`
          }
          className={className}
          hideSelectAll
          singleLine
          // This control only ever renders inside the FilterSheet, which is a
          // non-modal Radix Dialog. A non-modal popover nested in a non-modal
          // dialog doesn't reliably receive option clicks in the browser (the
          // dialog's dismissable/focus layer swallows them), so the dropdown
          // appears but selecting an option does nothing (AIS-168). Running the
          // popover modal makes it the top interactive layer so clicks land.
          modalPopover
        />
      );
    case "select":
      return (
        <Select
          value={(value as string | null) ?? SELECT_ALL_VALUE}
          onValueChange={(v) => onChange(v === SELECT_ALL_VALUE ? null : v)}
        >
          <SelectTrigger className={className}>
            <SelectValue
              placeholder={
                dim.placeholder ?? `Filter by ${dim.label.toLowerCase()}`
              }
            />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={SELECT_ALL_VALUE}>{allLabelFor(dim)}</SelectItem>
            {flattenOptions(options).map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      );
    case "text":
      return (
        <DebouncedTextInput
          value={value as string}
          onChange={(v) => onChange(v)}
          placeholder={dim.placeholder ?? `${dim.label} contains...`}
          ariaLabel={`Filter by ${dim.label.toLowerCase()}`}
          suggestions={flattenOptions(options).map((o) => o.value)}
          className={className}
        />
      );
    case "number":
      return (
        <DebouncedNumberInput
          value={value as number | null}
          onChange={(v) => onChange(v)}
          placeholder={dim.placeholder ?? `${dim.label}…`}
          ariaLabel={`Filter by ${dim.label.toLowerCase()}`}
          min={dim.min}
          max={dim.max}
          step={dim.step}
          className={className}
        />
      );
    case "boolean":
      return (
        <label className="border-border hover:bg-muted/50 inline-flex h-9 cursor-pointer items-center gap-2 rounded-md border px-3 text-sm">
          <Checkbox
            checked={value as boolean}
            onCheckedChange={(next) => onChange(next === true)}
            aria-label={dim.label}
          />
          <span>{dim.label}</span>
        </label>
      );
    case "daterange":
      return (
        <DateRangeControl
          value={value as DateRangeValue}
          defaultPreset={dim.defaultPreset ?? null}
          onChange={onChange}
          projectSlug={projectSlug}
          className={className}
        />
      );
  }
}

function DateRangeControl({
  value,
  defaultPreset,
  onChange,
  projectSlug,
  className,
}: {
  value: DateRangeValue;
  defaultPreset: DateRangeValue["preset"];
  onChange: (value: DateRangeValue) => void;
  projectSlug?: string;
  className?: string;
}): JSX.Element {
  return (
    <TimeRangePicker
      preset={value.customRange ? null : value.preset}
      customRange={value.customRange}
      customRangeLabel={value.customLabel}
      onPresetChange={(preset) =>
        onChange({ preset, customRange: null, customLabel: null })
      }
      onCustomRangeChange={(from, to, label) =>
        onChange({
          preset: null,
          customRange: { from, to },
          customLabel: label ?? null,
        })
      }
      onClearCustomRange={() =>
        onChange({
          preset: defaultPreset,
          customRange: null,
          customLabel: null,
        })
      }
      projectSlug={projectSlug}
      className={className}
    />
  );
}

// A debounced free-text input. Mirrors the previous inline RiskEvents filter so
// keystrokes don't fire a request (or URL write) on every character.
function DebouncedTextInput({
  value,
  onChange,
  placeholder,
  ariaLabel,
  suggestions,
  className,
}: {
  value: string;
  onChange: (next: string) => void;
  placeholder: string;
  ariaLabel: string;
  suggestions?: string[];
  className?: string;
}): JSX.Element {
  const [local, setLocal] = useState(value);
  const inputId = useId();
  const listId = useId();

  useEffect(() => {
    setLocal(value);
  }, [value]);

  useEffect(() => {
    if (local === value) return;
    const timer = setTimeout(() => onChange(local), 350);
    return () => clearTimeout(timer);
  }, [local, value, onChange]);

  // Browser-native <datalist> does substring matching client-side using these
  // as candidates; dedup and drop empties.
  const options = useMemo(
    () => Array.from(new Set((suggestions ?? []).filter(Boolean))),
    [suggestions],
  );

  return (
    <div className="border-border focus-within:border-ring inline-flex h-9 items-center gap-2 rounded-md border px-2">
      <Icon name="search" className="text-muted-foreground size-4 shrink-0" />
      <input
        id={inputId}
        type="text"
        value={local}
        onChange={(e) => setLocal(e.target.value)}
        placeholder={placeholder}
        aria-label={ariaLabel}
        autoComplete="off"
        list={options.length > 0 ? listId : undefined}
        className={
          className ??
          "placeholder:text-muted-foreground w-[200px] bg-transparent text-sm outline-none"
        }
      />
      {options.length > 0 && (
        <datalist id={listId}>
          {options.map((opt) => (
            <option key={opt} value={opt} />
          ))}
        </datalist>
      )}
      {local && (
        <button
          type="button"
          onClick={() => {
            // Propagate the clear immediately (bypass the debounce) so a quick
            // close/unmount can't drop it and leave a stale filter applied.
            setLocal("");
            onChange("");
          }}
          className="text-muted-foreground hover:text-foreground"
          aria-label="Clear filter"
        >
          <Icon name="x" className="size-3.5" />
        </button>
      )}
    </div>
  );
}

// A debounced numeric input. Mirrors DebouncedTextInput but parses to a number
// (empty / non-numeric → null) so number dimensions never round-trip through a
// string at the page layer.
function DebouncedNumberInput({
  value,
  onChange,
  placeholder,
  ariaLabel,
  min,
  max,
  step,
  className,
}: {
  value: number | null;
  onChange: (next: number | null) => void;
  placeholder: string;
  ariaLabel: string;
  min?: number;
  max?: number;
  step?: number;
  className?: string;
}): JSX.Element {
  const [local, setLocal] = useState(value === null ? "" : String(value));

  useEffect(() => {
    setLocal(value === null ? "" : String(value));
  }, [value]);

  useEffect(() => {
    const trimmed = local.trim();
    const parsed = trimmed === "" ? NaN : Number(trimmed);
    const next = Number.isFinite(parsed) ? parsed : null;
    if (next === value) return;
    const timer = setTimeout(() => onChange(next), 350);
    return () => clearTimeout(timer);
  }, [local, value, onChange]);

  return (
    <div className="border-border focus-within:border-ring inline-flex h-9 items-center gap-2 rounded-md border px-2">
      <input
        type="number"
        inputMode="numeric"
        value={local}
        min={min}
        max={max}
        step={step}
        onChange={(e) => setLocal(e.target.value)}
        // Scrolling over a focused number input natively edits the value;
        // blur so page scrolls pass through (same guard as ui/input).
        onWheel={(e) => e.currentTarget.blur()}
        placeholder={placeholder}
        aria-label={ariaLabel}
        autoComplete="off"
        className={
          className ??
          "placeholder:text-muted-foreground w-[200px] bg-transparent text-sm outline-none"
        }
      />
      {local && (
        <button
          type="button"
          onClick={() => {
            // Bypass the debounce on clear so a quick unmount can't drop it.
            setLocal("");
            onChange(null);
          }}
          className="text-muted-foreground hover:text-foreground"
          aria-label="Clear filter"
        >
          <Icon name="x" className="size-3.5" />
        </button>
      )}
    </div>
  );
}
