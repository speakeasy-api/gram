import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { formatPlatform } from "@/lib/formatPlatform";
import { Card } from "@/components/ui/card";
import { ChartCard } from "@/components/chart/ChartCard";
import { formatChartZoomRangeLabel } from "@/components/chart/chartUtils";
import { StackedBarChart } from "@/components/chart/StackedBarChart";
import { Timeseries } from "@/components/chart/Timeseries";
import { RankedBar, type RankedBarItem } from "@/components/chart/RankedBar";
import { buildAgentTokenTimeSeries } from "@/components/observe/agentTokenTimeSeriesChartData";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { Heading } from "@/components/ui/heading";
import { Progress } from "@/components/ui/progress";
import { Type } from "@/components/ui/type";
import { formatCompact } from "@/lib/format";
import { MetricCard } from "@/components/chart/MetricCard";
import { InsightsConfig } from "@/components/insights-dock";
import { INSIGHTS_SUGGESTIONS } from "@/lib/insights-suggestions";
import { useInsightsState } from "@/components/insights-context";
import { useTelemetry } from "@/contexts/Telemetry";
import { Dialog } from "@/components/ui/dialog";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { SegmentedControl } from "@/components/ui/segmented-control";
import { Skeleton } from "@/components/ui/skeleton";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { slugify } from "@/lib/constants";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { telemetryGetObservabilityOverview } from "@gram/client/funcs/telemetryGetObservabilityOverview";
import { telemetryGetProjectMetricsSummary } from "@gram/client/funcs/telemetryGetProjectMetricsSummary";
import { telemetrySearchUsers } from "@gram/client/funcs/telemetrySearchUsers";
import type { GetObservabilityOverviewResult } from "@gram/client/models/components/getobservabilityoverviewresult.js";
import type { ModelUsage } from "@gram/client/models/components/modelusage.js";
import type { ProjectSummary } from "@gram/client/models/components/projectsummary.js";
import type { RoleSummary } from "@gram/client/models/components/rolesummary.js";
import type { TimeSeriesBucket } from "@gram/client/models/components/timeseriesbucket.js";
import type { UserSummary } from "@gram/client/models/components/usersummary.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { type DateRangePreset, getPresetRange } from "@gram-ai/elements";
import {
  defineFilters,
  useFilterState,
  type FilterValue,
  type OptionsById,
} from "@/components/filters";
import { ACCOUNT_TYPE_OPTIONS } from "@/components/observe/observeFilterConstants";
import { Page } from "@/components/page-layout";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import {
  Alert,
  Badge,
  Button,
  type Column,
  Input,
  type SortDescriptor,
  Table,
  sortTableData,
} from "@/components/ui/moonshine";
import { ArrowRight } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";

type ValueMode = "tokens" | "cost";

const PRESET_RANGE_LABELS: Record<DateRangePreset, string> = {
  "15m": "the last 15 minutes",
  "1h": "the last hour",
  "4h": "the last 4 hours",
  "1d": "the last day",
  "2d": "the last 2 days",
  "3d": "the last 3 days",
  "7d": "the last 7 days",
  "15d": "the last 15 days",
  "30d": "the last 30 days",
  "90d": "the last 90 days",
};

function formatCost(value: number): string {
  if (value >= 1) return `$${value.toFixed(2)}`;
  if (value >= 0.01) return `$${value.toFixed(3)}`;
  if (value > 0) return `$${value.toFixed(4)}`;
  return "$0.00";
}

function formatValue(value: number, mode: ValueMode): string {
  return mode === "cost" ? formatCost(value) : formatCompact(value);
}

function initials(name: string): string {
  const parts = name.split(/[\s-]+/).filter(Boolean);
  if (parts.length >= 2)
    return (parts[0]![0]! + parts[parts.length - 1]![0]!).toUpperCase();
  return (name[0] ?? "?").toUpperCase();
}

function ValueModeToggle({
  mode,
  onChange,
}: {
  mode: ValueMode;
  onChange: (mode: ValueMode) => void;
}) {
  return (
    <SegmentedControl
      value={mode}
      onChange={onChange}
      options={[
        {
          value: "tokens",
          label: "Tokens",
          tooltip: "Show usage measured in tokens",
        },
        {
          value: "cost",
          label: "Cost ($)",
          tooltip: "Show usage measured in US dollars",
        },
      ]}
    />
  );
}

// Cost filters in the unified system. The date range is pinned (always-visible
// pill); the client/agent filter lives behind "More filters" and surfaces as a
// pill once set. `client` options are supplied at render from the usage data.
const COST_FILTERS = defineFilters([
  {
    id: "date",
    label: "Date range",
    kind: "daterange",
    pinned: true,
    defaultPreset: "30d",
  },
  { id: "client", label: "Agent", kind: "select" },
  {
    id: "account_type",
    label: "Account type",
    kind: "select",
    allLabel: "All",
  },
]);

