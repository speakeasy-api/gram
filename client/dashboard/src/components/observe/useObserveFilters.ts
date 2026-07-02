import { getPresetRange, type DateRangePreset } from "@gram-ai/elements";
import { telemetryGetHooksSummary } from "@gram/client/funcs/telemetryGetHooksSummary";
import type { TypesToInclude } from "@gram/client/models/components";
import { useCallback, useMemo } from "react";
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
import type { ObserveTypeFilterValue } from "@/components/observe/ObserveFilterBar";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { useGramContext } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { useQuery } from "@tanstack/react-query";

const SERVER_FILTER_PATH = "gram.tool_call.source";
const USER_EMAIL_FILTER_PATH = "user.email";
const HOOK_SOURCE_FILTER_PATH = "gram.hook.source";

// Sentinel labels the hooks summary substitutes for empty values. They are
// display-only ("local" for a missing server, "Unknown" for a missing email)
// and do not round-trip as real filter values, so they are excluded from the
// dropdown options.
const SERVER_SENTINEL = "local";
const USER_EMAIL_SENTINEL = "Unknown";

type UseObserveFiltersOptions<T extends ObserveTypeFilterValue> = {
  defaultTypes?: T[];
  validTypes?: T[];
};

function parseHookTypesParam<T extends ObserveTypeFilterValue>(
  raw: string | null,
  defaultTypes: T[],
  validTypes: T[],
): T[] {
  if (!raw) return [...defaultTypes];

  const parsed = raw
    .split(",")
    .filter((t): t is T => validTypes.includes(t as T));
  const unique = [...new Set(parsed)];
  return unique.length > 0 ? unique : [...defaultTypes];
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
    parseFilterParam(searchParams.get("source"), HOOK_SOURCE_FILTER_PATH),
  ].filter((filter): filter is FilterChip => filter !== null);
}

function useObserveFiltersImpl<
  T extends ObserveTypeFilterValue = TypesToInclude,
