import { useSeriesColors } from "./useSeriesColors";

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
 * Segments are drawn in the order given, so sort before passing them in.
 */
export function ShareBar({
  segments,
  ariaLabel,
}: {
  segments: ShareBarSegment[];
  ariaLabel: string;
}): JSX.Element | null {
  const colors = useSeriesColors();
  const total = segments.reduce((sum, segment) => sum + segment.value, 0);
  // With nothing to divide there are no shares to show, and every segment
  // would round to a 0%-wide sliver.
  if (total <= 0) {
    return null;
  }

  const shares = segments.map((segment, i) => ({
    ...segment,
    color: colors[i % colors.length] ?? "",
    percent: (segment.value / total) * 100,
  }));

  return (
    <div>
      <div
        className="flex h-3 w-full overflow-hidden"
        role="img"
        aria-label={ariaLabel}
      >
        {shares.map((share) => (
          <div
            key={share.key}
            // A share below ~1% still deserves to be visible as a hairline
            // rather than collapse to nothing.
            style={{
              width: `${Math.max(share.percent, 0.5)}%`,
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
