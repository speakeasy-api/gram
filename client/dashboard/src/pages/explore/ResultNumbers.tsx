import { MetricCard } from "@/components/ui/MetricCard";
import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import type { JSX } from "react";
import {
  completeMeasures,
  formatMeasureValue,
  measureAlias,
  measureLabel,
  measureUnit,
  numericCell,
  type ExploreSpec,
  type ResultRow,
} from "./exploreModel";

/** Whole-window figures as one tile per measure. */
export function ResultNumbers({
  dataset,
  spec,
  row,
}: {
  dataset: AnalyticsDataset | undefined;
  spec: ExploreSpec;
  row: ResultRow | undefined;
}): JSX.Element {
  return (
    <MetricCard.Group>
      {completeMeasures(spec.measures).map((measure) => {
        const value = numericCell(row?.[measureAlias(measure)]);
        return (
          <MetricCard
            key={measureAlias(measure)}
            label={measureLabel(measure)}
            value={
              value === null
                ? "—"
                : formatMeasureValue(value, measureUnit(dataset, measure))
            }
            tone="neutral"
            size="sm"
          />
        );
      })}
    </MetricCard.Group>
  );
}
