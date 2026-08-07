// Shared types and constants for the stacked time-series panel and the
// consumers that key their own UI to it (e.g. a table whose row dot colors
// match the chart legend). Kept in a non-component module so the panel
// component file satisfies the react-refresh "only export components" rule.

// The base label of a top-N remainder rollup series (a collision with a real
// group appends suffixes). The label is display-only: neutral rollup
// rendering is driven by TimeSeriesStack.rollup, never by this string.
export const OTHER_STACK_LABEL = "Other";

// One stacked series: a daily value series aligned by index to the panel's
// bucket grid. `rollup` marks a synthetic top-N remainder series — it renders
// in the theme-resolved rollup neutral (useOtherSeriesColor) regardless of
// its label, so a real group that happens to display as "Other" can't be
// mistaken for it.
export type TimeSeriesStack = {
  label: string;
  series: number[];
  rollup?: boolean;
};
