// The ranked horizontal bar list for the chart system (formerly duplicated as
// RankedBarList.tsx, now deleted — every call site has migrated here). Adds
// optional row links, sublabels, an active/selected highlight, and a
// rank-based color gradient. Bars are thin and squared, per the chart
// system's design language (no rounded()).
import { cn } from "@/components/ui/moonshine";
import type { CSSProperties } from "react";
import { Link } from "react-router";
import { withAlpha } from "./chart-theme";

export type RankedBarItem = {
  label: string;
  value: number;
  href?: string;
  sublabel?: string;
  /** Row click handler for drill-throughs that update in-page state rather
   * than navigate (e.g. a query-param filter) — ignored when `href` is set. */
  onSelect?: () => void;
  /** Highlights the row as the current selection (e.g. an active filter). */
  active?: boolean;
};

/** @public — part of the component's prop API. */
export type RankedBarColorMode = "single" | "rank-gradient";

export type RankedBarProps = {
  items: RankedBarItem[];
  /** "single": every bar uses the same accent. "rank-gradient": rank 1 is
   * fully saturated, fading toward the last row (a sequential, one-hue
   * encoding of rank — not the series-identity language palette). */
  colorMode?: RankedBarColorMode;
  formatValue?: (value: number) => string;
};

// The "information" semantic token — a design-token blue, not a raw
// Tailwind-palette color — used for both the single-color and the
// rank-gradient base hue.
const ACCENT_VAR = "var(--bg-information-default)";
const MIN_GRADIENT_OPACITY = 0.35;
const MAX_GRADIENT_FADE = 0.65;

export function RankedBar({
  items,
  colorMode = "single",
  formatValue,
}: RankedBarProps): JSX.Element {
  const max = Math.max(1, ...items.map((item) => item.value));
  const fadeStep =
    items.length > 1 ? MAX_GRADIENT_FADE / (items.length - 1) : 0;

  return (
    <ul className="my-1 space-y-3">
      {items.map((item, index) => {
        const displayValue = formatValue
          ? formatValue(item.value)
          : item.value.toLocaleString();
        const barStyle: CSSProperties = {
          width: `${(item.value / max) * 100}%`,
          backgroundColor:
            colorMode === "rank-gradient"
              ? withAlpha(
                  ACCENT_VAR,
                  Math.max(MIN_GRADIENT_OPACITY, 1 - index * fadeStep),
                )
              : undefined,
        };

        const content = (
          <div className="min-w-0 flex-1">
            <div className="mb-1 flex items-center justify-between gap-2">
              <span className="truncate text-sm">{item.label}</span>
              <span className="text-muted-foreground ml-2 shrink-0 text-xs">
                {displayValue}
              </span>
            </div>
            {item.sublabel && (
              <span className="text-muted-foreground block truncate text-xs">
                {item.sublabel}
              </span>
            )}
            <div className="bg-muted mt-1 h-1 w-full">
              <div
                className={cn(
                  "h-1",
                  colorMode === "single" && "bg-information-default",
                )}
                style={barStyle}
              />
            </div>
          </div>
        );

        return (
          <li
            key={`${item.label}-${index}`}
            className={cn(
              "flex items-center gap-3",
              item.active && "bg-muted -mx-2 px-2 py-1.5",
            )}
          >
            <span className="text-muted-foreground w-4 shrink-0 text-right text-xs">
              {index + 1}
            </span>
            {item.href ? (
              <Link
                to={item.href}
                className="flex min-w-0 flex-1 no-underline hover:opacity-80"
              >
                {content}
              </Link>
            ) : item.onSelect ? (
              <button
                type="button"
                onClick={item.onSelect}
                aria-pressed={item.active}
                className="flex min-w-0 flex-1 cursor-pointer bg-transparent p-0 text-left hover:opacity-80"
              >
                {content}
              </button>
            ) : (
              content
            )}
          </li>
        );
      })}
    </ul>
  );
}