export function InsightsAgentsContent(): JSX.Element {
  const client = useGramContext();
  const { isExpanded: isInsightsOpen } = useInsightsState();
  const mcpConfig = useObservabilityMcpConfig({
    toolsToInclude: ["gram_search_users", "gram_list_organization_users"],
  });

  // Filters now run through the unified system (URL-persisted). The existing
  // query/derivation code reads dateRange/customRange/customRangeLabel/
  // clientFilter, so we bridge the filter values back to those shapes rather
  // than rewiring every consumer.
  const costFilters = useFilterState(COST_FILTERS);
  const dateValue = costFilters.values.date;
  const dateRange: DateRangePreset = dateValue.preset ?? "30d";
  const customRange = dateValue.customRange;
  const customRangeLabel = dateValue.customLabel;
  const clientFilter = costFilters.values.client ?? "all";
  const accountType = costFilters.values.account_type ?? "";

  const [valueMode, setValueMode] = useState<ValueMode>("tokens");
  const [expandedChart, setExpandedChart] = useState<string | null>(null);
  const [groupByDimension, setGroupByDimension] = useState<"employee" | "role">(
    "employee",
  );

  const { from, to } = useMemo(() => {
    const range = customRange ?? getPresetRange(dateRange);
    return { from: range.from, to: range.to };
  }, [customRange, dateRange]);

  const rangeLabel = useMemo(() => {
    if (customRange) return customRangeLabel ?? "the selected range";
    return PRESET_RANGE_LABELS[dateRange] ?? "the selected range";
  }, [customRange, customRangeLabel, dateRange]);

  const {
    data: membersData,
    isLoading: membersLoading,
    error: membersError,
  } = useMembers();
  const memberMap = useMemo(
    () => new Map((membersData?.members ?? []).map((m) => [m.id, m])),
    [membersData],
  );
  // Telemetry groups by user_id with a user_email fallback, so match members
  // on both their ID and email to avoid dropping email-keyed activity.
  const memberIdentifiers = useMemo(
    () =>
      (membersData?.members ?? []).flatMap((m) => [
        m.id,
        m.email.toLowerCase(),
      ]),
    [membersData],
  );

  const usersQuery = useQuery({
    queryKey: [
      "insights",
      "agents",
      "users",
      from.toISOString(),
      to.toISOString(),
      memberIdentifiers,
      accountType,
    ],
    queryFn: () =>
      fetchAllUsers(client, from, to, memberIdentifiers, accountType),
    enabled: memberIdentifiers.length > 0,
    throwOnError: false,
  });

  const projectQuery = useQuery({
    queryKey: [
      "insights",
      "agents",
      "project",
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: () => fetchProjectMetrics(client, from, to),
    throwOnError: false,
  });

  const overviewQuery = useQuery({
    queryKey: [
      "insights",
      "agents",
      "overview",
      from.toISOString(),
      to.toISOString(),
      clientFilter,
      accountType,
    ],
    queryFn: () =>
      fetchOverview(
        client,
        from,
        to,
        clientFilter !== "all" ? clientFilter : undefined,
        accountType || undefined,
      ),
    placeholderData: keepPreviousData,
    throwOnError: false,
  });

  const roleUsageQuery = useQuery({
    queryKey: [
      "insights",
      "agents",
      "roleUsage",
      from.toISOString(),
      to.toISOString(),
      memberIdentifiers,
      accountType,
    ],
    queryFn: () =>
      fetchRoleUsage(client, from, to, memberIdentifiers, accountType),
    enabled: groupByDimension === "role" && memberIdentifiers.length > 0,
    throwOnError: false,
  });

  const users = useMemo(() => usersQuery.data ?? [], [usersQuery.data]);
  const roleUsage = useMemo(
    () => roleUsageQuery.data ?? [],
    [roleUsageQuery.data],
  );
  const projectMetrics = projectQuery.data ?? null;
  const timeSeries = overviewQuery.data?.timeSeries ?? [];

  // Derive total tokens from input + output — the API's totalTokens field
  // is not populated by many providers (only Anthropic reports it reliably).
  const effectiveTokens = (u: UserSummary) =>
    u.totalInputTokens + u.totalOutputTokens;

  const totalTokens = users.reduce((s, u) => s + effectiveTokens(u), 0);
  const totalCost = users.reduce((s, u) => s + u.totalCost, 0);
  const activeUsers = users.filter((u) => effectiveTokens(u) > 0).length;

  const clientBreakdown = useMemo(() => {
    const map = new Map<
      string,
      { tokens: number; cost: number; users: Set<string> }
    >();
    for (const user of users) {
      const userTotalEvents = user.hookSources.reduce(
        (s, hs) => s + hs.eventCount,
        0,
      );
      for (const hs of user.hookSources) {
        const entry = map.get(hs.source) ?? {
          tokens: 0,
          cost: 0,
          users: new Set<string>(),
        };
        entry.tokens += hs.eventCount;
        // Distribute user cost proportionally across hook sources
        if (userTotalEvents > 0) {
          entry.cost += user.totalCost * (hs.eventCount / userTotalEvents);
        }
        entry.users.add(user.userId);
        map.set(hs.source, entry);
      }
    }
    return Array.from(map.entries())
      .map(([source, data]) => ({
        source,
        label: formatPlatform(source),
        tokens: data.tokens,
        cost: data.cost,
        userCount: data.users.size,
      }))
      .sort((a, b) => b.tokens - a.tokens);
  }, [users]);

  const modelBreakdown = useMemo<ModelUsage[]>(
    () =>
      (projectMetrics?.models ?? []).slice().sort((a, b) => b.count - a.count),
    [projectMetrics],
  );

  const userRows = useMemo(
    () =>
      users
        .slice()
        .sort((a, b) =>
          valueMode === "cost"
            ? b.totalCost - a.totalCost
            : effectiveTokens(b) - effectiveTokens(a),
        )
        .map((u) => {
          const member = memberMap.get(u.userId);
          const uTokens = effectiveTokens(u);
          return {
            ...u,
            totalTokens: uTokens,
            displayName: member?.name ?? u.userId,
            email: member?.email ?? "",
            photoUrl: member?.photoUrl ?? null,
            costPerSession: u.totalChats > 0 ? u.totalCost / u.totalChats : 0,
            costShare: totalCost > 0 ? (u.totalCost / totalCost) * 100 : 0,
            tokenShare: totalTokens > 0 ? (uTokens / totalTokens) * 100 : 0,
            clients:
              u.hookSources.length > 0
                ? u.hookSources
                    .slice()
                    .sort((a, b) => b.eventCount - a.eventCount)
                    .map((hs) => formatPlatform(hs.source))
                : [],
          };
        }),
    [users, memberMap, valueMode, totalCost, totalTokens],
  );

  // Unique client sources for filter dropdown
  const availableClients = useMemo(() => {
    const sources = new Set<string>();
    for (const u of users) {
      for (const hs of u.hookSources) sources.add(hs.source);
    }
    return Array.from(sources)
      .sort()
      .map((s) => ({ value: s, label: formatPlatform(s) }));
  }, [users]);

  // Filtered rows: when a client is selected, proportionally attribute cost/tokens
  const filteredUserRows = useMemo(() => {
    if (clientFilter === "all") return userRows;

    return userRows
      .filter((u) => u.hookSources.some((hs) => hs.source === clientFilter))
      .map((u) => {
        const totalEvents = u.hookSources.reduce(
          (s, hs) => s + hs.eventCount,
          0,
        );
        const clientEvents =
          u.hookSources.find((hs) => hs.source === clientFilter)?.eventCount ??
          0;
        const ratio = totalEvents > 0 ? clientEvents / totalEvents : 0;
        const adjInput = Math.round(u.totalInputTokens * ratio);
        const adjOutput = Math.round(u.totalOutputTokens * ratio);
        const adjTokens = adjInput + adjOutput;
        const adjCost = u.totalCost * ratio;
        const adjSessions = Math.round(u.totalChats * ratio);
        return {
          ...u,
          totalTokens: adjTokens,
          totalInputTokens: adjInput,
          totalOutputTokens: adjOutput,
          totalCost: adjCost,
          totalChats: adjSessions,
          costPerSession: adjSessions > 0 ? adjCost / adjSessions : 0,
          costShare: totalCost > 0 ? (adjCost / totalCost) * 100 : 0,
          tokenShare: totalTokens > 0 ? (adjTokens / totalTokens) * 100 : 0,
        };
      })
      .sort((a, b) =>
        valueMode === "cost"
          ? b.totalCost - a.totalCost
          : b.totalTokens - a.totalTokens,
      );
  }, [userRows, clientFilter, valueMode, totalCost, totalTokens]);

  // Filtered aggregates for metric cards when a client is selected
  const filteredTotalTokens = filteredUserRows.reduce(
    (s, u) => s + u.totalTokens,
    0,
  );
  const filteredTotalCost = filteredUserRows.reduce(
    (s, u) => s + u.totalCost,
    0,
  );
  const filteredTotalSessions = filteredUserRows.reduce(
    (s, u) => s + u.totalChats,
    0,
  );
  const filteredActiveUsers = filteredUserRows.filter(
    (u) => u.totalTokens > 0,
  ).length;

  const isLoading =
    membersLoading || usersQuery.isLoading || projectQuery.isLoading;
  const error = membersError ?? usersQuery.error ?? projectQuery.error;

  const handleClearCustomRange = useCallback(() => {
    costFilters.clearValue("date");
  }, [costFilters]);
  const handleChartRangeSelect = useCallback(
    (rangeFrom: Date, rangeTo: Date) => {
      costFilters.setValue("date", {
        preset: null,
        customRange: { from: rangeFrom, to: rangeTo },
        customLabel: formatChartZoomRangeLabel(rangeFrom, rangeTo),
      });
    },
    [costFilters],
  );

  return (
    <>
      <InsightsConfig
        mcpConfig={mcpConfig}
        title="What would you like to know about AI agent costs?"
        subtitle="Ask about token spend, model costs, and usage by team or client"
        contextInfo={`Agents tab: ${activeUsers} active users, ${formatCompact(totalTokens)} tokens, ${formatCost(totalCost)} total cost in ${rangeLabel}. ${clientBreakdown.length} client types, ${modelBreakdown.length} models.`}
        suggestions={INSIGHTS_SUGGESTIONS["insights/costs"]}
      />
      <div className="min-h-0 w-full flex-1 overflow-y-auto p-8 pb-24">
        <div className="mx-auto flex max-w-7xl flex-col gap-6">
          {/* Page title, then the filter bar on its own row below it. */}
          <div className="flex flex-col gap-4">
            <div className="flex min-w-0 flex-col gap-1">
              <div className="flex items-center gap-2">
                <Heading variant="h1">AI Agent Costs</Heading>
                <ReleaseStageBadge stage="preview" />
              </div>
              <Type muted small>
                Track token consumption and costs across users, clients, and
                models over {rangeLabel}.
              </Type>
            </div>
            <Page.Toolbar>
              <Page.Toolbar.Filters
                schema={COST_FILTERS}
                values={costFilters.values}
                optionsById={
                  {
                    client: availableClients,
                    account_type: ACCOUNT_TYPE_OPTIONS,
                  } satisfies OptionsById
                }
                onChange={
                  costFilters.setValue as (
                    id: string,
                    value: FilterValue,
                  ) => void
                }
                onClear={costFilters.clearValue as (id: string) => void}
                onClearAll={costFilters.clearAll}
              />
              <Page.Toolbar.Actions>
                <ValueModeToggle mode={valueMode} onChange={setValueMode} />
              </Page.Toolbar.Actions>
              <Page.Toolbar.Refresh
                onRefresh={() => {
                  void usersQuery.refetch();
                  void projectQuery.refetch();
                  void overviewQuery.refetch();
                  void roleUsageQuery.refetch();
                }}
                isRefreshing={
                  usersQuery.isFetching ||
                  projectQuery.isFetching ||
                  overviewQuery.isFetching ||
                  roleUsageQuery.isFetching
                }
              />
            </Page.Toolbar>
          </div>

          {error ? (
            <Alert variant="error" dismissible={false}>
              <span className="font-medium">
                Unable to load agent usage data
              </span>
              <div>{error.message}</div>
            </Alert>
          ) : isLoading ? (
            <AgentsLoadingState isInsightsOpen={isInsightsOpen} />
          ) : (
            <>
              <section
                className={cn(
                  "grid gap-4 transition-all duration-300",
                  isInsightsOpen
                    ? "grid-cols-1 md:grid-cols-2"
                    : "grid-cols-1 md:grid-cols-2 lg:grid-cols-4",
                )}
              >
                <MetricCard
                  title="Total Tokens"
                  value={filteredTotalTokens}
                  subtext={`${formatCompact(filteredTotalTokens)} across ${formatCompact(filteredTotalSessions)} sessions`}
                />
                <MetricCard
                  title="Total Cost"
                  value={filteredTotalCost}
                  format="currency"
                  subtext={
                    filteredTotalCost > 0
                      ? formatCost(filteredTotalCost)
                      : "No cost data reported"
                  }
                />
                <MetricCard
                  title="Active Users"
                  value={filteredActiveUsers}
                  subtext={`of ${(membersData?.members ?? []).length} org members`}
                />
                <MetricCard
                  title="AI Clients"
                  value={clientBreakdown.length}
                  subtext={
                    clientBreakdown.length > 0
                      ? clientBreakdown.map((c) => c.label).join(", ")
                      : "No client data"
                  }
                />
              </section>

              <section
                className={cn(
                  "grid gap-4 transition-all duration-300",
                  isInsightsOpen || expandedChart
                    ? "grid-cols-1"
                    : "grid-cols-1 lg:grid-cols-2",
                )}
              >
                <TokenTimeSeriesChart
                  title={
                    valueMode === "cost" ? "Cost Over Time" : "Tokens Over Time"
                  }
                  chartId="tokens-over-time"
                  timeSeries={timeSeries}
                  valueMode={valueMode}
                  expandedChart={expandedChart}
                  onExpand={setExpandedChart}
                  onRangeSelect={handleChartRangeSelect}
                  isZoomed={customRange !== null}
                  onResetZoom={handleClearCustomRange}
                />
                <ClientBreakdownChart
                  title="Usage by Client"
                  chartId="client-breakdown"
                  data={
                    clientFilter === "all"
                      ? clientBreakdown
                      : clientBreakdown.filter((c) => c.source === clientFilter)
                  }
                  valueMode={valueMode}
                  expandedChart={expandedChart}
                  onExpand={setExpandedChart}
                />
              </section>

              <ModelBreakdownCard models={modelBreakdown} />

              <EmployeeCostTable
                users={filteredUserRows}
                roleUsage={roleUsage}
                valueMode={valueMode}
                clientFilter={clientFilter}
                groupByDimension={groupByDimension}
                onGroupByChange={setGroupByDimension}
                roleUsageLoading={roleUsageQuery.isLoading}
              />

              <CostDisclaimer providers={clientBreakdown.map((c) => c.label)} />
            </>
          )}
        </div>
      </div>
    </>
  );
}

function TokenTimeSeriesChart({
  title,
  subtitle,
  chartId,
  timeSeries,
  valueMode,
  expandedChart,
  onExpand,
  onRangeSelect,
  isZoomed,
  onResetZoom,
}: {
  title: string;
  subtitle?: string;
  chartId: string;
  timeSeries: TimeSeriesBucket[];
  valueMode: ValueMode;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
  onRangeSelect?: (from: Date, to: Date) => void;
  isZoomed?: boolean;
  onResetZoom?: () => void;
}) {
  const isExpanded = expandedChart === chartId;
  const height = isExpanded ? 420 : 260;

  const series = useMemo(
    () => buildAgentTokenTimeSeries(timeSeries, valueMode),
    [timeSeries, valueMode],
  );
  const hasData = series.some((s) => s.data.some((p) => p.y > 0));

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={hasData}
      isZoomed={isZoomed}
      onResetZoom={onResetZoom}
    >
      {subtitle && (
        <Type muted small className="-mt-3 mb-2 text-xs">
          {subtitle}
        </Type>
      )}
      <Timeseries
        series={hasData ? series : []}
        mode="bar-with-trend"
        height={height}
        valueFormatter={(v) => formatValue(v, valueMode)}
        enableZoom
        onZoomRange={onRangeSelect}
        emptyMessage="No data for selected time range"
      />
    </ChartCard>
  );
}

