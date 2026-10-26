import { formatChartZoomRangeLabel } from "@/components/chart/chartUtils";
import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { useCanViewToolLogs } from "@/components/observe/observeDeepLink";
import { encodeGatewayServerFilter } from "@/components/observe/observeTargetFilters";
import { useDateRangeFilter } from "@/components/observe/useDateRangeFilter";
import { Page } from "@/components/page-layout";
import { Icon } from "@/components/ui/Icon";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { useRoutes } from "@/routes";
import { telemetryGetMetaMcpServerUsage } from "@gram/client/funcs/telemetryGetMetaMcpServerUsage";
import type { GetMetaMcpServerUsageResult } from "@gram/client/models/components/getmetamcpserverusageresult.js";
import { useGramContext } from "@gram/client/react-query/_context";
import { unwrapAsync } from "@gram/client/types/fp";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { CallsOverTimeChart } from "./CallsOverTimeChart";
import { ExecutionShareChart } from "./ExecutionShareChart";
import {
  memberUsageRows,
  modeShares,
  positionLines,
  totalCalls,
  type MemberUsageRow,
} from "./gatewayActivity";
import { GatewayModeTiles } from "./GatewayModeTiles";
import type { MemberRow } from "./memberRows";

const memberColumns: Column<MemberUsageRow>[] = [
  {
    key: "label",
    header: "Member",
    width: "2fr",
    render: (row) => <Text className="truncate">{row.label}</Text>,
  },
  {
    key: "toolCalls",
    header: "Calls",
    width: "72px",
    render: (row) => <Text>{row.toolCalls}</Text>,
  },
  {
    key: "errorCount",
    header: "Errors",
    width: "72px",
    render: (row) => <Text>{row.errorCount}</Text>,
  },
  {
    key: "errorRate",
    header: "Error rate",
    width: "96px",
    render: (row) => <Text>{row.errorRate.toFixed(1)}%</Text>,
  },
  {
    key: "lastCalledAt",
    header: "Last call",
    // Sized to content: a localized timestamp has no fixed width.
    width: "auto",
    render: (row) => {
      const label = row.lastCalledAt
        ? row.lastCalledAt.toLocaleString([], {
            month: "short",
            day: "numeric",
            hour: "numeric",
            minute: "2-digit",
          })
        : "—";
      return (
        <Text
          muted
          className="block truncate whitespace-nowrap"
          title={row.lastCalledAt?.toLocaleString()}
        >
          {label}
        </Text>
      );
    },
  },
];

