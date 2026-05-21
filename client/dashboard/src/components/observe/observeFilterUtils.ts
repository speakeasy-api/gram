import type { DateRangePreset } from "@gram-ai/elements";
import {
  Operator,
  type LogFilter,
  type AccessMember,
} from "@gram/client/models/components";
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

export function resolveRoleEmails(
  roleIds: string[],
  members: AccessMember[],
): string[] {
  if (roleIds.length === 0) return [];
  const roleSet = new Set(roleIds);
  const emails = members
    .filter((m) => roleSet.has(m.roleId))
    .map((m) => m.email);
  return [...new Set(emails)];
}

export function buildLogFilters(
  activeFilters: FilterChip[],
  roleEmails: string[] = [],
): LogFilter[] | undefined {
  const byPath = new Map<string, string[]>();
  for (const chip of activeFilters) {
    byPath.set(chip.path, [...(byPath.get(chip.path) ?? []), ...chip.filters]);
  }
  if (roleEmails.length > 0) {
    const existing = byPath.get("user.email") ?? [];
    byPath.set("user.email", [...new Set([...existing, ...roleEmails])]);
  }
  const filters: LogFilter[] = [];
  for (const [path, values] of byPath) {
    filters.push({ path, operator: Operator.In, values });
  }
  return filters.length > 0 ? filters : undefined;
}

export function mergeFilterChip(
  activeFilters: FilterChip[],
  chip: FilterChip,
): { filters: FilterChip[]; merged: FilterChip | null } {
  const existing = activeFilters.find((f) => f.path === chip.path);
  const alreadyPresent = existing
    ? chip.filters.every((v) => existing.filters.includes(v))
    : false;
  if (alreadyPresent) return { filters: activeFilters, merged: null };

  const merged: FilterChip = existing
    ? {
        path: chip.path,
        filters: [...new Set([...existing.filters, ...chip.filters])],
        display: [...new Set([...existing.filters, ...chip.filters])].join(
          ", ",
        ),
      }
    : chip;

  return {
    filters: [...activeFilters.filter((f) => f.path !== chip.path), merged],
    merged,
  };
}