>(options: UseObserveFiltersOptions<T> = {}) {
  const [searchParams, setSearchParams] = useSearchParams();
  const defaultTypes = (options.defaultTypes ?? DEFAULT_HOOK_TYPES) as T[];
  const validTypes = (options.validTypes ?? VALID_HOOK_TYPES) as T[];

  const activeFilters = useMemo(
    () => buildActiveFilters(searchParams),
    [searchParams],
  );
  const selectedHookTypes = useMemo(
    () =>
      parseHookTypesParam(
        searchParams.get("hookTypes"),
        defaultTypes,
        validTypes,
      ),
    [searchParams, defaultTypes, validTypes],
  );
  const client = useGramContext();
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
    () =>
      (rolesData?.roles ?? [])
        .filter((r) => r.memberCount > 0)
        .map((r) => ({ id: r.id, name: r.name })),
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

  const logFilters = useMemo(
    () => buildLogFilters(activeFilters, roleEmails),
    [activeFilters, roleEmails],
  );

  const roleFilterPending = selectedRoleIds.length > 0 && membersLoading;

  // Fetch the full universe of servers and users for the selected time range.
  // Deliberately omits the active server/email/role/type filters so the
  // dropdowns always offer every value in the window — otherwise a filter that
  // returns no results would leave the dropdown empty and the user stuck,
  // unable to pivot to a different value. Keyed only on the range so the query
  // is shared across every page that uses this hook.
  const { data: filterOptionsSummary } = useQuery({
    queryKey: ["hooks-filter-options", from.toISOString(), to.toISOString()],
    queryFn: () =>
      unwrapAsync(
        telemetryGetHooksSummary(client, {
          getHooksSummaryPayload: { from, to },
        }),
      ),
    throwOnError: false,
  });

  const serverOptions = useMemo(() => {
    const selected = activeFilters
      .filter((f) => f.path === SERVER_FILTER_PATH)
      .flatMap((f) => f.filters)
      .filter(Boolean);
    const known = (filterOptionsSummary?.servers ?? [])
      .map((s) => s.serverName)
      .filter((name) => name && name !== SERVER_SENTINEL);
    return [...new Set([...known, ...selected])];
  }, [filterOptionsSummary, activeFilters]);

  const userEmailOptions = useMemo(() => {
    const selected = activeFilters
      .filter((f) => f.path === USER_EMAIL_FILTER_PATH)
      .flatMap((f) => f.filters)
      .filter(Boolean);
    const known = (filterOptionsSummary?.users ?? [])
      .map((u) => u.userEmail)
      .filter((email) => email && email !== USER_EMAIL_SENTINEL);
    return [...new Set([...known, ...selected])];
  }, [filterOptionsSummary, activeFilters]);

  // The hooks summary breakdown carries hook_source ("claude-code", "cursor",
  // "codex", ...) per (user, server, source, tool) row. Collapse it to the
  // distinct set of agents seen in the window so the dropdown only offers
  // sources that actually have data, mirroring serverOptions/userEmailOptions.
  const hookSourceOptions = useMemo(() => {
    const selected = activeFilters
      .filter((f) => f.path === HOOK_SOURCE_FILTER_PATH)
      .flatMap((f) => f.filters)
      .filter(Boolean);
    const known = (filterOptionsSummary?.breakdown ?? [])
      .map((b) => b.hookSource)
      .filter(Boolean);
    return [...new Set([...known, ...selected])];
  }, [filterOptionsSummary, activeFilters]);

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

  const handleHookSourceSelectionChange = useCallback(
    (values: string[]) => {
      setSearchParams(
        (urlPrev) => {
          const next = new URLSearchParams(urlPrev);
          if (values.length > 0) {
            next.set("source", values.join(","));
          } else {
            next.delete("source");
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
          } else if (chip.path === HOOK_SOURCE_FILTER_PATH) {
            next.set("source", merged.filters.join(","));
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
    (types: T[]) => {
      const nextTypes = [
        ...new Set(types.filter((t): t is T => validTypes.includes(t as T))),
      ];
      const resolved = nextTypes.length === 0 ? [...defaultTypes] : nextTypes;
      const isDefault =
        resolved.length === defaultTypes.length &&
        defaultTypes.every((t) => resolved.includes(t));

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
    [defaultTypes, setSearchParams, validTypes],
  );

  // account_type filter (team | personal); empty string = no filter. Opt-in:
  // only pages that pass these to ObserveFilterBar surface the control.
  const urlAccountType = searchParams.get("account_type");
  const accountType: string =
    urlAccountType === "team" || urlAccountType === "personal"
      ? urlAccountType
      : "";
  const handleAccountTypeChange = useCallback(
    (value: string) => {
      // Match the other filter handlers: replace (don't push) so toggling the
      // filter doesn't stack history entries.
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (value) {
            next.set("account_type", value);
          } else {
            next.delete("account_type");
          }
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  return {
    accountType,
    handleAccountTypeChange,
    activeFilters,
    selectedHookTypes,
    dateRange,
    customRange,
    customRangeLabel: urlLabel,
    from,
    to,
    logFilters,
    serverOptions,
    handleServerSelectionChange,
    userEmailOptions,
    handleUserEmailSelectionChange,
    hookSourceOptions,
    handleHookSourceSelectionChange,
    addFilter,
    handleHookTypesChange,
    setDateRangeParam,
    setCustomRangeParam,
    clearCustomRange,
    selectedRoleIds,
    roleOptions,
    roleEmails,
    handleRoleSelectionChange,
    roleFilterPending,
  };
}

export function useObserveFilters(
  options?: UseObserveFiltersOptions<TypesToInclude>,
): ReturnType<typeof useObserveFiltersImpl<TypesToInclude>>;
export function useObserveFilters<T extends ObserveTypeFilterValue>(
  options: UseObserveFiltersOptions<T>,
): ReturnType<typeof useObserveFiltersImpl<T>>;
export function useObserveFilters<T extends ObserveTypeFilterValue>(
  options?: UseObserveFiltersOptions<T>,
): ReturnType<typeof useObserveFiltersImpl<T>> {
  return useObserveFiltersImpl(options);
}
