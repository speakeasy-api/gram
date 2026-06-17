import type { ComponentType } from "react";
import type { DateRangePreset } from "@gram-ai/elements";
import { Operator } from "@gram/client/models/components/logfilter";
import type { MultiSelectGroup } from "@/components/ui/multi-select";

/**
 * Shared, strongly-typed filter system.
 *
 * Each page declares a `const` schema with {@link defineFilters}. The schema is
 * the single source of truth for three things:
 *   1. the controls rendered in the bar / sheet,
 *   2. the chips shown for active filters, and
 *   3. the typed value object the page reads (derived via {@link FilterValues}).
 *
 * The schema stays a pure literal — no hooks, no fetched data — so the value
 * types can be derived from it. Dynamic option lists (servers, policies, …) are
 * supplied separately at render time as an {@link OptionsById} map, which keeps
 * data-fetching hooks at the page's top level (rules-of-hooks safe).
 */

export type DateRangeValue = {
  preset: DateRangePreset | null;
  customRange: { from: Date; to: Date } | null;
  customLabel: string | null;
};

export interface FilterOption {
  label: string;
  value: string;
  icon?: ComponentType<{ className?: string }>;
}

/** Multiselect dimensions may supply grouped options (e.g. servers by type). */
export type FilterOptions = FilterOption[] | MultiSelectGroup[];

export function isGroupedOptions(
  options: FilterOptions,
): options is MultiSelectGroup[] {
  return options.length > 0 && "heading" in options[0]!;
}

/** Flatten grouped or flat options to a single list for label resolution. */
export function flattenOptions(
  options: FilterOptions | undefined,
): FilterOption[] {
  if (!options) return [];
  if (isGroupedOptions(options)) return options.flatMap((g) => g.options);
  return options;
}

/** Maps each filter `kind` to the TS type of its value. */
export interface FilterValueByKind {
  multiselect: string[];
  select: string | null;
  text: string;
  boolean: boolean;
  daterange: DateRangeValue;
}

export type FilterKind = keyof FilterValueByKind;

/** The union of every possible filter value (used by the generic UI components). */
export type FilterValue = FilterValueByKind[FilterKind];

interface BaseDimension<K extends FilterKind> {
  /** URL search-param key and the key under which the value is exposed. */
  id: string;
  /** Human label shown on the control and chips. */
  label: string;
  kind: K;
  icon?: ComponentType<{ className?: string }>;
  /** Pinned dimensions render inline in the bar; the rest live in the sheet. */
  pinned?: boolean;
  /**
   * Never render this dimension as a bar pill (sheet-only). Use for dimensions
   * that default to a non-empty value (e.g. a type filter) and would otherwise
   * always show a noisy chip.
   */
  hideChip?: boolean;
  placeholder?: string;
}

export interface MultiselectDimension extends BaseDimension<"multiselect"> {}
export interface SelectDimension extends BaseDimension<"select"> {}
export interface TextDimension extends BaseDimension<"text"> {
  /** Operator applied server-side; defaults to `contains`. */
  operator?: Operator;
}
export type BooleanDimension = BaseDimension<"boolean">;
export interface DateRangeDimension extends BaseDimension<"daterange"> {
  presets?: DateRangePreset[];
  defaultPreset?: DateRangePreset;
}

export type FilterDimension =
  | MultiselectDimension
  | SelectDimension
  | TextDimension
  | BooleanDimension
  | DateRangeDimension;

/**
 * Identity helper that preserves the literal tuple type of the schema so
 * {@link FilterValues} can map over it. Use `as const`-like inference:
 *
 *   const FILTERS = defineFilters([
 *     { id: "policy_id", label: "Policy", kind: "select" },
 *     { id: "unique", label: "Unique matches only", kind: "boolean" },
 *   ]);
 *   // FilterValues<typeof FILTERS> = { policy_id: string | null; unique: boolean }
 */
export function defineFilters<const T extends readonly FilterDimension[]>(
  dimensions: T,
): T {
  return dimensions;
}