function ClientBreakdownChart({
  title,
  chartId,
  data,
  valueMode,
  expandedChart,
  onExpand,
}: {
  title: string;
  chartId: string;
  data: Array<{
    label: string;
    tokens: number;
    cost: number;
    userCount: number;
  }>;
  valueMode: ValueMode;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const isExpanded = expandedChart === chartId;
  const height = isExpanded ? 420 : 260;
  const hasData = data.length > 0;

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={hasData}
    >
      <StackedBarChart
        labels={data.map((d) => d.label)}
        series={[
          {
            label: valueMode === "cost" ? "Cost" : "Events",
            values: data.map((d) => (valueMode === "cost" ? d.cost : d.tokens)),
          },
        ]}
        valueFormatter={(v) => formatValue(v, valueMode)}
        height={height}
        emptyMessage="No client data available"
      />
    </ChartCard>
  );
}

function ModelBreakdownCard({ models }: { models: ModelUsage[] }) {
  const total = models.reduce((s, m) => s + m.count, 0);
  const items = useMemo<RankedBarItem[]>(
    () => models.map((model) => ({ label: model.name, value: model.count })),
    [models],
  );

  return (
    <Card>
      <Card.Header>
        <Card.Title>Requests by Model</Card.Title>
      </Card.Header>
      <Card.Content>
        {items.length > 0 ? (
          <RankedBar
            items={items}
            formatValue={(value) =>
              `${value.toLocaleString()} requests (${
                total > 0 ? ((value / total) * 100).toFixed(1) : 0
              }%)`
            }
          />
        ) : (
          <Type muted small>
            No model usage data
          </Type>
        )}
      </Card.Content>
    </Card>
  );
}

