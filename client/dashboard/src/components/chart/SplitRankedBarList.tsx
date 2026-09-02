export type SplitRankedBarItem = {
  key: string;
  label: string;
  // Total occurrences; sizes the bar against the largest item.
  value: number;
  // The share of `value` that went wrong. Drawn as a red head on the bar.
  failed: number;
};

/**
 * A ranked bar list that also carries a failure share.
 *
 * A plain top-N list of tool calls answers "what does this person use most",
 * which is rarely the interesting question on its own — the useful reading is
 * where their volume is also unreliable. Splitting each bar surfaces that in
 * the same glance, so a heavily-used tool that fails a third of the time
 * cannot hide behind its rank.
 *
 * The bar lives in its own column rather than under the label: a full-width
 * rule beneath a line of text reads as an underline, not a measurement, and
 * gives the eye no common baseline to compare lengths against.
 *
 * Items render in the order given; sort before passing them in.
 */
export function SplitRankedBarList({
  items,
}: {
  items: SplitRankedBarItem[];
}): JSX.Element {
  // Widths normalize against the largest total so the bars stay bounded even
  // if a caller passes unsorted input.
  const max = Math.max(1, ...items.map((item) => item.value));

  return (
    <ul className="space-y-2.5">
      {items.map((item, i) => {
        const failed = Math.min(Math.max(item.failed, 0), item.value);
        const succeeded = item.value - failed;
        return (
          <li
            key={item.key}
            className="grid grid-cols-[1.25rem_minmax(0,1fr)_3.5rem_4.5rem] items-center gap-x-3 gap-y-1.5 sm:grid-cols-[1.25rem_minmax(0,1fr)_9rem_3.5rem_4.5rem]"
          >
            <span className="text-muted-foreground text-right font-mono text-xs tabular-nums">
              {i + 1}
            </span>
            <span
              className="col-span-3 truncate text-sm sm:col-span-1"
              title={item.label}
            >
              {item.label}
            </span>
            {/* On a narrow panel the label takes the whole line and the bar
                drops beneath it rather than crushing every column; both
                figures keep their own tracks so they stay beside the bar
                instead of being pushed onto further lines. */}
            <div className="col-start-2 flex h-1.5 sm:col-start-3">
              <div
                className="bg-foreground"
                style={{ width: `${(succeeded / max) * 100}%` }}
              />
              <div
                className="bg-destructive"
                style={{ width: `${(failed / max) * 100}%` }}
              />
              <div className="bg-muted flex-1" />
            </div>
            <span className="text-muted-foreground col-start-3 text-right font-mono text-xs tabular-nums sm:col-start-4">
              {item.value.toLocaleString()}
            </span>
            <span className="text-default-destructive col-start-4 text-right font-mono text-xs tabular-nums sm:col-start-5">
              {failed > 0 ? `${failed.toLocaleString()} fail` : ""}
            </span>
          </li>
        );
      })}
    </ul>
  );
}
