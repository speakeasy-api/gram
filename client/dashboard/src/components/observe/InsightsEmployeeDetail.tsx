import { Alert, Badge, Icon, type IconName } from "@/components/ui/moonshine";
import {
  ArrowRight,
  Boxes,
  Globe,
  KeyRound,
  type LucideIcon,
  Laptop,
  Maximize2,
} from "lucide-react";
import { formatPlatform } from "@/lib/formatPlatform";
import { ChartCard } from "@/components/chart/ChartCard";
import { formatChartLabel } from "@/components/chart/chartUtils";
import { MetricCard } from "@/components/chart/MetricCard";
import { InsightsConfig } from "@/components/insights-dock";
import { INSIGHTS_SUGGESTIONS } from "@/lib/insights-suggestions";
import { useInsightsState } from "@/components/insights-context";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Dialog } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { HookSourceIcon } from "@/pages/hooks/HookSourceIcon";
import { SessionRow } from "@/components/sessions/SessionRow";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { telemetryGetEmployeeDataFlowGraph } from "@gram/client/funcs/telemetryGetEmployeeDataFlowGraph";
import { telemetryGetObservabilityOverview } from "@gram/client/funcs/telemetryGetObservabilityOverview";
import { telemetryGetUserMetricsSummary } from "@gram/client/funcs/telemetryGetUserMetricsSummary";
import { telemetrySearchUsers } from "@gram/client/funcs/telemetrySearchUsers";
import type { EmployeeDataFlowNode } from "@gram/client/models/components/employeedataflownode.js";
import type { GetEmployeeDataFlowGraphResult } from "@gram/client/models/components/getemployeedataflowgraphresult.js";
import type { GetObservabilityOverviewResult } from "@gram/client/models/components/getobservabilityoverviewresult.js";
import type { ProjectSummary } from "@gram/client/models/components/projectsummary.js";
import type { TimeSeriesBucket } from "@gram/client/models/components/timeseriesbucket.js";
import type { UserAccount } from "@gram/client/models/components/useraccount.js";
import type { UserSummary } from "@gram/client/models/components/usersummary.js";
import { AccountRow } from "@/components/observe/account-display";
import { providerLabel } from "@/components/observe/account-display-utils";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useUserSessions } from "@gram/client/react-query/userSessions.js";
import { useRiskOverview } from "@gram/client/react-query/riskOverview.js";
import { unwrapAsync } from "@gram/client/types/fp";
import {
  TimeRangePicker,
  type DateRangePreset,
  getPresetRange,
} from "@gram-ai/elements";
import { useSlugs } from "@/contexts/Sdk";
import { formatDateRangeLabel } from "@/components/observe/useDateRangeFilter";
import {
  Chart as ChartJS,
  Filler,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip as ChartTooltip,
  type ChartOptions,
} from "chart.js";
import ZoomPlugin from "chartjs-plugin-zoom";
import { useChartZoom } from "@/components/chart/useChartZoom";
import { slugify } from "@/lib/constants";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Line } from "react-chartjs-2";
import { useParams } from "react-router";
import { useQuery } from "@tanstack/react-query";
import {
  Background,
  BaseEdge,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Position,
  ReactFlow,
  getBezierPath,
  type Edge,
  type EdgeProps,
  type Node,
  type NodeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

ChartJS.register(
  LinearScale,
  PointElement,
  LineElement,
  Filler,
  ChartTooltip,
  Legend,
  ZoomPlugin,
);

const CHART_COLOR = "#60a5fa";
const DATA_FLOW_TIER_ORDER: DataFlowTier[] = [
  "user",
  "origin",
  "client",
  "server",
  "tool",
];
const DATA_FLOW_TIER_LABELS: Record<string, string> = {
  user: "Employee",
  origin: "Origin",
  client: "MCP Client",
  server: "MCP Server",
  tool: "Tool",
};
const DATA_FLOW_TIER_ICONS: Record<string, IconName> = {
  user: "user",
  origin: "monitor",
  client: "terminal",
  server: "server",
  tool: "wrench",
};
const DATA_FLOW_TIER_TONES: Record<string, string> = {
  user: "bg-slate-500/10 text-slate-600 ring-slate-500/20",
  origin: "bg-blue-500/10 text-blue-600 ring-blue-500/20",
  client: "bg-purple-500/10 text-purple-600 ring-purple-500/20",
  server: "bg-amber-500/10 text-amber-600 ring-amber-500/20",
  tool: "bg-emerald-500/10 text-emerald-600 ring-emerald-500/20",
};
const SYNTHETIC_USER_NODE_ID = "synthetic:user";
const DATA_FLOW_TIER_MINIMAP_COLOR: Record<string, string> = {
  user: "#64748b",
  origin: "#3b82f6",
  client: "#a855f7",
  server: "#f59e0b",
  tool: "#10b981",
};
const DATA_FLOW_EDGE_COLOR = "var(--color-muted-foreground)";
const DATA_FLOW_EDGE_MARKER = {
  type: MarkerType.ArrowClosed,
  width: 9,
  height: 9,
  color: DATA_FLOW_EDGE_COLOR,
};
const DATA_FLOW_NODE_TYPES = { dataFlow: DataFlowNodeCard };
const DATA_FLOW_EDGE_TYPES = { dataFlow: DataFlowEdgeLine };

export function InsightsEmployeeDetailContent(): JSX.Element {
  const { userSlug } = useParams<{ userSlug: string }>();
  const client = useGramContext();
  const routes = useRoutes();
  const { isExpanded: isInsightsOpen } = useInsightsState();
  const mcpConfig = useObservabilityMcpConfig({
    toolsToInclude: ["gram_search_users", "gram_list_organization_users"],
  });
  const {
    data: membersData,
    isLoading: membersLoading,
    error: membersError,
  } = useMembers();
  const routeUser = useMemo(
    () => (userSlug ? decodeURIComponent(userSlug) : ""),
    [userSlug],
  );
  const members = useMemo(() => membersData?.members ?? [], [membersData]);
  const member = useMemo(
    () =>
      members.find(
        (m) => slugify(m.name) === userSlug || m.email === routeUser,
      ),
    [members, routeUser, userSlug],
  );

  const { projectSlug } = useSlugs();
  const [dateRange, setDateRange] = useState<DateRangePreset>("30d");
  const [customRange, setCustomRange] = useState<{
    from: Date;
    to: Date;
  } | null>(null);
  const [customRangeLabel, setCustomRangeLabel] = useState<string | null>(null);

  const { from, to } = useMemo(
    () => customRange ?? getPresetRange(dateRange),
    [customRange, dateRange],
  );
  const timeRangeMs = to.getTime() - from.getTime();
  const rangeLabel = useMemo(() => {
    if (customRange) return customRangeLabel ?? "the selected range";
    return formatDateRangeLabel(dateRange, null);
  }, [customRange, customRangeLabel, dateRange]);
  const employeeEmailFilter =
    member?.email ?? (routeUser.includes("@") ? routeUser : null);
  const agentSessionsHref = useMemo(() => {
    const params = new URLSearchParams();
    if (customRange) {
      params.set("from", from.toISOString());
      params.set("to", to.toISOString());
    } else {
      params.set("range", dateRange);
    }
    if (employeeEmailFilter) {
      params.set("search", employeeEmailFilter);
    }
    return `${routes.agentSessions.href()}?${params.toString()}`;
  }, [
    customRange,
    dateRange,
    employeeEmailFilter,
    from,
    routes.agentSessions,
    to,
  ]);
  const toolLogsHref = useMemo(() => {
    const params = new URLSearchParams();
    if (employeeEmailFilter) {
      params.set("user", employeeEmailFilter);
    }
    if (customRange) {
      params.set("from", from.toISOString());
      params.set("to", to.toISOString());
    } else {
      params.set("range", dateRange);
    }
    return `${routes.logs.href()}?${params.toString()}`;
  }, [customRange, dateRange, employeeEmailFilter, from, routes.logs, to]);
  const riskEventsHref = useMemo(() => {
    const params = new URLSearchParams();
    if (employeeEmailFilter) {
      params.set("user_id", employeeEmailFilter);
    }
    const query = params.toString();
    return query
      ? `${routes.riskEvents.href()}?${query}`
      : routes.riskEvents.href();
  }, [employeeEmailFilter, routes.riskEvents]);
  const riskOverviewQuery = useRiskOverview({ from, to }, undefined, {
    enabled: employeeEmailFilter != null,
    throwOnError: false,
  });
  const riskEventsCount = useMemo(() => {
    if (!employeeEmailFilter) return 0;
    const normalizedEmail = employeeEmailFilter.toLowerCase();
    return (
      riskOverviewQuery.data?.topUsers.find((user) => {
        return (
          user.email.toLowerCase() === normalizedEmail ||
          user.externalUserId.toLowerCase() === normalizedEmail
        );
      })?.findings ?? 0
    );
  }, [employeeEmailFilter, riskOverviewQuery.data?.topUsers]);

  const handlePresetChange = (preset: DateRangePreset) => {
    setDateRange(preset);
    setCustomRange(null);
    setCustomRangeLabel(null);
  };
  const handleCustomRangeChange = (
    rangeFrom: Date,
    rangeTo: Date,
    label?: string,
  ) => {
    setCustomRange({ from: rangeFrom, to: rangeTo });
    setCustomRangeLabel(label ?? null);
  };
  const handleClearCustomRange = () => {
    setCustomRange(null);
    setCustomRangeLabel(null);
  };
  const handleChartRangeSelect = useCallback(
    (from: Date, to: Date) => {
      const fmt = (d: Date) =>
        d.toLocaleString([], {
          month: "short",
          day: "numeric",
          hour: "numeric",
          minute: "2-digit",
        });
      setCustomRange({ from, to });
      setCustomRangeLabel(`${fmt(from)} – ${fmt(to)}`);
    },
    [setCustomRange, setCustomRangeLabel],
  );

  const fallbackUserQuery = useQuery({
    queryKey: [
      "insights",
      "employee-detail",
      "fallback-user",
      routeUser,
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: () => fetchMatchingUserSummary(client, from, to, routeUser),
    enabled: member == null && routeUser !== "",
    throwOnError: false,
  });
  const resolvedUserId = member?.id ?? fallbackUserQuery.data?.userId;

  // Optional per-account scoping. Empty string = the cumulative, all-accounts
  // view (default); otherwise the provider org id of a single selected account,
  // which re-scopes every query on the page to that one account.
  const [selectedOrgId, setSelectedOrgId] = useState("");
  // Reset the account scope when navigating to a different employee.
  useEffect(() => {
    setSelectedOrgId("");
  }, [resolvedUserId]);

  // Always unfiltered: this drives the accounts list/selector and the
  // cumulative view. The per-user accounts breakdown comes back regardless of
  // the account filter, so the selector stays stable across selections.
  const summaryQuery = useQuery({
    queryKey: [
      "insights",
      "employee-detail",
      "summary",
      resolvedUserId,
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: () => fetchUserSummary(client, from, to, resolvedUserId!, ""),
    enabled: resolvedUserId != null,
    throwOnError: false,
  });

  // Scoped summary for the metric cards/breakdowns when a single account is
  // selected. Only runs when an account is chosen; otherwise the cumulative
  // summaryQuery above is used.
  const scopedSummaryQuery = useQuery({
    queryKey: [
      "insights",
      "employee-detail",
      "summary",
      resolvedUserId,
      from.toISOString(),
      to.toISOString(),
      selectedOrgId,
    ],
    queryFn: () =>
      fetchUserSummary(client, from, to, resolvedUserId!, selectedOrgId),
    enabled: resolvedUserId != null && selectedOrgId !== "",
    throwOnError: false,
  });

  const metricsQuery = useQuery({
    queryKey: [
      "insights",
      "employee-detail",
      "metrics",
      resolvedUserId,
      from.toISOString(),
      to.toISOString(),
      selectedOrgId,
    ],
    queryFn: () =>
      fetchUserMetrics(client, from, to, resolvedUserId!, selectedOrgId),
    enabled: resolvedUserId != null,
    throwOnError: false,
  });

  const overviewQuery = useQuery({
    queryKey: [
      "insights",
      "employee-detail",
      "overview",
      resolvedUserId,
      from.toISOString(),
      to.toISOString(),
      selectedOrgId,
    ],
    queryFn: () =>
      fetchUserOverview(client, from, to, resolvedUserId!, selectedOrgId),
    enabled: resolvedUserId != null,
    throwOnError: false,
  });

  const dataFlowQuery = useQuery({
    queryKey: [
      "insights",
      "employee-detail",
      "data-flow",
      resolvedUserId,
      from.toISOString(),
      to.toISOString(),
      selectedOrgId,
    ],
    queryFn: () =>
      fetchEmployeeDataFlowGraph(
        client,
        from,
        to,
        resolvedUserId!,
        selectedOrgId,
      ),
    enabled: resolvedUserId != null,
    throwOnError: false,
  });

  // Accounts list/selector is driven by the unfiltered summary; the metric
  // cards switch to the scoped summary once an account is selected.
  const accountsSummary = summaryQuery.data ?? fallbackUserQuery.data ?? null;
  const accounts = accountsSummary?.accounts ?? [];
  const summary =
    selectedOrgId !== "" ? (scopedSummaryQuery.data ?? null) : accountsSummary;
  const metrics = metricsQuery.data;
  const overview = overviewQuery.data;
  const dataFlow = dataFlowQuery.data;
  const timeSeries = overview?.timeSeries ?? [];
  const [expandedChart, setExpandedChart] = useState<string | null>(null);

  const displayName =
    member?.name ?? fallbackUserQuery.data?.userId ?? routeUser ?? "Employee";
  const displayEmail =
    member?.email ??
    (resolvedUserId?.includes("@") ? resolvedUserId : "Unknown email");

  const totalTokens = getTotalTokens(summary);
  const totalCost = summary?.totalCost ?? 0;
  const isLoading =
    membersLoading ||
    (member == null && fallbackUserQuery.isLoading) ||
    (resolvedUserId != null && summaryQuery.isLoading) ||
    // When an account is scoped, the metric cards read the scoped summary — wait
    // on it too, else they briefly render zeros before it resolves.
    (selectedOrgId !== "" && scopedSummaryQuery.isLoading);
  const error =
    summaryQuery.error ??
    (selectedOrgId !== "" ? scopedSummaryQuery.error : null) ??
    fallbackUserQuery.error ??
    membersError;

  return (
    <>
      <InsightsConfig
        mcpConfig={mcpConfig}
        title={`What would you like to know about ${displayName}?`}
        subtitle="Ask about token usage, tool activity, and platform breakdown"
        suggestions={INSIGHTS_SUGGESTIONS["insights/employees/:userSlug"](
          displayName,
          displayEmail,
          rangeLabel,
        )}
      />
      <div className="min-h-0 w-full flex-1 overflow-y-auto p-8 pb-24">
        <div className="mx-auto flex max-w-7xl flex-col gap-6">
          <div
            className={cn(
              "flex gap-4 transition-all duration-300",
              isInsightsOpen
                ? "flex-col items-stretch"
                : "flex-row items-center justify-between",
            )}
          >
            <div className="flex min-w-0 items-center gap-3">
              <Avatar className="size-12">
                {member?.photoUrl && (
                  <AvatarImage src={member.photoUrl} alt={displayName} />
                )}
                <AvatarFallback className="text-base font-semibold">
                  {getInitials(displayName)}
                </AvatarFallback>
              </Avatar>
              <div className="min-w-0">
                <h1 className="font-display truncate text-2xl font-thin tracking-[-0.015em]">
                  {displayName}
                </h1>
                <p className="text-muted-foreground truncate text-sm">
                  {displayEmail}
                </p>
              </div>
            </div>
            <div
              className={cn(
                "flex items-center gap-2",
                isInsightsOpen ? "flex-wrap justify-start" : "shrink-0",
              )}
            >
              <AccountScopeSelector
                accounts={accounts}
                value={selectedOrgId}
                onChange={setSelectedOrgId}
                disabled={isLoading}
              />
              <TimeRangePicker
                preset={customRange ? null : dateRange}
                customRange={customRange}
                customRangeLabel={customRangeLabel}
                onPresetChange={handlePresetChange}
                onCustomRangeChange={handleCustomRangeChange}
                onClearCustomRange={handleClearCustomRange}
                disabled={isLoading}
                projectSlug={projectSlug}
              />
            </div>
          </div>

          {error ? (
            <Alert variant="error" dismissible={false}>
              <span className="font-medium">
                Unable to load employee usage data
              </span>
              <div>{error.message}</div>
            </Alert>
          ) : isLoading ? (
            <DetailLoadingState isInsightsOpen={isInsightsOpen} />
          ) : (
            <>
              <section
                className={cn(
                  "grid gap-4 transition-all duration-300",
                  isInsightsOpen
                    ? "grid-cols-1 md:grid-cols-2"
                    : "grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5",
                )}
              >
                <MetricCard
                  title="Total Tokens"
                  value={totalTokens}
                  icon="gauge"
                />
                <MetricCard
                  title="Total Cost"
                  value={totalCost}
                  format="currency"
                  icon="credit-card"
                  subtext={
                    totalCost > 0
                      ? `Over ${rangeLabel}`
                      : "No cost data reported"
                  }
                />
                <MetricCard
                  title="Tool Calls"
                  value={summary?.totalToolCalls ?? 0}
                  icon="wrench"
                  subtext={`${(summary?.toolCallSuccess ?? 0).toLocaleString()} succeeded / ${(summary?.toolCallFailure ?? 0).toLocaleString()} failed`}
                  link={toolLogsHref}
                />
                <MetricCard
                  title="Agent Sessions"
                  value={summary?.totalChats ?? 0}
                  icon="message-square"
                  subtext={`Over ${rangeLabel}`}
                  link={agentSessionsHref}
                />
                <MetricCard
                  title="Risk Events"
                  value={riskEventsCount}
                  displayValue={
                    riskOverviewQuery.isLoading || riskOverviewQuery.isError
                      ? "-"
                      : undefined
                  }
                  icon="flag"
                  subtext={`Over ${rangeLabel}`}
                  link={riskEventsHref}
                />
              </section>

              <section
                className={cn(
                  "grid gap-4 transition-all duration-300",
                  isInsightsOpen
                    ? "grid-cols-1"
                    : // Drop the accounts card (and its column) once a single
                      // account is selected — the breakdown is already scoped to
                      // it, so the full account list is redundant.
                      selectedOrgId === ""
                      ? "grid-cols-1 lg:grid-cols-3"
                      : "grid-cols-1 lg:grid-cols-2",
                )}
              >
                {selectedOrgId === "" && <AccountsCard accounts={accounts} />}
                <BreakdownCard
                  title="Platform Breakdown"
                  rows={(summary?.hookSources ?? []).map((source) => ({
                    label: formatPlatform(source.source),
                    value: source.eventCount,
                    valueLabel: `${source.eventCount.toLocaleString()} events`,
                  }))}
                  emptyLabel="No platform data"
                />
                <BreakdownCard
                  title="Top Used Tools"
                  rows={(summary?.tools ?? [])
                    .slice()
                    .sort((a, b) => b.count - a.count)
                    .slice(0, 8)
                    .map((tool) => ({
                      label: formatToolUrn(tool.urn),
                      value: tool.count,
                      valueLabel: `${tool.count.toLocaleString()} calls (${tool.successCount.toLocaleString()} ok / ${tool.failureCount.toLocaleString()} blocked)`,
                    }))}
                  emptyLabel="No tool calls"
                />
              </section>

              {member?.id && <EmployeeSessions userId={member.id} />}

              {dataFlowQuery.error ? (
                <Alert variant="error" dismissible={false}>
                  <span className="font-medium">
                    Unable to load employee data flow
                  </span>
                  <div>{dataFlowQuery.error.message}</div>
                </Alert>
              ) : dataFlowQuery.isLoading ? (
                <Skeleton className="h-[360px] rounded-lg" />
              ) : (
                <EmployeeDataFlowGraphCard
                  graph={dataFlow ?? { nodes: [], edges: [] }}
                  userName={displayName}
                  userPhotoUrl={member?.photoUrl ?? undefined}
                />
              )}

              {metricsQuery.error ? (
                <Alert variant="error" dismissible={false}>
                  <span className="font-medium">
                    Unable to load model metrics
                  </span>
                  <div>{metricsQuery.error.message}</div>
                </Alert>
              ) : metricsQuery.isLoading ? (
                <Skeleton className="h-40 rounded-lg" />
              ) : (
                <BreakdownCard
                  title="Model Usage"
                  rows={(metrics?.models ?? [])
                    .slice()
                    .sort((a, b) => b.count - a.count)
                    .map((model) => ({
                      label: model.name,
                      value: model.count,
                      valueLabel: `${model.count.toLocaleString()} requests`,
                    }))}
                  emptyLabel="No model usage"
                />
              )}

              {overviewQuery.error ? (
                <Alert variant="error" dismissible={false}>
                  <span className="font-medium">
                    Unable to load time series
                  </span>
                  <div>{overviewQuery.error.message}</div>
                </Alert>
              ) : overviewQuery.isLoading ? (
                <Skeleton className="h-72 rounded-lg" />
              ) : (
                <TokenTimeSeriesChart
                  title="Token Use Over Time"
                  chartId="user-tokens-over-time"
                  timeSeries={timeSeries}
                  timeRangeMs={timeRangeMs}
                  hasData={timeSeries.some(
                    (point) => getTotalTokens(point) > 0,
                  )}
                  expandedChart={expandedChart}
                  onExpand={setExpandedChart}
                  onRangeSelect={handleChartRangeSelect}
                  isZoomed={customRange !== null}
                  onResetZoom={handleClearCustomRange}
                />
              )}
            </>
          )}
        </div>
      </div>
    </>
  );
}

function EmployeeSessions({ userId }: { userId: string }): JSX.Element {
  const { data, isPending, isError, refetch } = useUserSessions({
    subjectUrn: `user:${userId}`,
    status: "active",
  });
  const sessions = data?.result.items ?? [];

  return (
    <section className="bg-card border-border rounded-lg border p-5">
      <div className="mb-3 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className="text-sm font-semibold">Active MCP Connections</span>
          {!isPending && !isError && sessions.length > 0 && (
            <span className="text-muted-foreground text-xs">
              {sessions.length}
            </span>
          )}
        </div>
        <div className="bg-muted/50 rounded-lg p-2">
          <KeyRound className="text-muted-foreground size-4" />
        </div>
      </div>
      {isPending ? (
        <Skeleton className="h-12 w-full" />
      ) : isError ? (
        <button
          type="button"
          onClick={() => void refetch()}
          className="text-destructive text-sm underline-offset-2 hover:underline"
        >
          Couldn&apos;t load sessions — retry
        </button>
      ) : sessions.length === 0 ? (
        <span className="text-muted-foreground text-sm">
          No active sessions
        </span>
      ) : (
        <ul className="divide-border max-h-80 divide-y overflow-y-auto rounded-md border">
          {sessions.map((s) => (
            <SessionRow
              key={s.id}
              session={s}
              onRevoked={() => void refetch()}
            />
          ))}
        </ul>
      )}
    </section>
  );
}

function DetailLoadingState({ isInsightsOpen }: { isInsightsOpen: boolean }) {
  return (
    <>
      <section
        className={cn(
          "grid gap-4 transition-all duration-300",
          isInsightsOpen
            ? "grid-cols-1 md:grid-cols-2"
            : "grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5",
        )}
      >
        {Array.from({ length: 5 }).map((_, index) => (
          <div key={index} className="bg-card rounded-lg border p-5">
            <Skeleton className="mb-4 h-4 w-28" />
            <Skeleton className="h-9 w-20" />
            <Skeleton className="mt-3 h-3 w-36" />
          </div>
        ))}
      </section>
      <section className="grid gap-4 lg:grid-cols-2">
        <Skeleton className="h-48 rounded-lg" />
        <Skeleton className="h-48 rounded-lg" />
      </section>
      <Skeleton className="h-40 rounded-lg" />
      <Skeleton className="h-[360px] rounded-lg" />
      <Skeleton className="h-72 rounded-lg" />
    </>
  );
}

type DataFlowTier = EmployeeDataFlowNode["tier"] | "user";

type DataFlowSourceNode = Omit<EmployeeDataFlowNode, "tier"> & {
  tier: DataFlowTier;
  photoUrl?: string;
};

type DataFlowNodeMetric = {
  value: number;
  successValue: number;
  failureValue: number;
};

type DataFlowNodeData = {
  node: DataFlowSourceNode;
  variant?: "detail" | "summary";
  tierCount?: number;
  metric?: DataFlowNodeMetric;
  serverClassCounts?: Partial<
    Record<NonNullable<EmployeeDataFlowNode["serverClass"]>, number>
  >;
};

type DataFlowEdgeData = {
  callCount: number;
  successCount: number;
  failureCount: number;
};

type DataFlowGraphNode = Node<DataFlowNodeData, "dataFlow">;
type DataFlowGraphEdge = Edge<DataFlowEdgeData, "dataFlow">;

function EmployeeDataFlowGraphCard({
  graph,
  userName,
  userPhotoUrl,
}: {
  graph: GetEmployeeDataFlowGraphResult;
  userName: string;
  userPhotoUrl?: string;
}) {
  const [expandedOpen, setExpandedOpen] = useState(false);
  const sourceGraph = useMemo(
    () => augmentGraphWithUser(graph, userName, userPhotoUrl),
    [graph, userName, userPhotoUrl],
  );
  const summaryLayout = useMemo(
    () => buildCollapsedDataFlowLayout(sourceGraph),
    [sourceGraph],
  );
  const detailLayout = useMemo(
    () => buildDataFlowLayout(sourceGraph),
    [sourceGraph],
  );
  const hasData =
    detailLayout.nodes.length > 0 && detailLayout.edges.length > 0;

  return (
    <section className="rounded-lg border p-4">
      <DataFlowEdgeAnimationStyles />
      <div className="flex items-start justify-between gap-4">
        <div>
          <h3 className="font-semibold">Data Flow</h3>
          <p className="text-muted-foreground mt-1 text-sm">
            From devices to MCP clients, servers, and the tools they use.
          </p>
        </div>
        {hasData && (
          <button
            type="button"
            onClick={() => setExpandedOpen(true)}
            className="text-muted-foreground hover:text-foreground rounded p-0.5 transition-colors"
            aria-label="Expand graph"
          >
            <Maximize2 className="size-4" />
          </button>
        )}
      </div>

      {!hasData ? (
        <div className="text-muted-foreground flex h-[280px] items-center justify-center text-sm">
          No MCP tool-call flow data for selected time range
        </div>
      ) : (
        <div className="bg-muted/20 mt-4 h-[240px] overflow-hidden rounded-lg border">
          <ReactFlow<DataFlowGraphNode, DataFlowGraphEdge>
            className="employee-data-flow-graph"
            nodes={summaryLayout.nodes}
            edges={summaryLayout.edges}
            nodeTypes={DATA_FLOW_NODE_TYPES}
            edgeTypes={DATA_FLOW_EDGE_TYPES}
            fitView
            fitViewOptions={{ padding: 0.3 }}
            minZoom={0.5}
            maxZoom={1.2}
            zoomOnScroll={false}
            zoomOnPinch={false}
            panOnScroll={false}
            panOnDrag={false}
            nodesDraggable={false}
            nodesConnectable={false}
            elementsSelectable={false}
          >
            <Background gap={24} size={1} />
          </ReactFlow>
        </div>
      )}
      <Dialog open={expandedOpen} onOpenChange={setExpandedOpen}>
        <Dialog.Content className="flex h-[90vh] max-h-[90vh] w-[calc(100vw-2rem)] max-w-[calc(100vw-2rem)] flex-col gap-4 p-4 sm:max-w-[calc(100vw-2rem)]">
          <Dialog.Header>
            <div className="flex items-start justify-between gap-4">
              <div>
                <Dialog.Title>Data Flow</Dialog.Title>
                <Dialog.Description>
                  From devices to MCP clients, servers, and the tools they use.
                </Dialog.Description>
              </div>
              <div className="mr-8 flex shrink-0 gap-2">
                <ServerClassBadge serverClass="gram" />
                <ServerClassBadge serverClass="external" />
                <ServerClassBadge serverClass="local" />
              </div>
            </div>
          </Dialog.Header>
          <div className="bg-muted/20 min-h-0 flex-1 overflow-hidden rounded-lg border">
            <ReactFlow<DataFlowGraphNode, DataFlowGraphEdge>
              className="employee-data-flow-graph"
              nodes={detailLayout.nodes}
              edges={detailLayout.edges}
              nodeTypes={DATA_FLOW_NODE_TYPES}
              edgeTypes={DATA_FLOW_EDGE_TYPES}
              fitView
              fitViewOptions={{ padding: 0.16, maxZoom: 1.25 }}
              minZoom={0.2}
              maxZoom={1.6}
              nodesDraggable={false}
              nodesConnectable={false}
              elementsSelectable={false}
            >
              <Background gap={24} size={1} />
              <MiniMap
                pannable
                zoomable
                ariaLabel="Data flow minimap"
                className="bg-card! border-border! rounded-md border"
                maskColor="hsl(0 0% 50% / 0.12)"
                nodeColor={getDataFlowMiniMapColor}
                nodeStrokeWidth={2}
              />
              <Controls showInteractive={false} />
            </ReactFlow>
          </div>
        </Dialog.Content>
      </Dialog>
    </section>
  );
}

function DataFlowEdgeAnimationStyles() {
  return (
    <style>{`
      @keyframes employee-data-flow-edge-dash {
        to {
          stroke-dashoffset: -11;
        }
      }

      /* Interactions are disabled on the graph, which makes React Flow set
         pointer-events: none on nodes. Re-enable it so node badge tooltips
         can receive hover. */
      .employee-data-flow-graph .react-flow__node {
        pointer-events: auto !important;
      }

      .employee-data-flow-graph .react-flow__handle {
        opacity: 0;
        pointer-events: none;
      }

      @media (prefers-reduced-motion: reduce) {
        .employee-data-flow-edge {
          animation: none !important;
        }
      }
    `}</style>
  );
}

function DataFlowNodeCard({ data }: NodeProps<DataFlowGraphNode>) {
  const node = data.node;
  const isSummary = data.variant === "summary";
  const isServer = node.tier === "server";
  const icon = DATA_FLOW_TIER_ICONS[node.tier] ?? "circle";
  const tone =
    DATA_FLOW_TIER_TONES[node.tier] ??
    "bg-muted text-muted-foreground ring-border";
  const serverClassCounts = data.serverClassCounts
    ? (Object.entries(data.serverClassCounts).filter(([, count]) =>
        Boolean(count),
      ) as [NonNullable<EmployeeDataFlowNode["serverClass"]>, number][])
    : [];

  return (
    <div
      className={cn(
        "bg-card/95 border-border rounded-lg border backdrop-blur",
        isSummary ? "max-w-64 min-w-56 p-4" : "max-w-56 min-w-48 p-3",
      )}
    >
      <Handle
        type="target"
        position={Position.Left}
        className="border-background! bg-muted-foreground!"
      />
      <div className="mb-2 flex items-center gap-2">
        <DataFlowNodeVisual
          node={node}
          isSummary={isSummary}
          tone={tone}
          icon={icon}
        />
        <div className="text-muted-foreground text-[11px] font-medium tracking-wide uppercase">
          {DATA_FLOW_TIER_LABELS[node.tier] ?? node.tier}
        </div>
      </div>
      <div className="truncate text-sm font-semibold" title={node.label}>
        {isSummary ? node.label : formatDataFlowNodeLabel(node)}
      </div>
      {(data.metric ||
        (isSummary ? serverClassCounts.length > 0 : isServer)) && (
        <div className="mt-2 flex flex-wrap items-center gap-1.5">
          {data.metric && <DataFlowMetricBadge metric={data.metric} />}
          {isSummary
            ? serverClassCounts.map(([serverClass, count]) => (
                <ServerClassBadge
                  key={serverClass}
                  serverClass={serverClass}
                  count={count}
                />
              ))
            : isServer && (
                <ServerClassBadge
                  serverClass={node.serverClass ?? "external"}
                />
              )}
        </div>
      )}
      <Handle
        type="source"
        position={Position.Right}
        className="border-background! bg-muted-foreground!"
      />
    </div>
  );
}

function DataFlowMetricBadge({ metric }: { metric: DataFlowNodeMetric }) {
  const tooltip = `${metric.value.toLocaleString()} calls received (${metric.successValue.toLocaleString()} ok / ${metric.failureValue.toLocaleString()} blocked)`;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Badge variant="neutral" background>
          <Badge.LeftIcon>
            <ArrowRight className="h-3.5 w-3.5" />
          </Badge.LeftIcon>
          <Badge.Text>{metric.value.toLocaleString()}</Badge.Text>
        </Badge>
      </TooltipTrigger>
      <TooltipContent>{tooltip}</TooltipContent>
    </Tooltip>
  );
}

function DataFlowNodeVisual({
  node,
  isSummary,
  tone,
  icon,
}: {
  node: DataFlowSourceNode;
  isSummary: boolean;
  tone: string;
  icon: IconName;
}) {
  if (node.tier === "user") {
    return (
      <Avatar className="size-7">
        {node.photoUrl && <AvatarImage src={node.photoUrl} alt={node.label} />}
        <AvatarFallback className="text-[10px] font-semibold">
          {getInitials(node.label)}
        </AvatarFallback>
      </Avatar>
    );
  }

  // Individual MCP client nodes show their product logo (Cursor, Claude, etc.).
  if (node.tier === "client" && !isSummary) {
    return (
      <span className="border-border bg-background inline-flex size-7 items-center justify-center rounded-md border">
        <HookSourceIcon source={node.label} className="size-4" />
      </span>
    );
  }

  return (
    <span
      className={cn(
        "inline-flex size-7 items-center justify-center rounded-md ring-1",
        tone,
      )}
    >
      {/* TODO(design-system): DynamicIcon */}
      <Icon name={icon} className="size-3.5" />
    </span>
  );
}

const SERVER_CLASS_BADGE_META: Record<
  NonNullable<EmployeeDataFlowNode["serverClass"]>,
  {
    variant: "information" | "warning" | "success";
    icon: LucideIcon;
    tooltip: string;
  }
> = {
  gram: {
    variant: "information",
    icon: Boxes,
    tooltip: "Gram-hosted MCP server",
  },
  external: {
    variant: "warning",
    icon: Globe,
    tooltip: "Third-party external MCP server",
  },
  local: {
    variant: "success",
    icon: Laptop,
    tooltip: "Local MCP server running on the employee's device",
  },
};

function ServerClassBadge({
  serverClass,
  count,
}: {
  serverClass: NonNullable<EmployeeDataFlowNode["serverClass"]>;
  count?: number;
}) {
  const meta = SERVER_CLASS_BADGE_META[serverClass];
  const ClassIcon = meta.icon;
  const tooltip =
    count !== undefined
      ? `${count.toLocaleString()} ${serverClass} ${count === 1 ? "server" : "servers"} — ${meta.tooltip}`
      : meta.tooltip;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Badge variant={meta.variant} background aria-label={meta.tooltip}>
          <Badge.LeftIcon>
            <ClassIcon className="h-3.5 w-3.5" />
          </Badge.LeftIcon>
          {count !== undefined && (
            <Badge.Text>{count.toLocaleString()}</Badge.Text>
          )}
        </Badge>
      </TooltipTrigger>
      <TooltipContent>{tooltip}</TooltipContent>
    </Tooltip>
  );
}

function DataFlowEdgeLine({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  markerEnd,
  style,
}: EdgeProps<DataFlowGraphEdge>) {
  const [edgePath] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });
  const edgeStyle = {
    ...style,
    strokeLinecap: "round" as const,
    animation: "employee-data-flow-edge-dash 900ms linear infinite",
  };

  return (
    <BaseEdge
      id={id}
      path={edgePath}
      markerEnd={markerEnd}
      style={edgeStyle}
      className="employee-data-flow-edge"
      interactionWidth={28}
    />
  );
}

