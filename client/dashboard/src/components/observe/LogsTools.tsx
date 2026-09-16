import { traceLogsQueryOptions } from "@/pages/logs/traceLogsQuery";
import { IdentityLink } from "@/components/identity-link";
import { identityRefForKind } from "@/lib/identity-urn";
import { AccountTypeIcon } from "@/components/account-type-icon";
import { EnableLoggingOverlay } from "@/components/EnableLoggingOverlay";
import { EnterpriseGate } from "@/components/enterprise-gate";
import { InsightsConfig } from "@/components/insights-dock";
import { INSIGHTS_SUGGESTIONS } from "@/lib/insights-suggestions";
import { LoggingPageHeader } from "@/components/observe/LoggingPageHeader";
import { ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import {
  FilterChip,
  type ObserveStatusFilterValue,
  type ObserveTypeFilterValue,
} from "@/components/observe/ObserveFilterBar";
import {
  encodeGatewayServerFilter,
  encodeHostedServerFilter,
  encodeShadowServerFilter,
  isDefaultToolUsageTypeSelection,
  selectedHookSources,
  selectedTargetValues,
  selectedUserEmails,
  TOOL_USAGE_DEFAULT_TYPES,
  TOOL_USAGE_STATUS_OPTIONS,
  TOOL_USAGE_TYPE_OPTIONS,
  TOOL_USAGE_VALID_TYPES,
  toStatuses,
} from "@/components/observe/observeTargetFilters";
import { perPage } from "@/components/observe/observeFilterUtils";
import { formatToolName } from "@/components/observe/toolNameDisplay";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { useObserveFilters } from "@/components/observe/useObserveFilters";
import { useAttributeSearchParams } from "@/pages/logs/useAttributeSearchParams";
import {
  LogsFacetRail,
  type FacetGroup,
  type FacetValue,
} from "@/components/observe/LogsFacetRail";
import { LogsTimelineStrip } from "@/components/observe/LogsTimelineStrip";
import { useToolUsagePayload } from "@/components/observe/toolUsagePayload";
import { useSlugs } from "@/contexts/Sdk";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { useServerNameMappings } from "@/hooks/useServerNameMappings";
import { HooksEmptyState } from "@/pages/hooks/HooksEmptyState";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { EditServerNameDialog } from "@/pages/hooks/EditServerNameDialog";
import { LogDetailSheet } from "@/pages/logs/LogDetailSheet";
import { LogFilterBar } from "@/pages/logs/LogFilterBar";
import {
  applyFilterAdd,
  type ActiveLogFilter,
} from "@/pages/logs/log-filter-types";
import { TraceLogsList } from "@/pages/logs/TraceLogsList";
import { formatPlatform } from "@/lib/formatPlatform";
import { cn } from "@/lib/utils";
import { useOrgRoutes, useRoutes } from "@/routes";
import { type DateRangePreset } from "@/elements";
import { telemetryGetToolUsageFilterOptions } from "@gram/client/funcs/telemetryGetToolUsageFilterOptions";
import { telemetryGetToolUsageTargetTimeSeries } from "@gram/client/funcs/telemetryGetToolUsageTargetTimeSeries";
import { telemetryGetToolUsageTotals } from "@gram/client/funcs/telemetryGetToolUsageTotals";
import { telemetryListToolUsageTraces } from "@gram/client/funcs/telemetryListToolUsageTraces";
import type { LogFilter } from "@gram/client/models/components/logfilter.js";
import type { TelemetryLogRecord } from "@gram/client/models/components/telemetrylogrecord.js";
import type { ToolUsageTargetTimeSeriesPoint } from "@gram/client/models/components/toolusagetargettimeseriespoint.js";
import type { ToolUsageTotals } from "@gram/client/models/components/toolusagetotals.js";
import type { ToolUsageTraceSummary } from "@gram/client/models/components/toolusagetracesummary.js";
import { Operator } from "@gram/client/models/components/logfilter";
import type { ListToolUsageTracesPayloadTargetTypes } from "@gram/client/models/components/listtoolusagetracespayload";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useListAttributeKeys } from "@gram/client/react-query/listAttributeKeys.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { Icon } from "@/components/ui/Icon";
import { Skeleton } from "@/components/ui/Skeleton";
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { format } from "date-fns";
import { Settings } from "lucide-react";
import { DateGroupHeader } from "@/components/auditlogs/feed";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router";

type ToolUsageType = (typeof TOOL_USAGE_VALID_TYPES)[number];

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

