import { ChartCard } from "@/components/chart/ChartCard";
import { formatChartLabel } from "@/components/chart/chartUtils";
import { MetricCard } from "@/components/chart/MetricCard";
import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { dateTimeFormatters } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { telemetryGetObservabilityOverview } from "@gram/client/funcs/telemetryGetObservabilityOverview";
import { telemetryGetUserMetricsSummary } from "@gram/client/funcs/telemetryGetUserMetricsSummary";
import { telemetrySearchUsers } from "@gram/client/funcs/telemetrySearchUsers";
import type {
  AccessMember,
  GetObservabilityOverviewResult,
  ProjectSummary,
  TimeSeriesBucket,
  UserSummary,
} from "@gram/client/models/components";
import { useGramContext, useMembers } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import {
  CategoryScale,
  Chart as ChartJS,
  Filler,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip,
  type ChartOptions,
} from "chart.js";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import { Line } from "react-chartjs-2";
import { useSearchParams } from "react-router";
import { useQuery } from "@tanstack/react-query";
import {
  InsightsOverviewShell,
  type InsightsContentProps,
} from "./InsightsMCP";

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Filler,
  Tooltip,
  Legend,
);

const PAGE_SIZE = 25;
const CHART_COLOR = "#60a5fa";
const USER_SOURCE_COLORS = [
  "#60a5fa",
  "#fb923c",
  "#34d399",
  "#f87171",
  "#a78bfa",
  "#facc15",
  "#22d3ee",
  "#f472b6",
  "#a3e635",
];
const LINE_CHART_HEIGHT = { collapsed: 250, expanded: 600 };
const MAX_CHART_USERS = USER_SOURCE_COLORS.length;

export function InsightsAgentsContent() {
  const {
    data: membersData,
    isLoading: membersLoading,
    error: membersError,
  } = useMembers();
  const members = useMemo(() => membersData?.members ?? [], [membersData]);
  const memberById = useMemo(
    () => new Map(members.map((member) => [member.id, member])),
    [members],
  );
  const mapFilterOptions = useCallback(
    (
      options: Array<{ id: string; label: string; count: number }>,
      dimension: string,
    ) => {
      if (dimension !== "user") return options;

      return options.map((option) => {
        const member = memberById.get(option.id);
        return member
          ? { ...option, label: member.name || member.email || option.label }
          : option;
      });
    },
    [memberById],
  );

  return (
    <InsightsOverviewShell
      noDataKind="agent_sessions"
      showMcpFilter={false}
      filterDimensions={["all", "user", "agent"]}
      userFilterType="internal"
      fixedEventSource="hook"
      mapFilterOptions={mapFilterOptions}
      title="Agent Session Usage Overview"
      subtitle="Monitor token consumption and tool usage across your team's agent sessions"
    >
      {(props) => (
        <InsightsAgentsInner
          {...props}
          members={members}
          membersLoading={membersLoading}
          membersError={membersError}
        />
      )}
    </InsightsOverviewShell>
  );
}

