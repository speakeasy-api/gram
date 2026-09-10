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

/**
 * The part of a rule a write is made of. Both the rules read back from the API
 * and the ones about to be sent have this shape, so an edit can be applied to
 * the result of another edit — which is what a row does when one click has to
 * change two rules.
 */
export interface AudienceRule {
  principalUrn: string;
  level: AudienceLevel;
  tools?: string[] | undefined;
  dispositions?: SetResourceAudienceEntryDispositions[] | undefined;
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
export function toWriteEntry(entry: AudienceRule): SetResourceAudienceEntry {
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

/**
 * Add or replace one principal's rule at one level. A principal holds one rule
 * per level, so this is the single write behind a scope line: turning it on,
 * and changing how far it reaches.
 */
export function withRule(
  entries: AudienceRule[],
  principalUrn: string,
  level: AudienceLevel,
  narrowing?: {
    tools?: string[];
    dispositions?: SetResourceAudienceEntryDispositions[];
  },
): SetResourceAudienceEntry[] {
  const written: SetResourceAudienceEntry = {
    principalUrn,
    level,
    tools: narrowing?.tools ?? [],
    dispositions: narrowing?.dispositions ?? [],
  };
  const id = ruleId({ principalUrn, level });
  if (!entries.some((entry) => ruleId(entry) === id)) {
    return [...entries.map(toWriteEntry), written];
  }
  return entries.map((entry) =>
    ruleId(entry) === id ? written : toWriteEntry(entry),
  );
}

/** The complete audience to send after removing rows. */
export function withoutRules(
  entries: AudienceRule[],
  ids: string[],
): SetResourceAudienceEntry[] {
  const removed = new Set(ids);
  return entries
    .filter((entry) => !removed.has(ruleId(entry)))
    .map(toWriteEntry);
}

/** The complete audience to send after removing every rule naming a principal. */
export function withoutPrincipal(
  entries: AudienceRule[],
  principalUrn: string,
): SetResourceAudienceEntry[] {
  return entries
    .filter((entry) => entry.principalUrn !== principalUrn)
    .map(toWriteEntry);
}

/** The complete audience to send after adding principals at a default level. */
export function withAdded(
  entries: AudienceRule[],
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