type DataFlowSourceGraph = {
  nodes: DataFlowSourceNode[];
  edges: GetEmployeeDataFlowGraphResult["edges"];
};

function augmentGraphWithUser(
  graph: GetEmployeeDataFlowGraphResult,
  userName: string,
  userPhotoUrl?: string,
): DataFlowSourceGraph {
  // The backend already prunes nodes that aren't reachable from an origin, so
  // here we only attach the synthetic "user" node that fronts the origins.
  const nodes: DataFlowSourceNode[] = graph.nodes.map((node) => ({ ...node }));
  const edges = graph.edges.map((edge) => ({ ...edge }));

  const origins = nodes.filter((node) => node.tier === "origin");
  if (origins.length === 0) return { nodes, edges };

  const outcomeByOrigin = new Map<
    string,
    { success: number; failure: number }
  >();
  for (const edge of graph.edges) {
    const outcome = outcomeByOrigin.get(edge.source) ?? {
      success: 0,
      failure: 0,
    };
    outcome.success += edge.successCount;
    outcome.failure += edge.failureCount;
    outcomeByOrigin.set(edge.source, outcome);
  }

  const totalCalls = origins.reduce((sum, node) => sum + node.totalCalls, 0);
  nodes.push({
    id: SYNTHETIC_USER_NODE_ID,
    label: userName || "Employee",
    tier: "user",
    totalCalls,
    photoUrl: userPhotoUrl,
  });

  for (const origin of origins) {
    const outcome = outcomeByOrigin.get(origin.id) ?? {
      success: origin.totalCalls,
      failure: 0,
    };
    edges.push({
      id: `synthetic:user->${origin.id}`,
      source: SYNTHETIC_USER_NODE_ID,
      target: origin.id,
      callCount: origin.totalCalls,
      successCount: outcome.success,
      failureCount: outcome.failure,
    });
  }

  return { nodes, edges };
}

