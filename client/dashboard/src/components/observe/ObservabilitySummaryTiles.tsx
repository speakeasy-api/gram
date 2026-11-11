import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { errorRate } from "@/lib/error-rate";
import type { ObservabilitySummary } from "@gram/client/models/components/observabilitysummary.js";

function summaryErrorRate(summary: ObservabilitySummary): number {
  return errorRate(summary.failedToolCalls, summary.totalToolCalls);
}

/**
 * The four headline tiles over an observability overview: calls, failures,
 * error rate and latency, each against the previous period.
 */
export function ObservabilitySummaryTiles({
  summary,
  comparison,
  isLoading,
  callsTitle,
}: {
  summary: ObservabilitySummary | undefined;
  comparison: ObservabilitySummary | undefined;
  isLoading: boolean;
  /** What the first tile counts, e.g. "Tool calls". */
  callsTitle: string;
}): JSX.Element {
  return (
    <StatTileGroup>
      {isLoading && !summary ? (
        Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-[116px] flex-1" />
        ))
      ) : (
        <>
          <StatTile
            title={callsTitle}
            value={summary?.totalToolCalls ?? 0}
            tone="information"
            previousValue={comparison?.totalToolCalls}
            format="compact"
            comparisonLabel="vs previous period"
          />
          <StatTile
            title="Failed calls"
            value={summary?.failedToolCalls ?? 0}
            tone={
              (summary?.failedToolCalls ?? 0) > 0 ? "destructive" : "neutral"
            }
            previousValue={comparison?.failedToolCalls}
            format="compact"
            invertDelta
            comparisonLabel="vs previous period"
          />
          <StatTile
            title="Error rate"
            value={summary ? summaryErrorRate(summary) : 0}
            previousValue={
              comparison ? summaryErrorRate(comparison) : undefined
            }
            format="percent"
            invertDelta
            thresholds={{ red: 10, amber: 5, inverted: true }}
            comparisonLabel="vs previous period"
          />
          <StatTile
            title="Avg latency"
            value={summary?.avgLatencyMs ?? 0}
            tone="information"
            previousValue={comparison?.avgLatencyMs}
            format="ms"
            invertDelta
            comparisonLabel="vs previous period"
          />
        </>
      )}
    </StatTileGroup>
  );
}

/** Stands in for an activity view when the organization has logs off. */
export function ObservabilityDisabledState({
  subject,
}: {
  /** What the usage would be for, e.g. "this gateway". */
  subject: string;
}): JSX.Element {
  return (
    <div className="flex flex-col items-center justify-center border p-12 text-center">
      <Text muted className="mb-1 block">
        Observability is not enabled
      </Text>
      <Text muted small>
        {`Enable logs for this organization to see usage for ${subject}.`}
      </Text>
    </div>
  );
}
