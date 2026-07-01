import { type DateRangePreset } from "@gram-ai/elements";
import { type ReactNode, useCallback, useMemo } from "react";
import { useSearchParams } from "react-router";
import {
  defineFilters,
  type FilterValue,
  type OptionsById,
} from "@/components/filters";
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

// Server, User and the date range are the most-used dimensions, so they pin as
// always-visible pills. Role and Agent appear as pills once active; Type is
// sheet-only (it always carries a default value and would otherwise pill).
const OBSERVE_FILTERS = defineFilters([
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
]);

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
    ],
  );

  const optionsById = useMemo<OptionsById>(
    () => ({
      server: serverOptionsResolved,
      user: userEmailOptions.map((e) => ({ label: e, value: e })),
      role: roleOptions.map((r) => ({ label: r.name, value: r.id })),
      source: sourceOptionsWithIcons,
      type: typeOptions,
    }),
    [
      serverOptionsResolved,
      userEmailOptions,
      roleOptions,
      sourceOptionsWithIcons,
      typeOptions,
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
      onCustomRangeChange,
      onDateRangeChange,
      onClearCustomRange,
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
      onDateRangeChange,
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
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        for (const key of [
          "server",
          "user",
          "source",
          "role",
          "hookTypes",
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

  // The arbitrary-attribute search/builder lives inside the sheet's "Custom
  // attributes" section (via FilterBar's customBuilder) rather than as a second
  // row under the bar — keeps the bar to one clean line and avoids the cramped
  // control sitting directly beneath the filters.
  return (
    <Page.Toolbar className={className}>
      <Page.Toolbar.Filters
        schema={OBSERVE_FILTERS}
        values={values}
        optionsById={optionsById}
        onChange={handleChange}
        onClear={handleClear}
        onClearAll={handleClearAll}
        projectSlug={projectSlug}
        customBuilder={attributeSearchControl}
      />
    </Page.Toolbar>
  );
}