function buildCollapsedDataFlowLayout(graph: DataFlowSourceGraph): {
  nodes: DataFlowGraphNode[];
  edges: DataFlowGraphEdge[];
} {
  const nodesByTier = groupDataFlowNodesByTier(graph.nodes);
  const visibleTiers = DATA_FLOW_TIER_ORDER.filter((tier) =>
    nodesByTier.has(tier),
  );
  const tierXGap = 280;

  const edgeCountsByTierPair = new Map<
    string,
    { callCount: number; successCount: number; failureCount: number }
  >();
  const tierByNodeId = new Map(graph.nodes.map((node) => [node.id, node.tier]));
  for (const edge of graph.edges) {
    const sourceTier = tierByNodeId.get(edge.source);
    const targetTier = tierByNodeId.get(edge.target);
    if (!sourceTier || !targetTier || sourceTier === targetTier) continue;

    const key = getTierPairKey(sourceTier, targetTier);
    const counts = edgeCountsByTierPair.get(key) ?? {
      callCount: 0,
      successCount: 0,
      failureCount: 0,
    };
    counts.callCount += edge.callCount;
    counts.successCount += edge.successCount;
    counts.failureCount += edge.failureCount;
    edgeCountsByTierPair.set(key, counts);
  }

  const nodes: DataFlowGraphNode[] = visibleTiers.map((tier, index) => {
    const tierNodes = nodesByTier.get(tier) ?? [];
    const totalCalls = tierNodes.reduce(
      (sum, node) => sum + node.totalCalls,
      0,
    );
    const isUser = tier === "user";
    const firstNode = tierNodes[0];
    const previousTier = index > 0 ? visibleTiers[index - 1] : undefined;
    const incoming = previousTier
      ? edgeCountsByTierPair.get(getTierPairKey(previousTier, tier))
      : undefined;

    const metric: DataFlowNodeMetric | undefined =
      !isUser && incoming
        ? {
            value: incoming.callCount,
            successValue: incoming.successCount,
            failureValue: incoming.failureCount,
          }
        : undefined;

    return {
      id: getAggregateDataFlowNodeId(tier),
      type: "dataFlow",
      position: {
        x: index * tierXGap,
        y: 0,
      },
      data: {
        node: {
          id: getAggregateDataFlowNodeId(tier),
          label: isUser
            ? (firstNode?.label ?? "Employee")
            : formatAggregateTierLabel(tier, tierNodes.length),
          tier,
          totalCalls,
          photoUrl: isUser ? firstNode?.photoUrl : undefined,
        },
        variant: "summary",
        tierCount: tierNodes.length,
        metric,
        serverClassCounts:
          tier === "server" ? getServerClassCounts(tierNodes) : undefined,
      },
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
    };
  });

  const aggregateEdges = visibleTiers.slice(0, -1).map((tier, index) => {
    const nextTier = visibleTiers[index + 1]!;
    const counts = edgeCountsByTierPair.get(getTierPairKey(tier, nextTier)) ?? {
      callCount: 0,
      successCount: 0,
      failureCount: 0,
    };

    return {
      id: `aggregate:${tier}->${nextTier}`,
      source: getAggregateDataFlowNodeId(tier),
      target: getAggregateDataFlowNodeId(nextTier),
      callCount: counts.callCount,
      successCount: counts.successCount,
      failureCount: counts.failureCount,
    };
  });
  const maxCalls = Math.max(...aggregateEdges.map((edge) => edge.callCount), 1);
  const edges: DataFlowGraphEdge[] = aggregateEdges.map((edge) => ({
    id: edge.id,
    source: edge.source,
    target: edge.target,
    type: "dataFlow",
    markerEnd: DATA_FLOW_EDGE_MARKER,
    style: getDataFlowEdgeStyle(edge.callCount, maxCalls, 2),
    data: {
      callCount: edge.callCount,
      successCount: edge.successCount,
      failureCount: edge.failureCount,
    },
  }));

  return { nodes, edges };
}

