import { ChartLegend } from "@/components/chart/ChartLegend";
import { overviewLabels } from "@/components/chart/overviewLabels";
import { Card } from "@/components/ui/Card";
import { ChartCard } from "@/components/chart/ChartCard";
import { StackedTimeBarChart } from "@/components/chart/StackedTimeBarChart";
import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
import { useSeriesColors } from "@/components/chart/useSeriesColors";
import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import type { UserSessionTrend } from "@gram/client/models/components/usersessiontrend.js";
import { useUserSessionTrend } from "@gram/client/react-query/userSessionTrend.js";
import { useState } from "react";
import { isUnavailable, type HealthWindow } from "./connectionHealth";
import { trendChartData } from "./connectionTrend";

// Hourly buckets up to a week; a 30-day window reads by day.
const BUCKETS: Record<HealthWindow, "hour" | "day"> = {
  "24h": "hour",
  "7d": "hour",
  "30d": "day",
};

const EVENTS_CHART = "connection-trend-events";
const ACTIVE_CHART = "connection-trend-active";

/**
 * A gateway's connections over the Debug tab's window: credentials created
 * and revoked during each interval, and those active (unexpired, with no
 * recorded revocation) just before each interval ends.
 */
export function GatewayConnectionTrend({
  gatewayId,
  span,
  from,
  to,
  phrase,
}: {
  gatewayId: string;
  span: HealthWindow;
  from: Date;
  to: Date;
  phrase: string;
}): JSX.Element | null {
  const project = useProject();
  const { hasScope, isLoading } = useRBAC();
  const canRead =
    !isLoading &&
    hasScope("project:read", project.id) &&
    hasScope("mcp:read", gatewayId, project.id);
  const [expandedChart, setExpandedChart] = useState<string | null>(null);
  const trend = useUserSessionTrend(
    {
      gramProject: project.slug,
      metaMcpServerId: gatewayId,
      from,
      to,
      bucket: BUCKETS[span],
    },
    undefined,
    { enabled: canRead, throwOnError: false, retry: false },
  );

  if (!isLoading && !canRead) return null;

  // Only an answer for this exact window, and never one a failed refetch
  // left behind.
  let answered: UserSessionTrend | undefined;
  if (
    !trend.isError &&
    trend.data &&
    trend.data.from.getTime() === from.getTime() &&
    trend.data.to.getTime() === to.getTime()
  ) {
    answered = trend.data;
  }

  let body: JSX.Element;
  // Until grants resolve nothing is shown, cached answers and errors
  // included: they may be for a reader who cannot see them.
  if (isLoading) {
    body = <Skeleton className="h-64 w-full" />;
  } else if (isUnavailable(trend.error)) {
    body = (
      <WidgetEmptyState
        message="This gateway's connection history is not available to you."
        className="h-[120px]"
      />
    );
  } else if (trend.isError) {
    body = (
      <div className="flex items-center justify-between gap-3">
        <Text small className="text-destructive">
          Couldn&apos;t load connections over time.
        </Text>
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => void trend.refetch()}
        >
          Retry
        </Button>
      </div>
    );
  } else if (!answered) {
    body = <Skeleton className="h-64 w-full" />;
  } else {
    body = (
      <TrendBody
        trend={answered}
        from={from}
        to={to}
        phrase={phrase}
        expandedChart={expandedChart}
        onExpand={setExpandedChart}
      />
    );
  }

  return (
    <Card.Dashboard
      title="Connections over time"
      headerClassName="px-4 py-3"
      bodyClassName="space-y-3 p-0"
      tooltip={`Connections created and revoked ${phrase}. A refresh continues a connection, so it is neither. Active means unexpired with no recorded revocation, not that tools worked or were used.`}
    >
      {body}
    </Card.Dashboard>
  );
}

function TrendBody({
  trend,
  from,
  to,
  phrase,
  expandedChart,
  onExpand,
}: {
  trend: UserSessionTrend;
  from: Date;
  to: Date;
  phrase: string;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}): JSX.Element {
  const activeColor = useSeriesColors()[0]!;
  const charts = trendChartData(trend, from, to, activeColor);
  const quiet =
    trend.activeAtStart === 0 &&
    trend.created === 0 &&
    trend.revoked === 0 &&
    trend.buckets.every((bucket) => bucket.active === 0);
  const incomplete =
    trend.excludedLegacyRows > 0 || (trend.unattributedRows ?? 0) > 0;

  return (
    <>
      <StatTileGroup compact className="border-x-0 border-t-0">
        <StatTile
          compact
          title="Active at start"
          value={trend.activeAtStart}
          format="number"
          tone="neutral"
          tooltip="Connections unexpired, with no recorded revocation, just before the window starts."
        />
        <StatTile
          compact
          title="Created"
          value={trend.created}
          format="number"
          tone="neutral"
          tooltip="First issued in the window. Refreshes are not counted."
        />
        <StatTile
          compact
          title="Revoked"
          value={trend.revoked}
          format="number"
          tone="neutral"
          tooltip="Ended by a revocation before their deadline. Connections that reach their deadline are not counted."
        />
        <StatTile
          compact
          title="Active at end"
          value={trend.activeAtEnd}
          format="number"
          tone="neutral"
          tooltip="Connections unexpired, with no recorded revocation, just before the window ends: until their sign-in deadline if they can refresh, or until their token expires."
        />
      </StatTileGroup>
      {quiet ? (
        <WidgetEmptyState
          message={
            incomplete
              ? `No recorded connection history to chart ${phrase}.`
              : `No connections to this gateway ${phrase}.`
          }
          className="h-[120px]"
        />
      ) : (
        <div
          className={
            expandedChart ? "p-3" : "grid grid-cols-1 gap-3 p-3 lg:grid-cols-2"
          }
        >
          <ChartCard
            title="Created and revoked"
            chartId={EVENTS_CHART}
            expandedChart={expandedChart}
            onExpand={onExpand}
            hasData
          >
            <StackedTimeBarChart
              {...charts.events}
              compact={expandedChart !== EVENTS_CHART}
              labels={
                expandedChart === EVENTS_CHART
                  ? charts.events.labels
                  : overviewLabels(charts.events.labels)
              }
              height={expandedChart === EVENTS_CHART ? 420 : 120}
              expanded={expandedChart === EVENTS_CHART}
            />
            {/* The shared chart shows its legend only when expanded. */}
            {expandedChart !== EVENTS_CHART && (
              <ChartLegend datasets={charts.events.datasets} />
            )}
          </ChartCard>
          <ChartCard
            title="Active at each interval's end"
            chartId={ACTIVE_CHART}
            expandedChart={expandedChart}
            onExpand={onExpand}
            hasData
          >
            <StackedTimeBarChart
              {...charts.active}
              compact={expandedChart !== ACTIVE_CHART}
              labels={
                expandedChart === ACTIVE_CHART
                  ? charts.active.labels
                  : overviewLabels(charts.active.labels)
              }
              height={expandedChart === ACTIVE_CHART ? 420 : 120}
              expanded={expandedChart === ACTIVE_CHART}
            />
          </ChartCard>
          <Text muted small className="lg:col-span-2">
            UTC intervals of one {trend.bucket}. Active means unexpired with no
            recorded revocation; it does not mean it worked, could reach its
            tools, or was used.
          </Text>
        </div>
      )}
      {incomplete && (
        <Text muted small className="block px-4 pb-3">
          Some older connection records can&apos;t be counted, so this history
          may be incomplete.
        </Text>
      )}
    </>
  );
}
