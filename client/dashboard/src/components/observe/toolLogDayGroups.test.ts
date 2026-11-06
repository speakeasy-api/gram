import { describe, expect, it } from "vitest";
import { groupByLocalDay, pinnedDayAt } from "./toolLogDayGroups";

// Local wall-clock times, so the expectations hold in any test time zone.
const at = (y: number, m: number, d: number, h: number, min = 0) =>
  new Date(y, m - 1, d, h, min);

describe("groupByLocalDay", () => {
  it("gives every local day its own group", () => {
    const rows = [
      at(2026, 11, 6, 8, 31),
      at(2026, 11, 6, 8, 30),
      at(2026, 11, 4, 16, 18),
      at(2026, 11, 4, 9, 0),
      at(2026, 11, 2, 23, 59),
    ];
    const groups = groupByLocalDay(rows, (row) => row);
    expect(groups.map((g) => g.key)).toEqual([
      "2026-11-06",
      "2026-11-04",
      "2026-11-02",
    ]);
    expect(groups.map((g) => g.items.length)).toEqual([2, 2, 1]);
    expect(groups[1]?.date).toEqual(at(2026, 11, 4, 16, 18));
  });

  it("splits at local midnight, not UTC midnight", () => {
    const groups = groupByLocalDay(
      [at(2026, 11, 5, 0, 5), at(2026, 11, 4, 23, 55)],
      (row) => row,
    );
    expect(groups.map((g) => g.key)).toEqual(["2026-11-05", "2026-11-04"]);
  });

  it("starts a new group when a day comes back after another", () => {
    const groups = groupByLocalDay(
      [at(2026, 11, 6, 9), at(2026, 11, 5, 9), at(2026, 11, 6, 8)],
      (row) => row,
    );
    expect(groups.map((g) => g.key)).toEqual([
      "2026-11-06",
      "2026-11-05",
      "2026-11-06",
    ]);
  });

  it("returns no groups for no rows", () => {
    expect(groupByLocalDay([], (row: Date) => row)).toEqual([]);
  });
});

describe("pinnedDayAt", () => {
  // Three days: rows of the second start at 1000 (its header spans
  // 960-1000), rows of the third at 2000.
  const groups = [
    { ts: 3, rowsTop: 0 },
    { ts: 2, rowsTop: 1000 },
    { ts: 1, rowsTop: 2000 },
  ];

  it("names the first day at the top of the list", () => {
    expect(pinnedDayAt(groups, 0)).toBe(3);
  });

  it("keeps the day above while the next day's header is in view", () => {
    expect(pinnedDayAt(groups, 980)).toBe(3);
  });

  it("changes once the next day's rows reach the top", () => {
    expect(pinnedDayAt(groups, 1000)).toBe(2);
    expect(pinnedDayAt(groups, 1500)).toBe(2);
    expect(pinnedDayAt(groups, 2400)).toBe(1);
  });

  it("names nothing for an empty list", () => {
    expect(pinnedDayAt([], 0)).toBeNull();
  });
});
