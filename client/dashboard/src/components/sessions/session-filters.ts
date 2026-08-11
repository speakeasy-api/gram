import { defineFilters } from "@/components/filters";

/**
 * Both tables sit on one page, so every dimension id here has to be unique
 * across the two schemas: `useFilterState` keys URL params by id.
 *
 * Dates are relative-preset `select` dimensions rather than the `daterange`
 * kind. `daterange` deliberately reads and writes the shared range/from/to
 * params so a bookmarked URL survives across pages, which means only one can
 * be active at a time -- and this page needs three (session created, session
 * expiry, client registered).
 */
// allLabel is set on every date dimension: the default empty-state label
// pluralizes the dimension name, which turns "Created" into "All createds".
export const SESSION_FILTERS = defineFilters([
  { id: "sessionClient", label: "Client", kind: "select", pinned: true },
  {
    id: "sessionCreated",
    label: "Created",
    kind: "select",
    pinned: true,
    allLabel: "Created any time",
  },
  {
    id: "sessionExpires",
    label: "Expires",
    kind: "select",
    pinned: true,
    allLabel: "Expires any time",
  },
]);

/**
 * The schema the sessions toolbar shows while the tab is drilled into one
 * client. The Client dimension is dropped rather than disabled: two
 * independent "filter by client" controls would silently AND together, so
 * picking a second client would empty the table while the banner above still
 * named the first.
 */
export const SESSION_FILTERS_IN_CLIENT = defineFilters([
  {
    id: "sessionCreated",
    label: "Created",
    kind: "select",
    pinned: true,
    allLabel: "Created any time",
  },
  {
    id: "sessionExpires",
    label: "Expires",
    kind: "select",
    pinned: true,
    allLabel: "Expires any time",
  },
]);

export const CLIENT_FILTERS = defineFilters([
  { id: "clientSource", label: "Source", kind: "select", pinned: true },
  {
    id: "clientRegistered",
    label: "Registered",
    kind: "select",
    pinned: true,
    allLabel: "Registered any time",
  },
]);

/** Windows into the past, for "created"/"registered" dimensions. */
export const AGE_OPTIONS = [
  { value: "24h", label: "Last 24 hours" },
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
];

/** Windows into the future, for the session expiry dimension. */
export const EXPIRY_OPTIONS = [
  { value: "24h", label: "Within 24 hours" },
  { value: "7d", label: "Within 7 days" },
  { value: "30d", label: "Within 30 days" },
];

export const CLIENT_SOURCE_OPTIONS = [
  { value: "cimd", label: "CIMD" },
  { value: "dcr", label: "DCR" },
];

const WINDOW_MS: Record<string, number> = {
  "24h": 24 * 60 * 60 * 1000,
  "7d": 7 * 24 * 60 * 60 * 1000,
  "30d": 30 * 24 * 60 * 60 * 1000,
};

/**
 * Whether `date` falls inside the selected window, measured backwards from
 * now. An unset or unrecognized window matches everything.
 */
export function withinRecentWindow(
  date: Date,
  window: string | null,
  now: number,
): boolean {
  const span = window ? WINDOW_MS[window] : undefined;
  if (span === undefined) return true;
  return now - date.getTime() <= span;
}

/**
 * Whether `date` falls inside the selected window, measured forwards from now.
 * A date already in the past is inside every forward window, which is what the
 * expiry filter wants: a session past its deadline is not "expiring later than
 * 7 days from now".
 */
export function withinUpcomingWindow(
  date: Date,
  window: string | null,
  now: number,
): boolean {
  const span = window ? WINDOW_MS[window] : undefined;
  if (span === undefined) return true;
  return date.getTime() - now <= span;
}