/** The typed value object derived from a schema, keyed by each dimension `id`. */
export type FilterValues<T extends readonly FilterDimension[]> = {
  [D in T[number] as D["id"]]: FilterValueByKind[D["kind"]];
};

/** Per-dimension default ("empty") value, used for clearing. */
export function defaultValueForDimension(
  dim: FilterDimension,
): FilterValueByKind[FilterKind] {
  switch (dim.kind) {
    case "multiselect":
      return [];
    case "select":
      return null;
    case "text":
      return "";
    case "boolean":
      return false;
    case "daterange":
      return {
        preset: dim.defaultPreset ?? null,
        customRange: null,
        customLabel: null,
      };
  }
}

/** Whether a dimension's value counts as an active filter (drives chips). */
export function isDimensionActive(
  dim: FilterDimension,
  value: FilterValueByKind[FilterKind],
): boolean {
  switch (dim.kind) {
    case "multiselect":
      return Array.isArray(value) && value.length > 0;
    case "select":
      return typeof value === "string" && value !== "";
    case "text":
      return typeof value === "string" && value.trim() !== "";
    case "boolean":
      return value === true;
    case "daterange": {
      const v = value as DateRangeValue;
      return v.preset !== null || v.customRange !== null;
    }
  }
}

/** Resolve a raw value to its option label, falling back to the raw value. */
function optionLabel(
  value: string,
  options: FilterOptions | undefined,
): string {
  return flattenOptions(options).find((o) => o.value === value)?.label ?? value;
}

/** Collapse a multi-value list to `a, b +N` so chips stay compact. */
function collapseValues(values: string[], options?: FilterOptions): string {
  const labels = values.map((v) => optionLabel(v, options));
  if (labels.length <= 2) return labels.join(", ");
  return `${labels.slice(0, 2).join(", ")} +${labels.length - 2}`;
}

const PRESET_LABELS: Record<string, string> = {
  "15m": "Last 15 min",
  "1h": "Last hour",
  "4h": "Last 4 hours",
  "1d": "Last 24 hours",
  "2d": "Last 2 days",
  "3d": "Last 3 days",
  "7d": "Last 7 days",
  "15d": "Last 15 days",
  "30d": "Last 30 days",
  "90d": "Last 90 days",
};

function dateRangeLabel(value: DateRangeValue): string {
  if (value.customRange) return value.customLabel ?? "Custom range";
  if (value.preset) return PRESET_LABELS[value.preset] ?? value.preset;
  return "All time";
}

// Naive English pluralizer for the "All …" empty-state chip (e.g. "All servers",
// "All policies"). Covers the dimension labels we use (server/user/policy/role).
function pluralize(word: string): string {
  if (/[^aeiou]y$/.test(word)) return `${word.slice(0, -1)}ies`;
  if (/(s|x|z|ch|sh)$/.test(word)) return `${word}es`;
  return `${word}s`;
}

/**
 * The text shown on a filter pill. Self-descriptive values (select/multiselect/
 * date) read as the value alone; text and boolean keep their label so a bare
 * substring isn't ambiguous. Unset pinned dimensions show a pluralized "All …"
 * default (e.g. "All servers").
 */
export function chipLabel(
  dim: FilterDimension,
  value: FilterValueByKind[FilterKind],
  options?: FilterOptions,
): string {
  switch (dim.kind) {
    case "daterange":
      return dateRangeLabel(value as DateRangeValue);
    case "select": {
      const v = value as string | null;
      return v
        ? optionLabel(v, options)
        : `All ${pluralize(dim.label.toLowerCase())}`;
    }
    case "multiselect": {
      const arr = value as string[];
      return arr.length > 0
        ? collapseValues(arr, options)
        : `All ${pluralize(dim.label.toLowerCase())}`;
    }
    case "text":
      return `${dim.label}: ${value as string}`;
    case "boolean":
      return dim.label;
  }
}

/** Page-supplied option lists for select/multiselect dimensions, keyed by id. */
export type OptionsById = Record<string, FilterOptions | undefined>;
