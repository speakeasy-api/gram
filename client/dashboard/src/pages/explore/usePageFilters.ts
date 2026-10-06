import { useFilterState, type OptionsById } from "@/components/filters";
import {
  defaultValueForDimension,
  type DateRangeValue,
  type FilterDimension,
  type FilterValue,
} from "@/components/filters/filter-schema";
import { DATE_RANGE_PARAMS } from "@/components/filters/useFilterState";
import { useProject } from "@/contexts/Auth";
import type { DateRangePreset } from "@/elements";
import { useAnalyticsDescribe } from "@gram/client/react-query/analyticsDescribe.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import { useQueries } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router";
import {
  findDataset,
  WINDOW_PRESETS,
  windowRange,
  type WindowPreset,
} from "./exploreModel";
import { pageCanFilter, type PageContext } from "./pageContext";
import { dimensionValuesQuery } from "./useDimensionValues";

/** A catalog dimension a page lets people filter its widgets by. */
export interface PageFilterField {
  /** The catalog dimension, as the widgets' datasets name it. */
  field: string;
  /** How the filter bar labels it. */
  label: string;
}

// The fields a page of widgets may offer, in order: the dimensions most
// questions about agent activity are cut by. A page offers the ones some
// widget on it can be filtered by.
const PAGE_FILTER_FIELDS: readonly PageFilterField[] = [
  { field: "user", label: "User" },
  { field: "surface", label: "Agent" },
  { field: "model", label: "Model" },
  { field: "mcp_server", label: "MCP server" },
  { field: "status", label: "Status" },
];

/**
 * The fields a page offers over the given datasets: those some of them can
 * filter by, in the usual order.
 */
export function pageFieldsFor(
  catalog: AnalyticsDataset[] | undefined,
  datasets: readonly string[],
): PageFilterField[] {
  return PAGE_FILTER_FIELDS.filter(({ field }) =>
    datasets.some((name) =>
      pageCanFilter(findDataset(catalog ?? [], name), field),
    ),
  );
}

/**
 * Removes every value a page's bar keeps in the URL, so the next page opens
 * on its own: a dashboard on its saved filters.
 */
export function clearPageFilterParams(params: URLSearchParams): void {
  for (const name of DATE_RANGE_PARAMS) params.delete(name);
  for (const { field } of PAGE_FILTER_FIELDS) params.delete(field);
}

/** Everything a page's bar is set to at once. */
export interface PageFilterValues {
  /** The date range; absent means the page's default. */
  window?: DateRangeValue | undefined;
  /** The values picked per field; a field left out is cleared. */
  filters: Readonly<Record<string, readonly string[]>>;
}

/** What a page's filter bar shows. */
export interface PageFilterConfig {
  /**
   * The dimensions the bar offers, in order. Only these: a page names the
   * few its widgets are about, not every field the catalog has.
   */
  fields: readonly PageFilterField[];
  /**
   * The date range the page opens on. Without one the page opens on no
   * range, and each widget answers over its own saved window until someone
   * picks one.
   */
  defaultPreset?: DateRangePreset | undefined;
  /**
   * The window the bar's options are read over while no range is picked:
   * the longest a widget on the page asks, so a value only its oldest days
   * hold can still be picked. Thirty days without one.
   */
  optionsWindow?: WindowPreset | undefined;
  /**
   * The datasets the bar's options come from, in order: the ones the page's
   * widgets ask, so a value is one a card can be narrowed to. Without them,
   * any catalog dataset that has the field.
   */
  optionsDatasets?: readonly string[] | undefined;
  /**
   * Whether the bar's options are fetched. A page that holds the bar's
   * state while hiding it turns this off, so nothing is asked of a bar
   * nobody can open. On by default.
   */
  optionsEnabled?: boolean | undefined;
}

/** The window options are read over when the page names none. */
const DEFAULT_OPTIONS_WINDOW: WindowPreset = "30d";

/** The dimension id the page's date range sits under. */
const DATE_ID = "date";

/** Props for Page.Toolbar.Filters, the dashboard's shared filter bar. */
interface ToolbarFiltersProps {
  schema: readonly FilterDimension[];
  values: Record<string, FilterValue>;
  optionsById: OptionsById;
  onChange: (id: string, value: FilterValue) => void;
  onClear: (id: string) => void;
  onClearAll: () => void;
}

