import {
  useIsDarkTheme,
  useSeriesColors,
} from "@/components/chart/useSeriesColors";
import { useState } from "react";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { RiskExposureSlice } from "@gram/client/models/components/riskexposureslice.js";
import { RULE_CATEGORY_META, type RuleCategory } from "../policy-data";
import { getRiskCategoryChartColor } from "../riskTrendChartData";

const FALLBACK_SLICE_COLOR = "hsl(0, 0%, 60%)";
// The dimmed state has to recede on both canvases, so it takes a neutral per
// theme: near-white behind the light ramp, near-black behind the lifted dark
// one. A single fixed grey would out-glow the dark ramp's mid-tone hues.
const INACTIVE_GREY_LIGHT = "hsl(0, 0%, 88%)";
const INACTIVE_GREY_DARK = "hsl(0, 0%, 27%)";

function categoryLabel(category: string): string {
  return RULE_CATEGORY_META[category as RuleCategory]?.label ?? category;
}

/**
 * Horizontal stacked bar of finding counts by category with an inline legend,
 * sharing the category palette with the risk trend chart so the same category
 * reads as the same color across the Secure section. Segments and legend
 * entries toggle the signals list's category filter; slices outside the
 * active selection dim so the bar doubles as the filter's state display.
 * Hovering a segment or legend entry previews that same dimming, so pointing
 * at a slice isolates it before committing to the filter.
 */
export function ExposureBar({
  slices,
  totalFindings,
  activeCategories,
  onToggleCategory,
}: {
  slices: RiskExposureSlice[];
  totalFindings: number;
  /** Currently filtered categories; empty means no filter (nothing dimmed). */
  activeCategories: string[];
  onToggleCategory: (category: string) => void;
}): JSX.Element {
  const seriesColors = useSeriesColors();
  const inactiveGrey = useIsDarkTheme()
    ? INACTIVE_GREY_DARK
    : INACTIVE_GREY_LIGHT;
  const [hovered, setHovered] = useState<string | null>(null);
  const sliceColor = (category: string) =>
    getRiskCategoryChartColor(category, seriesColors) ?? FALLBACK_SLICE_COLOR;
  const visible = slices.filter((slice) => slice.findings > 0);
  const active = new Set(activeCategories);
  // Hover previews an isolation of the pointed-at slice, overriding the
  // committed filter for as long as the pointer stays on the bar or legend.
  const isDimmed = (category: string) =>
    hovered !== null
      ? hovered !== category
      : active.size > 0 && !active.has(category);

  if (visible.length === 0) {
    return (
      <div className="space-y-4">
        <h3 className="text-eyebrow">Exposure by data type</h3>
        <Text small muted>
          No findings in this window.
        </Text>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-4">
        <h3 className="text-eyebrow">Exposure by data type</h3>
        <Text small muted>
          {totalFindings.toLocaleString()} findings
        </Text>
      </div>
      <div className="space-y-3">
        <div className="border-border flex h-3 w-full overflow-hidden rounded-full border">
          {visible.map((slice) => {
            const dimmed = isDimmed(slice.category);
            return (
              <button
                key={slice.category}
                type="button"
                aria-pressed={active.has(slice.category)}
                aria-label={`Filter by ${categoryLabel(slice.category)}`}
                title={`${categoryLabel(slice.category)} · ${Math.round(slice.share * 100)}%`}
                onClick={() => onToggleCategory(slice.category)}
                onMouseEnter={() => setHovered(slice.category)}
                onMouseLeave={() => setHovered(null)}
                onFocus={() => setHovered(slice.category)}
                onBlur={() => setHovered(null)}
                className="cursor-pointer transition-colors"
                style={{
                  width: `${Math.max(slice.share * 100, 1)}%`,
                  backgroundColor: dimmed
                    ? inactiveGrey
                    : sliceColor(slice.category),
                }}
              />
            );
          })}
        </div>
        <div className="flex flex-wrap gap-x-4 gap-y-1">
          {visible.map((slice) => {
            const dimmed = isDimmed(slice.category);
            const isActive = active.has(slice.category);
            const dotColor = dimmed ? inactiveGrey : sliceColor(slice.category);
            return (
              <button
                key={slice.category}
                type="button"
                aria-pressed={isActive}
                onClick={() => onToggleCategory(slice.category)}
                onMouseEnter={() => setHovered(slice.category)}
                onMouseLeave={() => setHovered(null)}
                onFocus={() => setHovered(slice.category)}
                onBlur={() => setHovered(null)}
                className={cn(
                  "text-muted-foreground hover:text-foreground inline-flex cursor-pointer items-center gap-1.5 text-xs transition-opacity",
                  dimmed && "opacity-50",
                  isActive && "text-foreground font-medium",
                )}
              >
                <span
                  className={cn(
                    "size-2 rounded-full transition-shadow",
                    isActive && "ring-2 ring-offset-1",
                  )}
                  style={{
                    backgroundColor: dotColor,
                    // Subtle ring in the category's color for active items
                    ...(isActive && {
                      ["--tw-ring-color" as string]: sliceColor(slice.category),
                    }),
                  }}
                />
                {categoryLabel(slice.category)}
                <span
                  className={cn(
                    "tabular-nums",
                    dimmed
                      ? "text-muted-foreground"
                      : "text-foreground font-medium",
                  )}
                >
                  {Math.round(slice.share * 100)}%
                </span>
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
}
