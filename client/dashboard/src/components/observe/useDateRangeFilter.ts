import { getPresetRange, type DateRangePreset } from "@/elements";
import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router";
import {
  isValidPreset,
  safeBase64Decode,
  safeBase64Encode,
} from "./observeFilterUtils";

export const DEFAULT_DATE_RANGE_PRESET: DateRangePreset = "7d";

export function useDateRangeFilter(
  defaultPreset: DateRangePreset = DEFAULT_DATE_RANGE_PRESET,
): {
  dateRange: DateRangePreset;
  customRange: { from: Date; to: Date } | null;
  customRangeLabel: string | null;
  from: Date;
  to: Date;
  setDateRangeParam: (
    preset: DateRangePreset,
    extraUpdates?: Record<string, string | null>,
  ) => void;
  setCustomRangeParam: (
    from: Date,
    to: Date,
    label?: string,
    extraUpdates?: Record<string, string | null>,
  ) => void;
  clearCustomRange: (extraUpdates?: Record<string, string | null>) => void;
} {
  const [searchParams, setSearchParams] = useSearchParams();

  const urlRange = searchParams.get("range");
  const urlFrom = searchParams.get("from");
  const urlTo = searchParams.get("to");
  const urlLabelEncoded = searchParams.get("label");
  const customRangeLabel = urlLabelEncoded
    ? safeBase64Decode(urlLabelEncoded)
    : null;

  const dateRange: DateRangePreset = isValidPreset(urlRange)
    ? urlRange
    : defaultPreset;

  const customRange = useMemo(() => {
    if (urlFrom && urlTo) {
      const from = new Date(urlFrom);
      const to = new Date(urlTo);
      if (!isNaN(from.getTime()) && !isNaN(to.getTime())) {
        return { from, to };
      }
    }
    return null;
  }, [urlFrom, urlTo]);

  const updateSearchParams = useCallback(
    (updates: Record<string, string | null>) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        for (const [key, value] of Object.entries(updates)) {
          if (value === null) {
            next.delete(key);
          } else {
            next.set(key, value);
          }
        }
        return next;
      });
    },
    [setSearchParams],
  );

  // The setters fold host-page params (extraUpdates) into the same
  // setSearchParams call: React Router's functional updater starts from the
  // render-time params, so a separate setter call in the same handler would
  // be lost, not merged.
  const setDateRangeParam = useCallback(
    (preset: DateRangePreset, extraUpdates?: Record<string, string | null>) => {
      updateSearchParams({
        range: preset,
        from: null,
        to: null,
        label: null,
        ...extraUpdates,
      });
    },
    [updateSearchParams],
  );

  const setCustomRangeParam = useCallback(
    (
      from: Date,
      to: Date,
      label?: string,
      extraUpdates?: Record<string, string | null>,
    ) => {
      updateSearchParams({
        range: null,
        from: from.toISOString(),
        to: to.toISOString(),
        label: label ? safeBase64Encode(label) : null,
        ...extraUpdates,
      });
    },
    [updateSearchParams],
  );

  const clearCustomRange = useCallback(
    (extraUpdates?: Record<string, string | null>) => {
      updateSearchParams({
        from: null,
        to: null,
        label: null,
        ...extraUpdates,
      });
    },
    [updateSearchParams],
  );

  const { from, to } = useMemo(
    () => customRange ?? getPresetRange(dateRange),
    [customRange, dateRange],
  );

  return {
    dateRange,
    customRange,
    customRangeLabel,
    from,
    to,
    setDateRangeParam,
    setCustomRangeParam,
    clearCustomRange,
  };
}

const PRESET_LABELS: Record<DateRangePreset, string> = {
  "15m": "last 15 minutes",
  "1h": "last hour",
  "4h": "last 4 hours",
  "1d": "last day",
  "2d": "last 2 days",
  "3d": "last 3 days",
  "7d": "last 7 days",
  "15d": "last 15 days",
  "30d": "last 30 days",
  "90d": "last 3 months",
};

export function formatDateRangeLabel(
  dateRange: DateRangePreset,
  customRangeLabel: string | null,
): string {
  if (customRangeLabel) return customRangeLabel;
  return PRESET_LABELS[dateRange] ?? "selected period";
}
