import { EnableLoggingOverlay } from "@/components/EnableLoggingOverlay";
import { InsightsConfig } from "@/components/insights-dock";
import { ObservabilitySkeleton } from "@/components/ObservabilitySkeleton";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import {
  FilterChip,
  ObserveFilterBar,
  type ObserveTypeFilterValue,
} from "@/components/observe/ObserveFilterBar";
import {
  SERVER_FILTER_PATH,
  TOOL_USAGE_DEFAULT_TYPES,
  TOOL_USAGE_TYPE_OPTIONS,
  TOOL_USAGE_VALID_TYPES,
  USER_EMAIL_FILTER_PATH,
  buildServerOptionGroups,
  encodeHostedServerFilter,
  encodeShadowServerFilter,
  parseTargetFilter,
  selectedHookSources,
  selectedTargetValues,
  selectedUserEmails,
  toTargetTypes,
} from "@/components/observe/observeTargetFilters";
import { useSlugs } from "@/contexts/Sdk";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { useServerNameMappings } from "@/hooks/useServerNameMappings";
import { cn } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import { type DateRangePreset } from "@gram-ai/elements";
import { telemetryGetToolUsageFilterOptions } from "@gram/client/funcs/telemetryGetToolUsageFilterOptions";
import { telemetryGetToolUsageSummary } from "@gram/client/funcs/telemetryGetToolUsageSummary";
import type { GetToolUsageSummaryResult } from "@gram/client/models/components/gettoolusagesummaryresult.js";
import type { ToolUsageTargetTimeSeriesPoint } from "@gram/client/models/components/toolusagetargettimeseriespoint.js";
import type { ToolUsageTargetToolBreakdownRow } from "@gram/client/models/components/toolusagetargettoolbreakdownrow.js";
import type { ToolUsageUsersByTargetRow } from "@gram/client/models/components/toolusageusersbytargetrow.js";
import type { ToolUsageUserSummary } from "@gram/client/models/components/toolusageusersummary.js";
import type { ToolUsageUserTimeSeriesPoint } from "@gram/client/models/components/toolusageusertimeseriespoint.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { Heading } from "@/components/ui/heading";
import { InlineEmptyState } from "@/components/ui/inline-empty-state";
import { Type } from "@/components/ui/type";
import { ChartCard } from "@/components/chart/ChartCard";
import { MetricCard } from "@/components/chart/MetricCard";
import { formatChartZoomRangeLabel } from "@/components/chart/chartUtils";
import { StackedBarChart } from "@/components/chart/StackedBarChart";
import { Timeseries } from "@/components/chart/Timeseries";
import { useExpandedChart } from "@/hooks/useExpandedChart";
import { useQuery } from "@tanstack/react-query";
import { Inbox, Settings } from "lucide-react";
import { useCallback, useEffect, useMemo } from "react";
import { Link } from "react-router";
import { useObserveFilters } from "@/components/observe/useObserveFilters";
import { HooksEmptyState } from "@/pages/hooks/HooksEmptyState";
import { HooksSetupButton } from "@/pages/hooks/HooksSetupDialog";
import type { MultiSelectGroup } from "@/components/ui/multi-select";
import { buildToolUsageTimeSeries } from "./toolUsageTimeSeriesChartData";

const COLLAPSED_BAR_CHART_MAX_ROWS = 6;
const LINE_CHART_HEIGHT = { collapsed: 250, expanded: 600 };

function displayTargetLabel(
  targetLabel: string,
  targetType: string,
  serverNameMappings: ReturnType<typeof useServerNameMappings>,
): string {
  if (targetType === "local_tool") return "Local Tools";
  if (targetType === "shadow_mcp_server") {
    return serverNameMappings.rawToDisplay.get(targetLabel) ?? targetLabel;
  }
  return targetLabel;
}

// Shared with both the logging-disabled empty state and the populated view
// below so the page title only has one copy to update.
function ToolInsightsHeading() {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <Heading variant="h1">MCP Servers & Tool Insights</Heading>
      <Type muted small>
        Monitor MCP servers and tool events across all users and agents in your
        project
      </Type>
    </div>
  );
}

