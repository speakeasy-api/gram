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

// The project overview returns at most this many top servers.
const TOP_SERVERS_CAP = 10;

/**
 * Servers ranked by the size of their week-over-week change. The inputs are
 * top-N lists: a server missing from a short list had no calls that week, but
 * one missing from a full list may just rank lower, so it is left out.
 */
export function computeMovers(
  previous: readonly { serverName: string; toolCallCount: number }[],
  current: readonly { serverName: string; toolCallCount: number }[],
  limit = 5,
): Mover[] {
  const count = (
    week: readonly { serverName: string; toolCallCount: number }[],
    name: string,
  ): number | undefined =>
    week.find((s) => s.serverName === name)?.toolCallCount ??
    (week.length < TOP_SERVERS_CAP ? 0 : undefined);
  const names = new Set([...previous, ...current].map((s) => s.serverName));
  return [...names]
    .flatMap((name) => {
      const prev = count(previous, name);
      const curr = count(current, name);
      if (prev === undefined || curr === undefined) return [];
      return [{ name, prev, curr, delta: curr - prev }];
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
  /** All tool calls in the data. The API zero-fills empty hours. */
  total: number;
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
    total: [...cells.values()].reduce((sum, n) => sum + n, 0),
  };
}

/** Widget ids, in grid order. */
export const HOME_WIDGET_IDS = [
  "attention",
  "spend",
  "movers",
  "active",
  "heatmap",
] as const;
export type HomeWidgetId = (typeof HOME_WIDGET_IDS)[number];