function buildDataFlowLayout(graph: DataFlowSourceGraph): {
  nodes: DataFlowGraphNode[];
  edges: DataFlowGraphEdge[];
} {
  const nodesByTier = groupDataFlowNodesByTier(graph.nodes);
  const visibleTiers = DATA_FLOW_TIER_ORDER.filter((tier) =>
    nodesByTier.has(tier),
  );
  // Tiers with many nodes (typically tools) wrap into multiple sub-columns so
  // the graph stays compact instead of one very tall, sparse column.
  const maxRowsPerColumn = 6;
  const subColumnGap = 264;
  const tierGap = 336;
  const nodeYGap = 132;

  const incomingSourcesByTarget = new Map<string, string[]>();
  const incomingCallsByTarget = new Map<
    string,
    { callCount: number; successCount: number; failureCount: number }
  >();
  for (const edge of graph.edges) {
    const sources = incomingSourcesByTarget.get(edge.target) ?? [];
    sources.push(edge.source);
    incomingSourcesByTarget.set(edge.target, sources);

    const counts = incomingCallsByTarget.get(edge.target) ?? {
      callCount: 0,
      successCount: 0,
      failureCount: 0,
    };
    counts.callCount += edge.callCount;
    counts.successCount += edge.successCount;
    counts.failureCount += edge.failureCount;
    incomingCallsByTarget.set(edge.target, counts);
  }

  // Order each tier so connected nodes line up vertically with their sources
  // (barycenter heuristic), which reduces edge crossings and empty-space edges.
  const rowIndexByNode = new Map<string, number>();
  const orderedNodesByTier = new Map<string, DataFlowSourceNode[]>();
  const barycenter = (nodeId: string) => {
    const sources = incomingSourcesByTarget.get(nodeId) ?? [];
    const indices = sources
      .map((source) => rowIndexByNode.get(source))
      .filter((index): index is number => index !== undefined);
    if (indices.length === 0) return Number.POSITIVE_INFINITY;
    return indices.reduce((sum, index) => sum + index, 0) / indices.length;
  };

  visibleTiers.forEach((tier, tierIndex) => {
    const tierNodes = (nodesByTier.get(tier) ?? []).slice();
    tierNodes.sort((a, b) => {
      if (tierIndex > 0) {
        const diff = barycenter(a.id) - barycenter(b.id);
        if (Number.isFinite(diff) && diff !== 0) return diff;
      }
      return b.totalCalls - a.totalCalls || a.label.localeCompare(b.label);
    });
    tierNodes.forEach((node, index) => {
      void rowIndexByNode.set(node.id, index);
    });
    orderedNodesByTier.set(tier, tierNodes);
  });

  // Pre-compute horizontal placement for each tier, accounting for tiers that
  // wrap into multiple sub-columns (so later tiers shift right accordingly).
  let cursorX = 0;
  const placementByTier = new Map<
    string,
    { baseX: number; rowsPerColumn: number }
  >();
  for (const tier of visibleTiers) {
    const count = orderedNodesByTier.get(tier)?.length ?? 0;
    const subColumns = Math.max(1, Math.ceil(count / maxRowsPerColumn));
    const rowsPerColumn = Math.max(1, Math.ceil(count / subColumns));
    placementByTier.set(tier, { baseX: cursorX, rowsPerColumn });
    cursorX += (subColumns - 1) * subColumnGap + tierGap;
  }

  const nodes: DataFlowGraphNode[] = visibleTiers.flatMap((tier) => {
    const tierNodes = orderedNodesByTier.get(tier) ?? [];
    const placement = placementByTier.get(tier)!;
    const offset = ((placement.rowsPerColumn - 1) * nodeYGap) / 2;

    return tierNodes.map((node, index) => {
      const isUser = node.tier === "user";
      const incoming = incomingCallsByTarget.get(node.id);
      const metric: DataFlowNodeMetric | undefined =
        !isUser && incoming
          ? {
              value: incoming.callCount,
              successValue: incoming.successCount,
              failureValue: incoming.failureCount,
            }
          : undefined;

      const column = Math.floor(index / placement.rowsPerColumn);
      const row = index % placement.rowsPerColumn;

      return {
        id: node.id,
        type: "dataFlow" as const,
        position: {
          x: placement.baseX + column * subColumnGap,
          y: row * nodeYGap - offset,
        },
        data: { node, variant: "detail" as const, metric },
        sourcePosition: Position.Right,
        targetPosition: Position.Left,
      };
    });
  });

  const maxCalls = Math.max(...graph.edges.map((edge) => edge.callCount), 1);
  const edges: DataFlowGraphEdge[] = graph.edges.map((edge) => ({
    id: edge.id,
    source: edge.source,
    target: edge.target,
    type: "dataFlow",
    markerEnd: DATA_FLOW_EDGE_MARKER,
    style: getDataFlowEdgeStyle(edge.callCount, maxCalls, 1.5),
    data: {
      callCount: edge.callCount,
      successCount: edge.successCount,
      failureCount: edge.failureCount,
    },
  }));

  return { nodes, edges };
}