type EmployeeRow = UserSummary & {
  displayName: string;
  email: string;
  photoUrl: string | null;
  clients: string[];
  costPerSession: number;
  costShare: number;
  tokenShare: number;
};

function employeeDetailSegment(user: EmployeeRow): string {
  if (user.email) {
    return slugify(user.displayName);
  }
  if (user.userId.includes("@")) {
    return encodeURIComponent(user.userId);
  }
  return slugify(user.userId);
}

function EmployeeCostTable({
  users,
  roleUsage,
  valueMode,
  clientFilter,
  groupByDimension,
  onGroupByChange,
  roleUsageLoading,
}: {
  users: EmployeeRow[];
  roleUsage: RoleSummary[];
  valueMode: ValueMode;
  clientFilter: string;
  groupByDimension: "employee" | "role";
  onGroupByChange: (dim: "employee" | "role") => void;
  roleUsageLoading: boolean;
}) {
  const PAGE_SIZE = 10;
  const routes = useRoutes();
  const [page, setPage] = useState(0);
  const isCost = valueMode === "cost";
  const isRoleView = groupByDimension === "role";
  const [sort, setSort] = useState<SortDescriptor | null>(null);

  // Reset sort + page when switching views
  const handleGroupByChange = (dim: "employee" | "role") => {
    setSort(null);
    setPage(0);
    onGroupByChange(dim);
  };

  const defaultSort = useMemo<SortDescriptor>(
    () => ({
      id: isCost ? "cost" : "totalTokens",
      direction: "desc",
    }),
    [isCost],
  );
  const effectiveSort = sort ?? defaultSort;

  const totalRoleCost = useMemo(
    () => roleUsage.reduce((sum, r) => sum + r.totalCost, 0),
    [roleUsage],
  );

  const roleColumns = useMemo<Column<RoleSummary>[]>(
    () => [
      {
        key: "roleName",
        id: "role",
        header: "Role",
        sortable: true,
        sortValue: (role) => role.roleName.toLowerCase(),
        width: "1.4fr",
        render: (role) => (
          <div className="flex items-center gap-2">
            <span className="font-medium">{role.roleName}</span>
            {role.roleId === "unassigned" && (
              <Badge size="sm" variant="neutral">
                <Badge.Text>No role</Badge.Text>
              </Badge>
            )}
          </div>
        ),
      },
      {
        key: "userCount",
        header: "Users",
        sortable: true,
        sortValue: (role) => role.userCount,
        width: "0.8fr",
        render: (role) => (
          <span className="font-mono tabular-nums">
            {role.userCount.toLocaleString()}
          </span>
        ),
      },
      {
        key: "totalCost",
        id: "cost",
        header: "Total Cost",
        sortable: true,
        sortValue: (role) => role.totalCost,
        width: "1fr",
        render: (role) => {
          const costShare =
            totalRoleCost > 0 ? (role.totalCost / totalRoleCost) * 100 : 0;
          return (
            <div className="flex items-center gap-2">
              <span className="font-mono font-medium tabular-nums">
                {formatCost(role.totalCost)}
              </span>
              <span className="text-muted-foreground font-mono text-[10px] tabular-nums">
                {costShare.toFixed(1)}%
              </span>
            </div>
          );
        },
      },
      {
        key: "costPerUser",
        header: "Avg Cost/User",
        sortable: true,
        sortValue: (role) => role.costPerUser,
        width: "1fr",
        render: (role) => (
          <span className="text-muted-foreground font-mono tabular-nums">
            {formatCost(role.costPerUser)}
          </span>
        ),
      },
      {
        key: "totalInputTokens",
        id: "input",
        header: "Input Tokens",
        sortable: true,
        sortValue: (role) => role.totalInputTokens,
        width: "1fr",
        render: (role) => (
          <span className="font-mono tabular-nums">
            {formatCompact(role.totalInputTokens)}
          </span>
        ),
      },
      {
        key: "totalOutputTokens",
        id: "output",
        header: "Output Tokens",
        sortable: true,
        sortValue: (role) => role.totalOutputTokens,
        width: "1fr",
        render: (role) => (
          <span className="font-mono tabular-nums">
            {formatCompact(role.totalOutputTokens)}
          </span>
        ),
      },
      {
        key: "totalChats",
        id: "sessions",
        header: "Sessions",
        sortable: true,
        sortValue: (role) => role.totalChats,
        width: "0.8fr",
        render: (role) => (
          <span className="font-mono tabular-nums">
            {role.totalChats.toLocaleString()}
          </span>
        ),
      },
    ],
    [totalRoleCost],
  );

  const employeeColumns = useMemo<Column<EmployeeRow>[]>(
    () => [
      {
        key: "displayName",
        id: "employee",
        header: "Employee",
        sortable: true,
        sortValue: (user) => user.displayName.toLowerCase(),
        width: "2fr",
        render: (user) => (
          <div className="flex min-w-[200px] items-center gap-3">
            <Avatar className="size-8 shrink-0">
              {user.photoUrl ? (
                <AvatarImage src={user.photoUrl} alt={user.displayName} />
              ) : null}
              <AvatarFallback className="text-xs">
                {initials(user.displayName)}
              </AvatarFallback>
            </Avatar>
            <div className="min-w-0">
              <Type small className="truncate font-medium">
                {user.displayName}
              </Type>
              {user.email ? (
                <Type muted small className="truncate text-xs">
                  {user.email}
                </Type>
              ) : null}
              {clientFilter === "all" && user.clients.length > 0 && (
                <Type
                  small
                  className="text-muted-foreground/70 mt-0.5 text-[10px]"
                >
                  {user.clients.join(", ")}
                </Type>
              )}
            </div>
          </div>
        ),
      },
      {
        key: "totalInputTokens",
        id: "input",
        header: "Input",
        sortable: true,
        sortValue: (user) => user.totalInputTokens,
        width: "0.8fr",
        render: (user) => (
          <span className="font-mono tabular-nums">
            {formatCompact(user.totalInputTokens)}
          </span>
        ),
      },
      {
        key: "totalOutputTokens",
        id: "output",
        header: "Output",
        sortable: true,
        sortValue: (user) => user.totalOutputTokens,
        width: "0.8fr",
        render: (user) => (
          <span className="font-mono tabular-nums">
            {formatCompact(user.totalOutputTokens)}
          </span>
        ),
      },
      {
        key: "totalTokens",
        header: "Total Tokens",
        sortable: true,
        sortValue: (user) => user.totalTokens,
        width: "1fr",
        render: (user) => (
          <span
            className={cn("font-mono tabular-nums", !isCost && "font-semibold")}
          >
            {formatCompact(user.totalTokens)}
          </span>
        ),
      },
      {
        key: "totalCost",
        id: "cost",
        header: "Cost",
        sortable: true,
        sortValue: (user) => user.totalCost,
        width: "0.8fr",
        render: (user) => (
          <span
            className={cn("font-mono tabular-nums", isCost && "font-semibold")}
          >
            {formatCost(user.totalCost)}
          </span>
        ),
      },
      {
        key: "costPerSession",
        header: "$/Session",
        sortable: true,
        sortValue: (user) => user.costPerSession,
        width: "0.8fr",
        render: (user) => (
          <span className="text-muted-foreground font-mono tabular-nums">
            {formatCost(user.costPerSession)}
          </span>
        ),
      },
      {
        key: "totalChats",
        id: "sessions",
        header: "Sessions",
        sortable: true,
        sortValue: (user) => user.totalChats,
        width: "0.8fr",
        render: (user) => (
          <span className="font-mono tabular-nums">
            {user.totalChats.toLocaleString()}
          </span>
        ),
      },
      {
        key: "share",
        header: "Share",
        sortable: true,
        sortValue: (user) => (isCost ? user.costShare : user.tokenShare),
        width: "1fr",
        render: (user) => {
          const share = isCost ? user.costShare : user.tokenShare;
          return (
            <div className="flex items-center gap-2">
              <Progress value={Math.max(share, 3)} className="h-1.5 w-12" />
              <span className="text-muted-foreground font-mono tabular-nums">
                {share.toFixed(1)}%
              </span>
            </div>
          );
        },
      },
      {
        key: "userId",
        id: "employeeDetail",
        header: "",
        width: "0.6fr",
        render: (user) => (
          <Link
            to={routes.employees.detail.href(employeeDetailSegment(user))}
            className="flex items-center gap-1"
            aria-label={`View ${user.displayName}`}
          >
            View
            <ArrowRight />
          </Link>
        ),
      },
    ],
    [clientFilter, isCost, routes.employees.detail],
  );

  const sortedUsers = useMemo(
    () => sortTableData(users, employeeColumns, effectiveSort) as EmployeeRow[],
    [effectiveSort, employeeColumns, users],
  );
  const sortedRoles = useMemo(() => {
    if (sort == null && !isCost) {
      return roleUsage.slice().sort((a, b) => b.totalTokens - a.totalTokens);
    }

    return sortTableData(
      roleUsage,
      roleColumns,
      effectiveSort,
    ) as RoleSummary[];
  }, [effectiveSort, isCost, roleColumns, roleUsage, sort]);

  const items = isRoleView ? sortedRoles : sortedUsers;
  const totalPages = Math.ceil(items.length / PAGE_SIZE);
  const safePage = Math.min(page, Math.max(totalPages - 1, 0));
  const pageItems = items.slice(
    safePage * PAGE_SIZE,
    (safePage + 1) * PAGE_SIZE,
  );

  return (
    <Card>
      <Card.Header>
        <div>
          <Card.Title>
            {isCost ? "Cost" : "Usage"} by {isRoleView ? "Role" : "Employee"}
          </Card.Title>
          <Card.Description className="text-xs">
            {!isRoleView &&
              clientFilter !== "all" &&
              `Filtered to ${formatPlatform(clientFilter)} · `}
            {items.length} {isRoleView ? "role" : "employee"}
            {items.length !== 1 ? "s" : ""}
          </Card.Description>
        </div>
        <Card.Actions>
          <SegmentedControl
            value={groupByDimension}
            onChange={handleGroupByChange}
            options={[
              {
                value: "employee",
                label: "Employee",
                tooltip: "Break usage down per individual employee",
              },
              {
                value: "role",
                label: "Role",
                tooltip: "Break usage down per role",
              },
            ]}
          />
        </Card.Actions>
      </Card.Header>
      <Card.Content>
        {isRoleView ? (
          roleUsageLoading ? (
            <div className="flex items-center justify-center py-10">
              <Skeleton className="h-4 w-32" />
            </div>
          ) : (
            <Table
              columns={roleColumns}
              data={pageItems as RoleSummary[]}
              rowKey={(role) => role.roleId}
              sort={sort}
              onSortChange={(nextSort) => {
                setSort(nextSort);
                setPage(0);
              }}
              noResultsMessage="No role usage data found for this time range."
            />
          )
        ) : (
          <Table
            columns={employeeColumns}
            data={pageItems as EmployeeRow[]}
            rowKey={(user) => user.userId}
            sort={sort}
            onSortChange={(nextSort) => {
              setSort(nextSort);
              setPage(0);
            }}
            noResultsMessage="No employee activity found for this time range."
          />
        )}
      </Card.Content>
      {totalPages > 1 && (
        <Card.Footer className="border-t">
          <Type muted small>
            {safePage * PAGE_SIZE + 1}–
            {Math.min((safePage + 1) * PAGE_SIZE, items.length)} of{" "}
            {items.length}
          </Type>
          <div className="flex items-center gap-1">
            <Button
              variant="tertiary"
              size="sm"
              onClick={() => setPage((p) => p - 1)}
              disabled={safePage === 0}
            >
              Prev
            </Button>
            <Button
              variant="tertiary"
              size="sm"
              onClick={() => setPage((p) => p + 1)}
              disabled={safePage >= totalPages - 1}
            >
              Next
            </Button>
          </div>
        </Card.Footer>
      )}
    </Card>
  );
}