function InsightsAgentsInner({
  summary,
  comparison,
  comparisonLabel,
  isInsightsOpen,
  effectiveFrom,
  effectiveTo,
  timeRangeMs,
  filterDimension,
  selectedFilterValue,
  members,
  membersLoading,
  membersError,
}: InsightsContentProps & {
  members: AccessMember[];
  membersLoading: boolean;
  membersError: unknown;
}) {
  const client = useGramContext();
  const [searchParams, setSearchParams] = useSearchParams();
  const [search, setSearch] = useState("");
  const [expandedChart, setExpandedChart] = useState<string | null>(null);
  const selectedUserId = searchParams.get("user");
  const selectedTopLevelUserId =
    filterDimension === "user" ? selectedFilterValue : null;
  const selectedAgent =
    filterDimension === "agent" ? selectedFilterValue : null;
  const usersQuery = useQuery({
    queryKey: [
      "insights",
      "agents",
      "users",
      effectiveFrom.toISOString(),
      effectiveTo.toISOString(),
      selectedTopLevelUserId,
      selectedAgent,
    ],
    queryFn: () =>
      fetchUserSummaries(client, effectiveFrom, effectiveTo, {
        userId: selectedTopLevelUserId,
        hookSource: selectedAgent,
      }),
    throwOnError: false,
  });
  const userRows = useMemo(
    () => buildUserRows(usersQuery.data ?? [], members),
    [usersQuery.data, members],
  );
  const chartUsers = useMemo(() => selectChartUsers(userRows), [userRows]);
  const userTimeSeriesQuery = useQuery({
    queryKey: [
      "insights",
      "agents",
      "user-time-series",
      effectiveFrom.toISOString(),
      effectiveTo.toISOString(),
      chartUsers.map((user) => user.id),
      selectedAgent,
    ],
    queryFn: () =>
      fetchUserTimeSeries(
        client,
        effectiveFrom,
        effectiveTo,
        chartUsers,
        selectedAgent,
      ),
    enabled: chartUsers.length > 0,
    throwOnError: false,
  });
  const filteredUsers = useMemo(() => {
    const term = search.trim().toLowerCase();
    if (!term) return userRows;

    return userRows.filter((row) =>
      [row.name, row.email, row.id, row.platforms.join(" ")]
        .join(" ")
        .toLowerCase()
        .includes(term),
    );
  }, [search, userRows]);
  const selectedUser = useMemo(
    () => getSelectedUserRow(selectedUserId, userRows, members),
    [selectedUserId, userRows, members],
  );
  const isLoading = usersQuery.isLoading || membersLoading;
  const membersErrorMessage =
    membersError instanceof Error || typeof membersError === "string"
      ? membersError
      : null;
  const error = usersQuery.error ?? membersErrorMessage;
  const userTimeSeries = userTimeSeriesQuery.data ?? [];
  const isUserTimeSeriesLoading =
    usersQuery.isLoading || userTimeSeriesQuery.isLoading;

  const openUser = (userId: string) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.set("user", userId);
      return next;
    });
  };

  const closeUser = () => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.delete("user");
      return next;
    });
  };

  return (
    <>
      <section className="space-y-4">
        <h2 className="text-lg font-semibold">Usage Summary</h2>
        <div
          className={cn(
            "grid gap-4 transition-all duration-300",
            isInsightsOpen
              ? "grid-cols-1 md:grid-cols-2"
              : "grid-cols-1 md:grid-cols-3",
          )}
        >
          <MetricCard
            title="Total Sessions"
            value={summary?.totalChats ?? 0}
            previousValue={comparison?.totalChats ?? 0}
            icon="message-circle"
            comparisonLabel={comparisonLabel}
          />
          <MetricCard
            title="Total Tokens"
            value={summary?.totalTokens ?? 0}
            previousValue={comparison?.totalTokens ?? 0}
            icon="gauge"
            comparisonLabel={comparisonLabel}
          />
          <MetricCard
            title="Total Tool Calls"
            value={summary?.totalToolCalls ?? 0}
            previousValue={comparison?.totalToolCalls ?? 0}
            icon="wrench"
            comparisonLabel={comparisonLabel}
          />
        </div>
      </section>

      <div
        className={cn(
          "grid gap-4",
          expandedChart || isInsightsOpen
            ? "grid-cols-1"
            : "grid-cols-1 lg:grid-cols-2",
        )}
      >
        <UserBreakdownTimeSeriesChart
          title="Token Use Over Time"
          chartId="tokens-over-time"
          userTimeSeries={userTimeSeries}
          users={chartUsers}
          timeRangeMs={timeRangeMs}
          valueKey="totalTokens"
          valueLabel="Tokens"
          isLoading={isUserTimeSeriesLoading}
          error={userTimeSeriesQuery.error}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
        />
        <UserBreakdownTimeSeriesChart
          title="Tool Calls Over Time"
          chartId="tool-calls-over-time"
          userTimeSeries={userTimeSeries}
          users={chartUsers}
          timeRangeMs={timeRangeMs}
          valueKey="totalToolCalls"
          valueLabel="Tool calls"
          isLoading={isUserTimeSeriesLoading}
          error={userTimeSeriesQuery.error}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
        />
      </div>

      <section className="space-y-3">
        <div
          className={cn(
            "flex gap-3",
            isInsightsOpen
              ? "flex-col items-stretch"
              : "flex-col sm:flex-row sm:items-center sm:justify-between",
          )}
        >
          <div>
            <h2 className="text-lg font-semibold">Usage by Employee</h2>
            <p className="text-muted-foreground text-sm">
              External user IDs are matched to organization members by email
              when possible.
            </p>
          </div>
          <Input
            value={search}
            onChange={setSearch}
            placeholder="Search employees or platforms"
            className="sm:w-80"
          />
        </div>

        {error ? (
          <ErrorAlert title="Unable to load employee usage" error={error} />
        ) : isLoading ? (
          <UsersLoadingState />
        ) : (
          <UserUsageTable users={filteredUsers} onOpenUser={openUser} />
        )}
      </section>

      <UserDetailDialog
        user={selectedUser}
        from={effectiveFrom}
        to={effectiveTo}
        timeRangeMs={timeRangeMs}
        hookSource={selectedAgent}
        onClose={closeUser}
      />
    </>
  );
}

