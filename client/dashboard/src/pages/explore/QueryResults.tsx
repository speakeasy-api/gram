import { Text } from "@/components/ui/Text";
import type { ExploreMetaResult } from "@gram/client/models/components/exploremetaresult.js";
import { useMemo } from "react";
import {
  completeCalcs,
  isTimeseries,
  queryBodyFromSpec,
  type ExploreSpec,
} from "./exploreModel";
import { ExploreResultsBody } from "./ExploreResults";
import { resultErrorMessage, useRunQuery } from "./useRunQuery";

/** Live timeseries and summary results for the canonical Explore builder. */
export function QueryResults({
  meta,
  spec,
}: {
  meta: ExploreMetaResult | undefined;
  spec: ExploreSpec;
}): JSX.Element {
  const enabled = completeCalcs(spec.calculations).length > 0;
  const timeseries = isTimeseries(spec.chartType);
  const timeseriesBody = useMemo(
    () => queryBodyFromSpec(spec, "timeseries"),
    [spec],
  );
  const summaryBody = useMemo(() => queryBodyFromSpec(spec, "summary"), [spec]);
  const timeseriesResult = useRunQuery(timeseriesBody, enabled && timeseries);
  const summaryResult = useRunQuery(summaryBody, enabled);

  return (
    <div className="border-border bg-card border p-4">
      <div className="mb-4">
        <h3 className="text-eyebrow">Results</h3>
      </div>
      {enabled ? (
        <div className="flex flex-col gap-6">
          {timeseries ? (
            <ExploreResultsBody
              result={timeseriesResult.data}
              meta={meta}
              chartType={spec.chartType}
              loading={timeseriesResult.isPending}
              errorMessage={resultErrorMessage(timeseriesResult.error)}
              height={300}
            />
          ) : null}
          <div className="flex flex-col gap-1">
            {timeseries ? (
              <Text mono muted className="text-xs">
                Summary
              </Text>
            ) : null}
            <ExploreResultsBody
              result={summaryResult.data}
              meta={meta}
              chartType={spec.chartType === "number" ? "number" : "table"}
              loading={summaryResult.isPending}
              errorMessage={resultErrorMessage(summaryResult.error)}
              height={260}
            />
          </div>
        </div>
      ) : (
        <div className="text-muted-foreground flex h-40 items-center justify-center text-sm">
          Complete a calculation to run the query.
        </div>
      )}
    </div>
  );
}
