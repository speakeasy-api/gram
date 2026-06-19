import type { ToolMetric } from "@gram/client/models/components";

export interface SourceTelemetrySummary {
  totalCalls: number;
  totalFailures: number;
  avgLatency: number;
  errorRate: number;
}

// computeTelemetrySummary aggregates a per-tool metric array into the headline
// totals/averages rendered above the bar list. Returns null when there is no
// activity so the empty state can render in place of the summary row.
export function computeTelemetrySummary(
  tools: ToolMetric[],
): SourceTelemetrySummary | null {
  if (tools.length === 0) return null;
  const totalCalls = tools.reduce((sum, m) => sum + m.callCount, 0);
  const totalFailures = tools.reduce((sum, m) => sum + m.failureCount, 0);
  const avgLatency =
    totalCalls > 0
      ? tools.reduce((sum, m) => sum + m.avgLatencyMs * m.callCount, 0) /
        totalCalls
      : 0;
  const errorRate = totalCalls > 0 ? (totalFailures / totalCalls) * 100 : 0;
  return { totalCalls, totalFailures, avgLatency, errorRate };
}