type UserUsageRow = {
  id: string;
  name: string;
  email: string;
  platforms: string[];
  totalTokens: number;
  totalToolCalls: number;
  toolCallSuccess: number;
  toolCallFailure: number;
  lastActivity: string;
  firstActivity: string;
  summary: UserSummary;
};

type UserTimeSeries = {
  userId: string;
  timeSeries: TimeSeriesBucket[];
};

type TimeSeriesDataset = {
  label: string;
  data: number[];
  borderColor: string;
  backgroundColor: string;
  pointBackgroundColor: string;
  fill: boolean;
  tension: number;
  borderWidth: number;
  pointRadius: number;
  pointHoverRadius: number;
};

function TokenTimeSeriesChart({
  title,
  chartId,
  timeSeries,
  timeRangeMs,
  valueKey = "totalTokens",
  label = "Tokens",
  hasData,
  expandedChart,
  onExpand,
}: {
  title: string;
  chartId: string;
  timeSeries: TimeSeriesBucket[];
  timeRangeMs: number;
  valueKey?: "totalTokens" | "totalToolCalls";
  label?: string;
  hasData: boolean;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const isExpanded = expandedChart === chartId;

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={hasData}
    >
      <SimpleLineChart
        timeSeries={timeSeries}
        timeRangeMs={timeRangeMs}
        valueKey={valueKey}
        label={label}
        hasData={hasData}
        height={isExpanded ? 420 : 220}
      />
    </ChartCard>
  );
}

function UserBreakdownTimeSeriesChart({
  title,
  chartId,
  userTimeSeries,
  users,
  timeRangeMs,
  valueKey,
  valueLabel,
  isLoading,
  error,
  expandedChart,
  onExpand,
}: {
  title: string;
  chartId: string;
  userTimeSeries: UserTimeSeries[];
  users: UserUsageRow[];
  timeRangeMs: number;
  valueKey: "totalTokens" | "totalToolCalls";
  valueLabel: string;
  isLoading: boolean;
  error: Error | null;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const isExpanded = expandedChart === chartId;
  const userLabels = useMemo(
    () => new Map(users.map((user) => [user.id, user.name || user.email])),
    [users],
  );
  const chartData = useMemo(
    () =>
      buildUserTimeSeriesChartData(
        userTimeSeries,
        userLabels,
        timeRangeMs,
        valueKey,
      ),
    [userTimeSeries, userLabels, timeRangeMs, valueKey],
  );
  const hasData = chartData.datasets.length > 0;

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={hasData || isLoading}
    >
      {error ? (
        <ErrorAlert
          title={`Unable to load ${title.toLowerCase()}`}
          error={error}
        />
      ) : isLoading ? (
        <Skeleton
          className="rounded-lg"
          style={{
            height: isExpanded
              ? LINE_CHART_HEIGHT.expanded
              : LINE_CHART_HEIGHT.collapsed,
          }}
        />
      ) : (
        <MultiLineChart
          labels={chartData.labels}
          tooltipLabels={chartData.tooltipLabels}
          datasets={chartData.datasets}
          valueLabel={valueLabel}
          height={
            isExpanded
              ? LINE_CHART_HEIGHT.expanded
              : LINE_CHART_HEIGHT.collapsed
          }
        />
      )}
    </ChartCard>
  );
}

