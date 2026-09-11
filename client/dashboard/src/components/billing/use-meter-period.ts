import { useCallback, useMemo, useState } from "react";

export type MeterCycleWindow = { from: Date; to: Date };
export type MeterPeriod = { from: Date; to: Date; label?: string };
export type MeterCustomRange = MeterPeriod;

function cycleKey(cycle: MeterCycleWindow): string {
  return cycle.from.toISOString();
}

function rangeFromPicker(from: Date, to: Date): MeterPeriod {
  const localDayStart = (date: Date): number =>
    new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
  if (
    from.getTime() !== localDayStart(from) ||
    to.getTime() !== localDayStart(to)
  ) {
    return { from, to };
  }
  return {
    from: new Date(
      Date.UTC(from.getFullYear(), from.getMonth(), from.getDate()),
    ),
    to: new Date(Date.UTC(to.getFullYear(), to.getMonth(), to.getDate() + 1)),
  };
}

export function useMeterPeriod(cycles: MeterCycleWindow[]): {
  period: MeterPeriod | null;
  selectedCycle: MeterCycleWindow | null;
  customRange: MeterCustomRange | null;
  viewNonce: number;
  selectCycle: (cycle: MeterCycleWindow) => void;
  setPickedRange: (from: Date, to: Date, label?: string) => void;
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
  const setPickedRange = useCallback((from: Date, to: Date, label?: string) => {
    const range = rangeFromPicker(from, to);
    if (range.to.getTime() <= range.from.getTime()) return;
    setCustomRange({ ...range, label });
  }, []);
  const clearCustomRange = useCallback(() => setCustomRange(null), []);
  const selectChartRange = useCallback(
    (from: Date, to: Date) => {
      if (!period) return;
      const start = Math.max(from.getTime(), period.from.getTime());
      const end = Math.min(to.getTime(), period.to.getTime());
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