function CostDisclaimer({ providers }: { providers: string[] }) {
  const [dialogOpen, setDialogOpen] = useState(false);
  const [selectedProvider, setSelectedProvider] = useState("");
  const [otherProvider, setOtherProvider] = useState("");
  const telemetry = useTelemetry();

  const providerOptions = useMemo(() => {
    const unique = Array.from(new Set(providers));
    return unique;
  }, [providers]);

  const handleSubmit = () => {
    const provider =
      selectedProvider === "__other__" ? otherProvider : selectedProvider;
    if (!provider) return;
    telemetry.capture("feature_requested", {
      action: "provider_cost_support",
      provider,
    });
    toast.success("Request submitted — thanks for the feedback!");
    setDialogOpen(false);
    setSelectedProvider("");
    setOtherProvider("");
  };

  return (
    <Card>
      <Card.Header>
        <Card.Title>About cost data</Card.Title>
      </Card.Header>
      <Card.Content className="max-w-3xl space-y-1">
        <Type muted small>
          Dollar costs are reported by the AI provider. Currently only Anthropic
          (Claude) reports cost data. For other providers, use token counts to
          estimate costs. Token counts are always available regardless of
          provider.
        </Type>
        <Type muted small className="pt-1">
          Missing cost data for your provider?{" "}
          <button
            type="button"
            onClick={() => setDialogOpen(true)}
            className="text-primary hover:text-primary/80 cursor-pointer font-medium underline underline-offset-2"
          >
            Request provider support
          </button>
        </Type>
      </Card.Content>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <Dialog.Content className="sm:max-w-md">
          <Dialog.Header>
            <Dialog.Title>Request cost support</Dialog.Title>
            <Dialog.Description>
              Which provider are you missing cost data for?
            </Dialog.Description>
          </Dialog.Header>

          <RadioGroup
            value={selectedProvider}
            onValueChange={setSelectedProvider}
            className="gap-3 py-2"
          >
            {providerOptions.map((p) => (
              <label
                key={p}
                className="flex cursor-pointer items-center gap-3 text-sm"
              >
                <RadioGroupItem value={p} />
                {p}
              </label>
            ))}
            <label className="flex cursor-pointer items-center gap-3 text-sm">
              <RadioGroupItem value="__other__" />
              Other
            </label>
          </RadioGroup>

          {selectedProvider === "__other__" && (
            <Input
              type="text"
              placeholder="Provider name"
              value={otherProvider}
              onChange={(e) => setOtherProvider(e.target.value)}
            />
          )}

          <Dialog.Footer>
            <Button
              variant="brand"
              disabled={
                !selectedProvider ||
                (selectedProvider === "__other__" && !otherProvider.trim())
              }
              onClick={handleSubmit}
            >
              Submit request
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </Card>
  );
}

