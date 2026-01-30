/**
 * Parse a tool URN to extract the human-readable tool name.
 * e.g. "urn:gram:tool/mcp:list_files" → "list_files"
 */
export function parseToolName(urn: string): string {
  const lastSegment = urn.split("/").pop() || urn;
  const afterLastColon = lastSegment.split(":").pop() || lastSegment;
  return afterLastColon;
}

/**
 * Format a number with K/M suffixes for compact display.
 */
export function formatNumber(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return n.toLocaleString();
}

/** Maximum number of items to show in chart bar lists. */
export const MAX_CHART_ITEMS = 8;

/**
 * Tailwind background classes for chart bar colors.
 * Uses Moonshine design tokens so they adapt to light/dark theme.
 */
export const CHART_COLORS = {
  primary: "bg-chart-1",
  secondary: "bg-chart-4",
  tertiary: "bg-chart-2",
  destructive: "bg-destructive",
} as const;
