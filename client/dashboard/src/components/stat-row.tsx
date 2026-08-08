import { MetricCard, type MetricCardProps } from "@/components/ui/MetricCard";
import { Skeleton } from "@/components/ui/Skeleton";
import { cn } from "@/lib/utils";

/**
 * StatRow — a row of {@link MetricCard} stat tiles that swaps to shaped
 * skeletons while loading.
 *
 * Overview pages repeatedly wrote a per-tile `isLoading ? <Skeleton/> :
 * <MetricCard/>` ternary inside `MetricCard.Group`. StatRow absorbs that: pass
 * the tiles as data and one `isLoading` flag.
 *
 *   <StatRow
 *     isLoading={q.isPending}
 *     metrics={[
 *       { label: "Total rules", value: total, tone: "information" },
 *       { label: "Violations", value: n, tone: n > 0 ? "destructive" : "neutral" },
 *     ]}
 *   />
 */
export type StatRowMetric = MetricCardProps & { key?: string };

export function StatRow({
  metrics,
  isLoading = false,
  className,
}: {
  metrics: StatRowMetric[];
  isLoading?: boolean;
  className?: string;
}): JSX.Element {
  return (
    <MetricCard.Group className={cn(className)}>
      {isLoading
        ? metrics.map((metric, index) => (
            <Skeleton key={metric.key ?? index} className="h-[136px] flex-1" />
          ))
        : metrics.map(({ key, ...metric }, index) => (
            <MetricCard key={key ?? index} {...metric} />
          ))}
    </MetricCard.Group>
  );
}