function isCustomSearchActive(
  attributeSearchQuery: string | null,
  attributeFilters: ActiveLogFilter[],
): boolean {
  return (attributeSearchQuery?.length ?? 0) > 0 || attributeFilters.length > 0;
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
    setRangeFromBrush,
    clearCustomRange,
    selectedRoleIds,
    roleEmails,
    roleFilterPending,
    accountType,
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

  const [searchParams, setSearchParams] = useSearchParams();
  const selectedClientKeys = useMemo(
    () => (searchParams.get("client") ?? "").split(",").filter(Boolean),
    [searchParams],
  );
  const setClientKeys = useCallback(
    (values: string[]) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (values.length > 0) {
            next.set("client", values.join(","));
          } else {
            next.delete("client");
          }
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const { summaryPayload, sharedQueryKey } = useToolUsagePayload({
    activeFilters,
    roleEmails,
    selectedHookTypes,
    accountType,
    clientKeys: selectedClientKeys,
    from,
    to,
  });

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

  // Feeds the strip. Same key Insights uses, so crossing over from a deep link
  // paints it from cache rather than refetching the window.
  const { data: timeSeriesData, isPending: timeSeriesPending } = useQuery({
    queryKey: ["tool-usage-target-time-series", ...sharedQueryKey],
    queryFn: () =>
      unwrapAsync(
        telemetryGetToolUsageTargetTimeSeries(client, {
          getToolUsageSummaryPayload: summaryPayload,
        }),
      ),
    enabled: !roleFilterPending,
    throwOnError: false,
  });

  // The rail is a second view over the same params the toolbar writes: every
  // toggle below routes into the same handlers, so the two cannot disagree.
  //
  // Counts come from the filter-options endpoint, which is keyed on the window
  // alone and is deliberately blind to the other applied filters — a
  // self-narrowing list dead-ends the moment a reader picks a value that
  // excludes everything else. Groups whose source has no counts show none
  // rather than inventing them.
  const selectedServerValues = useMemo(
    () => selectedTargetValues(activeFilters),
    [activeFilters],
  );
  const selectedEmails = useMemo(
    () => selectedUserEmails(activeFilters),
    [activeFilters],
  );
  const selectedSources = useMemo(
    () => selectedHookSources(activeFilters),
    [activeFilters],
  );

  const facetGroups = useMemo<FacetGroup[]>(() => {
    const serverValues: FacetValue[] = [
      ...(filterOptionsData?.hostedServers ?? []).map((server) => ({
        value: encodeHostedServerFilter(server.toolsetSlug),
        label: server.toolsetName || server.toolsetSlug,
        count: Number(server.eventCount),
        selected: selectedServerValues.includes(
          encodeHostedServerFilter(server.toolsetSlug),
        ),
      })),
      ...(filterOptionsData?.gateways ?? []).map((gateway) => ({
        value: encodeGatewayServerFilter(gateway.metaMcpServerId),
        label: gateway.name,
        count: Number(gateway.eventCount),
        selected: selectedServerValues.includes(
          encodeGatewayServerFilter(gateway.metaMcpServerId),
        ),
      })),
      ...(filterOptionsData?.shadowServers ?? []).map((server) => ({
        value: encodeShadowServerFilter(server.serverName),
        label:
          serverNameMappings.rawToDisplay.get(server.serverName) ??
          server.serverName,
        count: Number(server.eventCount),
        selected: selectedServerValues.includes(
          encodeShadowServerFilter(server.serverName),
        ),
      })),
    ].sort((a, b) => (b.count ?? 0) - (a.count ?? 0));

    return [
      { id: "server", label: "MCP server", values: serverValues },
      {
        id: "client",
        label: "Client",
        values: (filterOptionsData?.clients ?? []).map((client) => ({
          value: client.clientKey,
          label: client.clientLabel,
          count: Number(client.eventCount),
          selected: selectedClientKeys.includes(client.clientKey),
        })),
      },
      {
        id: "status",
        label: "Status",
        values: TOOL_USAGE_STATUS_OPTIONS.map((option) => ({
          value: option.value,
          label: option.label,
          selected: selectedStatuses.includes(option.value),
        })),
      },
      {
        id: "type",
        label: "Type",
        values: TOOL_USAGE_TYPE_OPTIONS.map((option) => ({
          value: option.value,
          label: option.label,
          selected: selectedHookTypes.includes(option.value as ToolUsageType),
        })),
      },
      {
        id: "source",
        label: "Agent",
        values: hookSourceOptions.map((source) => ({
          value: source,
          label: formatPlatform(source),
          selected: selectedSources.includes(source),
        })),
      },
      {
        id: "user",
        label: "User",
        values: (filterOptionsData?.users ?? []).map((user) => ({
          value: user.userKey,
          label: user.userLabel,
          count: Number(user.eventCount),
          selected: selectedEmails.includes(user.userKey),
        })),
      },
    ];
  }, [
    filterOptionsData,
    hookSourceOptions,
    selectedClientKeys,
    selectedEmails,
    selectedHookTypes,
    selectedServerValues,
    selectedSources,
    selectedStatuses,
    serverNameMappings,
  ]);

  const onFacetToggle = useCallback(
    (groupId: string, value: string, nextSelected: boolean) => {
      const next = (current: string[]) =>
        nextSelected
          ? [...new Set([...current, value])]
          : current.filter((entry) => entry !== value);

      switch (groupId) {
        case "server":
          handleServerSelectionChange(next(selectedServerValues));
          break;
        case "client":
          setClientKeys(next(selectedClientKeys));
          break;
        case "status":
          handleStatusesChange(
            next(selectedStatuses) as ObserveStatusFilterValue[],
          );
          break;
        case "type":
          handleHookTypesChange(
            next(selectedHookTypes) as ObserveTypeFilterValue[],
          );
          break;
        case "source":
          handleHookSourceSelectionChange(next(selectedSources));
          break;
        case "user":
          handleUserEmailSelectionChange(next(selectedEmails));
          break;
      }
    },
    [
      handleHookSourceSelectionChange,
      handleHookTypesChange,
      handleServerSelectionChange,
      handleStatusesChange,
      handleUserEmailSelectionChange,
      selectedClientKeys,
      selectedEmails,
      selectedHookTypes,
      selectedServerValues,
      selectedSources,
      selectedStatuses,
      setClientKeys,
    ],
  );

  const onFacetClearGroup = useCallback(
    (groupId: string) => {
      switch (groupId) {
        case "server":
          handleServerSelectionChange([]);
          break;
        case "client":
          setClientKeys([]);
          break;
        case "status":
          handleStatusesChange([]);
          break;
        case "type":
          handleHookTypesChange(TOOL_USAGE_DEFAULT_TYPES);
          break;
        case "source":
          handleHookSourceSelectionChange([]);
          break;
        case "user":
          handleUserEmailSelectionChange([]);
          break;
      }
    },
    [
      handleHookSourceSelectionChange,
      handleHookTypesChange,
      handleServerSelectionChange,
      handleStatusesChange,
      handleUserEmailSelectionChange,
      setClientKeys,
    ],
  );

  const { data: attributeKeysData, isLoading: isLoadingAttributeKeys } =
    useListAttributeKeys(
      { getProjectMetricsSummaryPayload: { from, to } },
      undefined,
      { throwOnError: false },
    );

  const [expandedTraceId, setExpandedTraceId] = useState<string | null>(null);
  const [selectedLog, setSelectedLog] = useState<TelemetryLogRecord | null>(
    null,
  );
  const containerRef = useRef<HTMLDivElement>(null);

  const attributeSdkFilters = useMemo(
    () => toSdkFilters(attributeFilters),
    [attributeFilters],
  );

  // account_type is sent as a first-class payload filter (below), not an
  // attribute filter, so it stays on the fast trace_summaries path rather than
  // forcing the raw-logs scan.
  const queryFilters = attributeSdkFilters;

  // The window's real totals, not a count of what has been scrolled into view.
  // Same query key Insights uses, so arriving from a deep link paints these
  // immediately from its cache.
  const { data: totalsData } = useQuery({
    queryKey: ["tool-usage-totals", ...sharedQueryKey],
    queryFn: () =>
      unwrapAsync(
        telemetryGetToolUsageTotals(client, {
          getToolUsageSummaryPayload: summaryPayload,
        }),
      ),
    enabled: !roleFilterPending,
    throwOnError: false,
  });

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
        ...sharedQueryKey,
        statuses,
        attributeSearchQuery,
        queryFilters,
      ],
      queryFn: ({ pageParam }) =>
        unwrapAsync(
          telemetryListToolUsageTraces(client, {
            listToolUsageTracesPayload: {
              ...summaryPayload,
              targetTypes: summaryPayload.targetTypes as
                | ListToolUsageTracesPayloadTargetTypes[]
                | undefined,
              statuses,
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

  // The day the pinned header shows: the last group whose header has scrolled
  // up past the top of the list.
  const [stickyDayTs, setStickyDayTs] = useState<number | null>(null);

  const syncStickyDay = useCallback((container: HTMLElement) => {
    const groups = container.querySelectorAll<HTMLElement>("[data-day-ts]");
    let current: number | null = null;
    for (const group of groups) {
      const ts = Number(group.dataset.dayTs);
      if (!Number.isFinite(ts)) continue;
      if (current === null || group.offsetTop <= container.scrollTop + 1) {
        current = ts;
      } else {
        break;
      }
    }
    setStickyDayTs(current);
  }, []);

  // The list changes height without a scroll event — a first page arrives, a
  // filter narrows it, a row expands under the pointer — and every one of
  // those moves a different day under the top of the list. Observing the
  // container catches all of them, including the expansion whose content
  // lands asynchronously.
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    syncStickyDay(container);
    const observer = new ResizeObserver(() => syncStickyDay(container));
    for (const child of container.children) observer.observe(child);
    return () => observer.disconnect();
  }, [traces, syncStickyDay]);

  const stickyDayDate = useMemo(
    () => (stickyDayTs === null ? null : new Date(stickyDayTs)),
    [stickyDayTs],
  );

  const handleScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const container = e.currentTarget;
    syncStickyDay(container);
    const distanceFromBottom =
      container.scrollHeight - (container.scrollTop + container.clientHeight);

    if (isFetchingNextPage || isFetching) return;
    if (!hasNextPage) return;

    if (distanceFromBottom < 200) {
      void fetchNextPage();
    }
  };

  const [selectedHostedToolsetSlug, setSelectedHostedToolsetSlug] =
    useState<string>();
  const handleLogClick = useCallback(
    (log: TelemetryLogRecord, trace?: ToolUsageTraceSummary) => {
      setSelectedLog(log);
      setSelectedHostedToolsetSlug(
        trace?.targetType === "hosted_mcp_server" ? trace.targetId : undefined,
      );
    },
    [],
  );

  const [openingTraceId, setOpeningTraceId] = useState<string | null>(null);
  const traceOpenRequest = useRef(0);
  const toggleExpand = useCallback(
    async (trace: ToolUsageTraceSummary) => {
      const request = ++traceOpenRequest.current;
      setOpeningTraceId(null);
      if (expandedTraceId === trace.id) {
        setExpandedTraceId(null);
        return;
      }
      const options = traceLogsQueryOptions(client, trace.logGroup, from, to);
      if (trace.logCount === 1 && options.enabled) {
        setOpeningTraceId(trace.id);
        try {
          const result = await queryClient.fetchQuery(options);
          if (request !== traceOpenRequest.current) return;
          const log = result.logs[0];
          if (log && result.logs.length === 1 && !result.nextCursor) {
            setExpandedTraceId(null);
            handleLogClick(log, trace);
            return;
          }
        } catch {
          // Expand to expose the normal span-loading error and retry behavior.
        } finally {
          if (request === traceOpenRequest.current) setOpeningTraceId(null);
        }
      }
      if (request === traceOpenRequest.current) setExpandedTraceId(trace.id);
    },
    [client, expandedTraceId, from, handleLogClick, queryClient, to],
  );

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
      {isLogsDisabled ? (
        <div className="min-h-0 w-full flex-1 space-y-6 overflow-y-auto p-8 pb-24">
          <LoggingPageHeader
            title="Tool Logs"
            description="Dive into tool traces across all tools, skills, and MCP servers used by organization members in this project"
          />
          <div className="flex-1">
            <EnableLoggingOverlay
              onEnabled={refetch}
              screenshotSrc="/empty-states/tool_logs_empty.png"
              screenshotAlt="Tool Logs dashboard with captured tool calls"
            />
          </div>
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
            totals={totalsData?.totals}
            timeSeries={timeSeriesData?.targetTimeSeries ?? []}
            timeSeriesPending={timeSeriesPending}
            facetGroups={facetGroups}
            onFacetToggle={onFacetToggle}
            onFacetClearGroup={onFacetClearGroup}
            onRangeSelect={setRangeFromBrush}
            onResetRange={clearCustomRange}
            isZoomed={customRange !== null}
            activeFilters={activeFilters}
            selectedTypes={selectedHookTypes}
            selectedStatuses={selectedStatuses}
            selectedRoleIds={selectedRoleIds}
            expandedTraceId={expandedTraceId}
            openingTraceId={openingTraceId}
            toggleExpand={toggleExpand}
            selectedLog={selectedLog}
            selectedHostedToolsetSlug={selectedHostedToolsetSlug}
            handleLogClick={handleLogClick}
            setSelectedLog={setSelectedLog}
            containerRef={containerRef}
            stickyDayDate={stickyDayDate}
            handleScroll={handleScroll}
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
            from={from}
            to={to}
          />
        </EnterpriseGate>
      )}
    </>
  );
}

