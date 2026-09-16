import { EnableLoggingOverlay } from "@/components/EnableLoggingOverlay";
import { Page } from "@/components/page-layout";
import { InsightsConfig } from "@/components/insights-dock";
import { ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Spinner } from "@/components/ui/Spinner";
import {
  FilterChip,
  ObserveFilterBar,
  type ObserveTypeFilterValue,
} from "@/components/observe/ObserveFilterBar";
import {
  TOOL_USAGE_DEFAULT_TYPES,
  TOOL_USAGE_TYPE_OPTIONS,
  TOOL_USAGE_VALID_TYPES,
  buildServerOptionGroups,
  selectedUserEmails,
} from "@/components/observe/observeTargetFilters";
import { useSlugs } from "@/contexts/Sdk";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { useServerNameMappings } from "@/hooks/useServerNameMappings";
import { useOrgRoutes } from "@/routes";
import { getPresetRange, type DateRangePreset } from "@/elements";
import { telemetryGetToolUsageClients } from "@gram/client/funcs/telemetryGetToolUsageClients";
import { telemetryGetToolUsageFilterOptions } from "@gram/client/funcs/telemetryGetToolUsageFilterOptions";
import { telemetryGetToolUsageTargets } from "@gram/client/funcs/telemetryGetToolUsageTargets";
import { telemetryGetToolUsageTargetTimeSeries } from "@gram/client/funcs/telemetryGetToolUsageTargetTimeSeries";
import { telemetryGetToolUsageTargetToolBreakdown } from "@gram/client/funcs/telemetryGetToolUsageTargetToolBreakdown";
import { telemetryGetToolUsageTotals } from "@gram/client/funcs/telemetryGetToolUsageTotals";
import { telemetryGetToolUsageUsers } from "@gram/client/funcs/telemetryGetToolUsageUsers";
import type { GetToolUsageSummaryResult } from "@gram/client/models/components/gettoolusagesummaryresult.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { Icon } from "@/components/ui/Icon";
import { formatChartZoomRangeLabel } from "@/components/chart/chartUtils";
import { useQuery } from "@tanstack/react-query";
import {
  BarElement,
  BarController,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Filler,
  Tooltip,
  Legend,
  Chart as ChartJS,
} from "chart.js";
import ZoomPlugin from "chartjs-plugin-zoom";
import { Settings } from "lucide-react";
import { useCallback, useMemo } from "react";
import { Link } from "react-router";
import { useObserveFilters } from "@/components/observe/useObserveFilters";
import { useToolUsagePayload } from "@/components/observe/toolUsagePayload";
import { InsightsGrid } from "@/components/observe/insights/InsightsGrid";
import { HooksEmptyState } from "@/pages/hooks/HooksEmptyState";
import type { MultiSelectGroup } from "@/components/ui/MultiSelect";

ChartJS.register(
  CategoryScale,
  LinearScale,
  BarElement,
  BarController,
  PointElement,
  LineElement,
  Filler,
  Tooltip,
  Legend,
  ZoomPlugin,
);

type ToolUsageSectionState = { pending: boolean; error: boolean };

