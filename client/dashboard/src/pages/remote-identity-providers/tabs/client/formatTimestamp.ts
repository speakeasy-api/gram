// formatTimestamp renders an ISO string or Date as a locale string, falling back
// to an em dash for empty values and the raw input for unparseable ones. Shared
// by the Remote Session Client Overview and Sessions tabs.
export function formatTimestamp(value?: string | Date): string {
  if (!value) return "—";
  const date = value instanceof Date ? value : new Date(value);
  return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString();
}
