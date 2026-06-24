export type RankedBarListItem = {
  key: string;
  label: string;
  value: number;
  // Optional display override for the value (e.g. "42%"); bar width still uses `value`.
  valueLabel?: string;
};

/**
 * A ranked horizontal bar list (1..N) sized relative to the largest value.
 * Items are rendered in the order provided, so sort before passing in.
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
    <ul className="my-1 space-y-3">
      {items.map((item, i) => (
        <li key={item.key} className="flex items-center gap-3">
          <span className="text-muted-foreground w-4 shrink-0 text-right text-xs">
            {i + 1}
          </span>
          <div className="min-w-0 flex-1">
            <div className="mb-1 flex items-center justify-between">
              <span className="truncate text-sm">{item.label}</span>
              <span className="text-muted-foreground ml-2 shrink-0 text-xs">
                {item.valueLabel ?? item.value.toLocaleString()}
              </span>
            </div>
            <div className="bg-muted h-1 w-full rounded-full">
              <div
                className="h-1 rounded-full bg-blue-700 dark:bg-blue-500"
                style={{ width: `${(item.value / max) * 100}%` }}
              />
            </div>
          </div>
        </li>
      ))}
    </ul>
  );
}