// Per-section load state for the split tool usage summary. Each entry drives the
// skeleton/error state of the card(s) derived from that section, so panels render
// — and fail — independently. `totals` is intentionally omitted: it gates the
// page shell via summaryPending/summaryIsError, not an individual card.
type ToolUsageSectionStatus = {
  targets: ToolUsageSectionState;
  users: ToolUsageSectionState;
  targetTimeSeries: ToolUsageSectionState;
  userTimeSeries: ToolUsageSectionState;
  usersByTarget: ToolUsageSectionState;
  targetToolBreakdown: ToolUsageSectionState;
  clients: ToolUsageSectionState;
};

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

  const { summaryPayload, sharedQueryKey } = useToolUsagePayload({
    activeFilters,
    roleEmails,
    selectedHookTypes,
    accountType,
    from,
    to,
  });

  // Totals is the gate query: it decides the page shell (logs-disabled overlay,
  // "no data" empty state, KPI cards) and is the cheapest, so the page appears as
  // soon as it resolves while the heavier panels stream in behind it.
  const {
    data: totalsData,
    error,
    isFetching: totalsFetching,
    isPending: summaryPending,
    isError: summaryIsError,
    refetch: refetchTotals,
    isLogsDisabled: isLogsLogsDisabled,
  } = useLogsEnabledErrorCheck(
    useQuery({
      queryKey: ["tool-usage-totals", ...sharedQueryKey],
      queryFn: () =>
        unwrapAsync(
          telemetryGetToolUsageTotals(client, {
            getToolUsageSummaryPayload: summaryPayload,
          }),
        ),
      enabled: !roleFilterPending,
      throwOnError: false,
    }),
  );

  const targetsQuery = useQuery({
    queryKey: ["tool-usage-targets", ...sharedQueryKey],
    queryFn: () =>
      unwrapAsync(
        telemetryGetToolUsageTargets(client, {
          getToolUsageSummaryPayload: summaryPayload,
        }),
      ),
    enabled: !roleFilterPending,
    throwOnError: false,
  });

  const usersQuery = useQuery({
    queryKey: ["tool-usage-users", ...sharedQueryKey],
    queryFn: () =>
      unwrapAsync(
        telemetryGetToolUsageUsers(client, {
          getToolUsageSummaryPayload: summaryPayload,
        }),
      ),
    enabled: !roleFilterPending,
    throwOnError: false,
  });

  const targetTimeSeriesQuery = useQuery({
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

  const clientsQuery = useQuery({
    queryKey: ["tool-usage-clients", ...sharedQueryKey],
    queryFn: () =>
      unwrapAsync(
        telemetryGetToolUsageClients(client, {
          getToolUsageSummaryPayload: summaryPayload,
        }),
      ),
    enabled: !roleFilterPending,
    throwOnError: false,
  });

  const targetToolBreakdownQuery = useQuery({
    queryKey: ["tool-usage-target-tool-breakdown", ...sharedQueryKey],
    queryFn: () =>
      unwrapAsync(
        telemetryGetToolUsageTargetToolBreakdown(client, {
          getToolUsageSummaryPayload: summaryPayload,
        }),
      ),
    enabled: !roleFilterPending,
    throwOnError: false,
  });

  // Assemble the sections that have resolved into the summary shape the panels
  // already consume. Undefined until totals lands (which gates the shell);
  // each array fills in as its query resolves.
  const summaryData: GetToolUsageSummaryResult | undefined = useMemo(
    () =>
      totalsData
        ? {
            totals: totalsData.totals,
            targets: targetsQuery.data?.targets ?? [],
            users: usersQuery.data?.users ?? [],
            targetTimeSeries:
              targetTimeSeriesQuery.data?.targetTimeSeries ?? [],
            // Part of the summary contract, but no card on this board reads
            // them, so they are not fetched.
            userTimeSeries: [],
            usersByTarget: [],
            targetToolBreakdown:
              targetToolBreakdownQuery.data?.targetToolBreakdown ?? [],
            clients: clientsQuery.data?.clients ?? [],
            clientToolBreakdown: [],
          }
        : undefined,
    [
      totalsData,
      targetsQuery.data,
      usersQuery.data,
      targetTimeSeriesQuery.data,
      targetToolBreakdownQuery.data,
      clientsQuery.data,
    ],
  );

  const sectionStatus: ToolUsageSectionStatus = {
    targets: { pending: targetsQuery.isPending, error: targetsQuery.isError },
    users: { pending: usersQuery.isPending, error: usersQuery.isError },
    targetTimeSeries: {
      pending: targetTimeSeriesQuery.isPending,
      error: targetTimeSeriesQuery.isError,
    },
    userTimeSeries: { pending: false, error: false },
    usersByTarget: { pending: false, error: false },
    targetToolBreakdown: {
      pending: targetToolBreakdownQuery.isPending,
      error: targetToolBreakdownQuery.isError,
    },
    clients: { pending: clientsQuery.isPending, error: clientsQuery.isError },
  };

  const { refetch: refetchTargets } = targetsQuery;
  const { refetch: refetchUsers } = usersQuery;
  const { refetch: refetchTargetTimeSeries } = targetTimeSeriesQuery;
  const { refetch: refetchClients } = clientsQuery;
  const { refetch: refetchTargetToolBreakdown } = targetToolBreakdownQuery;

  const isAnyFetching =
    totalsFetching ||
    targetsQuery.isFetching ||
    usersQuery.isFetching ||
    targetTimeSeriesQuery.isFetching ||
    clientsQuery.isFetching ||
    targetToolBreakdownQuery.isFetching;

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
        gateways: filterOptionsData?.gateways ?? [],
        activeFilters,
        serverNameMappings,
      }),
    [
      activeFilters,
      filterOptionsData?.hostedServers,
      filterOptionsData?.shadowServers,
      filterOptionsData?.gateways,
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
    void refetchTotals();
    void refetchTargets();
    void refetchUsers();
    void refetchTargetTimeSeries();
    void refetchClients();
    void refetchTargetToolBreakdown();
  }, [
    refetchTotals,
    refetchTargets,
    refetchUsers,
    refetchTargetTimeSeries,
    refetchClients,
    refetchTargetToolBreakdown,
  ]);

  const isLogsDisabled = isLogsLogsDisabled;
  // Gate the page shell on the totals query only; the heavier panels stream in
  // behind it via their own per-card loading states.
  const isLoading = totalsFetching && !totalsData;

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
          <div className="flex min-w-0 flex-col gap-1">
            <Page.Eyebrow />
            <h1 className="text-display-sm font-thin">
              MCP Servers & Tool Insights
            </h1>
            <p className="text-muted-foreground text-sm">
              Monitor MCP servers and tool events across all users and agents in
              your project
            </p>
          </div>
          <div className="flex-1">
            <EnableLoggingOverlay
              onEnabled={refetch}
              screenshotSrc="/empty-states/mcp_insights_empty.png"
              screenshotAlt="MCP and Tools insights dashboard with usage data"
            />
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
          sectionStatus={sectionStatus}
          accountType={accountType}
          onAccountTypeChange={handleAccountTypeChange}
          onRefresh={refetch}
          isRefreshing={isAnyFetching}
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
  sectionStatus,
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
  sectionStatus: ToolUsageSectionStatus;
  accountType: string;
  onAccountTypeChange: (value: string) => void;
  onRefresh: () => void;
  isRefreshing: boolean;
}) {
  const orgRoutes = useOrgRoutes();
  const { from, to } = useMemo(
    () => customRange ?? getPresetRange(dateRange),
    [customRange, dateRange],
  );
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
          <div className="flex min-w-0 flex-col gap-1">
            <Page.Eyebrow />
            <h1 className="text-display-sm font-thin">
              MCP Servers & Tool Insights
            </h1>
            <p className="text-muted-foreground text-sm">
              Monitor MCP servers and tool events across all users and agents in
              your project
            </p>
          </div>
          <div className="flex items-center gap-2">
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
              <ErrorAlert
                error={error}
                title="Error loading tool usage"
                className="mx-auto w-full"
              />
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
              <div className="py-12 text-center">
                <div className="flex flex-col items-center gap-3">
                  <div className="bg-muted flex size-12 items-center justify-center rounded-full">
                    <Icon
                      name="inbox"
                      className="text-muted-foreground size-6"
                    />
                  </div>
                  <span className="text-foreground font-medium">
                    No matching tool usage
                  </span>
                  <span className="text-muted-foreground max-w-sm text-sm">
                    Try adjusting your search query or time range
                  </span>
                </div>
              </div>
            ) : (
              <HooksAnalytics
                serverNameMappings={serverNameMappings}
                from={from}
                to={to}
                summaryData={summaryData}
                summaryPending={summaryPending}
                summaryIsError={summaryIsError}
                sectionStatus={sectionStatus}
                onRangeSelect={handleChartRangeSelect}
              />
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function HooksAnalytics({
  serverNameMappings,
  from,
  to,
  summaryData,
  summaryPending,
  summaryIsError,
  sectionStatus,
  onRangeSelect,
}: {
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  from: Date;
  to: Date;
  summaryData: GetToolUsageSummaryResult | undefined;
  summaryPending: boolean;
  summaryIsError: boolean;
  sectionStatus: ToolUsageSectionStatus;
  onRangeSelect?: (from: Date, to: Date) => void;
}) {
  return (
    <InsightsGrid
      totals={summaryData?.totals}
      targets={summaryData?.targets ?? []}
      users={summaryData?.users ?? []}
      clients={summaryData?.clients ?? []}
      targetToolBreakdown={summaryData?.targetToolBreakdown ?? []}
      timeSeries={summaryData?.targetTimeSeries ?? []}
      from={from}
      to={to}
      serverNameMappings={serverNameMappings}
      status={{
        totals: { pending: summaryPending, error: summaryIsError },
        targets: sectionStatus.targets,
        users: sectionStatus.users,
        clients: sectionStatus.clients,
        targetToolBreakdown: sectionStatus.targetToolBreakdown,
        targetTimeSeries: sectionStatus.targetTimeSeries,
      }}
      onRangeSelect={onRangeSelect}
    />
  );
}