function AgentsLoadingState({ isInsightsOpen }: { isInsightsOpen: boolean }) {
  return (
    <>
      <section
        className={cn(
          "grid gap-4 transition-all duration-300",
          isInsightsOpen
            ? "grid-cols-1 md:grid-cols-2"
            : "grid-cols-1 md:grid-cols-2 lg:grid-cols-4",
        )}
      >
        {Array.from({ length: 4 }).map((_, i) => (
          <Card key={i}>
            <Skeleton className="mb-4 h-4 w-28" />
            <Skeleton className="h-9 w-20" />
            <Skeleton className="mt-3 h-3 w-36" />
          </Card>
        ))}
      </section>
      <section className="grid gap-4 lg:grid-cols-2">
        <Skeleton className="h-72" />
        <Skeleton className="h-72" />
      </section>
      <Skeleton className="h-40" />
      <Skeleton className="h-64" />
    </>
  );
}

async function fetchAllUsers(
  client: Parameters<typeof telemetrySearchUsers>[0],
  from: Date,
  to: Date,
  userIds: string[],
  accountType?: string,
): Promise<UserSummary[]> {
  const users: UserSummary[] = [];
  let cursor: string | undefined;
  do {
    const result = await unwrapAsync(
      telemetrySearchUsers(client, {
        searchUsersPayload: {
          cursor,
          filter: { from, to, userIds, accountType: accountType || undefined },
          limit: 1000,
          sort: "desc",
          userType: "internal",
        },
      }),
    );
    users.push(...result.users);
    cursor = result.nextCursor;
  } while (cursor);
  return users;
}

