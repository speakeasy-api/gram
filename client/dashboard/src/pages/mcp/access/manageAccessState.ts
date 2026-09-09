import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import type {
  SetResourceAudienceEntry,
  SetResourceAudienceEntryDispositions,
} from "@gram/client/models/components/setresourceaudienceentry.js";
import type { AudienceLevel } from "./serverAudience";

/**
 * List state for the Manage access table: the page it is showing, and the
 * rules for turning the rows it holds into a write. Kept apart from the
 * component so both are testable on their own.
 */

const ACCESS_PAGE_SIZE = 10;

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
function toWriteEntry(entry: ResourceAudienceEntry): SetResourceAudienceEntry {
  return {
    principalUrn: entry.principalUrn,
    level: entry.level,
    tools: entry.tools,
    dispositions: entry.dispositions,
  };
}

/**
 * A row's identity. A principal can hold two levels on one resource — "manage
 * the server" and "connect to these two tools" are different rules — so the
 * principal alone does not name a row.
 */
export function ruleId(entry: { principalUrn: string; level: string }): string {
  return `${entry.principalUrn}::${entry.level}`;
}

export function withLevel(
  entries: ResourceAudienceEntry[],
  id: string,
  level: AudienceLevel,
): SetResourceAudienceEntry[] {
  const next = entries.map((entry) =>
    ruleId(entry) === id
      ? { ...toWriteEntry(entry), level }
      : toWriteEntry(entry),
  );
  if (!entries.some((entry) => ruleId(entry) === id)) {
    // The level is appended after the last delimiter, and a principal URN can
    // contain colons of its own, so the principal is what precedes it.
    next.push({ principalUrn: id.slice(0, id.lastIndexOf("::")), level });
  }
  return next;
}

/**
 * Replace one rule's narrowing. Tools and annotations are alternatives, so
 * setting one clears the other.
 */
export function withNarrowing(
  entries: ResourceAudienceEntry[],
  id: string,
  narrowing: {
    tools?: string[];
    dispositions?: SetResourceAudienceEntryDispositions[];
  },
): SetResourceAudienceEntry[] {
  return entries.map((entry) =>
    ruleId(entry) === id
      ? {
          principalUrn: entry.principalUrn,
          level: entry.level,
          tools: narrowing.tools ?? [],
          dispositions: narrowing.dispositions ?? [],
        }
      : toWriteEntry(entry),
  );
}

/** The complete audience to send after removing rows. */
export function withoutRules(
  entries: ResourceAudienceEntry[],
  ids: string[],
): SetResourceAudienceEntry[] {
  const removed = new Set(ids);
  return entries
    .filter((entry) => !removed.has(ruleId(entry)))
    .map(toWriteEntry);
}

/** The complete audience to send after adding principals at a default level. */
export function withAdded(
  entries: ResourceAudienceEntry[],
  principalUrns: string[],
  level: AudienceLevel = "use",
): SetResourceAudienceEntry[] {
  const existing = new Set(entries.map((entry) => entry.principalUrn));
  return [
    ...entries.map(toWriteEntry),
    ...principalUrns
      .filter((urn) => !existing.has(urn))
      .map((principalUrn) => ({ principalUrn, level })),
  ];
}
