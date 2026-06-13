import { useSlugs } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { cn } from "@/lib/utils";
import { useAuditLogsInfinite } from "@gram/client/react-query";
import { useMemo } from "react";

const WINDOW_DAYS = 14;
const WIDTH = 56;
const HEIGHT = 16;

/**
 * Bucket audit-log timestamps into per-day tool-call counts for the trailing
 * WINDOW_DAYS, oldest day first.
 */
function bucketByDay(timestamps: Date[], days: number): number[] {
  const counts = Array.from({ length: days }, () => 0);
  const now = Date.now();
  const dayMs = 24 * 60 * 60 * 1000;
  for (const ts of timestamps) {
    const ageDays = Math.floor((now - ts.getTime()) / dayMs);
    if (ageDays < 0 || ageDays >= days) continue;
    // index 0 = oldest day in the window, days-1 = today
    counts[days - 1 - ageDays] += 1;
  }
  return counts;
}

/**
 * A compact inline sparkline of an assistant's recent tool-call activity,
 * derived client-side from the assistant audit trail (the same events behind
 * the Assistants > Audit log tab). Rendered as hand-rolled SVG rather than a
 * chart library so each card carries a near-zero-cost graphic.
 *
 * NOTE: this reads the most recent page of audit events per assistant, so it
 * reflects recent activity rather than a guaranteed full WINDOW_DAYS window —
 * adequate for an at-a-glance card. A project with many high-traffic
 * assistants would warrant a server-side bucketed endpoint instead of N
 * per-card audit queries.
 */
export function AssistantActivitySparkline({
  assistantId,
  className,
}: {
  assistantId: string;
  className?: string;
}): JSX.Element | null {
  const { projectSlug } = useSlugs();
  const { hasScope } = useRBAC();
  const canRead = hasScope("org:read");

  const { data } = useAuditLogsInfinite(
    { projectSlug, subjectType: "assistant", subjectId: assistantId },
    undefined,
    { enabled: canRead, retry: false, throwOnError: false },
  );

  const counts = useMemo(() => {
    const logs = data?.pages.flatMap((page) => page.result.logs) ?? [];
    if (logs.length === 0) return null;
    const timestamps = logs.map((log) => new Date(log.createdAt));
    const buckets = bucketByDay(timestamps, WINDOW_DAYS);
    return buckets.some((c) => c > 0) ? buckets : null;
  }, [data]);

  if (!canRead || !counts) return null;

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
      className={cn("text-muted-foreground/50 overflow-visible", className)}
      aria-label="Recent activity"
      role="img"
    >
      <polyline
        points={points}
        fill="none"
        stroke="currentColor"
        strokeWidth={1}
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}
