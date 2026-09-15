import { Icon } from "@/components/ui/Icon";
import { Skeleton } from "@/components/ui/Skeleton";
import { cn } from "@/lib/utils";
import { Link } from "react-router";

/**
 * One cell of the insights grid: an eyebrow title that links into the logs for
 * whatever the cell is showing, and a body the caller fills.
 *
 * Deliberately small and uniform. The page reads as one scannable board of
 * answers rather than six large charts that each need decoding.
 */
export function InsightCard({
  title,
  href,
  loading = false,
  error = false,
  children,
  className,
}: {
  title: string;
  href?: string;
  loading?: boolean;
  error?: boolean;
  children: React.ReactNode;
  className?: string;
}): JSX.Element {
  return (
    <div
      className={cn(
        "border-border bg-card flex flex-col gap-3 border p-4",
        className,
      )}
    >
      <h3 className="text-eyebrow">
        {href ? (
          <Link
            to={href}
            className="hover:text-foreground inline-flex items-center gap-1 no-underline hover:underline"
          >
            {title}
            <Icon name="arrow-up-right" className="size-3" />
          </Link>
        ) : (
          title
        )}
      </h3>
      {error ? (
        <p className="text-muted-foreground text-sm">Failed to load</p>
      ) : loading ? (
        <Skeleton className="h-20 w-full" />
      ) : (
        children
      )}
    </div>
  );
}

/**
 * A headline number with the shape of the window behind it.
 *
 * The sparkline is drawn as an inline SVG polyline: it carries no axes, no
 * tooltip and no interaction, so a charting runtime would be weight without
 * benefit — the point is the silhouette next to the number.
 */
export function MetricSpark({
  value,
  series,
  tone = "default",
  caption,
}: {
  value: string;
  series: number[];
  tone?: "default" | "destructive" | "success";
  caption?: string;
}): JSX.Element {
  const stroke =
    tone === "destructive"
      ? "var(--color-destructive)"
      : tone === "success"
        ? "var(--color-success)"
        : "var(--color-muted-foreground)";

  const points = sparkPoints(series);

  return (
    <div className="flex items-end justify-between gap-3">
      <div className="flex flex-col gap-1">
        <span
          className={cn(
            "font-serif text-3xl leading-none",
            tone === "destructive" && "text-destructive",
          )}
        >
          {value}
        </span>
        {caption && (
          <span className="text-muted-foreground text-xs">{caption}</span>
        )}
      </div>
      {points && (
        <svg
          viewBox="0 0 100 32"
          preserveAspectRatio="none"
          className="h-8 w-24 shrink-0"
          aria-hidden
        >
          <polyline
            points={points}
            fill="none"
            stroke={stroke}
            strokeWidth={1.5}
            vectorEffect="non-scaling-stroke"
          />
        </svg>
      )}
    </div>
  );
}

/** Maps a series into the sparkline viewBox; null when there is nothing to draw. */
function sparkPoints(series: number[]): string | null {
  if (series.length < 2) return null;
  const max = Math.max(...series);
  const min = Math.min(...series);
  const span = max - min || 1;
  return series
    .map((point, index) => {
      const x = (index / (series.length - 1)) * 100;
      const y = 32 - ((point - min) / span) * 30 - 1;
      return `${x.toFixed(2)},${y.toFixed(2)}`;
    })
    .join(" ");
}
