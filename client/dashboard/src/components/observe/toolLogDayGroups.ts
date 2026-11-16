import { format } from "date-fns";

export type DayGroup<T> = {
  /** The local calendar day, yyyy-MM-dd. */
  key: string;
  /** The first row's time, which the date header formats. */
  date: Date;
  items: T[];
};

/**
 * Splits rows, newest first, into runs that share a local calendar day.
 * A day that comes back after another day starts a new run, so every run
 * stays contiguous and the list can head each one with its date.
 */
export function groupByLocalDay<T>(
  items: T[],
  timeOf: (item: T) => Date,
): DayGroup<T>[] {
  const groups: DayGroup<T>[] = [];
  for (const item of items) {
    const date = timeOf(item);
    const key = format(date, "yyyy-MM-dd");
    const current = groups[groups.length - 1];
    if (current?.key === key) {
      current.items.push(item);
      continue;
    }
    groups.push({ key, date, items: [item] });
  }
  return groups;
}

/**
 * The day the pinned header above a scrolled list should name: the last day
 * whose rows have reached the top of the list. A day's own header sits
 * above its rows, so the pinned date changes once that header has scrolled
 * out, never while both show the same date.
 */
export function pinnedDayAt(
  groups: Array<{ ts: number; rowsTop: number }>,
  scrollTop: number,
): number | null {
  let current: number | null = null;
  for (const group of groups) {
    if (current !== null && group.rowsTop > scrollTop + 1) break;
    current = group.ts;
  }
  return current;
}
