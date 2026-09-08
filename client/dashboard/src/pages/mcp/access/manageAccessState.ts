import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import type { AudienceLevel } from "./serverAudience";

/**
 * List state for the Manage access table: the filters above it and the page
 * it is showing. Kept apart from the component so the rules that decide which
 * rows a person sees are testable on their own.
 */

export const AUDIENCE_TYPE_FILTERS = [
  { value: "all", label: "All types" },
  { value: "user", label: "People" },
  { value: "directory_group", label: "Groups" },
  { value: "directory_attribute", label: "Attributes" },
  { value: "role", label: "Roles" },
  { value: "everyone", label: "Everyone" },
] as const;

export type AudienceTypeFilter =
  (typeof AUDIENCE_TYPE_FILTERS)[number]["value"];

export const AUDIENCE_LEVEL_FILTERS = [
  { value: "all", label: "All access" },
  { value: "use", label: "Use" },
  { value: "view", label: "View" },
  { value: "manage", label: "Manage" },
  { value: "blocked", label: "No access" },
] as const;

export type AudienceLevelFilter =
  (typeof AUDIENCE_LEVEL_FILTERS)[number]["value"];

export const ACCESS_PAGE_SIZE = 10;

export interface AudienceFilters {
  search: string;
  type: AudienceTypeFilter;
  level: AudienceLevelFilter;
}

export const EMPTY_FILTERS: AudienceFilters = {
  search: "",
  type: "all",
  level: "all",
};

/** Rows matching the toolbar's search box and its two dropdowns. */
export function filterAudience(
  entries: ResourceAudienceEntry[],
  filters: AudienceFilters,
): ResourceAudienceEntry[] {
  const query = filters.search.trim().toLowerCase();
  return entries.filter((entry) => {
    if (filters.type !== "all" && entry.kind !== filters.type) return false;
    if (filters.level !== "all" && entry.level !== filters.level) return false;
    if (query === "") return true;
    return (
      entry.displayName.toLowerCase().includes(query) ||
      (entry.description ?? "").toLowerCase().includes(query)
    );
  });
}

/** The slice of rows one page shows, clamped so a stale page never blanks. */
export function pageOf<T>(
  rows: T[],
  page: number,
  size = ACCESS_PAGE_SIZE,
): T[] {
  const lastPage = Math.max(0, Math.ceil(rows.length / size) - 1);
  const safePage = Math.min(Math.max(page, 0), lastPage);
  return rows.slice(safePage * size, safePage * size + size);
}

export function pageCount(total: number, size = ACCESS_PAGE_SIZE): number {
  return Math.max(1, Math.ceil(total / size));
}

/**
 * The complete audience to send after one row changes. The endpoint replaces
 * the whole set of rules naming this resource, so every write starts from the
 * rows currently shown, not from the one that changed.
 */
export function withLevel(
  entries: ResourceAudienceEntry[],
  principalUrn: string,
  level: AudienceLevel,
): { principalUrn: string; level: AudienceLevel }[] {
  const next = entries.map((entry) => ({
    principalUrn: entry.principalUrn,
    level: entry.principalUrn === principalUrn ? level : entry.level,
  }));
  if (!entries.some((entry) => entry.principalUrn === principalUrn)) {
    next.push({ principalUrn, level });
  }
  return next;
}

/** The complete audience to send after removing rows. */
export function withoutPrincipals(
  entries: ResourceAudienceEntry[],
  principalUrns: string[],
): { principalUrn: string; level: AudienceLevel }[] {
  const removed = new Set(principalUrns);
  return entries
    .filter((entry) => !removed.has(entry.principalUrn))
    .map((entry) => ({
      principalUrn: entry.principalUrn,
      level: entry.level,
    }));
}

/** The complete audience to send after adding principals at a default level. */
export function withAdded(
  entries: ResourceAudienceEntry[],
  principalUrns: string[],
  level: AudienceLevel = "use",
): { principalUrn: string; level: AudienceLevel }[] {
  const existing = new Set(entries.map((entry) => entry.principalUrn));
  return [
    ...entries.map((entry) => ({
      principalUrn: entry.principalUrn,
      level: entry.level,
    })),
    ...principalUrns
      .filter((urn) => !existing.has(urn))
      .map((principalUrn) => ({ principalUrn, level })),
  ];
}