/** One cell of the summary row: quiet label, the number doing the talking. */
function SummaryStat({
  label,
  value,
  tone = "default",
}: {
  label: string;
  value: string;
  tone?: "default" | "destructive";
}) {
  return (
    <div className="flex flex-col gap-1 px-4 py-3">
      <span className="text-eyebrow">{label}</span>
      <span
        className={cn(
          "font-mono text-2xl leading-none tabular-nums",
          tone === "destructive" ? "text-destructive" : "text-foreground",
        )}
      >
        {value}
      </span>
    </div>
  );
}

function LogsToolsContent({
  isLoading,
  isFetching,
  onRefresh,
  error,
  traces,
  activeFilters,
  selectedTypes,
  selectedStatuses,
  selectedRoleIds,
  expandedTraceId,
  openingTraceId,
  toggleExpand,
  selectedLog,
  selectedHostedToolsetSlug,
  handleLogClick,
  setSelectedLog,
  containerRef,
  stickyDayDate,
  handleScroll,
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
  from,
  to,
  totals,
  timeSeries,
  timeSeriesPending,
  facetGroups,
  onFacetToggle,
  onFacetClearGroup,
  onRangeSelect,
  onResetRange,
  isZoomed,
}: {
  isLoading: boolean;
  isFetching: boolean;
  onRefresh: () => void;
  error: Error | null;
  traces: ToolUsageTraceSummary[];
  activeFilters: FilterChip[];
  selectedTypes: ToolUsageType[];
  selectedStatuses: ObserveStatusFilterValue[];
  selectedRoleIds: string[];
  expandedTraceId: string | null;
  openingTraceId: string | null;
  toggleExpand: (trace: ToolUsageTraceSummary) => Promise<void>;
  selectedLog: TelemetryLogRecord | null;
  selectedHostedToolsetSlug?: string;
  handleLogClick: (
    log: TelemetryLogRecord,
    trace?: ToolUsageTraceSummary,
  ) => void;
  setSelectedLog: (log: TelemetryLogRecord | null) => void;
  containerRef: React.RefObject<HTMLDivElement | null>;
  stickyDayDate: Date | null;
  handleScroll: (e: React.UIEvent<HTMLDivElement>) => void;
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
  from: Date;
  to: Date;
  totals: ToolUsageTotals | undefined;
  timeSeries: ToolUsageTargetTimeSeriesPoint[];
  timeSeriesPending: boolean;
  facetGroups: FacetGroup[];
  onFacetToggle: (groupId: string, value: string, next: boolean) => void;
  onFacetClearGroup: (groupId: string) => void;
  onRangeSelect: (from: Date, to: Date) => void;
  onResetRange: () => void;
  isZoomed: boolean;
}) {
  const orgRoutes = useOrgRoutes();

  return (
    <>
      <div className="flex min-h-0 w-full flex-1 flex-col">
        <div className="flex min-h-0 flex-1 flex-col gap-6 px-8 pt-8">
          <div className="flex shrink-0 items-start justify-between gap-4">
            <LoggingPageHeader
              title="Tool Logs"
              description="Dive into tool traces across all tools, skills, and MCP servers used by organization members in this project"
            />
            <div className="flex items-center gap-2">
              <Button
                variant="secondary"
                size="sm"
                onClick={onRefresh}
                disabled={isFetching}
              >
                <Icon
                  name="refresh-cw"
                  className={cn("size-4", isFetching && "animate-spin")}
                />
                Refresh
              </Button>
              <Button variant="secondary" size="sm" asChild>
                <Link to={orgRoutes.logs.href()}>
                  <Settings className="h-4 w-4" />
                  Configure settings
                </Link>
              </Button>
            </div>
          </div>

          {/* Summary and the window's shape are one statement about the range,
              so they share a panel: four numbers, then the silhouette they
              came from. */}
          <div className="border-border bg-card shrink-0 border">
            <div className="divide-border grid grid-cols-2 divide-x md:grid-cols-4">
              <SummaryStat
                label="Tool calls"
                value={
                  totals ? Number(totals.eventCount).toLocaleString() : "—"
                }
              />
              <SummaryStat
                label="Failures"
                value={
                  totals ? Number(totals.failureCount).toLocaleString() : "—"
                }
                tone={
                  totals && Number(totals.failureCount) > 0
                    ? "destructive"
                    : "default"
                }
              />
              <SummaryStat
                label="Failure rate"
                value={
                  totals ? `${(totals.failureRate * 100).toFixed(1)}%` : "—"
                }
                tone={
                  totals && totals.failureRate > 0 ? "destructive" : "default"
                }
              />
              <SummaryStat
                label="Tools"
                value={
                  totals ? Number(totals.uniqueTools).toLocaleString() : "—"
                }
              />
            </div>

            <div className="border-border border-t px-4 pt-3 pb-2">
              <LogsTimelineStrip
                loading={timeSeriesPending}
                timeSeries={timeSeries}
                from={from}
                to={to}
                statuses={selectedStatuses}
                onRangeSelect={onRangeSelect}
                onResetRange={onResetRange}
                isZoomed={isZoomed}
                degraded={
                  // Error and success are honoured by the strip itself; what it
                  // still cannot express is a blocked/pending filter or a
                  // free-text search, since neither has a time series.
                  isCustomSearchActive(
                    attributeSearchQuery,
                    attributeFilters,
                  ) ||
                  selectedStatuses.some(
                    (status) => status === "blocked" || status === "pending",
                  )
                }
              />
            </div>
          </div>

          <div className="border-border bg-card flex min-h-0 flex-1 flex-col overflow-hidden border">
            {/* Search reads across both columns — it narrows the rows, not the
                facet list — so it spans them rather than sitting in one. */}
            <div className="border-border shrink-0 border-b p-3">
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

            <div className="flex min-h-0 flex-1 overflow-hidden">
              <LogsFacetRail
                header={
                  <TimeRangePicker
                    preset={customRange ? null : dateRange}
                    customRange={customRange}
                    customRangeLabel={customRangeLabel}
                    onPresetChange={(preset) => onDateRangeChange(preset)}
                    onCustomRangeChange={onCustomRangeChange}
                    onClearCustomRange={onClearCustomRange}
                    projectSlug={projectSlug}
                    // The rail is narrower than the picker's intrinsic width,
                    // so without letting the free-text input shrink the trigger
                    // overflows and the chevron sits outside its own border.
                    className="w-full [&_input]:min-w-0"
                  />
                }
                groups={facetGroups}
                onToggle={onFacetToggle}
                onClearGroup={onFacetClearGroup}
                className="border-border hidden w-[236px] shrink-0 flex-col border-r p-3 xl:flex"
              />
              <div className="bg-card min-h-0 flex-1 overflow-hidden">
                <div className="bg-background relative flex h-full min-h-0 flex-col">
                  {/* Refetches of the whole list only. Paging in the next
                      page keeps every loaded row on screen, so flashing a bar
                      across the header for it reads as a page reload. */}
                  {isFetching && !isFetchingNextPage && traces.length > 0 && (
                    <div className="bg-border absolute top-0 right-0 left-0 z-20 h-0.5">
                      <div className="bg-muted-foreground/60 h-full animate-pulse" />
                    </div>
                  )}

                  <div className="text-eyebrow bg-card sticky top-0 z-10 flex h-9 shrink-0 items-center gap-3 border-b px-4">
                    <div className="w-2 shrink-0" />
                    <div className="w-[76px] shrink-0">Time</div>
                    <div className="min-w-0 flex-1">Server</div>
                    <div className="min-w-0 flex-1">Tool</div>
                    <div className="w-20 shrink-0">Type</div>
                    <div className="min-w-[180px] flex-1 text-left">User</div>
                    <div className="w-32 shrink-0">Client</div>
                  </div>

                  {/* One header for the whole list instead of a sticky header
                      per day group. Sticky groups hand off by pushing each
                      other out of the viewport, which reads as a jump when
                      scrolling fast. This one never moves: only its date
                      changes, as the day under the top of the list changes. */}
                  {stickyDayDate && traces.length > 0 && (
                    <div className="bg-card shrink-0 border-b">
                      <DateGroupHeader date={stickyDayDate} mode="local" />
                    </div>
                  )}

                  <div
                    ref={containerRef}
                    className="relative flex-1 overflow-y-auto"
                    onScroll={handleScroll}
                  >
                    <LogsToolsTableContent
                      error={error}
                      isLoading={isLoading}
                      traces={traces}
                      hasActiveFilters={
                        activeFilters.length > 0 ||
                        !isDefaultToolUsageTypeSelection(selectedTypes) ||
                        selectedStatuses.length > 0 ||
                        selectedRoleIds.length > 0 ||
                        attributeFilters.length > 0 ||
                        accountType !== "" ||
                        Boolean(attributeSearchQuery)
                      }
                      expandedTraceId={expandedTraceId}
                      openingTraceId={openingTraceId}
                      isFetchingNextPage={isFetchingNextPage}
                      hasNextPage={hasNextPage}
                      onToggleExpand={toggleExpand}
                      onLogClick={handleLogClick}
                      serverNameMappings={serverNameMappings}
                      from={from}
                      to={to}
                    />
                  </div>

                  {traces.length > 0 && (
                    <div className="flex shrink-0 items-center gap-4 border-t px-5 py-3">
                      <span className="text-eyebrow">
                        {traces.length}{" "}
                        {traces.length === 1 ? "trace" : "traces"}
                        {hasNextPage && " · Scroll to load more"}
                      </span>
                    </div>
                  )}
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <LogDetailSheet
        log={selectedLog}
        hostedToolsetSlug={selectedHostedToolsetSlug}
        open={!!selectedLog}
        onOpenChange={(open) => {
          void (!open && setSelectedLog(null));
        }}
        onAddFilter={onAddFilterFromLog}
      />
    </>
  );
}

