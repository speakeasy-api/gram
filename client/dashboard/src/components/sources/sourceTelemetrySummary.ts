import type { ToolMetric } from "@gram/client/models/components/toolmetric.js";

export interface SourceTelemetrySummary {
  totalCalls: number;
  totalFailures: number;
  avgLatencyMs: number;
  /** Failures as a percentage of calls, 0–100. */
  errorRate: number;
}

// The overview endpoint ranks the project's ten most-called tools and takes
// no per-source filter, so a source's activity is the slice of that ranking
// its own tools occupy, in the same order — a source whose tools all rank
// lower reads as having none. The panel's copy says as much.
export function selectSourceToolMetrics(
  metrics: ToolMetric[],
  toolUrns: string[],
): ToolMetric[] {
  if (toolUrns.length === 0) return [];
  const urns = new Set(toolUrns);
  return metrics.filter((metric) => urns.has(metric.gramUrn));
}

// Aggregates per-tool metrics into the headline figures above the bar list.
// Returns null when there is no activity so the empty state can render in
// place of the summary row. Latency is weighted by call count, so a chatty
// fast tool is not averaged evenly with a rare slow one.
export function computeTelemetrySummary(
  tools: ToolMetric[],
): SourceTelemetrySummary | null {
  if (tools.length === 0) return null;
  const totalCalls = tools.reduce((sum, m) => sum + m.callCount, 0);
  const totalFailures = tools.reduce((sum, m) => sum + m.failureCount, 0);
  const avgLatencyMs =
    totalCalls > 0
      ? tools.reduce((sum, m) => sum + m.avgLatencyMs * m.callCount, 0) /
        totalCalls
      : 0;
  const errorRate = totalCalls > 0 ? (totalFailures / totalCalls) * 100 : 0;
  return { totalCalls, totalFailures, avgLatencyMs, errorRate };
}

export function formatLatency(ms: number): string {
  if (ms < 1000) return `${ms.toFixed(0)}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

// Tool URNs read `tools:http:<source>:<tool>`; the trailing segment is the
// name people know the tool by.
export function toolNameFromUrn(urn: string): string {
  const parts = urn.split(":");
  return parts[parts.length - 1] || urn;
}
