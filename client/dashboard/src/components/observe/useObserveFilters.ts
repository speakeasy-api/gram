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

const DEFAULT_HOOK_TYPES: TypesToInclude[] = ["mcp", "skill"];

export function useObserveFilters() {
  const [searchParams, setSearchParams] = useSearchParams();

  const [activeFilters, setActiveFilters] = useState<FilterChip[]>(() => {
    const filters: FilterChip[] = [];

    const initialServer = searchParams.get("server");
    if (initialServer) {
      initialServer
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean)
        .forEach((value) => {
          filters.push({
            display: value,
            filters: [value],
            path: "gram.tool_call.source",
          });
        });
    }

    const initialUserEmail = searchParams.get("user");
    if (initialUserEmail) {
      initialUserEmail
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean)
        .forEach((value) => {
          filters.push({
            display: value,
            filters: [value],
            path: "user.email",
          });
        });
    }

    return filters;
  });

  const [selectedHookTypes, setSelectedHookTypes] = useState<TypesToInclude[]>(
    () => {
      const raw = searchParams.get("hookTypes");
      if (!raw) return DEFAULT_HOOK_TYPES;
      return raw
        .split(",")
        .filter((t) =>
          ["mcp", "local", "skill"].includes(t),
        ) as TypesToInclude[];
    },
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
      .filter((f) => f.path === "gram.tool_call.source")
      .map((f) => f.filters[0])
      .filter((v): v is string => Boolean(v));
    return [...new Set([...knownServers, ...selected])];
  }, [knownServers, activeFilters]);

  const userEmailOptions = useMemo(() => {
    const selected = activeFilters
      .filter((f) => f.path === "user.email")
      .map((f) => f.filters[0])
      .filter((v): v is string => Boolean(v));
    return [...new Set([...knownUserEmails, ...selected])];
  }, [knownUserEmails, activeFilters]);

  const handleUserEmailSelectionChange = useCallback(
    (values: string[]) => {
      setActiveFilters((prev) => {
        const nonEmail = prev.filter((f) => f.path !== "user.email");
        const emailFilters: FilterChip[] = values.map((v) => ({
          display: v,
          filters: [v],
          path: "user.email",
        }));
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
        const nonServer = prev.filter(
          (f) => f.path !== "gram.tool_call.source",
        );
        const serverFilters: FilterChip[] = values.map((v) => ({
          display: v,
          filters: [v],
          path: "gram.tool_call.source",
        }));
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
        const exists = prev.some(
          (f) => f.path === chip.path && f.display === chip.display,
        );
        if (exists) return prev;

        const newFilters = [...prev, chip];

        setSearchParams(
          (urlPrev) => {
            const next = new URLSearchParams(urlPrev);
            if (chip.path === "gram.tool_call.source") {
              const serverFilters = newFilters
                .filter((f) => f.path === "gram.tool_call.source")
                .map((f) => f.display);
              next.set("server", serverFilters.join(","));
            } else if (chip.path === "user.email") {
              const userFilters = newFilters
                .filter((f) => f.path === "user.email")
                .map((f) => f.display);
              next.set("user", userFilters.join(","));
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

  const removeFilter = useCallback(
    (path: string, display?: string) => {
      setActiveFilters((prev) => {
        const newFilters = display
          ? prev.filter((f) => !(f.path === path && f.display === display))
          : prev.filter((f) => f.path !== path);

        setSearchParams(
          (urlPrev) => {
            const next = new URLSearchParams(urlPrev);
            if (path === "gram.tool_call.source") {
              const serverFilters = newFilters
                .filter((f) => f.path === "gram.tool_call.source")
                .map((f) => f.display);
              if (serverFilters.length > 0) {
                next.set("server", serverFilters.join(","));
              } else {
                next.delete("server");
              }
            } else if (path === "user.email") {
              const userFilters = newFilters
                .filter((f) => f.path === "user.email")
                .map((f) => f.display);
              if (userFilters.length > 0) {
                next.set("user", userFilters.join(","));
              } else {
                next.delete("user");
              }
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

  const handleHookTypesChange = useCallback(
    (types: TypesToInclude[]) => {
      setSelectedHookTypes(types);
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          const isDefault =
            types.length === 2 &&
            types.includes("mcp") &&
            types.includes("skill") &&
            !types.includes("local");
          if (isDefault) {
            next.delete("hookTypes");
          } else if (types.length > 0) {
            next.set("hookTypes", types.join(","));
          } else {
            next.delete("hookTypes");
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
    removeFilter,
    handleHookTypesChange,
    setDateRangeParam,
    setCustomRangeParam,
    clearCustomRange,
  };
}
