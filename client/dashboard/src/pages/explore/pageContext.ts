import type { DateRangeValue } from "@/components/filters/filter-schema";
import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import {
  fieldByName,
  MAX_FILTER_VALUES,
  operatorsForField,
  type ExploreSpec,
  type FilterDraft,
} from "./exploreModel";

// A page that places widgets owns one date range and one filter bar, the
// dashboard's shared ones, and every widget on it answers within them —
// Braintrust's dashboard filters, Datadog's template variables. The page's
// filter bar is built from catalog fields (see usePageFilters), so what it
// holds folds into a widget's question with no translation:
//
//   - The page's date range replaces the widget's window: a preset as the
//     window, a custom range as an absolute range. The grain follows what
//     runs, so a 30-day page draws days, not the hours a 1d widget was
//     saved with.
//   - A page filter is ANDed with the widget's own filters.
//   - A page filter the widget's dataset cannot apply — no such dimension,
//     or not by `in` — is skipped for that widget, and named, so the card
//     says it is not filtered rather than silently answering a wider
//     question than its neighbours.

/** What a page applies to every widget on it. */
export interface PageContext {
  /** The page's date range, as the shared filter bar holds it. */
  window?: DateRangeValue | undefined;
  /** The values picked for each catalog dimension the page filters by. */
  filters?: Readonly<Record<string, readonly string[]>> | undefined;
  /** A drag across a time chart narrows the page to the dragged range. */
  onRangeSelect?: ((from: Date, to: Date) => void) | undefined;
}

/** A widget's question with the page folded in. */
export interface PagedSpec {
  spec: ExploreSpec;
  /** The page filters this widget could not apply, by field. */
  skipped: string[];
  /** Whether the page changed the question the widget asks on its own. */
  changed: boolean;
}

export function applyPageContext(
  spec: ExploreSpec,
  dataset: AnalyticsDataset | undefined,
  page: PageContext | undefined,
): PagedSpec {
  if (!page) return { spec, skipped: [], changed: false };
  let next = spec;
  let changed = false;

  const window = page.window;
  if (window?.customRange) {
    const { from, to } = window.customRange;
    next = {
      ...next,
      range: {
        from: from.getTime(),
        to: to.getTime(),
        ...(window.customLabel ? { label: window.customLabel } : {}),
      },
    };
    changed = true;
  } else if (window?.preset) {
    if (window.preset !== spec.window || spec.range) {
      next = { ...next, window: window.preset, range: undefined };
      changed = true;
    }
  }

  const skipped: string[] = [];
  const added: FilterDraft[] = [];
  for (const [field, values] of Object.entries(page.filters ?? {})) {
    if (values.length === 0) continue;
    const found = fieldByName(dataset, field);
    if (
      found?.role !== "dimension" ||
      !operatorsForField(found).includes("in")
    ) {
      skipped.push(field);
      continue;
    }
    // Capped as the builder caps its own, so a long pick narrows the query
    // rather than breaking every widget on the page.
    added.push({
      field,
      operator: "in",
      values: values.slice(0, MAX_FILTER_VALUES),
    });
  }
  if (added.length > 0) {
    next = { ...next, filters: [...next.filters, ...added] };
    changed = true;
  }
  return { spec: next, skipped, changed };
}
