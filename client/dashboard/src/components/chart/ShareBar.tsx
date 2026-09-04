import { useOtherSeriesColor, useSeriesColors } from "./useSeriesColors";

export type ShareBarSegment = {
  key: string;
  label: string;
  value: number;
  // Optional display override for the value in the legend (e.g. "$12.40");
  // the segment width always comes from `value`.
  valueLabel?: string;
};

/**
 * A single full-width bar split into shares, with a legend beneath.
 *
 * Where RankedBarList answers "who is biggest", this answers "how does the
 * whole divide" — the question worth asking of a model mix, a finding
 * breakdown, or an allow/deny split, where the parts sum to something
 * meaningful and a stack of separate bars would hide that they do.
 *
 * Segments are drawn in the order given, so sort before passing them in: past
 * the length of the categorical ramp the tail folds into a single neutral
 * share, so no two segments are ever handed the same colour.
 */
export function ShareBar({
  segments,
  ariaLabel,
}: {
  segments: ShareBarSegment[];
  ariaLabel: string;
}): JSX.Element | null {
  const colors = useSeriesColors();
  const otherColor = useOtherSeriesColor();
  const total = segments.reduce((sum, segment) => sum + segment.value, 0);
  // With nothing to divide there are no shares to show, and every segment
  // would round to a 0%-wide sliver.
  if (total <= 0) {
    return null;
  }

  // The categorical ramp is finite, and cycling it would hand two segments the
  // same colour — the bar and its legend then claim two different things are
  // one. Callers pass sorted segments, so the tail is the small end: it folds
  // into a single neutral rollup that reads as "the rest" rather than as a
  // series of its own.
  const named =
    segments.length > colors.length
      ? segments.slice(0, colors.length - 1)
      : segments;
  const folded = segments.slice(named.length);
  const shares = named.map((segment, i) => ({
    ...segment,
    color: colors[i] ?? "",
    percent: (segment.value / total) * 100,
  }));
  if (folded.length > 0) {
    const foldedValue = folded.reduce((sum, segment) => sum + segment.value, 0);
    // No valueLabel: the folded segments may carry unit-bearing labels of
    // their own (a currency, say), which the fold cannot re-derive, so it
    // states its share instead.
    shares.push({
      key: "__other__",
      label: `${folded.length} more`,
      value: foldedValue,
      color: otherColor,
      percent: (foldedValue / total) * 100,
    });
  }

  // Every segment claims at least a hairline, which on a long tail pushes the
  // requested widths past 100%. Flexbox would then shrink every segment to
  // fit, including the ones whose width is the measurement being shown, so the
  // overflow is taken back from the segments that have room to spare instead.
  const MIN_PERCENT = 0.5;
  const widths = shares.map((share) => Math.max(share.percent, MIN_PERCENT));
  const overflow = widths.reduce((sum, width) => sum + width, 0) - 100;
  if (overflow > 0) {
    const headroom = widths.reduce(
      (sum, width) => sum + Math.max(width - MIN_PERCENT, 0),
      0,
    );
    if (headroom > 0) {
      for (const [i, width] of widths.entries()) {
        const spare = Math.max(width - MIN_PERCENT, 0);
        widths[i] = width - (spare / headroom) * overflow;
      }
    }
  }

  return (
    <div>
      <div
        className="flex h-3 w-full overflow-hidden"
        role="img"
        aria-label={ariaLabel}
      >
        {shares.map((share, i) => (
          <div
            key={share.key}
            style={{
              width: `${widths[i]}%`,
              backgroundColor: share.color,
            }}
          />
        ))}
      </div>
      <ul className="mt-3.5 grid grid-cols-[repeat(auto-fit,minmax(9rem,1fr))] gap-x-5 gap-y-2">
        {shares.map((share) => (
          <li key={share.key} className="flex items-center gap-2">
            <span
              aria-hidden
              className="size-2.5 shrink-0"
              style={{ backgroundColor: share.color }}
            />
            <span
              className="min-w-0 flex-1 truncate text-sm"
              title={share.label}
            >
              {share.label}
            </span>
            <span className="text-muted-foreground shrink-0 font-mono text-xs tabular-nums">
              {share.valueLabel ??
                // Rounding a real share to "0%" reads as absent rather than
                // small; the bar still shows its hairline.
                (share.percent < 1 ? "<1%" : `${Math.round(share.percent)}%`)}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}