// How the gateway's calls divide between discovery (finding a tool) and
// execution (calling one), by the discovery mode each connection was served
// in, and which members the tool calls reached.
export function GatewayActivitySection({
  metaMcpServerId,
  memberRows,
  isLoadingMembers = false,
}: {
  metaMcpServerId: string;
  memberRows: MemberRow[];
  isLoadingMembers?: boolean;
}): JSX.Element {
  const client = useGramContext();
  const routes = useRoutes();
  const [searchParams] = useSearchParams();
  const [expandedChart, setExpandedChart] = useState<string | null>(null);

  const {
    dateRange,
    customRange,
    customRangeLabel,
    from,
    to,
    setDateRangeParam,
    setCustomRangeParam,
    clearCustomRange,
  } = useDateRangeFilter();
  // Logs and insights read the same range params, so links carry the range shown here.
  const gatewayScopedSearch = useMemo(() => {
    const params = new URLSearchParams();
    params.set("server", encodeGatewayServerFilter(metaMcpServerId));
    for (const key of ["range", "from", "to", "label"]) {
      const value = searchParams.get(key);
      if (value) params.set(key, value);
    }
    return `?${params.toString()}`;
  }, [metaMcpServerId, searchParams]);
  const canViewToolLogs = useCanViewToolLogs();
  const logsHref = canViewToolLogs
    ? routes.logs.href() + gatewayScopedSearch
    : undefined;
  const insightsHref = routes.insights.href() + gatewayScopedSearch;
  const handleRangeSelect = useCallback(
    (rangeFrom: Date, rangeTo: Date) =>
      setCustomRangeParam(
        rangeFrom,
        rangeTo,
        formatChartZoomRangeLabel(rangeFrom, rangeTo),
      ),
    [setCustomRangeParam],
  );

  const usage = useLogsEnabledErrorCheck(
    useQuery<GetMetaMcpServerUsageResult>({
      queryKey: [
        "gateway-usage",
        metaMcpServerId,
        from.toISOString(),
        to.toISOString(),
      ],
      queryFn: () =>
        unwrapAsync(
          telemetryGetMetaMcpServerUsage(client, {
            getMetaMcpServerUsagePayload: { metaMcpServerId, from, to },
          }),
        ),
      placeholderData: keepPreviousData,
      throwOnError: false,
    }),
  );

  // With keepPreviousData a failed refetch still has stale data to show, so
  // the section falls back to its error state only with nothing to render.
  const usageFailed = usage.isError && !usage.data;
  const loading = usage.isLoading && !usage.data;
  const phases = usage.data?.phases;
  const modes = useMemo(() => (phases ? modeShares(phases) : []), [phases]);
  const lines = useMemo(() => (phases ? positionLines(phases) : []), [phases]);
  const members = useMemo(
    () => memberUsageRows(usage.data?.members ?? [], memberRows),
    [usage.data, memberRows],
  );
  const hasCalls = phases ? totalCalls(phases) > 0 : false;

  let body: JSX.Element;
  if (usage.isLogsDisabled) {
    body = (
      <InlineEmptyState
        icon="chart-no-axes-column"
        heading="Observability is not enabled"
        description="Enable logs for this organization to see how agents use this gateway."
      />
    );
  } else if (usageFailed) {
    body = (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load gateway activity"
        description="Try refreshing the page or changing the time range."
      />
    );
  } else if (!loading && !hasCalls) {
    body = (
      <InlineEmptyState
        icon="chart-no-axes-column"
        heading="No calls through this gateway in this range"
        description="Agents' tool lookups and tool calls show here once they use the gateway. Try a longer range."
      />
    );
  } else {
    body = (
      <div className="@container flex flex-col gap-6">
        <GatewayModeTiles modes={modes} loading={loading} />
        <ExecutionShareChart
          lines={lines}
          callsWithoutSession={phases?.callsWithoutSession ?? 0}
          hasCalls={hasCalls}
          loading={loading}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
        />
        {/* Side by side only with real room: the member table needs five
            columns and the chart wants width for its bars. */}
        <div className="grid grid-cols-1 gap-6 @6xl:grid-cols-2">
          <CallsOverTimeChart
            phases={phases}
            modes={modes}
            from={from}
            to={to}
            titleHref={logsHref}
            loading={loading}
            expandedChart={expandedChart}
            onExpand={setExpandedChart}
            onRangeSelect={handleRangeSelect}
            isZoomed={customRange !== null}
            onResetZoom={() => clearCustomRange()}
          />
          <div
            className={
              expandedChart ? "hidden" : "border-border bg-card border p-4"
            }
          >
            <h3 className="text-eyebrow mb-4">
              <Link
                to={insightsHref}
                className="hover:text-foreground inline-flex items-center gap-1 no-underline hover:underline"
              >
                Tool calls
                <Icon name="arrow-up-right" className="size-3" />
              </Link>
            </h3>
            {loading || isLoadingMembers ? (
              <SkeletonTable />
            ) : (
              <Table
                columns={memberColumns}
                data={members}
                rowKey={(row) => row.mcpServerId}
                noResultsMessage={
                  <WidgetEmptyState message="No tool calls reached a member in the selected range." />
                }
              />
            )}
          </div>
        </div>
      </div>
    );
  }

  return (
    <Page.Section>
      {/* Section heading under the Overview page title: no eyebrow, smaller
          serif, matching the Members section above. */}
      <Page.Section.Title area="" className="text-display-xs">
        Activity
      </Page.Section.Title>
      <Page.Section.Description>
        How much of the gateway's traffic is agents finding tools versus calling
        them, by discovery mode. Each connection keeps the mode it was approved
        with.
      </Page.Section.Description>
      <Page.Section.CTA>
        <TimeRangePicker
          preset={customRange ? null : dateRange}
          customRange={customRange}
          customRangeLabel={customRangeLabel}
          onPresetChange={setDateRangeParam}
          onCustomRangeChange={setCustomRangeParam}
          onClearCustomRange={clearCustomRange}
        />
      </Page.Section.CTA>
      <Page.Section.Body>{body}</Page.Section.Body>
    </Page.Section>
  );
}
