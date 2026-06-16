import { useRBAC } from "@/hooks/useRBAC";
import { cn } from "@/lib/utils";
import { useListChats } from "@gram/client/react-query";
import { useMemo } from "react";

const WINDOW_DAYS = 14;
const WIDTH = 56;
const HEIGHT = 16;
const SESSION_SAMPLE = 50;

type DayEvent = { date: Date; weight: number };

/** Local calendar-day index (days since the Unix epoch in local time). */
function dayIndex(date: Date): number {
  return Math.floor(
    new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime() /
      (24 * 60 * 60 * 1000),
  );
}

/**
 * Bucket weighted events into per-day totals for the trailing `days`, oldest
 * day first.
 */
function bucketByDay(events: DayEvent[], days: number): number[] {
  const counts = Array.from({ length: days }, () => 0);
  const today = dayIndex(new Date());
  for (const { date, weight } of events) {
    // Bucket by local calendar day so the curve aligns with day boundaries
    // (a rolling 24h age would smear events across the wrong day).
    const ageDays = today - dayIndex(date);
    if (ageDays < 0 || ageDays >= days) continue;
    // index 0 = oldest day in the window, days-1 = today
    const idx = days - 1 - ageDays;
    counts[idx] = (counts[idx] ?? 0) + weight;
  }
  return counts;
}

/**
 * A compact inline sparkline of an assistant's recent activity, derived from
 * its chat sessions. Any chat counts as activity — tool calls happen inside
 * sessions, so this is the inclusive signal (an assistant that only ever
 * answers questions still shows a curve). Each session contributes its message
 * count (min 1) to the day of its last activity. Rendered as hand-rolled SVG
 * rather than a chart library so each card carries a near-zero-cost graphic.
 *
 * NOTE: reads the most recent `SESSION_SAMPLE` sessions per assistant, so it
 * reflects recent activity rather than a guaranteed full window — adequate for
 * an at-a-glance card. A high-traffic project would warrant a server-side
 * bucketed endpoint instead of N per-card session queries.
 */
export function AssistantActivitySparkline({
  assistantId,
  className,
}: {
  assistantId: string;
  className?: string;
}): JSX.Element | null {
  const { hasScope } = useRBAC();
  const canRead = hasScope("project:read");

  const { data } = useListChats(
    {
      assistantId,
      sortBy: "last_message_timestamp",
      sortOrder: "desc",
      limit: SESSION_SAMPLE,
    },
    undefined,
    { enabled: canRead, retry: false, throwOnError: false },
  );

  const counts = useMemo(() => {
    const events: DayEvent[] = (data?.chats ?? []).map((chat) => ({
      date: new Date(chat.lastMessageTimestamp ?? chat.updatedAt),
      weight: Math.max(chat.numMessages, 1),
    }));
    return bucketByDay(events, WINDOW_DAYS);
  }, [data]);

  // Without project:read we can't fetch sessions, so render nothing. When we
  // can read but the assistant has no recent sessions, still draw a flat
  // baseline so the metric is visibly present (and populates as activity
  // arrives) rather than mysteriously absent.
  if (!canRead) return null;

  const hasActivity = counts.some((count) => count > 0);
  const max = Math.max(...counts, 1);
  const stepX = WIDTH / (counts.length - 1);
  const points = counts
    .map((count, i) => {
      const x = i * stepX;
      const y = HEIGHT - 1 - (count / max) * (HEIGHT - 2);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");

  return (
    <svg
      width={WIDTH}
      height={HEIGHT}
      viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
      className={cn(
        "overflow-visible",
        hasActivity ? "text-primary" : "text-muted-foreground/30",
        className,
      )}
      aria-label={hasActivity ? "Recent activity" : "No recent activity"}
      role="img"
    >
      <polyline
        points={points}
        fill="none"
        stroke="currentColor"
        strokeWidth={1.25}
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}