function LogsToolsTableContent({
  error,
  isLoading,
  traces,
  hasActiveFilters,
  expandedTraceId,
  openingTraceId,
  isFetchingNextPage,
  hasNextPage,
  onToggleExpand,
  onLogClick,
  serverNameMappings,
  from,
  to,
}: {
  error: Error | null;
  isLoading: boolean;
  traces: ToolUsageTraceSummary[];
  hasActiveFilters: boolean;
  expandedTraceId: string | null;
  openingTraceId: string | null;
  isFetchingNextPage: boolean;
  hasNextPage: boolean;
  onToggleExpand: (trace: ToolUsageTraceSummary) => Promise<void>;
  onLogClick: (log: TelemetryLogRecord, trace?: ToolUsageTraceSummary) => void;
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  from: Date;
  to: Date;
}) {
  if (error) {
    return (
      <ErrorAlert
        error={error}
        title="Error loading tool logs"
        className="m-4"
      />
    );
  }

  if (isLoading) {
    return (
      <div aria-label="Loading tool logs" role="status">
        {Array.from({ length: 8 }, (_, i) => (
          <div
            key={i}
            className="flex items-center gap-3 border-b px-5 py-2.5 last:border-b-0"
          >
            <div className="w-2 shrink-0" />
            <div className="w-[76px] shrink-0">
              <Skeleton className="h-3 w-14" />
            </div>
            <div className="min-w-0 flex-1">
              <Skeleton className="h-3 w-28" />
            </div>
            <div className="min-w-0 flex-1">
              <Skeleton className="h-3 w-36" />
            </div>
            <div className="w-20 shrink-0">
              <Skeleton className="h-3 w-12" />
            </div>
            <div className="min-w-[180px] flex-1">
              <Skeleton className="h-3 w-32" />
            </div>
            <div className="w-32 shrink-0">
              <Skeleton className="h-3 w-16" />
            </div>
          </div>
        ))}
      </div>
    );
  }

  if (traces.length === 0) {
    if (!hasActiveFilters) {
      return <HooksEmptyState />;
    }

    return (
      <div className="py-12 text-center">
        <div className="flex flex-col items-center gap-3">
          <div className="bg-muted flex size-12 items-center justify-center rounded-full">
            <Icon name="inbox" className="text-muted-foreground size-6" />
          </div>
          <span className="text-foreground font-medium">
            No matching tool logs
          </span>
          <span className="text-muted-foreground max-w-sm text-sm">
            Try adjusting your filters or time range
          </span>
        </div>
      </div>
    );
  }

  // The date is the same for long runs of rows, so it is stated once per day
  // rather than on every line. Each group carries the timestamp of its first
  // row so the pinned header above the list can read the current day straight
  // off the DOM while scrolling.
  const dayGroups: Array<{ key: string; date: Date; traces: typeof traces }> =
    [];
  for (const trace of traces) {
    const timestamp = new Date(
      Number(BigInt(trace.startTimeUnixNano) / 1_000_000n),
    );
    const key = format(timestamp, "yyyy-MM-dd");
    const current = dayGroups[dayGroups.length - 1];
    if (current?.key === key) {
      current.traces.push(trace);
    } else {
      dayGroups.push({ key, date: timestamp, traces: [trace] });
    }
  }

  return (
    <>
      {dayGroups.map((group) => (
        <div
          key={group.key}
          data-day-ts={group.date.getTime()}
          className="border-border/60 border-t first:border-t-0"
        >
          {group.traces.map((trace) => (
            <LogsToolsTraceRow
              key={trace.id}
              trace={trace}
              isExpanded={expandedTraceId === trace.id}
              isOpening={openingTraceId === trace.id}
              onToggle={() => void onToggleExpand(trace)}
              onLogClick={onLogClick}
              serverNameMappings={serverNameMappings}
              from={from}
              to={to}
            />
          ))}
        </div>
      ))}

      {/* Rendered for the whole time there is a next page, not just while one
          is in flight: a row that appears and disappears changes the scroll
          height mid-scroll, which is what makes paging feel like it jumps. */}
      {hasNextPage && (
        <div className="text-muted-foreground flex h-12 items-center justify-center gap-2 border-t">
          {isFetchingNextPage && (
            <>
              <Icon name="loader-circle" className="size-3.5 animate-spin" />
              <span className="text-eyebrow">Loading more</span>
            </>
          )}
        </div>
      )}
    </>
  );
}