export function InsightsToolsContent(): JSX.Element {
  const { projectSlug } = useSlugs();

  const mcpConfig = useObservabilityMcpConfig({
    toolsToInclude: ({ toolName }) =>
      toolName.includes("logs") || toolName.includes("hooks"),
  });

  const serverNameMappings = useServerNameMappings();

  const {
    from,
    to,
    selectedHookTypes,
    activeFilters,
    serverOptions,
    handleServerSelectionChange,
    handleUserEmailSelectionChange,
    hookSourceOptions,
    handleHookSourceSelectionChange,
    addFilter,
    handleHookTypesChange,
    dateRange,
    customRange,
    customRangeLabel,
    setDateRangeParam,
    setCustomRangeParam,
    clearCustomRange,
    selectedRoleIds,
    roleOptions,
    handleRoleSelectionChange,
    roleFilterPending,
    roleEmails,
    accountType,
    handleAccountTypeChange,
  } = useObserveFilters({
    defaultTypes: TOOL_USAGE_DEFAULT_TYPES,
    validTypes: TOOL_USAGE_VALID_TYPES,
  });

  const client = useGramContext();

  const serverFilters = useMemo(
    () => selectedTargetValues(activeFilters).map(parseTargetFilter),
    [activeFilters],
  );
  const hostedToolsetSlugs = useMemo(
    () =>
      serverFilters
        .filter((filter) => filter.type === "hosted")
        .map((filter) => filter.id),
    [serverFilters],
  );
  const shadowServerNames = useMemo(
    () =>
      serverFilters
        .filter((filter) => filter.type === "shadow")
        .map((filter) => filter.id),
    [serverFilters],
  );
  const userFilters = useMemo(() => {
    const emails = [
      ...new Set([...selectedUserEmails(activeFilters), ...roleEmails]),
    ];
    return emails.map((email) => ({ kind: "email" as const, key: email }));
  }, [activeFilters, roleEmails]);
  const hookSourceFilters = useMemo(
    () => selectedHookSources(activeFilters),
    [activeFilters],
  );

  const {
    data: summaryData,
    error,
    isFetching: summaryFetching,
    isPending: summaryPending,
    isError: summaryIsError,
    refetch: refetchSummary,
    isLogsDisabled: isLogsLogsDisabled,
  } = useLogsEnabledErrorCheck(
    useQuery({
      queryKey: [
        "tool-usage-summary",
        from.toISOString(),
        to.toISOString(),
        hostedToolsetSlugs,
        shadowServerNames,
        userFilters,
        hookSourceFilters,
        selectedHookTypes,
        accountType,
      ],
      queryFn: () =>
        unwrapAsync(
          telemetryGetToolUsageSummary(client, {
            getToolUsageSummaryPayload: {
              from,
              to,
              hostedToolsetSlugs:
                hostedToolsetSlugs.length > 0 ? hostedToolsetSlugs : undefined,
              shadowServerNames:
                shadowServerNames.length > 0 ? shadowServerNames : undefined,
              targetTypes: toTargetTypes(selectedHookTypes),
              userFilters: userFilters.length > 0 ? userFilters : undefined,
              hookSources:
                hookSourceFilters.length > 0 ? hookSourceFilters : undefined,
              accountType: accountType || undefined,
            },
          }),
        ),
      enabled: !roleFilterPending,
      throwOnError: false,
    }),
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

  const displayError = error
    ? new Error("Unable to load tool usage. Please try again.")
    : null;

  const refetch = useCallback(() => {
    void refetchSummary();
  }, [refetchSummary]);

  const isLogsDisabled = isLogsLogsDisabled;
  const isLoading = summaryFetching && !summaryData;

  return (
    <>
      <InsightsConfig
        mcpConfig={mcpConfig}
        title="Explore MCP Servers & Tools"
        subtitle="Ask me about your MCP servers and tools! Powered by Elements + platform MCP"
        hideTrigger={isLogsDisabled}
      />
      {isLogsDisabled ? (
        <div className="min-h-0 w-full flex-1 space-y-6 overflow-y-auto p-8 pb-24">
          <ToolInsightsHeading />
          <div className="relative flex-1">
            <div
              className="pointer-events-none h-full select-none"
              aria-hidden="true"
            >
              <ObservabilitySkeleton />
            </div>
            <EnableLoggingOverlay onEnabled={refetch} />
          </div>
        </div>
      ) : (
        <HooksInnerContent
          isLogsDisabled={isLogsDisabled}
          isLoading={isLoading}
          error={displayError}
          serverOptions={serverOptions}
          serverOptionGroups={serverOptionGroups}
          onServerSelectionChange={handleServerSelectionChange}
          userEmailOptions={toolUsageUserEmailOptions}
          onUserEmailSelectionChange={handleUserEmailSelectionChange}
          sourceOptions={hookSourceOptions}
          onSourceSelectionChange={handleHookSourceSelectionChange}
          activeFilters={activeFilters}
          addFilter={addFilter}
          selectedHookTypes={selectedHookTypes}
          onHookTypesChange={handleHookTypesChange}
          typeOptions={TOOL_USAGE_TYPE_OPTIONS}
          roleOptions={roleOptions}
          selectedRoleIds={selectedRoleIds}
          onRoleSelectionChange={handleRoleSelectionChange}
          dateRange={dateRange}
          customRange={customRange}
          customRangeLabel={customRangeLabel}
          onDateRangeChange={setDateRangeParam}
          onCustomRangeChange={setCustomRangeParam}
          onClearCustomRange={clearCustomRange}
          projectSlug={projectSlug}
          serverNameMappings={serverNameMappings}
          summaryData={summaryData}
          summaryPending={summaryPending}
          summaryIsError={summaryIsError}
          accountType={accountType}
          onAccountTypeChange={handleAccountTypeChange}
          onRefresh={refetch}
          isRefreshing={summaryFetching}
        />
      )}
    </>
  );
}

function HooksInnerContent({
  isLoading,
  error,
  serverOptions,
  serverOptionGroups,
  onServerSelectionChange,
  userEmailOptions,
  onUserEmailSelectionChange,
  sourceOptions,
  onSourceSelectionChange,
  activeFilters,
  addFilter,
  selectedHookTypes,
  onHookTypesChange,
  typeOptions,
  roleOptions,
  selectedRoleIds,
  onRoleSelectionChange,
  dateRange,
  customRange,
  customRangeLabel,
  onDateRangeChange,
  onCustomRangeChange,
  onClearCustomRange,
  projectSlug,
  serverNameMappings,
  summaryData,
  summaryPending,
  summaryIsError,
  accountType,
  onAccountTypeChange,
  onRefresh,
  isRefreshing,
}: {
  isLogsDisabled: boolean;
  isLoading: boolean;
  error: Error | null;
  serverOptions: string[];
  serverOptionGroups: MultiSelectGroup[];
  onServerSelectionChange: (values: string[]) => void;
  userEmailOptions: string[];
  onUserEmailSelectionChange: (values: string[]) => void;
  sourceOptions: string[];
  onSourceSelectionChange: (values: string[]) => void;
  activeFilters: FilterChip[];
  addFilter: (chip: FilterChip) => void;
  selectedHookTypes: ObserveTypeFilterValue[];
  onHookTypesChange: (types: ObserveTypeFilterValue[]) => void;
  typeOptions: Array<{ label: string; value: ObserveTypeFilterValue }>;
  roleOptions: Array<{ id: string; name: string }>;
  selectedRoleIds: string[];
  onRoleSelectionChange: (values: string[]) => void;
  dateRange: DateRangePreset;
  customRange: { from: Date; to: Date } | null;
  customRangeLabel: string | null;
  onDateRangeChange: (preset: DateRangePreset) => void;
  onCustomRangeChange: (from: Date, to: Date, label?: string) => void;
  onClearCustomRange: () => void;
  projectSlug?: string;
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  summaryData: GetToolUsageSummaryResult | undefined;
  summaryPending: boolean;
  summaryIsError: boolean;
  accountType: string;
  onAccountTypeChange: (value: string) => void;
  onRefresh: () => void;
  isRefreshing: boolean;
}) {
  const orgRoutes = useOrgRoutes();
  const { expandedChart, setExpandedChart } = useExpandedChart();
  useEffect(() => {
    if (summaryPending) setExpandedChart(null);
  }, [summaryPending, setExpandedChart]);
  const handleChartRangeSelect = useCallback(
    (from: Date, to: Date) => {
      onCustomRangeChange(from, to, formatChartZoomRangeLabel(from, to));
    },
    [onCustomRangeChange],
  );
  const hasSummaryData = (summaryData?.totals.eventCount ?? 0) > 0;

  return (
    <div className="flex min-h-0 w-full flex-1 flex-col">
      <div className="flex min-h-0 flex-1 flex-col gap-6 px-8 pt-8">
        <div className="flex shrink-0 items-start justify-between gap-4">
          <ToolInsightsHeading />
          <div className="flex items-center gap-2">
            <HooksSetupButton />
            <Button variant="secondary" size="sm" asChild>
              <Link to={orgRoutes.logs.href()}>
                <Settings className="h-4 w-4" />
                Configure settings
              </Link>
            </Button>
          </div>
        </div>

        <ObserveFilterBar
          serverOptions={serverOptions}
          serverOptionGroups={serverOptionGroups}
          onServerSelectionChange={onServerSelectionChange}
          userEmailOptions={userEmailOptions}
          onUserEmailSelectionChange={onUserEmailSelectionChange}
          sourceOptions={sourceOptions}
          onSourceSelectionChange={onSourceSelectionChange}
          activeFilters={activeFilters}
          selectedTypes={selectedHookTypes}
          onTypesChange={onHookTypesChange}
          typeOptions={typeOptions}
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
          onRefresh={onRefresh}
          isRefreshing={isRefreshing}
        />

        <div className="flex min-h-0 flex-1 overflow-hidden">
          <div className="min-h-0 flex-1 overflow-y-auto pb-4">
            {error ? (
              <Alert
                variant="error"
                dismissible={false}
                className="mx-auto w-full"
              >
                <span className="font-medium">Error loading tool usage</span>
                <div>{error.message}</div>
              </Alert>
            ) : isLoading ? (
              <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
                <Spinner className="mr-0 size-5" />
                <span>Loading tool usage...</span>
              </div>
            ) : !hasSummaryData &&
              activeFilters.length === 0 &&
              selectedRoleIds.length === 0 ? (
              <HooksEmptyState
                title="No Insights Generated"
                subtitle="Install Observability plugin in your AI agent to start generating tool insights"
              />
            ) : !hasSummaryData ? (
              <InlineEmptyState
                icon={<Inbox />}
                title="No matching tool usage"
                description="Try adjusting your search query or time range"
              />
            ) : (
              <HooksAnalytics
                serverNameMappings={serverNameMappings}
                compact={false}
                addFilter={addFilter}
                onHookTypesChange={onHookTypesChange}
                summaryData={summaryData}
                summaryPending={summaryPending}
                summaryIsError={summaryIsError}
                expandedChart={expandedChart}
                onExpandedChartChange={setExpandedChart}
                onRangeSelect={handleChartRangeSelect}
                isZoomed={customRange !== null}
                onResetZoom={onClearCustomRange}
              />
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function UsersPerServerChart({
  title,
  breakdown,
  serverNameMappings,
  handleFilter,
  expandedChart,
  onExpand,
}: {
  title: string;
  breakdown: ToolUsageUsersByTargetRow[];
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  handleFilter?: (userEmail: string, serverName: string) => void;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const chartId = "users-per-server";
  const expanded = expandedChart === chartId;
  const { labels, series } = useMemo(() => {
    const serverMap = new Map<string, Map<string, number>>();
    const userSet = new Set<string>();
    for (const row of breakdown) {
      const user = row.userLabel || "unknown";
      const displayName = displayTargetLabel(
        row.targetLabel,
        row.targetType,
        serverNameMappings,
      );
      userSet.add(user);
      const inner = serverMap.get(displayName) ?? new Map<string, number>();
      inner.set(user, (inner.get(user) ?? 0) + row.eventCount);
      serverMap.set(displayName, inner);
    }

    const sortedServers = Array.from(serverMap.entries())
      .map(([server, userCounts]) => ({
        server,
        total: Array.from(userCounts.values()).reduce((a, b) => a + b, 0),
        userCounts,
      }))
      .sort((a, b) => b.total - a.total);

    const sortedUsers = Array.from(userSet).sort((a, b) => {
      const aTotal = sortedServers.reduce(
        (s, srv) => s + (srv.userCounts.get(a) ?? 0),
        0,
      );
      const bTotal = sortedServers.reduce(
        (s, srv) => s + (srv.userCounts.get(b) ?? 0),
        0,
      );
      return bTotal - aTotal;
    });

    return {
      labels: sortedServers.map((s) => s.server),
      series: sortedUsers.map((user) => ({
        label: user,
        values: sortedServers.map((s) => s.userCounts.get(user) ?? 0),
      })),
    };
  }, [breakdown, serverNameMappings]);

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={labels.length > 0}
    >
      <StackedBarChart
        labels={labels}
        series={series}
        valueFormatter={(v) => v.toLocaleString()}
        showTotals
        expanded={expanded}
        maxRows={COLLAPSED_BAR_CHART_MAX_ROWS}
        onShowAll={() => onExpand(chartId)}
        onBarClick={handleFilter}
      />
    </ChartCard>
  );
}

function UserEventCountsChart({
  title,
  users,
  handleFilter,
  expandedChart,
  onExpand,
}: {
  title: string;
  users: ToolUsageUserSummary[];
  handleFilter?: (datasetLabel: string, userEmail: string) => void;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const chartId = "user-event-counts";
  const expanded = expandedChart === chartId;
  const { labels, series } = useMemo(() => {
    const sortedUsers = [...users].sort((a, b) => b.eventCount - a.eventCount);
    return {
      labels: sortedUsers.map((user) => user.userLabel || "unknown"),
      series: [
        {
          label: "Events",
          values: sortedUsers.map((user) => user.eventCount),
        },
      ],
    };
  }, [users]);

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={labels.length > 0}
    >
      <StackedBarChart
        labels={labels}
        series={series}
        valueFormatter={(v) => v.toLocaleString()}
        showTotals
        expanded={expanded}
        maxRows={COLLAPSED_BAR_CHART_MAX_ROWS}
        onShowAll={() => onExpand(chartId)}
        onBarClick={handleFilter}
      />
    </ChartCard>
  );
}

function ServerErrorRateChart({
  title,
  breakdown,
  serverNameMappings,
  expandedChart,
  onExpand,
}: {
  title: string;
  breakdown: ToolUsageTargetToolBreakdownRow[];
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const chartId = "errors-per-server";
  const expanded = expandedChart === chartId;
  const { labels, series } = useMemo(() => {
    const serverMap = new Map<string, Map<string, number>>();
    const toolSet = new Set<string>();
    for (const row of breakdown) {
      if (row.failureCount === 0) continue;
      const displayName = displayTargetLabel(
        row.targetLabel,
        row.targetType,
        serverNameMappings,
      );
      const tool = row.toolName || "unknown";
      toolSet.add(tool);
      const inner = serverMap.get(displayName) ?? new Map<string, number>();
      inner.set(tool, (inner.get(tool) ?? 0) + row.failureCount);
      serverMap.set(displayName, inner);
    }

    const sortedServers = Array.from(serverMap.entries())
      .map(([displayName, toolCounts]) => ({
        displayName,
        total: Array.from(toolCounts.values()).reduce((a, b) => a + b, 0),
        toolCounts,
      }))
      .sort((a, b) => b.total - a.total);

    const sortedTools = Array.from(toolSet).sort((a, b) => {
      const aTotal = sortedServers.reduce(
        (s, srv) => s + (srv.toolCounts.get(a) ?? 0),
        0,
      );
      const bTotal = sortedServers.reduce(
        (s, srv) => s + (srv.toolCounts.get(b) ?? 0),
        0,
      );
      return bTotal - aTotal;
    });

    return {
      labels: sortedServers.map((s) => s.displayName),
      series: sortedTools.map((tool) => ({
        label: tool,
        values: sortedServers.map((s) => s.toolCounts.get(tool) ?? 0),
      })),
    };
  }, [breakdown, serverNameMappings]);

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={labels.length > 0}
    >
      <StackedBarChart
        labels={labels}
        series={series}
        valueFormatter={(v) => v.toLocaleString()}
        expanded={expanded}
        maxRows={COLLAPSED_BAR_CHART_MAX_ROWS}
        onShowAll={() => onExpand(chartId)}
        emptyMessage="No errors in this period"
      />
    </ChartCard>
  );
}

function ServerUsageTimeSeries({
  timeSeries,
  serverNameMappings,
  expandedChart,
  onExpand,
  onRangeSelect,
  isZoomed,
  onResetZoom,
}: {
  timeSeries: ToolUsageTargetTimeSeriesPoint[];
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
  onRangeSelect?: (from: Date, to: Date) => void;
  isZoomed?: boolean;
  onResetZoom?: () => void;
}) {
  const chartId = "server-usage";
  const expanded = expandedChart === chartId;
  const series = useMemo(
    () =>
      buildToolUsageTimeSeries(timeSeries, (pt) =>
        displayTargetLabel(pt.targetLabel, pt.targetType, serverNameMappings),
      ),
    [timeSeries, serverNameMappings],
  );
  return (
    <ChartCard
      title="Activity by Source"
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={series.length > 0}
      isZoomed={isZoomed}
      onResetZoom={onResetZoom}
    >
      <Timeseries
        series={series}
        mode="line"
        height={
          expanded ? LINE_CHART_HEIGHT.expanded : LINE_CHART_HEIGHT.collapsed
        }
        enableZoom
        onZoomRange={onRangeSelect}
      />
    </ChartCard>
  );
}

function UserUsageTimeSeries({
  timeSeries,
  expandedChart,
  onExpand,
  onRangeSelect,
  isZoomed,
  onResetZoom,
}: {
  timeSeries: ToolUsageUserTimeSeriesPoint[];
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
  onRangeSelect?: (from: Date, to: Date) => void;
  isZoomed?: boolean;
  onResetZoom?: () => void;
}) {
  const chartId = "user-usage";
  const expanded = expandedChart === chartId;
  const series = useMemo(
    () => buildToolUsageTimeSeries(timeSeries, (pt) => pt.userLabel),
    [timeSeries],
  );
  return (
    <ChartCard
      title="User Usage"
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={series.length > 0}
      isZoomed={isZoomed}
      onResetZoom={onResetZoom}
    >
      <Timeseries
        series={series}
        mode="line"
        height={
          expanded ? LINE_CHART_HEIGHT.expanded : LINE_CHART_HEIGHT.collapsed
        }
        enableZoom
        onZoomRange={onRangeSelect}
      />
    </ChartCard>
  );
}

function SkillUsageTimeSeries({
  skillTimeSeries,
  expandedChart,
  onExpand,
  onRangeSelect,
  isZoomed,
  onResetZoom,
}: {
  skillTimeSeries: ToolUsageTargetTimeSeriesPoint[];
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
  onRangeSelect?: (from: Date, to: Date) => void;
  isZoomed?: boolean;
  onResetZoom?: () => void;
}) {
  const chartId = "skill-usage";
  const expanded = expandedChart === chartId;
  const series = useMemo(
    () => buildToolUsageTimeSeries(skillTimeSeries, (pt) => pt.targetLabel),
    [skillTimeSeries],
  );
  return (
    <ChartCard
      title="Skill Usage"
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={series.length > 0}
      isZoomed={isZoomed}
      onResetZoom={onResetZoom}
    >
      <Timeseries
        series={series}
        mode="line"
        height={
          expanded ? LINE_CHART_HEIGHT.expanded : LINE_CHART_HEIGHT.collapsed
        }
        enableZoom
        onZoomRange={onRangeSelect}
      />
    </ChartCard>
  );
}

function UsersPerSkillChart({
  title,
  skillBreakdown,
  expandedChart,
  onExpand,
}: {
  title: string;
  skillBreakdown: ToolUsageUsersByTargetRow[];
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const chartId = "users-per-skill";
  const expanded = expandedChart === chartId;
  const { labels, series } = useMemo(() => {
    const skillMap = new Map<string, Map<string, number>>();
    const userSet = new Set<string>();
    for (const row of skillBreakdown) {
      const user = row.userLabel || "unknown";
      userSet.add(user);
      const inner = skillMap.get(row.targetLabel) ?? new Map<string, number>();
      inner.set(user, (inner.get(user) ?? 0) + row.eventCount);
      skillMap.set(row.targetLabel, inner);
    }

    const sortedSkills = Array.from(skillMap.entries())
      .map(([skill, userCounts]) => ({
        skill,
        total: Array.from(userCounts.values()).reduce((a, b) => a + b, 0),
        userCounts,
      }))
      .sort((a, b) => b.total - a.total);

    const userTotals = new Map<string, number>();
    for (const user of userSet) {
      userTotals.set(
        user,
        sortedSkills.reduce((s, sk) => s + (sk.userCounts.get(user) ?? 0), 0),
      );
    }
    const sortedUsers = Array.from(userSet).sort(
      (a, b) => (userTotals.get(b) ?? 0) - (userTotals.get(a) ?? 0),
    );

    return {
      labels: sortedSkills.map((s) => s.skill),
      series: sortedUsers.map((user) => ({
        label: user,
        values: sortedSkills.map((s) => s.userCounts.get(user) ?? 0),
      })),
    };
  }, [skillBreakdown]);

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={labels.length > 0}
    >
      <StackedBarChart
        labels={labels}
        series={series}
        valueFormatter={(v) => v.toLocaleString()}
        showTotals
        expanded={expanded}
        maxRows={COLLAPSED_BAR_CHART_MAX_ROWS}
        onShowAll={() => onExpand(chartId)}
      />
    </ChartCard>
  );
}

function ErrorsOverTimeChart({
  timeSeries,
  serverNameMappings,
  expandedChart,
  onExpand,
  onRangeSelect,
  isZoomed,
  onResetZoom,
}: {
  timeSeries: ToolUsageTargetTimeSeriesPoint[];
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
  onRangeSelect?: (from: Date, to: Date) => void;
  isZoomed?: boolean;
  onResetZoom?: () => void;
}) {
  const chartId = "errors-over-time";
  const expanded = expandedChart === chartId;
  // Broken down per source (rather than a single aggregate line) — the
  // consolidated Timeseries component doesn't support the old per-bucket
  // tooltip breakdown, so the breakdown is expressed as real series instead,
  // matching the "Activity by Source" chart above it.
  const series = useMemo(() => {
    const failures = timeSeries.filter((pt) => pt.failureCount > 0);
    return buildToolUsageTimeSeries(
      failures,
      (pt) =>
        displayTargetLabel(pt.targetLabel, pt.targetType, serverNameMappings),
      (pt) => pt.failureCount,
    );
  }, [timeSeries, serverNameMappings]);
  const hasErrors = series.length > 0;

  return (
    <ChartCard
      title="Errors Over Time"
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={hasErrors}
      isZoomed={isZoomed}
      onResetZoom={onResetZoom}
    >
      <Timeseries
        series={series}
        mode="line"
        height={
          expanded ? LINE_CHART_HEIGHT.expanded : LINE_CHART_HEIGHT.collapsed
        }
        enableZoom
        onZoomRange={onRangeSelect}
        emptyMessage="No errors in this period"
      />
    </ChartCard>
  );
}

function HooksAnalytics({
  serverNameMappings,
  compact = false,
  addFilter,
  onHookTypesChange,
  summaryData,
  summaryPending,
  summaryIsError,
  expandedChart,
  onExpandedChartChange: setExpandedChart,
  onRangeSelect,
  isZoomed,
  onResetZoom,
}: {
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  compact?: boolean;
  addFilter: (chip: FilterChip) => void;
  onHookTypesChange: (types: ObserveTypeFilterValue[]) => void;
  summaryData: GetToolUsageSummaryResult | undefined;
  summaryPending: boolean;
  summaryIsError: boolean;
  expandedChart: string | null;
  onExpandedChartChange: (id: string | null) => void;
  onRangeSelect?: (from: Date, to: Date) => void;
  isZoomed?: boolean;
  onResetZoom?: () => void;
}) {
  const targets = summaryData?.targets;
  const users = summaryData?.users ?? [];
  const timeSeries = summaryData?.targetTimeSeries ?? [];
  const userTimeSeries = summaryData?.userTimeSeries ?? [];
  const usersByTarget = summaryData?.usersByTarget ?? [];
  const targetToolBreakdown = summaryData?.targetToolBreakdown ?? [];
  const skillTimeSeries = timeSeries.filter(
    (point) => point.targetType === "skill",
  );
  const skillBreakdown = usersByTarget.filter(
    (row) => row.targetType === "skill",
  );

  const kpis = useMemo(() => {
    if (!summaryData) return null;
    const totalEvents = summaryData.totals.eventCount;
    const totalSuccesses = summaryData.totals.successCount;
    const totalFailures = summaryData.totals.failureCount;
    const completedEvents = totalSuccesses + totalFailures;
    const avgSuccessRate =
      completedEvents > 0 ? (totalSuccesses / completedEvents) * 100 : null;

    const activeUsers = summaryData.totals.uniqueUsers;
    const activeTargets = summaryData.totals.uniqueTargets;
    const uniqueTools = summaryData.totals.uniqueTools;

    return {
      avgSuccessRate,
      totalEvents,
      activeUsers,
      activeTargets,
      uniqueTools,
    };
  }, [summaryData]);

  const targetFiltersByLabel = useMemo(() => {
    const filters = new Map<string, string[]>();
    for (const target of targets ?? []) {
      const label = displayTargetLabel(
        target.targetLabel,
        target.targetType,
        serverNameMappings,
      );
      if (target.targetType === "hosted_mcp_server") {
        filters.set(label, [encodeHostedServerFilter(target.targetId)]);
      } else if (target.targetType === "shadow_mcp_server") {
        filters.set(label, [encodeShadowServerFilter(target.targetId)]);
      } else if (target.targetType === "local_tool") {
        filters.set(label, ["local_tool"]);
      } else if (target.targetType === "skill") {
        filters.set(label, ["skill"]);
      }
    }
    return filters;
  }, [serverNameMappings, targets]);

  type FilterAxisConfig = Partial<Record<"user" | "server", "dataset" | "row">>;

  const makeFilterHandler = useCallback(
    (config: FilterAxisConfig) => (datasetLabel: string, rowLabel: string) => {
      const localToolsDisplayName =
        serverNameMappings.rawToDisplay.get("") ?? "Local Tools";
      const apply = (value: string, filterType: "server" | "user") => {
        if (!value || value === "unknown") return;
        if (filterType === "server") {
          if (value === localToolsDisplayName) {
            onHookTypesChange(["local_tool"]);
            return;
          }
          const targetFilter = targetFiltersByLabel.get(value);
          if (targetFilter?.includes("local_tool")) {
            onHookTypesChange(["local_tool"]);
            return;
          }
          if (targetFilter?.includes("skill")) {
            onHookTypesChange(["skill"]);
            return;
          }
          const rawFilters = targetFilter ??
            serverNameMappings.displayToRaws.get(value) ?? [value];
          addFilter({
            display: value,
            filters: rawFilters,
            path: SERVER_FILTER_PATH,
          });
        } else {
          addFilter({
            display: value,
            filters: [value],
            path: USER_EMAIL_FILTER_PATH,
          });
        }
      };
      for (const [filterType, axis] of Object.entries(config) as [
        "server" | "user",
        "dataset" | "row",
      ][]) {
        apply(axis === "dataset" ? datasetLabel : rowLabel, filterType);
      }
    },
    [
      addFilter,
      onHookTypesChange,
      serverNameMappings.rawToDisplay,
      serverNameMappings.displayToRaws,
      targetFiltersByLabel,
    ],
  );

  return (
    <div className="space-y-4">
      <div
        className={cn(
          "grid gap-3 transition-all duration-200 ease-in-out",
          compact
            ? "grid-cols-2 md:grid-cols-3"
            : "grid-cols-2 md:grid-cols-3 lg:grid-cols-5",
          expandedChart && "hidden",
        )}
      >
        {summaryIsError && !summaryData ? (
          <div className="col-span-full">
            <Alert variant="error" dismissible={false}>
              <span className="font-medium">Error loading analytics</span>
              <div>Failed to load analytics summary</div>
            </Alert>
          </div>
        ) : summaryPending || !summaryData ? (
          <>
            {Array.from({ length: compact ? 3 : 5 }).map((_, i) => (
              <Skeleton key={i} className="h-[104px]" />
            ))}
          </>
        ) : (
          <>
            <MetricCard
              title="Avg Success Rate"
              value={kpis?.avgSuccessRate ?? 0}
              format="percent"
            />
            <MetricCard title="Total Events" value={kpis?.totalEvents ?? 0} />
            <MetricCard title="Active Users" value={kpis?.activeUsers ?? 0} />
            <MetricCard
              title="Active Targets"
              value={kpis?.activeTargets ?? 0}
            />
            <MetricCard title="Unique Tools" value={kpis?.uniqueTools ?? 0} />
          </>
        )}
      </div>

      <div
        className={cn(
          "grid gap-4",
          expandedChart
            ? "grid-cols-1"
            : compact
              ? "grid-cols-1"
              : "grid-cols-1 lg:grid-cols-2",
        )}
      >
        <ServerUsageTimeSeries
          timeSeries={timeSeries}
          serverNameMappings={serverNameMappings}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
          onRangeSelect={onRangeSelect}
          isZoomed={isZoomed}
          onResetZoom={onResetZoom}
        />

        <UsersPerServerChart
          title="Users by Source"
          breakdown={usersByTarget}
          serverNameMappings={serverNameMappings}
          handleFilter={makeFilterHandler({
            server: "row",
            user: "dataset",
          })}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
        />

        <UserUsageTimeSeries
          timeSeries={userTimeSeries}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
          onRangeSelect={onRangeSelect}
          isZoomed={isZoomed}
          onResetZoom={onResetZoom}
        />

        <UserEventCountsChart
          title="User Event Counts"
          users={users}
          handleFilter={makeFilterHandler({ user: "row" })}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
        />

        <SkillUsageTimeSeries
          skillTimeSeries={skillTimeSeries}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
          onRangeSelect={onRangeSelect}
          isZoomed={isZoomed}
          onResetZoom={onResetZoom}
        />

        <UsersPerSkillChart
          title="Users per Skill"
          skillBreakdown={skillBreakdown}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
        />

        <ErrorsOverTimeChart
          timeSeries={timeSeries}
          serverNameMappings={serverNameMappings}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
          onRangeSelect={onRangeSelect}
          isZoomed={isZoomed}
          onResetZoom={onResetZoom}
        />

        <ServerErrorRateChart
          title="Failures by Source and Tool"
          breakdown={targetToolBreakdown}
          serverNameMappings={serverNameMappings}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
        />
      </div>
    </div>
  );
}
