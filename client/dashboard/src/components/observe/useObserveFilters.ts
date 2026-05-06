import { getPresetRange, type DateRangePreset } from "@gram-ai/elements";
import type { TypesToInclude } from "@gram/client/models/components";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import type { FilterChip } from "@/components/observe/ObserveFilterBar";
import {
  buildLogFilters,
  isValidPreset,
  safeBase64Decode,
  safeBase64Encode,
} from "./observeFilterUtils";
import { DEFAULT_HOOK_TYPES, VALID_HOOK_TYPES } from "./observeFilterConstants";

const SERVER_FILTER_PATH = "gram.tool_call.source";
const USER_EMAIL_FILTER_PATH = "user.email";

function parseHookTypesParam(raw: string | null): TypesToInclude[] {
  if (!raw) return [...DEFAULT_HOOK_TYPES];

  const parsed = raw
    .split(",")
    .filter((t): t is TypesToInclude =>
      VALID_HOOK_TYPES.includes(t as TypesToInclude),
    );
  const unique = [...new Set(parsed)];
  return unique.length > 0 ? unique : [...DEFAULT_HOOK_TYPES];
}

export function useObserveFilters() {
  const [searchParams, setSearchParams] = useSearchParams();

  const [activeFilters, setActiveFilters] = useState<FilterChip[]>(() => {
    const filters: FilterChip[] = [];

    const initialServer = searchParams.get("server");
    if (initialServer) {
      const values = initialServer
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      if (values.length > 0) {
        filters.push({
          display: values.join(", "),
          filters: values,
          path: SERVER_FILTER_PATH,
        });
      }
    }

    const initialUserEmail = searchParams.get("user");
    if (initialUserEmail) {
      const values = initialUserEmail
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      if (values.length > 0) {
        filters.push({
          display: values.join(", "),
          filters: values,
          path: USER_EMAIL_FILTER_PATH,
        });
      }
    }

    return filters;
  });

  const [selectedHookTypes, setSelectedHookTypes] = useState<TypesToInclude[]>(
    () => parseHookTypesParam(searchParams.get("hookTypes")),
  );
  const [knownServers, setKnownServers] = useState<string[]>([]);
  const [knownUserEmails, setKnownUserEmails] = useState<string[]>([]);

  const urlRange = searchParams.get("range");
  const urlFrom = searchParams.get("from");
  const urlTo = searchParams.get("to");
  const urlLabelEncoded = searchParams.get("label");
  const urlLabel = urlLabelEncoded ? safeBase64Decode(urlLabelEncoded) : null;

  const dateRange: DateRangePreset = isValidPreset(urlRange) ? urlRange : "7d";

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

  const setDateRangeParam = useCallback(
    (preset: DateRangePreset) => {
      updateSearchParams({ range: preset, from: null, to: null, label: null });
    },
    [updateSearchParams],
  );

  const setCustomRangeParam = useCallback(
    (from: Date, to: Date, label?: string) => {
      updateSearchParams({
        range: null,
        from: from.toISOString(),
        to: to.toISOString(),
        label: label ? safeBase64Encode(label) : null,
      });
    },
    [updateSearchParams],
  );

  const clearCustomRange = useCallback(() => {
    updateSearchParams({ from: null, to: null, label: null });
  }, [updateSearchParams]);

  const { from, to } = useMemo(
    () => customRange ?? getPresetRange(dateRange),
    [customRange, dateRange],
  );

  useEffect(() => {
    setKnownServers([]);
    setKnownUserEmails([]);
  }, [from, to]);

  const logFilters = useMemo(
    () => buildLogFilters(activeFilters),
    [activeFilters],
  );

  const addKnownServers = useCallback((names: string[]) => {
    if (names.length === 0) return;
    setKnownServers((prev) => {
      const merged = [...new Set([...prev, ...names])];
      return merged.length === prev.length ? prev : merged;
    });
  }, []);

  const addKnownUserEmails = useCallback((emails: string[]) => {
    if (emails.length === 0) return;
    setKnownUserEmails((prev) => {
      const merged = [...new Set([...prev, ...emails])];
      return merged.length === prev.length ? prev : merged;
    });
  }, []);

  const serverOptions = useMemo(() => {
    const selected = activeFilters
      .filter((f) => f.path === SERVER_FILTER_PATH)
      .flatMap((f) => f.filters)
      .filter(Boolean);
    return [...new Set([...knownServers, ...selected])];
  }, [knownServers, activeFilters]);

  const userEmailOptions = useMemo(() => {
    const selected = activeFilters
      .filter((f) => f.path === USER_EMAIL_FILTER_PATH)
      .flatMap((f) => f.filters)
      .filter(Boolean);
    return [...new Set([...knownUserEmails, ...selected])];
  }, [knownUserEmails, activeFilters]);

  const handleUserEmailSelectionChange = useCallback(
    (values: string[]) => {
      setActiveFilters((prev) => {
        const nonEmail = prev.filter((f) => f.path !== USER_EMAIL_FILTER_PATH);
        const emailFilters: FilterChip[] =
          values.length > 0
            ? [
                {
                  display: values.join(", "),
                  filters: values,
                  path: USER_EMAIL_FILTER_PATH,
                },
              ]
            : [];
        return [...nonEmail, ...emailFilters];
      });
      setSearchParams(
        (urlPrev) => {
          const next = new URLSearchParams(urlPrev);
          if (values.length > 0) {
            next.set("user", values.join(","));
          } else {
            next.delete("user");
          }
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const handleServerSelectionChange = useCallback(
    (values: string[]) => {
      setActiveFilters((prev) => {
        const nonServer = prev.filter((f) => f.path !== SERVER_FILTER_PATH);
        const serverFilters: FilterChip[] =
          values.length > 0
            ? [
                {
                  display: values.join(", "),
                  filters: values,
                  path: SERVER_FILTER_PATH,
                },
              ]
            : [];
        return [...nonServer, ...serverFilters];
      });
      setSearchParams(
        (urlPrev) => {
          const next = new URLSearchParams(urlPrev);
          if (values.length > 0) {
            next.set("server", values.join(","));
          } else {
            next.delete("server");
          }
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const addFilter = useCallback(
    (chip: FilterChip) => {
      setActiveFilters((prev) => {
        const existing = prev.find((f) => f.path === chip.path);
        const alreadyPresent = existing?.filters.some((v) =>
          chip.filters.includes(v),
        );
        if (alreadyPresent) return prev;

        const merged: FilterChip = existing
          ? {
              path: chip.path,
              filters: [...new Set([...existing.filters, ...chip.filters])],
              display: [
                ...new Set([...existing.filters, ...chip.filters]),
              ].join(", "),
            }
          : chip;

        const newFilters = [
          ...prev.filter((f) => f.path !== chip.path),
          merged,
        ];

        setSearchParams(
          (urlPrev) => {
            const next = new URLSearchParams(urlPrev);
            if (chip.path === SERVER_FILTER_PATH) {
              next.set("server", merged.filters.join(","));
            } else if (chip.path === USER_EMAIL_FILTER_PATH) {
              next.set("user", merged.filters.join(","));
            }
            return next;
          },
          { replace: true },
        );

        return newFilters;
      });
    },
    [setSearchParams],
  );

  // Passing an empty array resets to DEFAULT_HOOK_TYPES and clears the URL param.
  const handleHookTypesChange = useCallback(
    (types: TypesToInclude[]) => {
      const nextTypes = [
        ...new Set(
          types.filter((t): t is TypesToInclude =>
            VALID_HOOK_TYPES.includes(t as TypesToInclude),
          ),
        ),
      ];
      const resolved =
        nextTypes.length === 0 ? [...DEFAULT_HOOK_TYPES] : nextTypes;
      const isDefault =
        resolved.length === DEFAULT_HOOK_TYPES.length &&
        DEFAULT_HOOK_TYPES.every((t) => resolved.includes(t));

      setSelectedHookTypes(resolved);
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (isDefault) {
            next.delete("hookTypes");
          } else {
            next.set("hookTypes", resolved.join(","));
          }
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  return {
    activeFilters,
    selectedHookTypes,
    dateRange,
    customRange,
    customRangeLabel: urlLabel,
    from,
    to,
    logFilters,
    serverOptions,
    addKnownServers,
    handleServerSelectionChange,
    userEmailOptions,
    addKnownUserEmails,
    handleUserEmailSelectionChange,
    addFilter,
    handleHookTypesChange,
    setDateRangeParam,
    setCustomRangeParam,
    clearCustomRange,
  };
}
