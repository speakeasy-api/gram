import { getPresetRange, type DateRangePreset } from "@gram-ai/elements";
import type { TypesToInclude } from "@gram/client/models/components";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import type { FilterChip } from "@/components/observe/ObserveFilterBar";
import {
  buildLogFilters,
  isValidPreset,
  mergeFilterChip,
  resolveRoleEmails,
  safeBase64Decode,
  safeBase64Encode,
} from "./observeFilterUtils";
import { DEFAULT_HOOK_TYPES, VALID_HOOK_TYPES } from "./observeFilterConstants";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";

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

function parseFilterParam(raw: string | null, path: string): FilterChip | null {
  if (!raw) return null;

  const values = raw
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);

  if (values.length === 0) return null;

  return {
    display: values.join(", "),
    filters: values,
    path,
  };
}

function buildActiveFilters(searchParams: URLSearchParams): FilterChip[] {
  return [
    parseFilterParam(searchParams.get("server"), SERVER_FILTER_PATH),
    parseFilterParam(searchParams.get("user"), USER_EMAIL_FILTER_PATH),
  ].filter((filter): filter is FilterChip => filter !== null);
}

export function useObserveFilters() {
  const [searchParams, setSearchParams] = useSearchParams();

  const activeFilters = useMemo(
    () => buildActiveFilters(searchParams),
    [searchParams],
  );
  const selectedHookTypes = useMemo(
    () => parseHookTypesParam(searchParams.get("hookTypes")),
    [searchParams],
  );
  const [knownServers, setKnownServers] = useState<string[]>([]);
  const [knownUserEmails, setKnownUserEmails] = useState<string[]>([]);

  const { data: membersData, isLoading: membersLoading } = useMembers();
  const { data: rolesData } = useRoles();

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

  const selectedRoleIds = useMemo(() => {
    const raw = searchParams.get("role");
    if (!raw) return [];
    return raw
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
  }, [searchParams]);

  const roleOptions = useMemo(
    () => (rolesData?.roles ?? []).map((r) => ({ id: r.id, name: r.name })),
    [rolesData],
  );

  const roleEmails = useMemo(
    () => resolveRoleEmails(selectedRoleIds, membersData?.members ?? []),
    [selectedRoleIds, membersData],
  );

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
    () => buildLogFilters(activeFilters, roleEmails),
    [activeFilters, roleEmails],
  );

  const roleFilterPending = selectedRoleIds.length > 0 && membersLoading;

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

  const handleRoleSelectionChange = useCallback(
    (values: string[]) => {
      setSearchParams(
        (urlPrev) => {
          const next = new URLSearchParams(urlPrev);
          if (values.length > 0) {
            next.set("role", values.join(","));
          } else {
            next.delete("role");
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
      setSearchParams(
        (urlPrev) => {
          const { merged } = mergeFilterChip(buildActiveFilters(urlPrev), chip);
          if (!merged) return urlPrev;

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
    selectedRoleIds,
    roleOptions,
    handleRoleSelectionChange,
    roleFilterPending,
  };
}
