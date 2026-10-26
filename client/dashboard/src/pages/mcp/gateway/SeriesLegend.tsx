import type { LegendItem } from "./gatewayActivityColors";

// The section's charts share one legend: mono micro-labels beside square
// swatches, the canvas legend's idiom rendered in HTML so it wraps at narrow
// widths instead of overlapping the plot.
export function SeriesLegend({ items }: { items: LegendItem[] }): JSX.Element {
  return (
    <ul className="text-eyebrow flex flex-wrap gap-x-4 gap-y-1">
      {items.map((item) => (
        <li key={item.label} className="inline-flex items-center gap-1.5">
          <span
            aria-hidden="true"
            className="inline-block size-2 shrink-0"
            style={{ backgroundColor: item.color }}
          />
          {item.label}
        </li>
      ))}
    </ul>
  );
}
