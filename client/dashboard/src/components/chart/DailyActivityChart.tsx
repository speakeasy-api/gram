import { useSeriesColors } from "./useSeriesColors";

export type DailyActivitySeries = {
  key: string;
  label: string;
  // Event timestamps; the component buckets them by calendar day itself so
  // every caller gets the same bucketing and the same empty-day handling.
  dates: Date[];
};

const DAY_MS = 24 * 60 * 60 * 1000;

function startOfDay(date: Date): number {
  const copy = new Date(date);
  copy.setHours(0, 0, 0, 0);
  return copy.getTime();
}

/**
 * Stacked columns, one per day of the selected window.
 *
 * The lists on this page answer "what happened"; they cannot answer "when,
 * and in what rhythm". A person who works in two bursts a week and one who
 * works every afternoon produce identical row lists and very different
 * columns. Empty days are drawn as empty slots rather than skipped, because
 * the gaps are the signal.
 *
 * Columns are capped in width and spread evenly across the panel rather than
 * stretched to fill it: a seven-day window given the full width produces
 * 100px-wide bars, which read as colour blocks rather than as a measurement.
 */
export function DailyActivityChart({
  from,
  to,
  series,
}: {
  from: Date;
  to: Date;
  series: DailyActivitySeries[];
}): JSX.Element | null {
  const colors = useSeriesColors();

  const firstDay = startOfDay(from);
  const lastDay = startOfDay(to);
  const dayCount = Math.round((lastDay - firstDay) / DAY_MS) + 1;
  // A window that resolves to no whole day (or an inverted one) has no axis to
  // draw against; the panel's own empty state covers it.
  if (!Number.isFinite(dayCount) || dayCount < 1) {
    return null;
  }

  const counts = series.map((s) => {
    const perDay: number[] = Array.from({ length: dayCount }, () => 0);
    for (const date of s.dates) {
      const index = Math.round((startOfDay(date) - firstDay) / DAY_MS);
      if (index >= 0 && index < dayCount) {
        perDay[index] = (perDay[index] ?? 0) + 1;
      }
    }
    return perDay;
  });

  const dayTotals = Array.from({ length: dayCount }, (_, day) =>
    counts.reduce((sum, perDay) => sum + (perDay[day] ?? 0), 0),
  );
  const busiest = Math.max(1, ...dayTotals);
  const dayLabel = (day: number) =>
    new Date(firstDay + day * DAY_MS).toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
    });

  return (
    <div>
      {/* Spread across the full width so the first and last columns sit under
          the date labels below them; capped so a short window does not turn
          each day into a colour block. */}
      <div className="flex h-20 items-end justify-between gap-1.5">
        {dayTotals.map((total, day) => (
          <div
            key={day}
            className="flex h-full max-w-8 min-w-1.5 flex-1 flex-col justify-end"
            // The column is the only affordance here, so the whole day —
            // including its empty headroom — carries the readout.
            title={`${dayLabel(day)}: ${series
              .map(
                (s, i) => `${counts[i]?.[day] ?? 0} ${s.label.toLowerCase()}`,
              )
              .join(", ")}`}
          >
            {total === 0
              ? null
              : series.map((s, i) => {
                  const value = counts[i]?.[day] ?? 0;
                  if (value === 0) return null;
                  return (
                    <div
                      key={s.key}
                      // Below a pixel or two a real day reads as an empty one,
                      // so any non-zero count keeps a visible floor.
                      style={{
                        height: `${Math.max((value / busiest) * 100, 4)}%`,
                        backgroundColor: colors[i % colors.length],
                      }}
                    />
                  );
                })}
          </div>
        ))}
      </div>
      {/* The baseline gives the columns something to stand on; without it an
          empty day is indistinguishable from the panel's background. */}
      <div className="bg-border h-px w-full" />
      <div className="text-muted-foreground mt-2 flex justify-between font-mono text-xs">
        <span>{dayLabel(0)}</span>
        <span>{dayLabel(dayCount - 1)}</span>
      </div>
      {series.length > 1 && (
        <ul className="mt-3 flex flex-wrap gap-x-5 gap-y-2">
          {series.map((s, i) => (
            <li key={s.key} className="flex items-center gap-2 text-sm">
              <span
                aria-hidden
                className="size-2.5 shrink-0"
                style={{ backgroundColor: colors[i % colors.length] }}
              />
              <span>{s.label}</span>
              <span className="text-muted-foreground font-mono text-xs tabular-nums">
                {(counts[i]?.reduce((a, b) => a + b, 0) ?? 0).toLocaleString()}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