function MultiLineChart({
  labels,
  tooltipLabels,
  datasets,
  valueLabel,
  height = LINE_CHART_HEIGHT.collapsed,
}: {
  labels: string[];
  tooltipLabels: string[];
  datasets: TimeSeriesDataset[];
  valueLabel: string;
  height?: number;
}) {
  const options = useMemo<ChartOptions<"line">>(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: "index", intersect: false },
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            title: (items) => tooltipLabels[items[0]?.dataIndex ?? 0] ?? "",
            label: (item) => {
              if ((item.parsed.y ?? 0) === 0) return undefined;
              return `${item.dataset.label}: ${Number(
                item.parsed.y ?? 0,
              ).toLocaleString()} ${valueLabel.toLowerCase()}`;
            },
          },
        },
      },
      scales: {
        x: {
          grid: { display: true, color: "rgba(128, 128, 128, 0.1)" },
          ticks: { maxTicksLimit: 8 },
        },
        y: {
          beginAtZero: true,
          grid: { color: "rgba(128, 128, 128, 0.2)" },
          ticks: { precision: 0 },
        },
      },
      transitions: {
        resize: { animation: { duration: 0 } },
      },
    }),
    [tooltipLabels, valueLabel],
  );

  if (labels.length === 0 || datasets.length === 0) {
    return (
      <div className="text-muted-foreground flex h-[220px] items-center justify-center text-sm">
        No data for selected time range
      </div>
    );
  }

  return (
    <div style={{ height }}>
      <Line data={{ labels, datasets }} options={options} />
    </div>
  );
}

