import { MetricCard } from "@/components/chart/MetricCard";
import { cn } from "@/lib/utils";
import {
  InsightsOverviewShell,
  type InsightsContentProps,
  ResolvedChatsChart,
  ResolutionStatusChart,
  SessionDurationChart,
} from "./InsightsMCP";

export function InsightsAgentsContent() {
  return (
    <InsightsOverviewShell noDataKind="chats" showMcpFilter={false}>
      {(props) => <InsightsAgentsInner {...props} />}
    </InsightsOverviewShell>
  );
}

function InsightsAgentsInner({
  summary,
  comparison,
  timeSeries,
  comparisonLabel,
  isInsightsOpen,
  isRefetching,
  showNoDataOverlay,
  effectiveFrom,
  effectiveTo,
  timeRangeMs,
  onTimeRangeSelect,
}: InsightsContentProps) {
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">Chat Resolution</h2>
      <div
        className={cn(
          "grid gap-4 transition-all duration-300",
          isInsightsOpen
            ? "grid-cols-1 md:grid-cols-2"
            : "grid-cols-1 md:grid-cols-2 lg:grid-cols-4",
        )}
      >
        <MetricCard
          title="Total Chats"
          value={summary?.totalChats ?? 0}
          previousValue={comparison?.totalChats ?? 0}
          icon="message-circle"
          thresholds={{ red: 10, amber: 50 }}
          comparisonLabel={comparisonLabel}
        />
        <MetricCard
          title="Resolution Rate"
          value={
            summary?.totalChats
              ? ((summary.resolvedChats ?? 0) / summary.totalChats) * 100
              : 0
          }
          previousValue={
            comparison?.totalChats
              ? ((comparison.resolvedChats ?? 0) / comparison.totalChats) * 100
              : 0
          }
          format="percent"
          icon="circle-check"
          thresholds={{ red: 30, amber: 60 }}
          comparisonLabel={comparisonLabel}
        />
        <MetricCard
          title="Avg Session Duration"
          value={(summary?.avgSessionDurationMs ?? 0) / 1000}
          previousValue={(comparison?.avgSessionDurationMs ?? 0) / 1000}
          format="seconds"
          icon="timer"
          invertDelta
          thresholds={{ red: 300, amber: 120, inverted: true }}
          comparisonLabel={comparisonLabel}
        />
        <MetricCard
          title="Avg Resolution Time"
          value={(summary?.avgResolutionTimeMs ?? 0) / 1000}
          previousValue={(comparison?.avgResolutionTimeMs ?? 0) / 1000}
          format="seconds"
          icon="clock"
          invertDelta
          thresholds={{ red: 180, amber: 60, inverted: true }}
          comparisonLabel={comparisonLabel}
        />
      </div>
      <div
        className={cn(
          "grid gap-4",
          isInsightsOpen ? "grid-cols-1" : "grid-cols-1 lg:grid-cols-2",
        )}
      >
        <ResolvedChatsChart
          data={timeSeries}
          timeRangeMs={timeRangeMs}
          title="Resolution Rate Over Time"
          onTimeRangeSelect={onTimeRangeSelect}
          isLoading={isRefetching}
          from={effectiveFrom}
          to={effectiveTo}
          showNoData={showNoDataOverlay}
        />
        <ResolutionStatusChart
          data={timeSeries}
          timeRangeMs={timeRangeMs}
          title="Chats by Resolution Status"
          onTimeRangeSelect={onTimeRangeSelect}
          isLoading={isRefetching}
          from={effectiveFrom}
          to={effectiveTo}
          showNoData={showNoDataOverlay}
        />
      </div>
      <SessionDurationChart
        data={timeSeries}
        timeRangeMs={timeRangeMs}
        title="Avg Session Duration Over Time"
        onTimeRangeSelect={onTimeRangeSelect}
        isLoading={isRefetching}
        from={effectiveFrom}
        to={effectiveTo}
        showNoData={showNoDataOverlay}
      />
    </section>
  );
}