function getDataFlowMiniMapColor(node: DataFlowGraphNode) {
  return DATA_FLOW_TIER_MINIMAP_COLOR[node.data.node.tier] ?? "#94a3b8";
}

function getDataFlowEdgeStyle(
  callCount: number,
  maxCalls: number,
  minStrokeWidth: number,
) {
  return {
    stroke: DATA_FLOW_EDGE_COLOR,
    strokeDasharray: "5 6",
    strokeWidth: Math.max(
      minStrokeWidth,
      Math.min(3, (callCount / maxCalls) * 3),
    ),
    opacity: 1,
  };
}

function groupDataFlowNodesByTier(nodes: DataFlowSourceNode[]) {
  const nodesByTier = new Map<string, DataFlowSourceNode[]>();
  for (const node of nodes) {
    const tierNodes = nodesByTier.get(node.tier) ?? [];
    tierNodes.push(node);
    nodesByTier.set(node.tier, tierNodes);
  }

  return nodesByTier;
}

function getAggregateDataFlowNodeId(tier: string) {
  return `aggregate:${tier}`;
}

function getTierPairKey(sourceTier: string, targetTier: string) {
  return `${sourceTier}->${targetTier}`;
}

function formatAggregateTierLabel(tier: string, count: number) {
  const label = DATA_FLOW_TIER_LABELS[tier] ?? tier;
  const suffix = count === 1 ? label : pluralizeDataFlowTierLabel(label);
  return `${count.toLocaleString()} ${suffix}`;
}

