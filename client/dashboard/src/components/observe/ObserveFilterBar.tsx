import { type DateRangePreset } from "@/elements";
import { type ReactNode, useCallback, useMemo } from "react";
import { useSearchParams } from "react-router";
import {
  defineFilters,
  type FilterDimension,
  type FilterValue,
  type OptionsById,
} from "@/components/filters";
import { ACCOUNT_TYPE_OPTIONS } from "@/components/observe/observeFilterConstants";
import { Page } from "@/components/page-layout";
import type { useServerNameMappings } from "@/hooks/useServerNameMappings";
import { HookSourceIcon } from "@/pages/hooks/HookSourceIcon";
import type {
  MultiSelectGroup,
  MultiSelectOption,
} from "@/components/ui/multi-select";

const SERVER_FILTER_PATH = "gram.tool_call.source";
const USER_EMAIL_FILTER_PATH = "user.email";
const HOOK_SOURCE_FILTER_PATH = "gram.hook.source";
const DEFAULT_PRESET: DateRangePreset = "7d";

export interface FilterChip {
  display: string;
  filters: string[];
  path: string;
}

export type ObserveTypeFilterValue =
  | "mcp"
  | "local"
  | "skill"
  | "hosted_mcp_server"
  | "tunneled_mcp_server"
  | "shadow_mcp_server"
  | "local_tool";

const SERVER_TYPES: Array<{ label: string; value: ObserveTypeFilterValue }> = [
  { label: "Shadow MCP Servers", value: "mcp" },
  { label: "Local Tools", value: "local" },
  { label: "Skills", value: "skill" },
];

// Trace outcome, mapped to the ListToolUsageTraces `statuses` payload field.
export type ObserveStatusFilterValue =
  | "error"
  | "success"
  | "blocked"
  | "pending";

const STATUS_TYPES: Array<{ label: string; value: ObserveStatusFilterValue }> =
  [
    { label: "Error", value: "error" },
    { label: "Success", value: "success" },
    { label: "Blocked", value: "blocked" },
    { label: "Pending", value: "pending" },
  ];

// Server, User and the date range are the most-used dimensions, so they pin as
// always-visible pills. Role and Agent appear as pills once active; Type is
// sheet-only (it always carries a default value and would otherwise pill).
const OBSERVE_FILTER_BASE = [
  {
    id: "server",
    label: "Server",
    kind: "multiselect",
    pinned: true,
    placeholder: "Filter by server name",
  },
  {
    id: "user",
    label: "User",
    kind: "multiselect",
    pinned: true,
    placeholder: "Filter by user email",
  },
  {
    id: "date",
    label: "Date range",
    kind: "daterange",
    pinned: true,
    defaultPreset: DEFAULT_PRESET,
  },
  {
    id: "role",
    label: "Role",
    kind: "multiselect",
    placeholder: "Filter by role",
  },
  {
    id: "source",
    label: "Agent",
    kind: "multiselect",
    placeholder: "Filter by agent",
  },
  { id: "type", label: "Type", kind: "multiselect", hideChip: true },
] as const;

// Account-type and status are opt-in: only consumers whose data source can
// serve them (the tool-usage traces/summary paths) pass the matching handlers,
// so the controls aren't shown where they can't be filtered. Appended to the
// base schema at render time based on which handlers were provided.
const ACCOUNT_TYPE_DIMENSION: FilterDimension = {
  id: "account_type",
  label: "Account type",
  kind: "select",
  allLabel: "All",
};
const STATUS_DIMENSION: FilterDimension = {
  id: "status",
  label: "Status",
  kind: "multiselect",
};

