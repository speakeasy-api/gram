// The one place money is formatted for display. Every costs surface (chart,
// tables, widgets, CSV-adjacent captions) imports from here so a change to
// how spend renders — cents policy, currency, grouping — lands everywhere at
// once instead of drifting across per-file copies.

const compactDollars = new Intl.NumberFormat("en-US", {
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