function pluralizeDataFlowTierLabel(label: string) {
  if (label.endsWith("y")) return `${label.slice(0, -1)}ies`;
  return `${label}s`;
}

function getServerClassCounts(nodes: DataFlowSourceNode[]) {
  return nodes.reduce<
    Partial<Record<NonNullable<EmployeeDataFlowNode["serverClass"]>, number>>
  >((counts, node) => {
    const serverClass = node.serverClass ?? "external";
    counts[serverClass] = (counts[serverClass] ?? 0) + 1;
    return counts;
  }, {});
}

// Account scope control shown next to the date range. "All accounts" is the
// cumulative default; picking a single account re-scopes the whole page to it.
// Only accounts with a provider org id (the telemetry discriminator) can be
// scoped, so unclassifiable ones are omitted from the options.
const ALL_ACCOUNTS_VALUE = "all";

function AccountScopeSelector({
  accounts,
  value,
  onChange,
  disabled,
}: {
  accounts: UserAccount[];
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  const scopable = accounts.filter((a) => (a.externalOrgId ?? "") !== "");
  if (scopable.length === 0) return null;

  // Compact single-line label for the trigger; the dropdown list uses the full
  // two-line AccountRow (the trigger line-clamps, which would mangle it).
  const selected = scopable.find((a) => a.externalOrgId === value);
  const triggerLabel = selected
    ? `${selected.email || "(no email)"} · ${providerLabel(selected.provider)}`
    : "All accounts";

  return (
    <Select
      value={value === "" ? ALL_ACCOUNTS_VALUE : value}
      onValueChange={(v) => onChange(v === ALL_ACCOUNTS_VALUE ? "" : v)}
      disabled={disabled}
    >
      <SelectTrigger className="w-[240px]">
        <SelectValue placeholder="All accounts">{triggerLabel}</SelectValue>
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={ALL_ACCOUNTS_VALUE}>All accounts</SelectItem>
        {scopable.map((account, i) => (
          <SelectItem
            key={`${account.externalOrgId}:${i}`}
            value={account.externalOrgId!}
            // The Select wraps item content in a content-width flex-col; force it
            // full-width so AccountRow's justify-between right-aligns the badge.
            className="[&>div]:w-full"
          >
            <AccountRow
              account={{
                email: account.email ?? "",
                provider: account.provider,
                accountType: account.accountType ?? "",
              }}
              className="w-full"
            />
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

// Lists every AI account linked to this employee (team + personal, across
// providers). Mirrors the accounts popover on the employees list, expanded into
// a full card for the detail page.
function AccountsCard({ accounts }: { accounts: UserAccount[] }) {
  const display = accounts.map((a) => ({
    email: a.email ?? "",
    provider: a.provider,
    accountType: a.accountType ?? "",
  }));
  const personalCount = display.filter(
    (a) => a.accountType === "personal",
  ).length;

  return (
    <section className="rounded-lg border p-4">
      <div className="flex items-center justify-between gap-2">
        <h3 className="font-semibold">AI Accounts</h3>
        {display.length > 0 && (
          <span className="text-muted-foreground shrink-0 text-xs">
            {display.length} total
            {personalCount > 0 ? ` · ${personalCount} personal` : ""}
          </span>
        )}
      </div>
      {/* Cap the height so the next row is partially visible — a deliberate cue
          that the list scrolls — without stretching the card out of line with
          the breakdown cards beside it. */}
      <div className="mt-4 max-h-[9.5rem] space-y-3 overflow-y-auto pr-1">
        {display.length > 0 ? (
          display.map((account, i) => (
            <AccountRow
              key={`${account.provider}:${account.email}:${i}`}
              account={account}
            />
          ))
        ) : (
          <p className="text-muted-foreground text-sm">No linked accounts</p>
        )}
      </div>
    </section>
  );
}

function BreakdownCard({
  title,
  rows,
  emptyLabel,
}: {
  title: string;
  rows: Array<{ label: string; value: number; valueLabel: string }>;
  emptyLabel: string;
}) {
  const total = rows.reduce((sum, row) => sum + row.value, 0);

  return (
    <section className="rounded-lg border p-4">
      <h3 className="font-semibold">{title}</h3>
      <div className="mt-4 space-y-3">
        {rows.length > 0 ? (
          rows.map((row) => (
            <div key={row.label} className="space-y-1.5">
              <div className="flex items-center justify-between gap-3 text-sm">
                <span className="truncate">{row.label}</span>
                <span className="text-muted-foreground shrink-0">
                  {row.valueLabel}
                </span>
              </div>
              <div className="bg-muted h-2 overflow-hidden rounded-full">
                <div
                  className="bg-primary h-full rounded-full"
                  style={{
                    width: `${total > 0 ? Math.max((row.value / total) * 100, 4) : 0}%`,
                  }}
                />
              </div>
            </div>
          ))
        ) : (
          <p className="text-muted-foreground text-sm">{emptyLabel}</p>
        )}
      </div>
    </section>
  );
}

function TokenTimeSeriesChart({
  title,
  chartId,
  timeSeries,
  timeRangeMs,
  hasData,
  expandedChart,
  onExpand,
  onRangeSelect,
  isZoomed,
  onResetZoom,
}: {
  title: string;
  chartId: string;
  timeSeries: TimeSeriesBucket[];
  timeRangeMs: number;
  hasData: boolean;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
  onRangeSelect?: (from: Date, to: Date) => void;
  isZoomed?: boolean;
  onResetZoom?: () => void;
}) {
  const isExpanded = expandedChart === chartId;
  const height = isExpanded ? 420 : 220;

  const chartData = useMemo(
    () =>
      timeSeries.map((point) => ({
        x: unixNanoToDate(point.bucketTimeUnixNano).getTime(),
        y: getTotalTokens(point),
      })),
    [timeSeries],
  );

  const { chartRef, zoomPluginOptions, resetZoom } = useChartZoom({
    onRangeSelect,
  });

  useEffect(() => {
    resetZoom();
  }, [timeSeries, resetZoom]);

  const options = useMemo<ChartOptions<"line">>(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: "index", intersect: false },
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            title: (items) => {
              const x = items[0]?.parsed.x;
              if (x == null) return "";
              return new Date(x).toLocaleString([], {
                month: "short",
                day: "numeric",
                hour: "numeric",
                minute: "2-digit",
              });
            },
            label: (item) =>
              `Tokens: ${Number(item.parsed.y ?? 0).toLocaleString()}`,
          },
        },
        zoom: zoomPluginOptions,
      },
      scales: {
        x: {
          type: "linear",
          grid: { display: true, color: "rgba(128, 128, 128, 0.1)" },
          ticks: {
            maxTicksLimit: 8,
            callback: (value) =>
              formatChartLabel(new Date(value as number), timeRangeMs),
          },
        },
        y: {
          beginAtZero: true,
          grid: { color: "rgba(128, 128, 128, 0.2)" },
          ticks: { precision: 0 },
        },
      },
    }),
    [zoomPluginOptions, timeRangeMs],
  );

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
      {!hasData ? (
        <div className="text-muted-foreground flex h-[220px] items-center justify-center text-sm">
          No data for selected time range
        </div>
      ) : (
        <div style={{ height }}>
          <Line
            ref={chartRef}
            data={{
              datasets: [
                {
                  label: "Tokens",
                  data: chartData,
                  borderColor: CHART_COLOR,
                  backgroundColor: `${CHART_COLOR}1a`,
                  pointBackgroundColor: CHART_COLOR,
                  fill: true,
                  tension: 0.45,
                  borderWidth: 1.5,
                  pointRadius: 0,
                  pointHoverRadius: 4,
                },
              ],
            }}
            options={options}
          />
        </div>
      )}
    </ChartCard>
  );
}

