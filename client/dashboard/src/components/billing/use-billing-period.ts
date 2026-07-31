import { useCallback, useMemo, useState } from "react";
import {
  type BillingCycle,
  type BillingPeriod,
  cycleKey,
  periodFromCycle,
} from "./billing-cycles";

// A custom time range overriding the cycle selection, with the range
// picker's parse label when it came from one (e.g. "Last 7 days").
export type CustomRange = { start: Date; end: Date; label?: string };

// The range picker's calendar hands back the start of the local day for both
// ends. The page's data is bucketed by UTC day (matching the billing-cycle
// boundaries), so a picked day means that UTC calendar day — otherwise a
// one-day pick spans two UTC buckets and the chart grows a phantom extra
// day. The last day is inclusive. Natural-language parses carry real times
// and pass through untouched.
function customRangeFromPicker(
  from: Date,
  to: Date,
): { start: Date; end: Date } {
  // "Start of the local day", not "local midnight": on a DST spring-forward
  // day that skips midnight (e.g. 00:00 → 01:00 in Chile or Cuba), the
  // calendar pick lands at 01:00 and a midnight check would silently drop
  // the UTC-day mapping for that pick.
  const isStartOfLocalDay = (d: Date) =>
    d.getTime() ===
    new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  if (!isStartOfLocalDay(from) || !isStartOfLocalDay(to)) {
    return { start: from, end: to };
  }
  return {
    start: new Date(
      Date.UTC(from.getFullYear(), from.getMonth(), from.getDate()),
    ),
    end: new Date(Date.UTC(to.getFullYear(), to.getMonth(), to.getDate() + 1)),
  };
}

/**
 * Selection state behind the billing page's effective time period: a billing
 * cycle from the cycle picker (defaulting to the active cycle once TUM
 * loads), overridden by a custom range typed into the range picker or
 * drilled into via a chart bar click. A custom range that happens to match a
 * cycle's exact boundaries IS that cycle (billed normalization applies).
 */
export function useBillingPeriod(cycles: BillingCycle[]): {
  // The effective period; null until the cycles are known.
  period: BillingPeriod | null;
  // The picked (or defaulted) cycle, regardless of any range override.
  selectedCycle: BillingCycle | null;
  // The active range override, if any.
  customRange: CustomRange | null;
  // Remount key for view state; bumped by reset().
  viewNonce: number;
  // Scope the page to a cycle, clearing any range override.
  selectCycle: (cycle: BillingCycle) => void;
  // Scope the page to a range picked in the range picker (calendar or
  // natural-language parse).
  setPickedRange: (from: Date, to: Date, label?: string) => void;
  // Drop the range override, returning to the selected cycle.
  clearCustomRange: () => void;
  // Bar-click drill-down: narrow to the clicked bucket, clamped to the
  // current period (week/month buckets can overhang the period's edges).
  selectBarRange: (start: Date, end: Date) => void;
  // Back to the default cycle with all view state remounted.
  reset: () => void;
} {
  // Derived (not synced) so the current cycle is the default once TUM loads.
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const selectedCycle =
    cycles.find((c) => cycleKey(c) === selectedKey) ??
    cycles.find((c) => c.current) ??
    cycles[0] ??
    null;

  const [customRange, setCustomRange] = useState<CustomRange | null>(null);
  const [viewNonce, setViewNonce] = useState(0);

  // The effective period. A custom range that happens to match a cycle's
  // exact boundaries IS that cycle (billed normalization applies).
  const period: BillingPeriod | null = useMemo(() => {
    if (customRange) {
      const exact =
        cycles.find(
          (c) =>
            c.start.getTime() === customRange.start.getTime() &&
            c.end.getTime() === customRange.end.getTime(),
        ) ?? null;
      return {
        start: customRange.start,
        end: customRange.end,
        cycle: exact,
        label: customRange.label,
      };
    }
    return selectedCycle ? periodFromCycle(selectedCycle) : null;
  }, [customRange, cycles, selectedCycle]);

  const selectCycle = useCallback((cycle: BillingCycle): void => {
    setCustomRange(null);
    setSelectedKey(cycleKey(cycle));
  }, []);

  const setPickedRange = useCallback(
    (from: Date, to: Date, label?: string): void => {
      const range = customRangeFromPicker(from, to);
      // A natural-language parse can hand back an inverted window; keep the
      // prior selection rather than scoping the page to an empty range.
      if (range.end.getTime() <= range.start.getTime()) return;
      setCustomRange({ ...range, label });
    },
    [],
  );

  const clearCustomRange = useCallback((): void => {
    setCustomRange(null);
  }, []);

  // Stable identity — it feeds the chart panel's chartOptions memo.
  const selectBarRange = useCallback(
    (start: Date, end: Date): void => {
      if (!period) return;
      const s = Math.max(start.getTime(), period.start.getTime());
      const e = Math.min(end.getTime(), period.end.getTime());
      if (e <= s) return;
      setCustomRange({ start: new Date(s), end: new Date(e) });
    },
    [period],
  );

  const reset = useCallback((): void => {
    setSelectedKey(null);
    setCustomRange(null);
    setViewNonce((n) => n + 1);
  }, []);

  return {
    period,
    selectedCycle,
    customRange,
    viewNonce,
    selectCycle,
    setPickedRange,
    clearCustomRange,
    selectBarRange,
    reset,
  };
}