function SimpleLineChart({
  timeSeries,
  timeRangeMs,
  valueKey,
  label,
  hasData,
  height = 220,
}: {
  timeSeries: TimeSeriesBucket[];
  timeRangeMs: number;
  valueKey: "totalTokens" | "totalToolCalls";
  label: string;
  hasData: boolean;
  height?: number;
}) {
  const chartData = useMemo(() => {
    const points = timeSeries.map((point) => {
      const date = unixNanoToDate(point.bucketTimeUnixNano);
      return {
        label: formatChartLabel(date, timeRangeMs),
        tooltipLabel: date.toLocaleString([], {
          month: "short",
          day: "numeric",
          hour: "numeric",
          minute: "2-digit",
        }),
        value: point[valueKey],
      };
    });

    return {
      labels: points.map((point) => point.label),
      tooltipLabels: points.map((point) => point.tooltipLabel),
      values: points.map((point) => point.value),
    };
  }, [timeSeries, timeRangeMs, valueKey]);

  const options = useMemo<ChartOptions<"line">>(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: "index", intersect: false },
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            title: (items) =>
              chartData.tooltipLabels[items[0]?.dataIndex ?? 0] ?? "",
            label: (item) =>
              `${label}: ${Number(item.parsed.y ?? 0).toLocaleString()}`,
          },
        },
      },
      scales: {
        x: {
          grid: { display: true, color: "rgba(128, 128, 128, 0.1)" },
          ticks: { maxTicksLimit: 8 },
        },
        y: {
          beginAtZero: true,
          grid: { color: "rgba(128, 128, 128, 0.2)" },
          ticks: { precision: 0 },
        },
      },
    }),
    [chartData.tooltipLabels, label],
  );

  if (!hasData) {
    return (
      <div className="text-muted-foreground flex h-[220px] items-center justify-center text-sm">
        No data for selected time range
      </div>
    );
  }

  return (
    <div style={{ height }}>
      <Line
        data={{
          labels: chartData.labels,
          datasets: [
            {
              label,
              data: chartData.values,
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
  );
}

function UserUsageTable({
  users,
  onOpenUser,
}: {
  users: UserUsageRow[];
  onOpenUser: (userId: string) => void;
}) {
  const [page, setPage] = useState(0);
  const totalPages = Math.ceil(users.length / PAGE_SIZE);
  const safePage = totalPages > 0 ? Math.min(page, totalPages - 1) : 0;
  const pageUsers = users.slice(
    safePage * PAGE_SIZE,
    (safePage + 1) * PAGE_SIZE,
  );

  return (
    <section className="bg-card rounded-xl border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Employee</TableHead>
            <TableHead>Platform(s)</TableHead>
            <TableHead>Tokens</TableHead>
            <TableHead>Tool Calls</TableHead>
            <TableHead>Last Activity</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {pageUsers.length > 0 ? (
            pageUsers.map((user) => (
              <TableRow
                key={user.id}
                className="cursor-pointer"
                onClick={() => onOpenUser(user.id)}
              >
                <TableCell>
                  <div className="flex items-center gap-3">
                    <div className="bg-muted flex size-9 items-center justify-center rounded-full text-sm font-semibold">
                      {getInitials(user.name)}
                    </div>
                    <div>
                      <p className="font-medium">{user.name}</p>
                      <p className="text-muted-foreground text-xs">
                        {user.email}
                      </p>
                    </div>
                  </div>
                </TableCell>
                <TableCell>
                  <PlatformList platforms={user.platforms} />
                </TableCell>
                <TableCell className="font-mono text-sm">
                  {user.totalTokens.toLocaleString()}
                </TableCell>
                <TableCell className="text-sm">
                  <span className="font-mono">
                    {user.totalToolCalls.toLocaleString()}
                  </span>
                  <span className="text-muted-foreground ml-2 text-xs">
                    {user.toolCallSuccess.toLocaleString()} ok /{" "}
                    {user.toolCallFailure.toLocaleString()} blocked
                  </span>
                </TableCell>
                <TableCell className="text-muted-foreground text-sm">
                  {user.lastActivity}
                </TableCell>
              </TableRow>
            ))
          ) : (
            <TableRow>
              <TableCell
                colSpan={5}
                className="text-muted-foreground py-10 text-center text-sm"
              >
                No employees found for the selected filters.
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
      {totalPages > 1 && (
        <div className="flex items-center justify-between border-t px-4 py-3">
          <p className="text-muted-foreground text-sm">
            {safePage * PAGE_SIZE + 1}–
            {Math.min((safePage + 1) * PAGE_SIZE, users.length)} of{" "}
            {users.length}
          </p>
          <div className="flex items-center gap-1">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setPage((p) => Math.max(p - 1, 0))}
              disabled={safePage === 0}
            >
              <ChevronLeft className="size-4" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setPage((p) => Math.min(p + 1, totalPages - 1))}
              disabled={safePage >= totalPages - 1}
            >
              <ChevronRight className="size-4" />
            </Button>
          </div>
        </div>
      )}
    </section>
  );
}

function UserDetailDialog({
  user,
  from,
  to,
  timeRangeMs,
  hookSource,
  onClose,
}: {
  user: UserUsageRow | null;
  from: Date;
  to: Date;
  timeRangeMs: number;
  hookSource: string | null;
  onClose: () => void;
}) {
  const client = useGramContext();
  const metricsQuery = useQuery({
    queryKey: [
      "insights",
      "agents",
      "user-metrics",
      user?.id,
      from.toISOString(),
      to.toISOString(),
      hookSource,
    ],
    queryFn: () => fetchUserMetrics(client, from, to, user!.id, hookSource),
    enabled: user != null,
    throwOnError: false,
  });
  const overviewQuery = useQuery({
    queryKey: [
      "insights",
      "agents",
      "user-overview",
      user?.id,
      from.toISOString(),
      to.toISOString(),
      hookSource,
    ],
    queryFn: () => fetchUserOverview(client, from, to, user!.id, hookSource),
    enabled: user != null,
    throwOnError: false,
  });
  const metrics = metricsQuery.data;
  const overview = overviewQuery.data;
  const detailTimeSeries = overview?.timeSeries ?? [];
  const [detailExpandedChart, setDetailExpandedChart] = useState<string | null>(
    null,
  );

  return (
    <Dialog open={user != null} onOpenChange={(open) => !open && onClose()}>
      <Dialog.Content className="flex max-h-[85vh] flex-col overflow-hidden sm:max-w-4xl">
        <Dialog.Header>
          <Dialog.Title>{user?.name ?? "Employee Usage"}</Dialog.Title>
          <Dialog.Description>
            {user?.email ?? "Detailed token and tool usage for this employee."}
          </Dialog.Description>
        </Dialog.Header>
        {user && (
          <div className="min-h-0 flex-1 space-y-5 overflow-y-auto pr-1">
            <div className="grid gap-3 sm:grid-cols-3">
              <DetailStat label="Join Date" value={user.firstActivity} />
              <DetailStat
                label="Tokens"
                value={user.totalTokens.toLocaleString()}
              />
              <DetailStat
                label="Tool Calls"
                value={`${user.toolCallSuccess.toLocaleString()} ok / ${user.toolCallFailure.toLocaleString()} blocked`}
              />
            </div>

            <section className="grid gap-4 lg:grid-cols-2">
              <BreakdownCard
                title="Platform Breakdown"
                rows={user.summary.hookSources.map((source) => ({
                  label: formatPlatform(source.source),
                  value: source.eventCount,
                  valueLabel: `${source.eventCount.toLocaleString()} events`,
                }))}
                emptyLabel="No platform data"
              />
              <BreakdownCard
                title="Top Used Tools"
                rows={user.summary.tools
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

            {metricsQuery.error ? (
              <ErrorAlert
                title="Unable to load model metrics"
                error={metricsQuery.error}
              />
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
              <ErrorAlert
                title="Unable to load user time series"
                error={overviewQuery.error}
              />
            ) : overviewQuery.isLoading ? (
              <Skeleton className="h-72 rounded-lg" />
            ) : (
              <TokenTimeSeriesChart
                title="User Token Use Over Time"
                chartId="user-tokens-over-time"
                timeSeries={detailTimeSeries}
                timeRangeMs={timeRangeMs}
                hasData={detailTimeSeries.some(
                  (point) => point.totalTokens > 0,
                )}
                expandedChart={detailExpandedChart}
                onExpand={setDetailExpandedChart}
              />
            )}
          </div>
        )}
      </Dialog.Content>
    </Dialog>
  );
}

function DetailStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border p-4">
      <p className="text-muted-foreground text-xs font-medium uppercase">
        {label}
      </p>
      <p className="mt-1 text-sm font-semibold">{value}</p>
    </div>
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

function PlatformList({ platforms }: { platforms: string[] }) {
  if (platforms.length === 0) {
    return <span className="text-muted-foreground text-sm">Unknown</span>;
  }

  return (
    <div className="flex flex-wrap gap-1.5">
      {platforms.map((platform) => (
        <span
          key={platform}
          className="bg-muted rounded-full px-2 py-0.5 text-xs font-medium"
        >
          {formatPlatform(platform)}
        </span>
      ))}
    </div>
  );
}

function UsersLoadingState() {
  return (
    <section className="bg-card rounded-xl border p-5">
      <Skeleton className="h-5 w-44" />
      <Skeleton className="mt-2 h-4 w-80" />
      <div className="mt-6 space-y-3">
        {Array.from({ length: 5 }).map((_, index) => (
          <Skeleton key={index} className="h-12 w-full" />
        ))}
      </div>
    </section>
  );
}

function selectChartUsers(users: UserUsageRow[]): UserUsageRow[] {
  const selected = new Map<string, UserUsageRow>();
  const byTokens = users.slice().sort((a, b) => b.totalTokens - a.totalTokens);
  const byToolCalls = users
    .slice()
    .sort((a, b) => b.totalToolCalls - a.totalToolCalls);

  for (const user of byTokens.slice(0, 5)) {
    if (user.totalTokens > 0) selected.set(user.id, user);
  }

  for (const user of byToolCalls) {
    if (selected.size >= MAX_CHART_USERS) break;
    if (user.totalToolCalls > 0) selected.set(user.id, user);
  }

  for (const user of byTokens) {
    if (selected.size >= MAX_CHART_USERS) break;
    if (user.totalTokens > 0 || user.totalToolCalls > 0) {
      selected.set(user.id, user);
    }
  }

  return Array.from(selected.values());
}

function buildUserTimeSeriesChartData(
  userTimeSeries: UserTimeSeries[],
  userLabels: Map<string, string>,
  timeRangeMs: number,
  valueKey: "totalTokens" | "totalToolCalls",
) {
  if (userTimeSeries.length === 0) {
    return { labels: [], tooltipLabels: [], datasets: [] };
  }

  const allTimestamps = new Set<number>();
  const valuesByUser = new Map<string, Map<number, number>>();

  for (const series of userTimeSeries) {
    const values = new Map<number, number>();
    for (const point of series.timeSeries) {
      const date = unixNanoToDate(point.bucketTimeUnixNano);
      const timestamp = date.getTime();
      allTimestamps.add(timestamp);
      values.set(timestamp, (values.get(timestamp) ?? 0) + point[valueKey]);
    }
    valuesByUser.set(series.userId, values);
  }

  const sortedTimestamps = Array.from(allTimestamps).sort((a, b) => a - b);
  const labels = sortedTimestamps.map((timestamp) =>
    formatChartLabel(new Date(timestamp), timeRangeMs),
  );
  const tooltipLabels = sortedTimestamps.map((timestamp) =>
    new Date(timestamp).toLocaleString([], {
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
    }),
  );

  const datasets = userTimeSeries
    .map((series, index) => {
      const values = valuesByUser.get(series.userId) ?? new Map();
      const data = sortedTimestamps.map(
        (timestamp) => values.get(timestamp) ?? 0,
      );
      const total = data.reduce((sum, value) => sum + value, 0);
      if (total === 0) return null;

      const color = USER_SOURCE_COLORS[index % USER_SOURCE_COLORS.length]!;
      return {
        label: userLabels.get(series.userId) ?? series.userId,
        data,
        borderColor: color,
        backgroundColor: `${color}1a`,
        pointBackgroundColor: color,
        fill: false,
        tension: 0.45,
        borderWidth: 1.5,
        pointRadius: 0,
        pointHoverRadius: 4,
      };
    })
    .filter((dataset): dataset is TimeSeriesDataset => dataset != null);

  return { labels, tooltipLabels, datasets };
}

async function fetchUserSummaries(
  client: Parameters<typeof telemetrySearchUsers>[0],
  from: Date,
  to: Date,
  filters: { userId: string | null; hookSource: string | null },
): Promise<UserSummary[]> {
  const users: UserSummary[] = [];
  let cursor: string | undefined;

  do {
    const result = await unwrapAsync(
      telemetrySearchUsers(client, {
        searchUsersPayload: {
          cursor,
          filter: {
            from,
            to,
            eventSource: "hook",
            hookSource: filters.hookSource ?? undefined,
            userIds: filters.userId ? [filters.userId] : undefined,
          },
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

async function fetchUserMetrics(
  client: Parameters<typeof telemetryGetUserMetricsSummary>[0],
  from: Date,
  to: Date,
  userId: string,
  hookSource: string | null = null,
): Promise<ProjectSummary> {
  const result = await unwrapAsync(
    telemetryGetUserMetricsSummary(client, {
      getUserMetricsSummaryPayload: {
        from,
        to,
        userId,
        eventSource: "hook",
        hookSource: hookSource ?? undefined,
      },
    }),
  );

  return result.metrics;
}

async function fetchUserTimeSeries(
  client: Parameters<typeof telemetryGetObservabilityOverview>[0],
  from: Date,
  to: Date,
  users: UserUsageRow[],
  hookSource: string | null,
): Promise<UserTimeSeries[]> {
  const result: UserTimeSeries[] = [];

  for (const user of users) {
    const overview = await fetchUserOverview(
      client,
      from,
      to,
      user.id,
      hookSource,
    );
    result.push({
      userId: user.id,
      timeSeries: overview.timeSeries,
    });
  }

  return result;
}

async function fetchUserOverview(
  client: Parameters<typeof telemetryGetObservabilityOverview>[0],
  from: Date,
  to: Date,
  userId: string,
  hookSource: string | null = null,
): Promise<GetObservabilityOverviewResult> {
  return unwrapAsync(
    telemetryGetObservabilityOverview(client, {
      getObservabilityOverviewPayload: {
        from,
        to,
        includeTimeSeries: true,
        userId,
        eventSource: "hook",
        hookSource: hookSource ?? undefined,
      },
    }),
  );
}

function buildUserRows(
  summaries: UserSummary[],
  members: AccessMember[],
): UserUsageRow[] {
  const memberById = new Map(members.map((member) => [member.id, member]));

  return summaries
    .map((summary) => {
      const member = memberById.get(summary.userId);
      const displayId = summary.userId;

      return {
        id: displayId,
        name: member?.name ?? displayId,
        email: member?.email ?? "Unknown email",
        platforms: summary.hookSources.map((source) => source.source),
        totalTokens: summary.totalTokens,
        totalToolCalls: summary.totalToolCalls,
        toolCallSuccess: summary.toolCallSuccess,
        toolCallFailure: summary.toolCallFailure,
        firstActivity: formatUnixNano(summary.firstSeenUnixNano),
        lastActivity: formatUnixNano(summary.lastSeenUnixNano),
        summary,
      };
    })
    .sort((a, b) => b.totalTokens - a.totalTokens);
}

function getSelectedUserRow(
  selectedUserId: string | null,
  userRows: UserUsageRow[],
  members: AccessMember[],
): UserUsageRow | null {
  if (!selectedUserId) return null;

  const usageRow = userRows.find((row) => row.id === selectedUserId);
  if (usageRow) return usageRow;

  const member = members.find((item) => item.id === selectedUserId);
  if (!member) return null;

  return {
    id: member.id,
    name: member.name,
    email: member.email,
    platforms: [],
    totalTokens: 0,
    totalToolCalls: 0,
    toolCallSuccess: 0,
    toolCallFailure: 0,
    firstActivity: "No activity found",
    lastActivity: "No activity found",
    summary: emptyUserSummary(member.id),
  };
}

function emptyUserSummary(userId: string): UserSummary {
  return {
    userId,
    avgTokensPerRequest: 0,
    cacheCreationInputTokens: 0,
    cacheReadInputTokens: 0,
    firstSeenUnixNano: "0",
    hookSources: [],
    lastSeenUnixNano: "0",
    toolCallFailure: 0,
    toolCallSuccess: 0,
    tools: [],
    totalChatRequests: 0,
    totalChats: 0,
    totalCost: 0,
    totalInputTokens: 0,
    totalOutputTokens: 0,
    totalTokens: 0,
    totalToolCalls: 0,
  };
}

function getInitials(name: string) {
  return name
    .split(/[ @._-]+/)
    .filter(Boolean)
    .map((part) => part[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();
}

function formatUnixNano(value: string) {
  return dateTimeFormatters.humanize(unixNanoToDate(value));
}

function unixNanoToDate(value: string) {
  const nanos = BigInt(value);
  const millis = Number(nanos / 1_000_000n);

  return new Date(millis);
}

function formatPlatform(value: string) {
  return value
    .split(/[-_]/)
    .filter(Boolean)
    .map((part) => part[0]!.toUpperCase() + part.slice(1))
    .join(" ");
}

function formatToolUrn(value: string) {
  const parts = value.split(/[/:]/).filter(Boolean);

  return parts[parts.length - 1] ?? value;
}
