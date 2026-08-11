import { useSeriesColors } from "@/components/chart/useSeriesColors";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { RiskExposureSlice } from "@gram/client/models/components/riskexposureslice.js";
import { RULE_CATEGORY_META, type RuleCategory } from "../policy-data";
import { getRiskCategoryChartColor } from "../riskTrendChartData";

const FALLBACK_SLICE_COLOR = "hsl(0, 0%, 60%)";

function categoryLabel(category: string): string {
  return RULE_CATEGORY_META[category as RuleCategory]?.label ?? category;
}

/**
 * Horizontal stacked bar of finding counts by category with an inline legend,
 * sharing the category palette with the risk trend chart so the same category
 * reads as the same color across the Secure section. Segments and legend
 * entries toggle the signals list's category filter; slices outside the
 * active selection dim so the bar doubles as the filter's state display.
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
  const sliceColor = (category: string) =>
    getRiskCategoryChartColor(category, seriesColors) ?? FALLBACK_SLICE_COLOR;
  const visible = slices.filter((slice) => slice.findings > 0);
  const active = new Set(activeCategories);
  const isDimmed = (category: string) =>
    active.size > 0 && !active.has(category);

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
          {visible.map((slice) => (
            <button
              key={slice.category}
              type="button"
              aria-pressed={active.has(slice.category)}
              aria-label={`Filter by ${categoryLabel(slice.category)}`}
              title={`${categoryLabel(slice.category)} · ${Math.round(slice.share * 100)}%`}
              onClick={() => onToggleCategory(slice.category)}
              className={cn(
                "cursor-pointer transition-opacity hover:opacity-80",
                isDimmed(slice.category) && "opacity-30",
              )}
              style={{
                width: `${Math.max(slice.share * 100, 1)}%`,
                backgroundColor: sliceColor(slice.category),
              }}
            />
          ))}
        </div>
        <div className="flex flex-wrap gap-x-4 gap-y-1">
          {visible.map((slice) => (
            <button
              key={slice.category}
              type="button"
              aria-pressed={active.has(slice.category)}
              onClick={() => onToggleCategory(slice.category)}
              className={cn(
                "text-muted-foreground hover:text-foreground inline-flex cursor-pointer items-center gap-1.5 text-xs transition-opacity",
                isDimmed(slice.category) && "opacity-40",
                active.has(slice.category) && "text-foreground font-medium",
              )}
            >
              <span
                className="size-2 rounded-full"
                style={{ backgroundColor: sliceColor(slice.category) }}
              />
              {categoryLabel(slice.category)}
              <span className="text-foreground font-medium tabular-nums">
                {Math.round(slice.share * 100)}%
              </span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
