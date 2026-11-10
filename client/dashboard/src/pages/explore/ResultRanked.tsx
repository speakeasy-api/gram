import { useSeriesColors } from "@/components/chart/useSeriesColors";
import { RankedList } from "@/components/observe/insights/RankedList";
import type { JSX } from "react";
import { measureLabel, type ExploreSpec, type ResultRow } from "./exploreModel";
import { rankedMeasure, rankedRows } from "./rankedRows";

/** Whole-window figures as horizontal bars, the MCP & Tools ranking. */
export function ResultRanked({
  spec,
  rows,
}: {
  spec: ExploreSpec;
  rows: ResultRow[];
}): JSX.Element | null {
  const [color] = useSeriesColors();
  const measure = rankedMeasure(spec);
  if (!measure) return null;
  return (
    <div className="flex flex-col gap-2">
      <span className="text-muted-foreground text-xs">
        Ranked by {measureLabel(measure)}
      </span>
      <RankedList
        rows={rankedRows(rows, spec, measure)}
        color={color ?? "currentColor"}
      />
    </div>
  );
}