async function fetchRoleUsage(
  client: Parameters<typeof telemetrySearchUsers>[0],
  from: Date,
  to: Date,
  userIds: string[],
  accountType?: string,
): Promise<RoleSummary[]> {
  const result = await unwrapAsync(
    telemetrySearchUsers(client, {
      searchUsersPayload: {
        filter: { from, to, userIds, accountType: accountType || undefined },
        groupBy: "role",
        limit: 1000,
        sort: "desc",
        userType: "internal",
      },
    }),
  );
  return result.roles ?? [];
}

async function fetchProjectMetrics(
  client: Parameters<typeof telemetryGetProjectMetricsSummary>[0],
  from: Date,
  to: Date,
): Promise<ProjectSummary> {
  const result = await unwrapAsync(
    telemetryGetProjectMetricsSummary(client, {
      getProjectMetricsSummaryPayload: { from, to },
    }),
  );
  return result.metrics;
}

async function fetchOverview(
  client: Parameters<typeof telemetryGetObservabilityOverview>[0],
  from: Date,
  to: Date,
  hookSource?: string,
  accountType?: string,
): Promise<GetObservabilityOverviewResult> {
  return unwrapAsync(
    telemetryGetObservabilityOverview(client, {
      getObservabilityOverviewPayload: {
        from,
        to,
        includeTimeSeries: true,
        hookSource,
        accountType,
      },
    }),
  );
}
