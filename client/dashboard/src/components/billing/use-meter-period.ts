import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";

export type MeterCycleWindow = { from: Date; to: Date };
export type MeterPeriod = { from: Date; to: Date };
export type MeterCustomRange = MeterPeriod;

function cycleKey(cycle: MeterCycleWindow): string {
  return cycle.from.toISOString();
}

function rangeFromPicker(from: Date, to: Date): MeterPeriod {
  return {
    from: new Date(
      Date.UTC(from.getFullYear(), from.getMonth(), from.getDate()),
    ),
    to: new Date(Date.UTC(to.getFullYear(), to.getMonth(), to.getDate() + 1)),
  };
}
export function meterPeriodDisplayRange(period: MeterPeriod): MeterPeriod {
  const calendarDate = (date: Date): Date =>
    new Date(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate());
  return {
    from: calendarDate(period.from),
    to: calendarDate(new Date(period.to.getTime() - 1)),
  };
}

export function useMeterPeriod(cycles: MeterCycleWindow[]): {
  period: MeterPeriod | null;
  // Omit bounds until the user selects a period; the API defaults to the active cycle.
  requestPeriod: MeterPeriod | null;
  selectedCycle: MeterCycleWindow | null;
  customRange: MeterCustomRange | null;
  viewNonce: number;
  selectCycle: (cycle: MeterCycleWindow) => void;
  setPickedRange: (from: Date, to: Date) => void;
  clearCustomRange: () => void;
  selectChartRange: (from: Date, to: Date) => void;
  reset: () => void;
} {
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [customRange, setCustomRange] = useState<MeterCustomRange | null>(null);
  const [viewNonce, setViewNonce] = useState(0);
  const selectedCycle =
    cycles.find((cycle) => cycleKey(cycle) === selectedKey) ??
    cycles[cycles.length - 1] ??
    null;
  const period = useMemo<MeterPeriod | null>(
    () => customRange ?? selectedCycle,
    [customRange, selectedCycle],
  );

  const selectCycle = useCallback((cycle: MeterCycleWindow) => {
    setSelectedKey(cycleKey(cycle));
    setCustomRange(null);
  }, []);
  const setPickedRange = useCallback((from: Date, to: Date) => {
    const range = rangeFromPicker(from, to);
    if (range.to.getTime() <= range.from.getTime()) return;
    const lastDay = new Date(
      Date.UTC(range.from.getUTCFullYear(), range.from.getUTCMonth() + 4, 0),
    ).getUTCDate();
    const maxTo = new Date(
      Date.UTC(
        range.from.getUTCFullYear(),
        range.from.getUTCMonth() + 3,
        Math.min(range.from.getUTCDate(), lastDay),
      ),
    );
    if (range.to > maxTo) {
      toast.error("Select a range of at most three calendar months.");
      return;
    }
    setCustomRange(range);
  }, []);
  const clearCustomRange = useCallback(() => setCustomRange(null), []);
  const selectChartRange = useCallback(
    (from: Date, to: Date) => {
      if (!period) return;
      const dayMs = 24 * 60 * 60 * 1000;
      const start = Math.max(
        Math.floor(from.getTime() / dayMs) * dayMs,
        period.from.getTime(),
      );
      const end = Math.min(
        Math.ceil(to.getTime() / dayMs) * dayMs,
        period.to.getTime(),
      );
      if (end <= start) return;
      setCustomRange({ from: new Date(start), to: new Date(end) });
    },
    [period],
  );
  const reset = useCallback(() => {
    setSelectedKey(null);
    setCustomRange(null);
    setViewNonce((value) => value + 1);
  }, []);

  return {
    period,
    requestPeriod: customRange ?? (selectedKey === null ? null : selectedCycle),
    selectedCycle,
    customRange,
    viewNonce,
    selectCycle,
    setPickedRange,
    clearCustomRange,
    selectChartRange,
    reset,
  };
}