/**
 * A page's filter bar over its widgets: the dashboard's shared filter
 * system — its date picker, chips, sheet and URL parameters — configured to
 * show only the catalog dimensions the page names. Because the bar's fields
 * are catalog fields, what it holds is the page context its widgets fold
 * in, as is: `toolbar` goes to Page.Toolbar.Filters, `context` to every
 * WidgetView on the page.
 *
 * A dimension's options are the values the catalog reports for it over the
 * page's range, from the first of the page's datasets that can filter by it.
 */
export function usePageFilters(config: PageFilterConfig): {
  toolbar: ToolbarFiltersProps;
  context: PageContext;
  /**
   * Whether the URL holds any of the bar's values: a range, or a value for
   * one of its fields. A link that says what to show is left alone.
   */
  touched: boolean;
  /** Set the whole bar at once: a dashboard opening on its saved filters. */
  apply: (values: PageFilterValues) => void;
} {
  // Keyed on the configuration's content, so a page may declare it inline.
  const configKey = JSON.stringify(config);
  const schema = useMemo<readonly FilterDimension[]>(
    () => [
      {
        id: DATE_ID,
        label: "Date range",
        kind: "daterange",
        pinned: true,
        presets: [...WINDOW_PRESETS],
        ...(config.defaultPreset
          ? { defaultPreset: config.defaultPreset }
          : { allLabel: "Each widget's window" }),
      },
      ...config.fields.map(({ field, label }): FilterDimension => ({
        id: field,
        label,
        kind: "multiselect",
        pinned: true,
      })),
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [configKey],
  );
  const state = useFilterState(schema);
  const values = state.values as Record<string, FilterValue>;
  const date = values[DATE_ID] as DateRangeValue;

  const filters = useMemo(() => {
    const out: Record<string, readonly string[]> = {};
    for (const { field } of config.fields) {
      const picked = values[field];
      if (Array.isArray(picked) && picked.length > 0) out[field] = picked;
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [configKey, values]);

  const { setValue } = state;
  const onRangeSelect = useCallback(
    (from: Date, to: Date) =>
      setValue(DATE_ID, {
        preset: null,
        customRange: { from, to },
        customLabel: null,
      } as never),
    [setValue],
  );

  const optionsById = useFieldOptions(config, date);

  const [params] = useSearchParams();
  const touched =
    DATE_RANGE_PARAMS.some((name) => params.has(name)) ||
    config.fields.some(({ field }) => params.has(field));
  const { setValues } = state;
  const apply = useCallback(
    (next: PageFilterValues) => {
      const [dateDimension] = schema;
      const out: Record<string, unknown> = {
        [DATE_ID]:
          next.window ??
          (dateDimension ? defaultValueForDimension(dateDimension) : null),
      };
      for (const dimension of schema.slice(1)) {
        out[dimension.id] = [...(next.filters[dimension.id] ?? [])];
      }
      setValues(out as never);
    },
    [schema, setValues],
  );

  return {
    toolbar: {
      schema,
      values,
      optionsById,
      onChange: (id, value) => state.setValue(id as never, value as never),
      onClear: (id) => state.clearValue(id as never),
      onClearAll: state.clearAll,
    },
    context: { window: date, filters, onRangeSelect },
    touched,
    apply,
  };
}

/** Each field's values over the page's range, as filter options. */
function useFieldOptions(
  { fields, optionsWindow, optionsDatasets, optionsEnabled }: PageFilterConfig,
  date: DateRangeValue,
): OptionsById {
  const client = useGramContext();
  const project = useProject();
  const catalog = useAnalyticsDescribe().data?.datasets ?? [];
  const { from, to } =
    date.customRange ??
    windowRange(date.preset ?? optionsWindow ?? DEFAULT_OPTIONS_WINDOW);
  const candidates =
    optionsDatasets?.flatMap((name) => findDataset(catalog, name) ?? []) ??
    catalog;
  const sources = fields.map(({ field }) => ({
    field,
    dataset:
      candidates.find((dataset) => pageCanFilter(dataset, field))?.name ?? "",
  }));
  const results = useQueries({
    queries: sources.map(({ field, dataset }) =>
      dimensionValuesQuery(
        client,
        project.id,
        dataset,
        field,
        from,
        to,
        optionsEnabled ?? true,
      ),
    ),
  });
  const out: OptionsById = {};
  sources.forEach(({ field }, index) => {
    out[field] = (results[index]?.data?.values ?? []).map(({ value }) => ({
      value,
      label: value,
    }));
  });
  return out;
}