function LogsToolsTraceRow({
  trace,
  isExpanded,
  isOpening,
  onToggle,
  onLogClick,
  serverNameMappings,
  from,
  to,
}: {
  trace: ToolUsageTraceSummary;
  isExpanded: boolean;
  isOpening: boolean;
  onToggle: () => void;
  onLogClick: (log: TelemetryLogRecord, trace?: ToolUsageTraceSummary) => void;
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  from: Date;
  to: Date;
}) {
  const routes = useRoutes();
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const timestamp = new Date(
    Number(BigInt(trace.startTimeUnixNano) / 1_000_000n),
  );
  const now = new Date();
  const diff = now.getTime() - timestamp.getTime();
  const seconds = Math.max(0, Math.floor(diff / 1000));
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);
  const timeAgo =
    days > 0
      ? `${days}d ago`
      : hours > 0
        ? `${hours}h ago`
        : minutes > 0
          ? `${minutes}m ago`
          : `${seconds}s ago`;

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

  const statusConfig = getStatusConfig(trace);
  const targetConfig = getTargetConfig(trace.targetType);
  const userLabel = trace.userLabel || "—";

  return (
    <div className="border-b last:border-b-0">
      <div
        role="button"
        tabIndex={0}
        onClick={onToggle}
        aria-busy={isOpening}
        onKeyDown={(e) => {
          // The row holds focusable children (the identity link): a key press
          // aimed at one of those must act on it alone rather than also
          // toggling the row it bubbles through.
          if (e.target !== e.currentTarget) return;
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onToggle();
          }
        }}
        className={cn(
          "bg-card flex w-full cursor-pointer items-center gap-3 px-4 py-2 text-left",
          "hover:bg-accent/30",
          isExpanded && "bg-accent/40",
        )}
      >
        <div className="flex w-2 shrink-0 justify-center">
          {statusConfig && (
            <SimpleTooltip tooltip={statusConfig.label}>
              <span
                aria-label={statusConfig.label}
                className={cn(
                  "size-1.5 rounded-full",
                  statusConfig.dotClassName,
                )}
              />
            </SimpleTooltip>
          )}
        </div>

        <div
          className="text-muted-foreground w-[76px] shrink-0 font-mono text-xs tabular-nums"
          title={`${format(timestamp, "MMM d, HH:mm:ss")} · ${timeAgo}`}
        >
          {format(timestamp, "HH:mm:ss")}
        </div>

        <div className="group/server flex min-w-0 flex-1 items-center gap-1.5">
          {showTargetLabel ? (
            <span className="text-muted-foreground min-w-0 truncate font-mono text-xs">
              {trace.targetType === "hosted_mcp_server" && trace.targetId ? (
                <Link
                  to={routes.mcp.details.overview.href(trace.targetId)}
                  onClick={(event) => event.stopPropagation()}
                  className="hover:text-foreground hover:underline"
                >
                  {targetLabel}
                </Link>
              ) : trace.targetType === "meta_mcp_server" && trace.targetId ? (
                <Link
                  to={routes.mcp.gateway.overview.href(trace.targetId)}
                  onClick={(event) => event.stopPropagation()}
                  className="hover:text-foreground hover:underline"
                >
                  {targetLabel}
                </Link>
              ) : (
                targetLabel
              )}
            </span>
          ) : (
            <span className="text-muted-foreground/50 font-mono text-xs">
              —
            </span>
          )}
          {editDialogProps && (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                setEditDialogOpen(true);
              }}
              className="text-muted-foreground hover:text-foreground invisible shrink-0 transition-colors group-hover/server:visible"
              aria-label="Edit display name"
            >
              <Icon name="pencil" className="size-3" />
            </button>
          )}
          {trace.viaMetaMcpServerId && (
            // The gateway that dispatched this member call.
            <SimpleTooltip
              tooltip={`Dispatched through gateway "${trace.viaMetaMcpServerName ?? trace.viaMetaMcpServerId}"`}
            >
              <Link
                to={routes.mcp.gateway.overview.href(trace.viaMetaMcpServerId)}
                onClick={(event) => event.stopPropagation()}
                aria-label={`Dispatched through gateway "${trace.viaMetaMcpServerName ?? trace.viaMetaMcpServerId}"`}
                className="flex shrink-0 items-center text-[var(--color-feedback-blue-700)] hover:text-[var(--color-feedback-blue-500)] dark:text-[var(--color-feedback-blue-500)] dark:hover:text-[var(--color-feedback-blue-400)]"
              >
                <Icon name="network" className="size-4" />
              </Link>
            </SimpleTooltip>
          )}
        </div>

        <div className="min-w-0 flex-1">
          <span className="text-foreground block truncate font-mono text-xs font-medium">
            {formatToolName(trace.toolName)}
          </span>
        </div>

        <div className="text-muted-foreground w-20 shrink-0 truncate font-mono text-[10px] tracking-wide uppercase">
          {targetConfig.shortLabel}
        </div>

        <div className="flex min-w-[180px] flex-1 items-center gap-2 text-xs">
          <AccountTypeIcon
            accountType={trace.accountType}
            className="shrink-0"
          />
          <IdentityLink
            identifier={identityRefForKind(trace.userKind, trace.userKey)}
            className="text-muted-foreground min-w-0 truncate"
          >
            {userLabel || "—"}
          </IdentityLink>
        </div>

        <div className="flex w-32 shrink-0 items-center gap-1.5">
          {trace.hookSource ? (
            <>
              <AgentProviderIcon
                source={trace.hookSource}
                className="size-3.5 shrink-0"
              />
              <span className="text-muted-foreground truncate text-xs">
                {formatPlatform(trace.hookSource)}
              </span>
            </>
          ) : (
            <span className="text-muted-foreground/70 truncate text-xs">
              Direct
            </span>
          )}
        </div>
      </div>

      {isExpanded && (
        <div className="border-border border-t border-l-2">
          {trace.hookStatus === "blocked" && (
            <div className="flex items-start gap-3 border-b px-5 py-3 text-xs">
              <Icon
                name="shield-alert"
                className="mt-0.5 size-4 shrink-0 text-[var(--color-feedback-orange-600)] dark:text-[var(--color-feedback-orange-400)]"
              />
              <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                <div className="font-mono tracking-wide uppercase text-[var(--color-feedback-orange-600)] dark:text-[var(--color-feedback-orange-400)]">
                  Blocked
                </div>
                <div className="text-foreground wrap-break-words ">
                  {trace.blockReason || "No reason provided"}
                </div>
              </div>
            </div>
          )}
          <TraceLogsList
            logGroup={trace.logGroup}
            toolName={trace.toolName}
            isExpanded={isExpanded}
            onLogClick={(log) => onLogClick(log, trace)}
            parentTimestamp={trace.startTimeUnixNano}
            from={from}
            to={to}
          />
        </div>
      )}

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

