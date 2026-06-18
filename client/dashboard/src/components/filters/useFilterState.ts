import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router";
import { Operator } from "@gram/client/models/components/logfilter";
import {
  isValidPreset,
  safeBase64Decode,
  safeBase64Encode,
} from "@/components/observe/observeFilterUtils";
import {
  applyFilterAdd,
  applyFilterEdit,
  type ActiveLogFilter,
} from "@/pages/logs/log-filter-types";
import { parseFilters, serializeFilters } from "@/pages/logs/log-filter-url";
import {
  defaultValueForDimension,
  type DateRangeValue,
  type FilterDimension,
  type FilterValueByKind,
  type FilterValues,
} from "./filter-schema";

type AnyValue = FilterValueByKind[keyof FilterValueByKind];

// The date range deliberately reads/writes the shared range/from/to/label params
// (not a dimension-named param) so a bookmarked URL keeps working across pages and
// only one date range can be active at a time.
function parseDateRange(
  dim: Extract<FilterDimension, { kind: "daterange" }>,
  params: URLSearchParams,
): DateRangeValue {
  const from = params.get("from");
  const to = params.get("to");
  const label = params.get("label");

  let customRange: DateRangeValue["customRange"] = null;
  if (from && to) {
    const fromDate = new Date(from);
    const toDate = new Date(to);
    if (!isNaN(fromDate.getTime()) && !isNaN(toDate.getTime())) {
      customRange = { from: fromDate, to: toDate };
    }
  }

  const rawRange = params.get("range");
  const preset = customRange
    ? null
    : isValidPreset(rawRange)
      ? rawRange
      : (dim.defaultPreset ?? null);

  return {
    preset,
    customRange,
    customLabel: label ? safeBase64Decode(label) : null,
  };
}

function parseValue(dim: FilterDimension, params: URLSearchParams): AnyValue {
  switch (dim.kind) {
    case "multiselect": {
      const raw = params.get(dim.id);
      if (!raw) return [];
      return raw
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
    }
    case "select":
      return params.get(dim.id) || null;
    case "text":
      return params.get(dim.id) ?? "";
    case "boolean":
      return params.get(dim.id) === "1";
    case "daterange":
      return parseDateRange(dim, params);
  }
}

function writeDateRange(next: URLSearchParams, value: DateRangeValue): void {
  if (value.customRange) {
    next.set("from", value.customRange.from.toISOString());
    next.set("to", value.customRange.to.toISOString());
    next.delete("range");
    if (value.customLabel) {
      next.set("label", safeBase64Encode(value.customLabel));
    } else {
      next.delete("label");
    }
    return;
  }
  next.delete("from");
  next.delete("to");
  next.delete("label");
  if (value.preset) {
    next.set("range", value.preset);
  } else {
    next.delete("range");
  }
}

function writeValue(
  next: URLSearchParams,
  dim: FilterDimension,
  value: AnyValue,
): void {
  switch (dim.kind) {
    case "multiselect": {
      const arr = value as string[];
      if (arr.length > 0) {
        next.set(dim.id, arr.join(","));
      } else {
        next.delete(dim.id);
      }
      return;
    }
    case "select": {
      const v = value as string | null;
      if (v) {
        next.set(dim.id, v);
      } else {
        next.delete(dim.id);
      }
      return;
    }
    case "text": {
      const v = value as string;
      if (v.trim()) {
        next.set(dim.id, v);
      } else {
        next.delete(dim.id);
      }
      return;
    }
    case "boolean": {
      if (value === true) {
        next.set(dim.id, "1");
      } else {
        next.delete(dim.id);
      }
      return;
    }
    case "daterange":
      writeDateRange(next, value as DateRangeValue);
      return;
  }
}

export interface UseFilterStateResult<T extends readonly FilterDimension[]> {
  /** Typed value object keyed by dimension id. */
  values: FilterValues<T>;
  setValue: <Id extends keyof FilterValues<T>>(
    id: Id,
    value: FilterValues<T>[Id],
  ) => void;
  clearValue: (id: keyof FilterValues<T>) => void;
  clearAll: () => void;
  /** Arbitrary attribute filters (the `af` URL param). */
  customFilters: ActiveLogFilter[];
  addCustomFilter: (path: string, op: Operator, value?: string) => void;
  removeCustomFilter: (id: string) => void;
  editCustomFilter: (id: string, op: Operator, value?: string) => void;
  setCustomFilters: (filters: ActiveLogFilter[]) => void;
}

/**
 * Generic URL-param filter state for a strongly-typed schema. Generalizes the
 * per-dimension parse / `updateSearchParams` machinery previously hard-coded in
 * `observe/useObserveFilters`.
 */
export function useFilterState<const T extends readonly FilterDimension[]>(
  schema: T,
): UseFilterStateResult<T> {
  const [searchParams, setSearchParams] = useSearchParams();

  const values = useMemo(() => {
    const out: Record<string, AnyValue> = {};
    for (const dim of schema) {
      out[dim.id] = parseValue(dim, searchParams);
    }
    return out as FilterValues<T>;
  }, [schema, searchParams]);

  const setValue = useCallback(
    <Id extends keyof FilterValues<T>>(id: Id, value: FilterValues<T>[Id]) => {
      const dim = schema.find((d) => d.id === id);
      if (!dim) return;
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          writeValue(next, dim, value as AnyValue);
          return next;
        },
        { replace: true },
      );
    },
    [schema, setSearchParams],
  );

  const clearValue = useCallback(
    (id: keyof FilterValues<T>) => {
      const dim = schema.find((d) => d.id === id);
      if (!dim) return;
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          writeValue(next, dim, defaultValueForDimension(dim));
          return next;
        },
        { replace: true },
      );
    },
    [schema, setSearchParams],
  );

  const clearAll = useCallback(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        for (const dim of schema) {
          writeValue(next, dim, defaultValueForDimension(dim));
        }
        next.delete("af");
        return next;
      },
      { replace: true },
    );
  }, [schema, setSearchParams]);

  const customFilters = useMemo(
    () => parseFilters(searchParams.get("af")),
    [searchParams],
  );

  const setCustomFilters = useCallback(
    (filters: ActiveLogFilter[]) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          const serialized = serializeFilters(filters);
          if (serialized) {
            next.set("af", serialized);
          } else {
            next.delete("af");
          }
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const addCustomFilter = useCallback(
    (path: string, op: Operator, value?: string) => {
      setCustomFilters(
        applyFilterAdd(parseFilters(searchParams.get("af")), {
          path,
          op,
          value,
        }),
      );
    },
    [searchParams, setCustomFilters],
  );

  const removeCustomFilter = useCallback(
    (id: string) => {
      setCustomFilters(
        parseFilters(searchParams.get("af")).filter((f) => f.id !== id),
      );
    },
    [searchParams, setCustomFilters],
  );

  const editCustomFilter = useCallback(
    (id: string, op: Operator, value?: string) => {
      setCustomFilters(
        applyFilterEdit(parseFilters(searchParams.get("af")), id, {
          op,
          value,
        }),
      );
    },
    [searchParams, setCustomFilters],
  );

  return {
    values,
    setValue,
    clearValue,
    clearAll,
    customFilters,
    addCustomFilter,
    removeCustomFilter,
    editCustomFilter,
    setCustomFilters,
  };
}
