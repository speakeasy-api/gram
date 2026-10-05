import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import {
  fieldByName,
  isWindowPreset,
  operatorsForField,
  type ExploreSpec,
  type FilterDraft,
  type TimeRange,
  type WindowPreset,
} from "./exploreModel";

// A page that places widgets owns one time window and one filter bar, and
// every widget on it answers within them — Braintrust's dashboard filters,
// Datadog's template variables. The rules for folding a page into a widget's
// own question:
//
//   - The page's window replaces the widget's. The grain follows the window
//     that runs, so a 30-day page draws days, not the hours a 24h widget was
//     saved with.
//   - A page filter is ANDed with the widget's own filters.
//   - A page filter the widget's dataset cannot apply — no such field, or
//     not by that operator — is skipped for that widget, and named, so the
//     card says it is not filtered rather than silently answering a wider
//     question than its neighbours.

/** What a page applies to every widget on it. */
export interface PageContext {
  /** A relative window, or the absolute range the page is showing. */
  window?: WindowPreset | TimeRange | undefined;
  /** Filters over dimensions, by catalog field name. */
  filters?: FilterDraft[] | undefined;
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
  if (isWindowPreset(window)) {
    if (window !== spec.window || spec.range) {
      next = { ...next, window, range: undefined };
      changed = true;
    }
  } else if (window) {
    next = { ...next, range: { from: window.from, to: window.to } };
    changed = true;
  }

  const skipped: string[] = [];
  const added: FilterDraft[] = [];
  for (const filter of page.filters ?? []) {
    if (filter.values.length === 0) continue;
    const field = fieldByName(dataset, filter.field);
    if (
      field?.role !== "dimension" ||
      !operatorsForField(field).includes(filter.operator)
    ) {
      if (!skipped.includes(filter.field)) skipped.push(filter.field);
      continue;
    }
    added.push(filter);
  }
  if (added.length > 0) {
    next = { ...next, filters: [...next.filters, ...added] };
    changed = true;
  }
  return { spec: next, skipped, changed };
}