type TokenUsageTotals = {
  totalTokens?: number | null;
  totalInputTokens?: number | null;
  totalOutputTokens?: number | null;
};

function getTotalTokens(metrics: TokenUsageTotals | null | undefined) {
  if (!metrics) return 0;
  const totalTokens = metrics.totalTokens ?? 0;
  if (totalTokens > 0) return totalTokens;
  return (metrics.totalInputTokens ?? 0) + (metrics.totalOutputTokens ?? 0);
}

function getInitials(name: string) {
  return name
    .split(" ")
    .map((part) => part[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();
}

async function fetchMatchingUserSummary(
  client: Parameters<typeof telemetrySearchUsers>[0],
  from: Date,
  to: Date,
  identifier: string,
): Promise<UserSummary | null> {
  let cursor: string | undefined;
  do {
    const result = await unwrapAsync(
      telemetrySearchUsers(client, {
        searchUsersPayload: {
          cursor,
          filter: {
            from,
            to,
          },
          limit: 1000,
          sort: "desc",
          userType: "internal",
        },
      }),
    );

    const match = result.users.find(
      (user) =>
        user.userId === identifier || slugify(user.userId) === identifier,
    );
    if (match) return match;
    cursor = result.nextCursor;
  } while (cursor);

  return null;
}

async function fetchUserSummary(
  client: Parameters<typeof telemetrySearchUsers>[0],
  from: Date,
  to: Date,
  userId: string,
  externalOrgId: string,
): Promise<UserSummary | null> {
  const result = await unwrapAsync(
    telemetrySearchUsers(client, {
      searchUsersPayload: {
        filter: {
          from,
          to,
          userIds: [userId],
          externalOrgId: externalOrgId || undefined,
        },
        limit: 1,
        sort: "desc",
        userType: "internal",
      },
    }),
  );

  return result.users[0] ?? null;
}

async function fetchUserMetrics(
  client: Parameters<typeof telemetryGetUserMetricsSummary>[0],
  from: Date,
  to: Date,
  userId: string,
  externalOrgId: string,
): Promise<ProjectSummary> {
  const result = await unwrapAsync(
    telemetryGetUserMetricsSummary(client, {
      getUserMetricsSummaryPayload: {
        from,
        to,
        userId,
        externalOrgId: externalOrgId || undefined,
      },
    }),
  );

  return result.metrics;
}

async function fetchUserOverview(
  client: Parameters<typeof telemetryGetObservabilityOverview>[0],
  from: Date,
  to: Date,
  userId: string,
  externalOrgId: string,
): Promise<GetObservabilityOverviewResult> {
  return unwrapAsync(
    telemetryGetObservabilityOverview(client, {
      getObservabilityOverviewPayload: {
        from,
        to,
        includeTimeSeries: true,
        userId,
        externalOrgId: externalOrgId || undefined,
      },
    }),
  );
}

async function fetchEmployeeDataFlowGraph(
  client: Parameters<typeof telemetryGetEmployeeDataFlowGraph>[0],
  from: Date,
  to: Date,
  userId: string,
  externalOrgId: string,
): Promise<GetEmployeeDataFlowGraphResult> {
  return unwrapAsync(
    telemetryGetEmployeeDataFlowGraph(client, {
      getEmployeeDataFlowGraphPayload: {
        from,
        to,
        userId,
        externalOrgId: externalOrgId || undefined,
      },
    }),
  );
}

function unixNanoToDate(value: string) {
  const nanos = BigInt(value);
  const millis = Number(nanos / 1_000_000n);
  return new Date(millis);
}

function formatToolUrn(value: string) {
  const parts = value.split(/[/:]/).filter(Boolean);
  return parts[parts.length - 1] ?? value;
}

function formatDataFlowNodeLabel(node: DataFlowSourceNode) {
  if (node.tier === "client") return formatPlatform(node.label);
  if (node.tier === "tool") return formatToolUrn(node.label);
  if (node.tier === "origin") return formatOriginLabel(node.label);
  if (node.tier === "server") return formatServerLabel(node);
  return node.label;
}

const UUID_LIKE_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function formatServerLabel(node: DataFlowSourceNode) {
  if (UUID_LIKE_PATTERN.test(node.label)) {
    const serverClass = node.serverClass ?? "external";
    const shortId = node.label.slice(0, 8);
    const prefix =
      serverClass === "gram"
        ? "Gram server"
        : serverClass === "local"
          ? "Local server"
          : "MCP server";
    return `${prefix} ${shortId}`;
  }
  return node.label;
}

function formatOriginLabel(value: string) {
  if (value === "local") return "local";
  if (/^https?:\/\//.test(value)) {
    try {
      return new URL(value).hostname || value;
    } catch {
      return value;
    }
  }
  return value;
}