export function ObserveFilterBar({
  serverOptions,
  serverOptionGroups,
  onServerSelectionChange,
  userEmailOptions,
  onUserEmailSelectionChange,
  sourceOptions,
  onSourceSelectionChange,
  roleOptions,
  selectedRoleIds,
  onRoleSelectionChange,
  activeFilters,
  selectedTypes,
  onTypesChange,
  typeOptions = SERVER_TYPES,
  selectedStatuses,
  onStatusesChange,
  statusOptions = STATUS_TYPES,
  dateRange,
  customRange,
  customRangeLabel,
  onDateRangeChange,
  onCustomRangeChange,
  onClearCustomRange,
  projectSlug,
  className,
  serverNameMappings,
  attributeSearchControl,
  accountType,
  onAccountTypeChange,
  onRefresh,
  isRefreshing,
}: {
  serverOptions: string[];
  serverOptionGroups?: MultiSelectGroup[];
  onServerSelectionChange: (values: string[]) => void;
  userEmailOptions: string[];
  onUserEmailSelectionChange: (values: string[]) => void;
  sourceOptions: string[];
  onSourceSelectionChange: (values: string[]) => void;
  roleOptions: Array<{ id: string; name: string }>;
  selectedRoleIds: string[];
  onRoleSelectionChange: (values: string[]) => void;
  activeFilters: FilterChip[];
  selectedTypes: ObserveTypeFilterValue[];
  onTypesChange: (types: ObserveTypeFilterValue[]) => void;
  typeOptions?: MultiSelectOption[];
  // Opt-in trace-outcome ("error" | "success" | "blocked" | "pending") filter.
  // Only wire these on pages backed by the tool-usage traces query.
  selectedStatuses?: ObserveStatusFilterValue[];
  onStatusesChange?: (statuses: ObserveStatusFilterValue[]) => void;
  statusOptions?: MultiSelectOption[];
  dateRange: DateRangePreset;
  customRange: { from: Date; to: Date } | null;
  customRangeLabel: string | null;
  onDateRangeChange: (preset: DateRangePreset) => void;
  onCustomRangeChange: (from: Date, to: Date, label?: string) => void;
  onClearCustomRange: () => void;
  projectSlug?: string;
  className?: string;
  serverNameMappings?: ReturnType<typeof useServerNameMappings>;
  attributeSearchControl?: ReactNode;
  // Opt-in account-type ("team" | "personal" | "") filter. Only wire these on
  // pages whose data source can filter on account_type (e.g. raw logs).
  accountType?: string;
  onAccountTypeChange?: (value: string) => void;
  onRefresh?: () => void;
  isRefreshing?: boolean;
}): JSX.Element {
  const valuesByPath = useCallback(
    (path: string) =>
      activeFilters
        .filter((f) => f.path === path)
        .flatMap((f) => f.filters)
        .filter(Boolean),
    [activeFilters],
  );

  const selectedServers = useMemo(
    () => valuesByPath(SERVER_FILTER_PATH),
    [valuesByPath],
  );
  const selectedEmails = useMemo(
    () => valuesByPath(USER_EMAIL_FILTER_PATH),
    [valuesByPath],
  );
  const selectedSources = useMemo(
    () => valuesByPath(HOOK_SOURCE_FILTER_PATH),
    [valuesByPath],
  );

  const serverOptionsResolved = useMemo(() => {
    if (serverOptionGroups) return serverOptionGroups;
    const rawToDisplay = serverNameMappings?.rawToDisplay;
    return serverOptions.map((rawName) => ({
      label: rawToDisplay?.get(rawName) ?? rawName,
      value: rawName,
    }));
  }, [serverOptionGroups, serverOptions, serverNameMappings?.rawToDisplay]);

  // The raw hook_source value (e.g. "claude-code") is shown verbatim — matching
  // the table row — and paired with its brand icon.
  const sourceOptionsWithIcons = useMemo(
    () =>
      sourceOptions.map((source) => ({
        label: source,
        value: source,
        icon: ({ className: iconClassName }: { className?: string }) => (
          <HookSourceIcon source={source} className={iconClassName} />
        ),
      })),
    [sourceOptions],
  );

  const values = useMemo<Record<string, FilterValue>>(
    () => ({
      server: selectedServers,
      user: selectedEmails,
      date: {
        preset: customRange ? null : dateRange,
        customRange,
        customLabel: customRangeLabel,
      },
      role: selectedRoleIds,
      source: selectedSources,
      type: selectedTypes,
      status: selectedStatuses ?? [],
      account_type: accountType || null,
    }),
    [
      selectedServers,
      selectedEmails,
      customRange,
      dateRange,
      customRangeLabel,
      selectedRoleIds,
      selectedSources,
      selectedTypes,
      selectedStatuses,
      accountType,
    ],
  );

  const optionsById = useMemo<OptionsById>(
    () => ({
      server: serverOptionsResolved,
      user: userEmailOptions.map((e) => ({ label: e, value: e })),
      role: roleOptions.map((r) => ({ label: r.name, value: r.id })),
      source: sourceOptionsWithIcons,
      type: typeOptions,
      status: statusOptions,
      account_type: ACCOUNT_TYPE_OPTIONS,
    }),
    [
      serverOptionsResolved,
      userEmailOptions,
      roleOptions,
      sourceOptionsWithIcons,
      typeOptions,
      statusOptions,
    ],
  );

  const handleChange = useCallback(
    (id: string, value: FilterValue) => {
      switch (id) {
        case "server":
          onServerSelectionChange(value as string[]);
          return;
        case "user":
          onUserEmailSelectionChange(value as string[]);
          return;
        case "role":
          onRoleSelectionChange(value as string[]);
          return;
        case "source":
          onSourceSelectionChange(value as string[]);
          return;
        case "type":
          onTypesChange(value as ObserveTypeFilterValue[]);
          return;
        case "status":
          onStatusesChange?.(value as ObserveStatusFilterValue[]);
          return;
        case "account_type":
          onAccountTypeChange?.((value as string | null) ?? "");
          return;
        case "date": {
          const d = value as {
            preset: DateRangePreset | null;
            customRange: { from: Date; to: Date } | null;
            customLabel: string | null;
          };
          if (d.customRange) {
            onCustomRangeChange(
              d.customRange.from,
              d.customRange.to,
              d.customLabel ?? undefined,
            );
          } else if (d.preset) {
            onDateRangeChange(d.preset);
          } else {
            onClearCustomRange();
          }
          return;
        }
      }
    },
    [
      onServerSelectionChange,
      onUserEmailSelectionChange,
      onRoleSelectionChange,
      onSourceSelectionChange,
      onTypesChange,
      onStatusesChange,
      onCustomRangeChange,
      onDateRangeChange,
      onClearCustomRange,
      onAccountTypeChange,
    ],
  );

  const handleClear = useCallback(
    (id: string) => {
      switch (id) {
        case "server":
          onServerSelectionChange([]);
          return;
        case "user":
          onUserEmailSelectionChange([]);
          return;
        case "role":
          onRoleSelectionChange([]);
          return;
        case "source":
          onSourceSelectionChange([]);
          return;
        case "type":
          onTypesChange([]);
          return;
        case "status":
          onStatusesChange?.([]);
          return;
        case "account_type":
          onAccountTypeChange?.("");
          return;
        case "date":
          onDateRangeChange(DEFAULT_PRESET);
          return;
      }
    },
    [
      onServerSelectionChange,
      onUserEmailSelectionChange,
      onRoleSelectionChange,
      onSourceSelectionChange,
      onTypesChange,
      onStatusesChange,
      onDateRangeChange,
      onAccountTypeChange,
    ],
  );

  // react-router's `setSearchParams(fn)` closes over a memoized `searchParams`
  // that only updates after a navigation commits, so firing one call per filter
  // (as the per-field handlers do) makes every call build off the SAME baseline
  // and the last `navigate` clobbers the rest — clearing only the date. Clearing
  // every filter param in a single update is the only way to reset them at once.
  // Deleting the params resets to defaults: absent `range` → 7d, absent
  // `hookTypes` → DEFAULT_HOOK_TYPES (see useObserveFilters). The attribute
  // search (`q`/`af`) is owned by a separate hook + local state, so it's left
  // untouched here to avoid desyncing that control.
  const [, setSearchParams] = useSearchParams();
  const handleClearAll = useCallback(() => {
    // Clear every filter param in a single update. react-router's setSearchParams
    // reads a memoized snapshot, so a second call (e.g. onAccountTypeChange) would
    // clobber this one — account_type is deleted here instead.
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        for (const key of [
          "server",
          "user",
          "source",
          "role",
          "hookTypes",
          "status",
          "account_type",
          "range",
          "from",
          "to",
          "label",
        ]) {
          next.delete(key);
        }
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  // Compose the schema from the base filters plus whichever opt-in dimensions
  // the caller wired handlers for. defineFilters is an identity helper, so the
  // runtime array is all the toolbar needs (values/options are keyed by id).
  const schema = useMemo(() => {
    const dimensions: FilterDimension[] = [...OBSERVE_FILTER_BASE];
    if (onStatusesChange) dimensions.push(STATUS_DIMENSION);
    if (onAccountTypeChange) dimensions.push(ACCOUNT_TYPE_DIMENSION);
    return defineFilters(dimensions);
  }, [onStatusesChange, onAccountTypeChange]);

  // The arbitrary-attribute search/builder lives inside the sheet's "Custom
  // attributes" section (via FilterBar's customBuilder) rather than as a second
  // row under the bar — keeps the bar to one clean line and avoids the cramped
  // control sitting directly beneath the filters.
  return (
    <Page.Toolbar className={className}>
      <Page.Toolbar.Filters
        schema={schema}
        values={values}
        optionsById={optionsById}
        onChange={handleChange}
        onClear={handleClear}
        onClearAll={handleClearAll}
        projectSlug={projectSlug}
        customBuilder={attributeSearchControl}
      />
      {onRefresh && (
        <Page.Toolbar.Refresh
          onRefresh={onRefresh}
          isRefreshing={isRefreshing}
        />
      )}
    </Page.Toolbar>
  );
}
