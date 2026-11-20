/** Pure data shaping for the homepage widgets. */

type Bucket = {
  bucketTimeUnixNano: string;
  totalCost: number;
  totalToolCalls: number;
};

function bucketDate(b: Pick<Bucket, "bucketTimeUnixNano">): Date {
  return new Date(Number(b.bucketTimeUnixNano) / 1e6);
}

/** Hourly buckets summed into local calendar days, oldest first. */
export function dailyCost(buckets: readonly Bucket[]): number[] {
  const days = new Map<string, { at: number; cost: number }>();
  for (const b of buckets) {
    const date = bucketDate(b);
    const key = date.toDateString();
    const day = days.get(key) ?? { at: date.getTime(), cost: 0 };
    days.set(key, {
      at: Math.min(day.at, date.getTime()),
      cost: day.cost + b.totalCost,
    });
  }
  return [...days.values()].sort((a, b) => a.at - b.at).map((d) => d.cost);
}

export type Mover = { name: string; prev: number; curr: number; delta: number };

/**
 * Servers ranked by the size of their week-over-week change. The inputs are
 * top-N lists, so a server missing from one week counts as zero there.
 */
export function computeMovers(
  previous: readonly { serverName: string; toolCallCount: number }[],
  current: readonly { serverName: string; toolCallCount: number }[],
  limit = 5,
): Mover[] {
  const before = new Map(previous.map((s) => [s.serverName, s.toolCallCount]));
  const after = new Map(current.map((s) => [s.serverName, s.toolCallCount]));
  return [...new Set([...before.keys(), ...after.keys()])]
    .map((name) => {
      const prev = before.get(name) ?? 0;
      const curr = after.get(name) ?? 0;
      return { name, prev, curr, delta: curr - prev };
    })
    .filter((m) => m.delta !== 0)
    .sort((a, b) => Math.abs(b.delta) - Math.abs(a.delta))
    .slice(0, limit);
}

export type Heatmap = {
  /** Keyed by `${date.toDateString()}|${hour}` in local time. */
  cells: Map<string, number>;
  /** Local calendar days present in the data, oldest first. */
  days: Date[];
  /** Busiest hour; at least 1 so it can divide. */
  max: number;
};

export function heatmapKey(day: Date, hour: number): string {
  return `${day.toDateString()}|${hour}`;
}

/** Tool calls per local day and hour. */
export function buildHeatmap(buckets: readonly Bucket[]): Heatmap {
  const cells = new Map<string, number>();
  const days = new Map<string, Date>();
  for (const b of buckets) {
    const date = bucketDate(b);
    const key = heatmapKey(date, date.getHours());
    cells.set(key, (cells.get(key) ?? 0) + b.totalToolCalls);
    days.set(date.toDateString(), new Date(date.toDateString()));
  }
  return {
    cells,
    days: [...days.values()].sort((a, b) => a.getTime() - b.getTime()),
    max: Math.max(1, ...cells.values()),
  };
}