// The surface a call arrived through. Rendered as a dot plus a short mono
// label rather than a filled badge: there is one on every row, and a column of
// coloured blocks reads louder than the tool name it sits next to.
function getTargetConfig(targetType: ToolUsageTraceSummary["targetType"]) {
  switch (targetType) {
    case "hosted_mcp_server":
      return {
        label: "Hosted MCP",
        shortLabel: "Hosted",
        dotClassName: "bg-blue-500",
      };
    case "tunneled_mcp_server":
      return {
        label: "Tunneled MCP",
        shortLabel: "Tunnel",
        dotClassName: "bg-green-500",
      };
    case "meta_mcp_server":
      return {
        label: "Gateway",
        shortLabel: "Gateway",
        dotClassName: "bg-cyan-500",
      };
    case "shadow_mcp_server":
      return {
        label: "Shadow MCP",
        shortLabel: "Shadow",
        dotClassName: "bg-orange-500",
      };
    case "skill":
      return {
        label: "Skill",
        shortLabel: "Skill",
        dotClassName: "bg-purple-500",
      };
    case "local_tool":
    default:
      return {
        label: "Local Tools",
        shortLabel: "Local",
        dotClassName: "bg-muted-foreground/40",
      };
  }
}

// Status renders as a plain mono word: color only where attention is due
// (error red, blocked orange); success and pending stay muted ink.
function getStatusConfig(trace: ToolUsageTraceSummary): {
  className: string;
  dotClassName: string;
  label: string;
} | null {
  const ok = {
    className: "text-muted-foreground",
    dotClassName: "bg-emerald-500",
    label: "Success",
  };
  const failed = {
    className: "text-destructive",
    dotClassName: "bg-rose-500",
    label: "Error",
  };

  if (trace.hookStatus) {
    switch (trace.hookStatus) {
      case "blocked":
        // Blocked is a denial, not a fault, but it is the same answer to "did
        // this call run": it did not.
        return { ...failed, dotClassName: "bg-rose-500", label: "Blocked" };
      case "failure":
        return failed;
      case "success":
        return ok;
      case "pending":
        return {
          className: "text-muted-foreground",
          dotClassName: "bg-muted-foreground/30",
          label: "Pending",
        };
      default:
        return null;
    }
  }

  if (trace.httpStatusCode !== undefined) {
    if (trace.httpStatusCode >= 400) return failed;
    if (trace.httpStatusCode >= 200 && trace.httpStatusCode < 400) return ok;
  }

  return null;
}
