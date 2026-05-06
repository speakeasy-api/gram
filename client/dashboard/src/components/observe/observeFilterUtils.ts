import type { DateRangePreset } from "@gram-ai/elements";
import { Operator, type LogFilter } from "@gram/client/models/components";
import type { FilterChip } from "@/components/observe/ObserveFilterBar";

export const validPresets: DateRangePreset[] = [
  "15m",
  "1h",
  "4h",
  "1d",
  "2d",
  "3d",
  "7d",
  "15d",
  "30d",
  "90d",
];

export const perPage = 100;

export function isValidPreset(value: string | null): value is DateRangePreset {
  return value !== null && validPresets.includes(value as DateRangePreset);
}

export function safeBase64Encode(str: string): string {
  try {
    return btoa(str);
  } catch {
    return btoa(encodeURIComponent(str));
  }
}

export function safeBase64Decode(str: string): string | null {
  try {
    const decoded = atob(str);
    try {
      return decodeURIComponent(decoded);
    } catch {
      return decoded;
    }
  } catch {
    return null;
  }
}

export function buildLogFilters(
  activeFilters: FilterChip[],
): LogFilter[] | undefined {
  const byPath = new Map<string, string[]>();
  for (const chip of activeFilters) {
    byPath.set(chip.path, [...(byPath.get(chip.path) ?? []), ...chip.filters]);
  }
  const filters: LogFilter[] = [];
  for (const [path, values] of byPath) {
    filters.push({
      path,
      operator: values.length > 1 ? Operator.In : Operator.Contains,
      values,
    });
  }
  return filters.length > 0 ? filters : undefined;
}
