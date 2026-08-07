// The money formatters for the costs surfaces (chart, tables, widgets,
// captions) — one definition so a change to how spend renders (cents policy,
// currency, grouping) lands across those pages at once instead of drifting
// per file. Not yet universal: a few older surfaces (e.g. InsightsAgents)
// keep bespoke adaptive-precision formatters with different semantics.

// Browser-default locale, matching formatCost's toLocaleString(undefined) —
// the two must agree or the same page mixes locale conventions between the
// axis labels and the exact figures.
const compactDollars = new Intl.NumberFormat(undefined, {
  notation: "compact",
  maximumFractionDigits: 1,
});

// Exact spend with cents, e.g. "$1,234.56".
export function formatCost(value: number): string {
  return `$${value.toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`;
}

// Compact spend for axes and tight labels, e.g. "$1.2K".
export function formatCompactDollars(value: number): string {
  return `$${compactDollars.format(value)}`;
}
