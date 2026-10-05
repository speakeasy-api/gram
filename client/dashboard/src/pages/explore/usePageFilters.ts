import { useFilterState, type OptionsById } from "@/components/filters";
import type {
  DateRangeValue,
  FilterDimension,
  FilterValue,
} from "@/components/filters/filter-schema";
import { useProject } from "@/contexts/Auth";
import type { DateRangePreset } from "@/elements";
import { useAnalyticsDescribe } from "@gram/client/react-query/analyticsDescribe.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useQueries } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";
import { WINDOW_PRESETS, windowRange } from "./exploreModel";
import type { PageContext } from "./pageContext";
import { dimensionValuesQuery } from "./useDimensionValues";

/** A catalog dimension a page lets people filter its widgets by. */
export interface PageFilterField {
  /** The catalog dimension, as the widgets' datasets name it. */
  field: string;
  /** How the filter bar labels it. */
  label: string;
}

/** What a page's filter bar shows. */
export interface PageFilterConfig {
  /**
   * The dimensions the bar offers, in order. Only these: a page names the
   * few its widgets are about, not every field the catalog has.
   */
  fields: readonly PageFilterField[];
  /** The date range the page opens on. */
  defaultPreset: DateRangePreset;
}

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
 * page's range, from the first dataset that has it.
 */
export function usePageFilters(config: PageFilterConfig): {
  toolbar: ToolbarFiltersProps;
  context: PageContext;
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
        defaultPreset: config.defaultPreset,
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

  const optionsById = useFieldOptions(config.fields, date);

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
  };
}

/** Each field's values over the page's range, as filter options. */
function useFieldOptions(
  fields: readonly PageFilterField[],
  date: DateRangeValue,
): OptionsById {
  const client = useGramContext();
  const project = useProject();
  const datasets = useAnalyticsDescribe().data?.datasets;
  const { from, to } = date.customRange ?? windowRange(date.preset ?? "1d");
  const sources = fields.map(({ field }) => ({
    field,
    dataset:
      datasets?.find((dataset) =>
        dataset.fields.some(
          (candidate) =>
            candidate.name === field && candidate.role === "dimension",
        ),
      )?.name ?? "",
  }));
  const results = useQueries({
    queries: sources.map(({ field, dataset }) =>
      dimensionValuesQuery(client, project.id, dataset, field, from, to, true),
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
