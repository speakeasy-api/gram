import type { ActiveLogFilter } from "@/pages/logs/log-filter-types";
import { parseFilters, serializeFilters } from "@/pages/logs/log-filter-url";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router";

/**
 * The free-text query and attribute chips, read from the URL.
 *
 * Both are derived from the params rather than seeded into state from them, so
 * back/forward and a pasted deep link actually change what is shown. Only the
 * text being typed is local state — it is not a filter until it is submitted.
 */
export function useAttributeSearchParams(): {
  attributeSearchInput: string;
  attributeSearchQuery: string | null;
  attributeFilters: ActiveLogFilter[];
  setAttributeSearchInput: (value: string) => void;
  updateAttributeFilters: (filters: ActiveLogFilter[]) => void;
  updateAttributeSearchQuery: (query: string) => void;
} {
  const [searchParams, setSearchParams] = useSearchParams();

  const attributeSearchQuery = searchParams.get("q") || null;
  const afParam = searchParams.get("af");

  // Keyed on the raw param string, not on searchParams: parseFilters mints a
  // fresh id per filter, and this array is part of the traces query key, so a
  // new array identity on every render would refetch the whole list forever.
  const attributeFilters = useMemo(() => parseFilters(afParam), [afParam]);

  const [attributeSearchInput, setAttributeSearchInput] = useState(
    attributeSearchQuery ?? "",
  );

  // Resync the box when the query changes from somewhere other than typing:
  // browser navigation, or a deep link landing with a search already applied.
  useEffect(() => {
    setAttributeSearchInput(attributeSearchQuery ?? "");
  }, [attributeSearchQuery]);

  const updateAttributeFilters = useCallback(
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

  const updateAttributeSearchQuery = useCallback(
    (query: string) => {
      const trimmed = query.trim();
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (trimmed) {
            next.set("q", trimmed);
          } else {
            next.delete("q");
          }
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  return {
    attributeSearchInput,
    attributeSearchQuery,
    attributeFilters,
    setAttributeSearchInput,
    updateAttributeFilters,
    updateAttributeSearchQuery,
  };
}
