export type RankedBarListItem = {
  key: string;
  label: string;
  value: number;
  // Optional display override for the value (e.g. "42%"); bar width still uses `value`.
  valueLabel?: string;
};

/**
 * A ranked horizontal bar list (1..N) sized relative to the largest value.
 *
 * The bar sits in its own column beside the label rather than under it: a
 * full-width rule beneath a line of text reads as an underline rather than a
 * measurement, and gives the eye no shared baseline to compare lengths
 * against. Keep this layout in step with SplitRankedBarList, which is the same
 * idiom carrying an extra failure segment.
 *
 * Items are rendered in the order provided, so sort before passing them in.
 */
export function RankedBarList({
  items,
}: {
  items: RankedBarListItem[];
}): JSX.Element {
  // Bar widths are normalized against the largest value across all items so
  // they stay bounded even if callers pass unsorted input.
  const max = Math.max(1, ...items.map((item) => item.value));
  return (
    <ul className="space-y-2.5">
      {items.map((item, i) => (
        <li
          key={item.key}
          className="grid grid-cols-[1.25rem_minmax(0,1fr)] items-center gap-x-3 gap-y-1.5 sm:grid-cols-[1.25rem_minmax(0,1fr)_9rem_3.5rem]"
        >
          <span className="text-muted-foreground text-right font-mono text-xs tabular-nums">
            {i + 1}
          </span>
          <span className="truncate text-sm" title={item.label}>
            {item.label}
          </span>
          {/* On a narrow panel the bar and its figure drop to a second line
              under the label rather than crushing every column. */}
          <div className="col-start-2 flex h-1.5 sm:col-start-3">
            <div
              className="bg-foreground"
              style={{ width: `${(item.value / max) * 100}%` }}
            />
            <div className="bg-muted flex-1" />
          </div>
          <span className="text-muted-foreground col-start-2 text-right font-mono text-xs tabular-nums sm:col-start-4">
            {item.valueLabel ?? item.value.toLocaleString()}
          </span>
        </li>
      ))}
    </ul>
  );
}
