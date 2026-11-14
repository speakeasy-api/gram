import { useSeriesColors } from "@/components/chart/useSeriesColors";
import { RankedList } from "@/components/observe/insights/RankedList";
import { cn } from "@/lib/utils";
import type { JSX } from "react";
import { measureLabel, type ExploreSpec, type ResultRow } from "./exploreModel";
import { rankedMeasure, rankedRows } from "./rankedRows";

/** Whole-window figures as horizontal bars, the MCP & Tools ranking. */
export function ResultRanked({
  spec,
  rows,
  fill = false,
}: {
  spec: ExploreSpec;
  rows: ResultRow[];
  /** Show as many rows as the container's height holds, for a sized card. */
  fill?: boolean;
}): JSX.Element | null {
  const [color] = useSeriesColors();
  const measure = rankedMeasure(spec);
  if (!measure) return null;
  return (
    <div className={cn("flex flex-col gap-2", fill && "min-h-0 flex-1")}>
      <span className="text-muted-foreground text-xs">
        Ranked by {measureLabel(measure)}
      </span>
      <RankedList
        rows={rankedRows(rows, spec, measure)}
        color={color ?? "currentColor"}
        fill={fill}
      />
    </div>
  );
}
