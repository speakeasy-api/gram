// Shared types and constants for the stacked time-series panel and the
// consumers that key their own UI to it (e.g. a table whose row dot colors
// match the chart legend). Kept in a non-component module so the panel
// component file satisfies the react-refresh "only export components" rule.

// The chart series palette, shared with consumers that key row/dot colors to
// the chart legend (e.g. the billing usage details table).
export const CHART_COLORS = [
  "#60a5fa", // blue
  "#34d399", // emerald
  "#f97316", // orange
  "#a78bfa", // violet
  "#fb7185", // rose
  "#facc15", // yellow
  "#38bdf8", // sky
  "#c084fc", // purple
  "#4ade80", // green
  "#f472b6", // pink
];
export const OTHER_COLOR = "#94a3b8"; // slate — the top-N remainder rollup

// The label of a top-N remainder rollup series. Stacks with this label render
// in the neutral OTHER_COLOR instead of walking the palette.
export const OTHER_STACK_LABEL = "Other";

// One stacked series: a daily value series aligned by index to the panel's
// bucket grid.
export type TimeSeriesStack = { label: string; series: number[] };

// Telemetry bucket timestamps arrive as unix-nano strings, which exceed
// Number precision — divide as BigInt first.
export function unixNanoToMs(nano: string): number {
  try {
    return Number(BigInt(nano) / 1_000_000n);
  } catch {
    return 0;
  }
}
