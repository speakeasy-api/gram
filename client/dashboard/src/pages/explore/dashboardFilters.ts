import type { DateRangeValue } from "@/components/filters/filter-schema";
import type { DashboardFilters } from "@gram/client/models/components/dashboardfilters.js";
import { windowPreset, type WindowPreset } from "./exploreModel";
import type { PageContext } from "./pageContext";
import type { PageFilterField, PageFilterValues } from "./usePageFilters";

// A dashboard's saved filters are what it opens on, for everyone; the bar
// then holds each viewer's own view until they save it back. These convert
// between the two shapes: what the dashboards service stores, and what the
// page's filter bar holds.

/** The window a dashboard opens on when none is saved. */
const DEFAULT_DASHBOARD_WINDOW: WindowPreset = "7d";

/**
 * The relative window a dashboard's bar falls back to: the saved preset,
 * when the bar offers it; otherwise the default. A saved absolute range
 * leaves the default for when the range is cleared.
 */
export function savedPreset(filters: DashboardFilters): WindowPreset {
  return windowPreset(filters.range?.preset) ?? DEFAULT_DASHBOARD_WINDOW;
}

/** The bar's values for a dashboard's saved filters, over the fields it offers. */
export function barValuesFromSaved(
  filters: DashboardFilters,
  fields: readonly PageFilterField[],
): PageFilterValues {
  const range = filters.range;
  const window: DateRangeValue =
    range?.from && range.to
      ? {
          preset: null,
          customRange: { from: range.from, to: range.to },
          customLabel: range.label ?? null,
        }
      : { preset: savedPreset(filters), customRange: null, customLabel: null };
  const values: Record<string, readonly string[]> = {};
  for (const { field } of fields) values[field] = filters.values[field] ?? [];
  return { window, filters: values };
}

/** The bar as it stands, in the shape the dashboards service saves. */
export function savedFromContext(
  page: PageContext,
  fields: readonly PageFilterField[],
): DashboardFilters {
  const window = page.window;
  let range: DashboardFilters["range"];
  if (window?.customRange) {
    range = {
      from: window.customRange.from,
      to: window.customRange.to,
      ...(window.customLabel ? { label: window.customLabel } : {}),
    };
  } else if (window?.preset) {
    range = { preset: window.preset };
  }
  const values: Record<string, string[]> = {};
  for (const { field } of fields) {
    const picked = page.filters?.[field];
    if (picked && picked.length > 0) values[field] = [...picked];
  }
  return { ...(range ? { range } : {}), values };
}

/**
 * Whether two saved filters open on the same thing. No saved range means
 * the default window, so a bar on the default has nothing to save.
 */
export function sameFilters(a: DashboardFilters, b: DashboardFilters): boolean {
  if (!sameRange(a, b)) return false;
  const fields = new Set([...Object.keys(a.values), ...Object.keys(b.values)]);
  for (const field of fields) {
    const x = [...(a.values[field] ?? [])].sort();
    const y = [...(b.values[field] ?? [])].sort();
    if (x.length !== y.length || x.some((value, i) => value !== y[i])) {
      return false;
    }
  }
  return true;
}

function sameRange(a: DashboardFilters, b: DashboardFilters): boolean {
  const absolute = (filters: DashboardFilters) =>
    filters.range?.from && filters.range.to
      ? `${filters.range.from.getTime()}-${filters.range.to.getTime()}`
      : null;
  const x = absolute(a);
  const y = absolute(b);
  if (x !== null || y !== null) return x === y;
  return savedPreset(a) === savedPreset(b);
}
