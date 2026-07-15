import type { TimeseriesSeries } from "@/components/chart/Timeseries";

function bucketStartNsToMs(bucketStartNs: string): number | null {
  try {
    const ms = Number(BigInt(bucketStartNs) / 1_000_000n);
    return Number.isFinite(ms) ? ms : null;
  } catch {
    return null;
  }
}

/**
 * Buckets a tool-usage time series into one `<Timeseries>` series per key
 * (server, user, skill, ...). Malformed bucket timestamps are skipped rather
 * than thrown on, since telemetry buckets are untrusted input.
 */
export function buildToolUsageTimeSeries<
  T extends { bucketStartNs: string; eventCount: number },
>(
  timeSeries: T[],
  keyFn: (p: T) => string,
  valueFn: (p: T) => number = (p) => p.eventCount,
): TimeseriesSeries[] {
  const seriesMap = new Map<string, Map<number, number>>();

  for (const pt of timeSeries) {
    const key = keyFn(pt);
    if (!key) continue;
    const ms = bucketStartNsToMs(pt.bucketStartNs);
    if (ms == null) continue;
    const series = seriesMap.get(key) ?? new Map<number, number>();
    series.set(ms, (series.get(ms) ?? 0) + valueFn(pt));
    seriesMap.set(key, series);
  }

  return Array.from(seriesMap.entries()).map(([key, series]) => ({
    label: key,
    data: Array.from(series.entries()).map(([x, y]) => ({ x, y })),
  }));
}
