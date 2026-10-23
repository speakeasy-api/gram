import { useMemo, useState } from "react";
import { getPresetRange } from "@/elements";
import type { DateRangeValue } from "@/components/filters/filter-schema";

/** Keep cursor bounds stable until filters change or the user refreshes. */
export function useSessionDateRange(value: DateRangeValue) {
  const [revision, setRevision] = useState(0);
  const from = value.customRange?.from.getTime();
  const to = (value.calendarEnd ?? value.customRange?.to)?.getTime();
  const dates = useMemo(() => {
    if (from !== undefined && to !== undefined) {
      const end = new Date(to);
      return { from: new Date(from), to: end };
    }
    if (value.preset) return getPresetRange(value.preset);
    return { from: undefined, to: undefined };
  }, [from, to, value.preset, revision]);
  return {
    dates,
    refreshDates: () => {
      if (value.customRange || !value.preset) return false;
      // A new upper bound changes the query key and starts at page one.
      setRevision((current) => current + 1);
      return true;
    },
  };
}
