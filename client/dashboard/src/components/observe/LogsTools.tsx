import { AccountTypeIcon } from "@/components/account-type-icon";
import { EnableLoggingOverlay } from "@/components/EnableLoggingOverlay";
import { EnterpriseGate } from "@/components/enterprise-gate";
import { InsightsConfig } from "@/components/insights-dock";
import { INSIGHTS_SUGGESTIONS } from "@/lib/insights-suggestions";
import { ObservabilitySkeleton } from "@/components/ObservabilitySkeleton";
import { Spinner } from "@/components/ui/spinner";
import { SimpleTooltip } from "@/components/ui/tooltip";
import {
  FilterChip,
  ObserveFilterBar,
  type ObserveStatusFilterValue,
  type ObserveTypeFilterValue,
} from "@/components/observe/ObserveFilterBar";
import {
  buildServerOptionGroups,
  parseTargetFilter,
  selectedHookSources,
  selectedTargetValues,
  selectedUserEmails,
  TOOL_USAGE_DEFAULT_TYPES,
  TOOL_USAGE_STATUS_OPTIONS,
  TOOL_USAGE_TYPE_OPTIONS,
  TOOL_USAGE_VALID_TYPES,
  toStatuses,
  toTargetTypes,
} from "@/components/observe/observeTargetFilters";
import { perPage } from "@/components/observe/observeFilterUtils";
import { formatToolName } from "@/components/observe/toolNameDisplay";
import { useObserveFilters } from "@/components/observe/useObserveFilters";
import { useSlugs } from "@/contexts/Sdk";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { useServerNameMappings } from "@/hooks/useServerNameMappings";
import { HooksEmptyState } from "@/pages/hooks/HooksEmptyState";
import { HookSourceIcon } from "@/pages/hooks/HookSourceIcon";
import { HooksSetupButton } from "@/pages/hooks/HooksSetupDialog";
import { EditServerNameDialog } from "@/pages/hooks/EditServerNameDialog";
import { LogDetailSheet } from "@/pages/logs/LogDetailSheet";
import { LogFilterBar } from "@/pages/logs/LogFilterBar";
import {
  applyFilterAdd,
  type ActiveLogFilter,
} from "@/pages/logs/log-filter-types";
import { parseFilters, serializeFilters } from "@/pages/logs/log-filter-url";
import { TraceLogsList } from "@/pages/logs/TraceLogsList";
import { cn } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import { type DateRangePreset } from "@gram-ai/elements";
import { telemetryGetToolUsageFilterOptions } from "@gram/client/funcs/telemetryGetToolUsageFilterOptions";
import { telemetryListToolUsageTraces } from "@gram/client/funcs/telemetryListToolUsageTraces";
import type { LogFilter } from "@gram/client/models/components/logfilter.js";
import type { TelemetryLogRecord } from "@gram/client/models/components/telemetrylogrecord.js";
import type { ToolUsageTraceSummary } from "@gram/client/models/components/toolusagetracesummary.js";
import { Operator } from "@gram/client/models/components/logfilter";
import type { ListToolUsageTracesPayloadTargetTypes } from "@gram/client/models/components/listtoolusagetracespayload";
import type { ToolUsageUserFilter } from "@gram/client/models/components/toolusageuserfilter";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useListAttributeKeys } from "@gram/client/react-query/listAttributeKeys.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Alert } from "@/components/ui/alert";
import { type BadgeProps } from "@/components/ui/badge";
import { ObservabilityLayout } from "@/components/layouts/observability-layout";
import { InlineEmptyState } from "@/components/ui/inline-empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, type Column } from "@/components/ui/table";
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  Inbox,
  Info,
  LoaderCircle,
  Pencil,
  Settings,
  ShieldAlert,
} from "lucide-react";
import { useCallback, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router";

type ToolUsageType = (typeof TOOL_USAGE_VALID_TYPES)[number];

function isToolUsageType(
  value: ObserveTypeFilterValue,
): value is ToolUsageType {
  return TOOL_USAGE_VALID_TYPES.includes(value);
}

function toSdkFilters(filters: ActiveLogFilter[]): LogFilter[] {
  return filters.map((filter) => {
    let values: string[] | undefined;
    if (filter.op === Operator.In) {
      values = filter.value
        ?.split(",")
        .map((value) => value.trim())
        .filter(Boolean);
    } else if (filter.value !== undefined) {
      values = [filter.value];
    }

    return {
      path: filter.path,
      operator: filter.op,
      ...(values !== undefined ? { values } : {}),
    };
  });
}

// Free-text search and arbitrary attribute filters can't be served by the
// pre-aggregated trace_summaries view, so they fall back to scanning raw logs.
// Surface that to the user when either is active; the structured filters
// (server, user, agent, type, date) stay on the fast summary path.
function isCustomSearchActive(
  attributeSearchQuery: string | null,
  attributeFilters: ActiveLogFilter[],
): boolean {
  return (attributeSearchQuery?.length ?? 0) > 0 || attributeFilters.length > 0;
}

function SlowSearchNotice() {
  return (
    <SimpleTooltip tooltip="Free-text search and custom attribute filters scan raw logs instead of the pre-aggregated summaries, so results may take longer to load. Server, user, agent, type, and date filters stay fast.">
      <Badge variant="warning">
        <Badge.LeftIcon>
          <Info className="size-3" />
        </Badge.LeftIcon>
        <Badge.Text>Custom search — may be slower</Badge.Text>
      </Badge>
    </SimpleTooltip>
  );
}

function useAttributeSearchParams() {
  const [searchParams, setSearchParams] = useSearchParams();
  const initialSearch = searchParams.get("q") ?? "";

  const [attributeSearchQuery, setAttributeSearchQuery] = useState(
    initialSearch || null,
  );
  const [attributeSearchInput, setAttributeSearchInput] =
    useState(initialSearch);
  const [attributeFilters, setAttributeFilters] = useState<ActiveLogFilter[]>(
    () => parseFilters(searchParams.get("af")),
  );

  const updateAttributeFilters = useCallback(
    (filters: ActiveLogFilter[]) => {
      setAttributeFilters(filters);
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
      setAttributeSearchQuery(trimmed || null);
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

export function LogsTools(): JSX.Element {
  const { projectSlug } = useSlugs();
  const queryClient = useQueryClient();
  const client = useGramContext();

  const mcpConfig = useObservabilityMcpConfig({
    toolsToInclude: ({ toolName }) =>
      toolName.includes("logs") || toolName.includes("hooks"),
  });

  const serverNameMappings = useServerNameMappings();

  const {
    from,
    to,
    selectedHookTypes,
    selectedStatuses,
    handleStatusesChange,
    activeFilters,
    handleServerSelectionChange,
    handleUserEmailSelectionChange,
    hookSourceOptions,
    handleHookSourceSelectionChange,
    handleHookTypesChange,
    dateRange,
    customRange,
    customRangeLabel,
    setDateRangeParam,
    setCustomRangeParam,
    clearCustomRange,
    selectedRoleIds,
    roleOptions,
    roleEmails,
    handleRoleSelectionChange,
    roleFilterPending,
  } = useObserveFilters<ToolUsageType>({
    defaultTypes: TOOL_USAGE_DEFAULT_TYPES,
    validTypes: TOOL_USAGE_VALID_TYPES,
  });

  const {
    attributeSearchInput,
    attributeSearchQuery,
    attributeFilters,
    setAttributeSearchInput,
    updateAttributeFilters,
    updateAttributeSearchQuery,
  } = useAttributeSearchParams();

  const selectedTargets = useMemo(
    () => selectedTargetValues(activeFilters).map(parseTargetFilter),
    [activeFilters],
  );

  const hostedToolsetSlugs = useMemo(
    () =>
      selectedTargets
        .filter((target) => target.type === "hosted")
        .map((target) => target.id),
    [selectedTargets],
  );

  const shadowServerNames = useMemo(
    () =>
      selectedTargets
        .filter((target) => target.type === "shadow")
        .map((target) => target.id),
    [selectedTargets],
  );

  const userFilters = useMemo<ToolUsageUserFilter[]>(() => {
    const emails = [
      ...new Set([...selectedUserEmails(activeFilters), ...roleEmails]),
    ];
    return emails.map((email) => ({ kind: "email", key: email }));
  }, [activeFilters, roleEmails]);

  const hookSources = useMemo(
    () => selectedHookSources(activeFilters),
    [activeFilters],
  );

  const targetTypes = useMemo(
    () =>
      toTargetTypes(selectedHookTypes) as
        | ListToolUsageTracesPayloadTargetTypes[]
        | undefined,
    [selectedHookTypes],
  );

  const statuses = useMemo(
    () => toStatuses(selectedStatuses),
    [selectedStatuses],
  );

  const { data: filterOptionsData } = useQuery({
    queryKey: [
      "tool-usage-filter-options",
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: () =>
      unwrapAsync(
        telemetryGetToolUsageFilterOptions(client, {
          getToolUsageFilterOptionsPayload: {
            from,
            to,
          },
        }),
      ),
    throwOnError: false,
  });

  const { data: attributeKeysData, isLoading: isLoadingAttributeKeys } =
    useListAttributeKeys(
      { getProjectMetricsSummaryPayload: { from, to } },
      undefined,
      { throwOnError: false },
    );

  const serverOptionGroups = useMemo(
    () =>
      buildServerOptionGroups({
        hostedServers: filterOptionsData?.hostedServers ?? [],
        shadowServers: filterOptionsData?.shadowServers ?? [],
        activeFilters,
        serverNameMappings,
      }),
    [
      activeFilters,
      filterOptionsData?.hostedServers,
      filterOptionsData?.shadowServers,
      serverNameMappings,
    ],
  );

  const toolUsageUserEmailOptions = useMemo(() => {
    const selected = selectedUserEmails(activeFilters);
    const known = (filterOptionsData?.users ?? [])
      .filter((user) => user.userKind === "email")
      .map((user) => user.userKey || user.userLabel)
      .filter(Boolean);
    return [...new Set([...known, ...selected])];
  }, [activeFilters, filterOptionsData?.users]);

  const [selectedLog, setSelectedLog] = useState<TelemetryLogRecord | null>(
    null,
  );
  const containerRef = useRef<HTMLDivElement>(null);

  const attributeSdkFilters = useMemo(
    () => toSdkFilters(attributeFilters),
    [attributeFilters],
  );

  // Account-type scope ("team" | "personal" | ""), persisted in the URL. It
  // filters on the materialized gram.account_type column via the raw-logs path.
  // "team" is expressed as "not personal" so unclassified rows count as team
  // (matching the badge semantics elsewhere).
  const [searchParams, setSearchParams] = useSearchParams();
  const accountType = ((): string => {
    const v = searchParams.get("account_type");
    return v === "team" || v === "personal" ? v : "";
  })();
  const setAccountType = useCallback(
    (value: string) => {
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
  // account_type is sent as a first-class payload filter (below), not an
  // attribute filter, so it stays on the fast trace_summaries path rather than
  // forcing the raw-logs scan.
  const queryFilters = attributeSdkFilters;

  const {
    data: tracesData,
    error,
    fetchNextPage,
    hasNextPage,
    isFetching,
    isFetchingNextPage,
    refetch: refetchLogs,
    isLogsDisabled: isLogsLogsDisabled,
  } = useLogsEnabledErrorCheck(
    useInfiniteQuery({
      queryKey: [
        "tool-usage-traces",
        from.toISOString(),
        to.toISOString(),
        hostedToolsetSlugs,
        shadowServerNames,
        targetTypes,
        statuses,
        userFilters,
        hookSources,
        attributeSearchQuery,
        queryFilters,
        accountType,
      ],
      queryFn: ({ pageParam }) =>
        unwrapAsync(
          telemetryListToolUsageTraces(client, {
            listToolUsageTracesPayload: {
              from,
              to,
              hostedToolsetSlugs:
                hostedToolsetSlugs.length > 0 ? hostedToolsetSlugs : undefined,
              shadowServerNames:
                shadowServerNames.length > 0 ? shadowServerNames : undefined,
              targetTypes,
              statuses,
              userFilters: userFilters.length > 0 ? userFilters : undefined,
              hookSources: hookSources.length > 0 ? hookSources : undefined,
              accountType: accountType || undefined,
              query: attributeSearchQuery ?? undefined,
              filters: queryFilters.length > 0 ? queryFilters : undefined,
              cursor: pageParam,
              limit: perPage,
              sort: "desc",
            },
          }),
        ),
      initialPageParam: undefined as string | undefined,
      getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
      enabled: !roleFilterPending,
      throwOnError: false,
    }),
  );

  const traces = useMemo(
    () => tracesData?.pages.flatMap((page) => page.traces) ?? [],
    [tracesData],
  );

  const handleScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const container = e.currentTarget;
    const distanceFromBottom =
      container.scrollHeight - (container.scrollTop + container.clientHeight);

    if (isFetchingNextPage || isFetching) return;
    if (!hasNextPage) return;

    if (distanceFromBottom < 200) {
      void fetchNextPage();
    }
  };

  const handleLogClick = useCallback((log: TelemetryLogRecord) => {
    setSelectedLog(log);
  }, []);

  const refetch = useCallback(() => {
    void refetchLogs();
    void queryClient.invalidateQueries({ queryKey: ["trace-logs"] });
  }, [queryClient, refetchLogs]);

  const handleAddFilterFromLog = useCallback(
    (path: string, op: Operator, value: string) => {
      updateAttributeFilters(
        applyFilterAdd(attributeFilters, { path, op, value }),
      );
    },
    [attributeFilters, updateAttributeFilters],
  );

  const isLogsDisabled = isLogsLogsDisabled;
  const isLoading = isFetching && traces.length === 0;
  const displayError = error
    ? new Error("Unable to load tool logs. Please try again.")
    : null;

  return (
    <>
      <InsightsConfig
        mcpConfig={mcpConfig}
        title="Explore Tool Logs"
        subtitle="Ask me about your tool logs! Powered by Elements + platform MCP"
        hideTrigger={isLogsDisabled}
        suggestions={INSIGHTS_SUGGESTIONS["logs/tools"]}
      />
      <ObservabilityLayout fullHeight>
        <ObservabilityLayout.Header
          title="Tool Logs"
          subtitle="Dive into tool traces across all tools, skills, and MCP servers used by organization members in this project"
          className="px-8 pt-4"
        />
        <ObservabilityLayout.Scroll
          ref={containerRef}
          onScroll={handleScroll}
          className="px-8 py-6"
        >
          {isLogsDisabled ? (
            <div className="relative flex-1">
              <div
                className="pointer-events-none h-full select-none"
                aria-hidden="true"
              >
                <ObservabilitySkeleton />
              </div>
              <EnableLoggingOverlay onEnabled={refetch} />
            </div>
          ) : (
            <EnterpriseGate
              icon="workflow"
              description="Tools are available on the Enterprise plan. Book a time to get started."
            >
              <LogsToolsContent
                isLoading={isLoading}
                isFetching={isFetching}
                onRefresh={refetch}
                error={displayError}
                traces={traces}
                serverOptionGroups={serverOptionGroups}
                onServerSelectionChange={handleServerSelectionChange}
                userEmailOptions={toolUsageUserEmailOptions}
                onUserEmailSelectionChange={handleUserEmailSelectionChange}
                sourceOptions={hookSourceOptions}
                onSourceSelectionChange={handleHookSourceSelectionChange}
                activeFilters={activeFilters}
                selectedTypes={selectedHookTypes}
                onTypesChange={(types) =>
                  handleHookTypesChange(types.filter(isToolUsageType))
                }
                selectedStatuses={selectedStatuses}
                onStatusesChange={handleStatusesChange}
                roleOptions={roleOptions}
                selectedRoleIds={selectedRoleIds}
                onRoleSelectionChange={handleRoleSelectionChange}
                selectedLog={selectedLog}
                handleLogClick={handleLogClick}
                setSelectedLog={setSelectedLog}
                hasNextPage={hasNextPage}
                isFetchingNextPage={isFetchingNextPage}
                dateRange={dateRange}
                customRange={customRange}
                customRangeLabel={customRangeLabel}
                onDateRangeChange={setDateRangeParam}
                onCustomRangeChange={setCustomRangeParam}
                onClearCustomRange={clearCustomRange}
                projectSlug={projectSlug}
                serverNameMappings={serverNameMappings}
                attributeSearchInput={attributeSearchInput}
                attributeSearchQuery={attributeSearchQuery}
                attributeFilters={attributeFilters}
                attributeKeys={attributeKeysData?.keys ?? []}
                isLoadingAttributeKeys={isLoadingAttributeKeys}
                onAttributeSearchInputChange={setAttributeSearchInput}
                onAttributeSearchSubmit={updateAttributeSearchQuery}
                onAttributeFiltersChange={updateAttributeFilters}
                onAddFilterFromLog={handleAddFilterFromLog}
                accountType={accountType}
                onAccountTypeChange={setAccountType}
                from={from}
                to={to}
              />
            </EnterpriseGate>
          )}
        </ObservabilityLayout.Scroll>
      </ObservabilityLayout>
    </>
  );
}

function LogsToolsContent({
  isLoading,
  isFetching,
  onRefresh,
  error,
  traces,
  serverOptionGroups,
  onServerSelectionChange,
  userEmailOptions,
  onUserEmailSelectionChange,
  sourceOptions,
  onSourceSelectionChange,
  activeFilters,
  selectedTypes,
  onTypesChange,
  selectedStatuses,
  onStatusesChange,
  roleOptions,
  selectedRoleIds,
  onRoleSelectionChange,
  selectedLog,
  handleLogClick,
  setSelectedLog,
  hasNextPage,
  isFetchingNextPage,
  dateRange,
  customRange,
  customRangeLabel,
  onDateRangeChange,
  onCustomRangeChange,
  onClearCustomRange,
  projectSlug,
  serverNameMappings,
  attributeSearchInput,
  attributeSearchQuery,
  attributeFilters,
  attributeKeys,
  isLoadingAttributeKeys,
  onAttributeSearchInputChange,
  onAttributeSearchSubmit,
  onAttributeFiltersChange,
  onAddFilterFromLog,
  accountType,
  onAccountTypeChange,
  from,
  to,
}: {
  isLoading: boolean;
  isFetching: boolean;
  onRefresh: () => void;
  error: Error | null;
  traces: ToolUsageTraceSummary[];
  serverOptionGroups: Parameters<
    typeof ObserveFilterBar
  >[0]["serverOptionGroups"];
  onServerSelectionChange: (values: string[]) => void;
  userEmailOptions: string[];
  onUserEmailSelectionChange: (values: string[]) => void;
  sourceOptions: string[];
  onSourceSelectionChange: (values: string[]) => void;
  activeFilters: FilterChip[];
  selectedTypes: ToolUsageType[];
  onTypesChange: (types: ObserveTypeFilterValue[]) => void;
  selectedStatuses: ObserveStatusFilterValue[];
  onStatusesChange: (statuses: ObserveStatusFilterValue[]) => void;
  roleOptions: Array<{ id: string; name: string }>;
  selectedRoleIds: string[];
  onRoleSelectionChange: (values: string[]) => void;
  selectedLog: TelemetryLogRecord | null;
  handleLogClick: (log: TelemetryLogRecord) => void;
  setSelectedLog: (log: TelemetryLogRecord | null) => void;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  dateRange: DateRangePreset;
  customRange: { from: Date; to: Date } | null;
  customRangeLabel: string | null;
  onDateRangeChange: (preset: DateRangePreset) => void;
  onCustomRangeChange: (from: Date, to: Date, label?: string) => void;
  onClearCustomRange: () => void;
  projectSlug?: string;
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  attributeSearchInput: string;
  attributeSearchQuery: string | null;
  attributeFilters: ActiveLogFilter[];
  attributeKeys: string[];
  isLoadingAttributeKeys: boolean;
  onAttributeSearchInputChange: (value: string) => void;
  onAttributeSearchSubmit: (query: string) => void;
  onAttributeFiltersChange: (filters: ActiveLogFilter[]) => void;
  onAddFilterFromLog: (path: string, op: Operator, value: string) => void;
  accountType: string;
  onAccountTypeChange: (value: string) => void;
  from: Date;
  to: Date;
}) {
  const orgRoutes = useOrgRoutes();

  return (
    <>
      <div className="flex flex-col gap-6">
        <div className="flex shrink-0 items-center justify-end gap-2">
          <HooksSetupButton />
          <Button variant="secondary" size="sm" asChild>
            <Link to={orgRoutes.logs.href()}>
              <Settings className="h-4 w-4" />
              Configure settings
            </Link>
          </Button>
        </div>

        <ObserveFilterBar
          serverOptions={[]}
          serverOptionGroups={serverOptionGroups}
          onServerSelectionChange={onServerSelectionChange}
          userEmailOptions={userEmailOptions}
          onUserEmailSelectionChange={onUserEmailSelectionChange}
          sourceOptions={sourceOptions}
          onSourceSelectionChange={onSourceSelectionChange}
          activeFilters={activeFilters}
          selectedTypes={selectedTypes}
          onTypesChange={onTypesChange}
          typeOptions={TOOL_USAGE_TYPE_OPTIONS}
          selectedStatuses={selectedStatuses}
          onStatusesChange={onStatusesChange}
          statusOptions={TOOL_USAGE_STATUS_OPTIONS}
          roleOptions={roleOptions}
          selectedRoleIds={selectedRoleIds}
          onRoleSelectionChange={onRoleSelectionChange}
          dateRange={dateRange}
          customRange={customRange}
          customRangeLabel={customRangeLabel}
          onDateRangeChange={onDateRangeChange}
          onCustomRangeChange={onCustomRangeChange}
          onClearCustomRange={onClearCustomRange}
          projectSlug={projectSlug}
          serverNameMappings={serverNameMappings}
          accountType={accountType}
          onAccountTypeChange={onAccountTypeChange}
          attributeSearchControl={
            <div className="min-w-[260px] flex-[1.2]">
              <LogFilterBar
                filters={attributeFilters}
                onChange={onAttributeFiltersChange}
                attributeKeys={attributeKeys}
                isLoadingKeys={isLoadingAttributeKeys}
                searchInput={attributeSearchInput}
                onSearchInputChange={onAttributeSearchInputChange}
                onSearchSubmit={onAttributeSearchSubmit}
              />
            </div>
          }
          onRefresh={onRefresh}
          isRefreshing={isFetching}
        />

        {isCustomSearchActive(attributeSearchQuery, attributeFilters) && (
          <div className="flex shrink-0">
            <SlowSearchNotice />
          </div>
        )}

        <div className="bg-card relative flex flex-col border">
          {isFetching && traces.length > 0 && (
            <Skeleton className="bg-primary/60 absolute top-0 right-0 left-0 z-20 h-1 w-full" />
          )}

          <LogsToolsTableContent
            error={error}
            isLoading={isLoading}
            traces={traces}
            hasActiveFilters={
              activeFilters.length > 0 ||
              selectedTypes.length > 0 ||
              selectedStatuses.length > 0 ||
              selectedRoleIds.length > 0 ||
              attributeFilters.length > 0 ||
              accountType !== "" ||
              Boolean(attributeSearchQuery)
            }
            isFetchingNextPage={isFetchingNextPage}
            onLogClick={handleLogClick}
            serverNameMappings={serverNameMappings}
            from={from}
            to={to}
          />

          {traces.length > 0 && (
            <div className="bg-card text-muted-foreground flex shrink-0 items-center gap-4 border-t px-5 py-3 text-sm">
              <span>
                {traces.length} {traces.length === 1 ? "trace" : "traces"}
                {hasNextPage && " • Scroll to load more"}
              </span>
            </div>
          )}
        </div>
      </div>

      <LogDetailSheet
        log={selectedLog}
        open={!!selectedLog}
        onOpenChange={(open) => {
          void (!open && setSelectedLog(null));
        }}
        onAddFilter={onAddFilterFromLog}
      />
    </>
  );
}

function traceTimeAgo(startTimeUnixNano: string): string {
  const timestamp = new Date(Number(BigInt(startTimeUnixNano) / 1_000_000n));
  const diff = Date.now() - timestamp.getTime();
  const seconds = Math.max(0, Math.floor(diff / 1000));
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);
  return days > 0
    ? `${days}d ago`
    : hours > 0
      ? `${hours}h ago`
      : minutes > 0
        ? `${minutes}m ago`
        : `${seconds}s ago`;
}

function LogsToolsTableContent({
  error,
  isLoading,
  traces,
  hasActiveFilters,
  isFetchingNextPage,
  onLogClick,
  serverNameMappings,
  from,
  to,
}: {
  error: Error | null;
  isLoading: boolean;
  traces: ToolUsageTraceSummary[];
  hasActiveFilters: boolean;
  isFetchingNextPage: boolean;
  onLogClick: (log: TelemetryLogRecord) => void;
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  from: Date;
  to: Date;
}) {
  const columns = useMemo<Column<ToolUsageTraceSummary>[]>(
    () => [
      {
        key: "timestamp",
        header: "Timestamp",
        width: "150px",
        render: (trace) => (
          <span className="text-muted-foreground min-w-0 truncate font-mono text-xs">
            {traceTimeAgo(trace.startTimeUnixNano)}
          </span>
        ),
      },
      {
        key: "source",
        header: "Source / Tool",
        width: "2fr",
        render: (trace) => (
          <SourceToolCell
            trace={trace}
            serverNameMappings={serverNameMappings}
          />
        ),
      },
      {
        key: "user",
        header: "User",
        width: "1fr",
        render: (trace) => (
          <div className="flex min-w-0 items-center gap-2 text-xs">
            <AccountTypeIcon
              accountType={trace.accountType}
              className="shrink-0"
            />
            <span className="text-muted-foreground min-w-0 truncate">
              {trace.userLabel || "—"}
            </span>
          </div>
        ),
      },
      {
        key: "agent",
        header: "Agent",
        width: "150px",
        render: (trace) =>
          trace.hookSource ? (
            <div className="flex min-w-0 items-center gap-2">
              <HookSourceIcon
                source={trace.hookSource}
                className="size-4 shrink-0"
              />
              <span className="text-foreground truncate text-xs font-medium">
                {trace.hookSource}
              </span>
            </div>
          ) : (
            <span className="text-muted-foreground truncate text-xs">
              Direct
            </span>
          ),
      },
      {
        key: "status",
        header: "Status",
        width: "110px",
        render: (trace) => {
          const statusConfig = getStatusConfig(trace);
          return statusConfig ? (
            <Badge variant={statusConfig.variant}>
              <Badge.Text>{statusConfig.label}</Badge.Text>
            </Badge>
          ) : null;
        },
      },
    ],
    [serverNameMappings],
  );

  const renderExpandedContent = useCallback(
    (trace: ToolUsageTraceSummary) => (
      <>
        {trace.hookStatus === "blocked" && (
          <div className="border-warning/30 bg-warning/10 flex items-start gap-3 border-y px-5 py-3 text-xs">
            <ShieldAlert className="text-warning mt-0.5 size-4 shrink-0" />
            <div className="flex min-w-0 flex-1 flex-col gap-0.5">
              <div className="text-warning font-semibold tracking-wide uppercase">
                Blocked
              </div>
              <div className="text-foreground wrap-break-words">
                {trace.blockReason || "No reason provided"}
              </div>
            </div>
          </div>
        )}
        <TraceLogsList
          logGroup={trace.logGroup}
          toolName={trace.toolName}
          isExpanded
          onLogClick={onLogClick}
          parentTimestamp={trace.startTimeUnixNano}
          from={from}
          to={to}
        />
      </>
    ),
    [onLogClick, from, to],
  );

  if (error) {
    return (
      <Alert variant="error" dismissible={false} className="m-4">
        <span className="font-medium">Error loading tool logs</span>
        <div>{error.message}</div>
      </Alert>
    );
  }

  if (isLoading) {
    return (
      <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
        <Spinner className="mr-0 size-5" />
        <span>Loading tool logs...</span>
      </div>
    );
  }

  if (traces.length === 0) {
    if (!hasActiveFilters) {
      return <HooksEmptyState />;
    }

    return (
      <InlineEmptyState
        className="py-12"
        icon={<Inbox />}
        title="No matching tool logs"
        description="Try adjusting your filters or time range"
      />
    );
  }

  return (
    <>
      <Table
        columns={columns}
        data={traces}
        rowKey={(trace) => trace.id}
        renderExpandedContent={renderExpandedContent}
        lazyExpandedContent
        className="min-w-[900px] rounded-none border-none"
      />

      {isFetchingNextPage && (
        <div className="text-muted-foreground flex items-center justify-center gap-2 border-t py-4">
          <LoaderCircle className="size-4 animate-spin" />
          <span className="text-sm">Loading more logs...</span>
        </div>
      )}
    </>
  );
}

// The Source / Tool cell: a target-type tag, the (optional) server label and
// tool name, plus the hover-only pencil that opens the shadow-MCP rename dialog.
// Holds the dialog's open state per row (each cell owns its own dialog).
function SourceToolCell({
  trace,
  serverNameMappings,
}: {
  trace: ToolUsageTraceSummary;
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
}) {
  const [editDialogOpen, setEditDialogOpen] = useState(false);

  const targetLabel =
    trace.targetType === "shadow_mcp_server"
      ? (serverNameMappings.rawToDisplay.get(trace.targetId) ??
        trace.targetLabel)
      : trace.targetLabel;
  const showTargetLabel =
    trace.targetType !== "local_tool" && trace.targetType !== "skill";

  const editDialogProps = useMemo(() => {
    if (trace.targetType !== "shadow_mcp_server") return null;
    const overrides =
      serverNameMappings.displayToOverrides.get(targetLabel) ?? [];
    const hasOverride = overrides.some(
      (o) => o.rawServerName === trace.targetId,
    );
    return {
      serverName: targetLabel,
      groupedOverrides: overrides,
      unmappedRawName: hasOverride ? null : trace.targetId,
    };
  }, [
    serverNameMappings.displayToOverrides,
    targetLabel,
    trace.targetId,
    trace.targetType,
  ]);

  const targetConfig = getTargetConfig(trace.targetType);

  return (
    <div className="flex min-w-0 items-center gap-2">
      <div className="group/server relative flex shrink-0 items-center">
        <span
          className={cn(
            "shrink-0 truncate px-2 py-1 font-mono text-xs",
            targetConfig.className,
          )}
        >
          {targetConfig.label}
        </span>
        {editDialogProps && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              setEditDialogOpen(true);
            }}
            className="text-muted-foreground hover:text-foreground bg-card hover:bg-muted border-border invisible absolute -right-6 size-6 border p-1 transition-colors group-hover/server:visible"
            aria-label="Edit display name"
          >
            <Pencil className="size-3" />
          </button>
        )}
      </div>
      <div className="flex min-w-0 items-baseline gap-2">
        {showTargetLabel && (
          <span className="text-muted-foreground min-w-0 truncate font-mono text-xs">
            {targetLabel}
            {" /"}
          </span>
        )}
        <span className="truncate font-mono text-xs">
          {formatToolName(trace.toolName)}
        </span>
      </div>

      {editDialogProps && (
        <EditServerNameDialog
          open={editDialogOpen}
          onOpenChange={setEditDialogOpen}
          serverName={editDialogProps.serverName}
          groupedOverrides={editDialogProps.groupedOverrides}
          unmappedRawName={editDialogProps.unmappedRawName}
          upsert={serverNameMappings.upsert}
          remove={serverNameMappings.remove}
          isUpserting={serverNameMappings.isUpserting}
          isDeleting={serverNameMappings.isDeleting}
        />
      )}
    </div>
  );
}

function getTargetConfig(targetType: ToolUsageTraceSummary["targetType"]) {
  switch (targetType) {
    case "hosted_mcp_server":
      return {
        label: "Hosted MCP",
        className: "bg-primary/15 text-primary",
      };
    case "tunneled_mcp_server":
      return {
        label: "Tunneled MCP",
        className: "bg-primary/15 text-primary",
      };
    case "shadow_mcp_server":
      return {
        label: "Shadow MCP",
        className: "bg-warning/15 text-warning",
      };
    case "skill":
      return {
        label: "Skill",
        className: "bg-accent text-accent-foreground",
      };
    case "local_tool":
    default:
      return {
        label: "Local Tools",
        className: "bg-muted/50 text-primary",
      };
  }
}

function getStatusConfig(trace: ToolUsageTraceSummary): {
  variant: NonNullable<BadgeProps["variant"]>;
  label: string;
} | null {
  if (trace.hookStatus) {
    switch (trace.hookStatus) {
      case "blocked":
        return {
          variant: "warning",
          label: "Blocked",
        };
      case "failure":
        return {
          variant: "destructive",
          label: "Error",
        };
      case "success":
        return {
          variant: "success",
          label: "Success",
        };
      case "pending":
        return {
          variant: "neutral",
          label: "Pending",
        };
      default:
        return null;
    }
  }

  if (trace.httpStatusCode !== undefined) {
    if (trace.httpStatusCode >= 400) {
      return {
        variant: "destructive",
        label: "Error",
      };
    }
    if (trace.httpStatusCode >= 200 && trace.httpStatusCode < 400) {
      return {
        variant: "success",
        label: "Success",
      };
    }
  }

  return null;
}
