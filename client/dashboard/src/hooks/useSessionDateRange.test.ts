import { act, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { calendarDateRangeValue } from "@/components/filters/filter-schema";
import { useSessionDateRange } from "./useSessionDateRange";

vi.mock("@/elements", () => ({
  getPresetRange: () => ({
    from: new Date(Date.now() - 3_600_000),
    to: new Date(),
  }),
}));
afterEach(() => {
  vi.useRealTimers();
});

it("keeps cursor bounds stable across renders and advances them on Refresh", () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-01-01T12:00:00Z"));
  const { result, rerender } = renderHook(() =>
    useSessionDateRange({ preset: "1h", customRange: null, customLabel: null }),
  );
  const first = result.current.dates;
  vi.setSystemTime(new Date("2026-01-01T13:00:00Z"));
  rerender();
  expect(result.current.dates).toBe(first);
  act(() => {
    expect(result.current.refreshDates()).toBe(true);
  });
  expect(result.current.dates.to?.toISOString()).toBe(
    "2026-01-01T13:00:00.000Z",
  );
});

it("includes the selected final calendar day but preserves exact timestamp ranges", () => {
  const from = new Date(2026, 2, 7);
  const to = new Date(2026, 2, 8);
  const { result, rerender } = renderHook(
    ({ calendarDays }) =>
      useSessionDateRange({
        preset: null,
        customRange: { from, to },
        customLabel: null,
        calendarEnd: calendarDays
          ? calendarDateRangeValue(from, to).calendarEnd
          : undefined,
      }),
    { initialProps: { calendarDays: true } },
  );
  expect(result.current.dates.to).toEqual(new Date(2026, 2, 9));
  expect(result.current.refreshDates()).toBe(false);
  rerender({ calendarDays: false });
  expect(result.current.dates.to).toEqual(to);
});

it.each([
  {
    to: "2026-03-08T06:00:00.000Z",
    expected: "2026-03-09T05:00:00.000Z",
    hours: 23,
  },
  {
    to: "2026-11-01T05:00:00.000Z",
    expected: "2026-11-02T06:00:00.000Z",
    hours: 25,
  },
])(
  "computes and shares the $hours-hour Chicago calendar day",
  ({ to, expected, hours }) => {
    const selectedEnd = new Date(to);
    const value = calendarDateRangeValue(
      new Date(selectedEnd.getTime() - 86_400_000),
      selectedEnd,
      "America/Chicago",
    );
    expect(value.calendarEnd?.toISOString()).toBe(expected);
    expect(
      (value.calendarEnd!.getTime() - selectedEnd.getTime()) / 3_600_000,
    ).toBe(hours);
    const serializedEnd = value.calendarEnd!.toISOString();
    const { result } = renderHook(() =>
      useSessionDateRange({
        ...value,
        calendarEnd: new Date(serializedEnd),
      }),
    );
    expect(result.current.dates.to?.toISOString()).toBe(expected);
  },
);
