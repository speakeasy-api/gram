import { compareItems, rankItem } from "@tanstack/match-sorter-utils";

/**
 * Thin matchSorter shim built on @tanstack/match-sorter-utils. Rank each item
 * against `query` across `accessors`, keep the ones that pass, sort by rank.
 * Returns the original list when the query is empty.
 */
export function matchSorter<T>(
  items: T[],
  query: string,
  accessors: ReadonlyArray<(item: T) => string>,
): T[] {
  if (!query.trim()) return items;
  return items
    .map((item) => ({
      item,
      ranking: rankItem(item, query, { accessors }),
    }))
    .filter((r) => r.ranking.passed)
    .sort((a, b) => compareItems(a.ranking, b.ranking))
    .map((r) => r.item);
}
