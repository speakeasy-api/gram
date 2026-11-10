import { ChartCard } from "@/components/chart/ChartCard";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { StatRow } from "@/components/stat-row";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/Sheet";
import { Table } from "@/components/ui/Table";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { downloadBlob } from "@/lib/download";
import { ResultChart } from "@/pages/explore/ResultChart";
import type { Grain } from "@/pages/explore/exploreModel";
import type { AnalyticsQueryResult } from "@gram/client/models/components/analyticsqueryresult.js";
import type { AnalyticsQueryPayload } from "@gram/client/models/components/analyticsquerypayload.js";
import { useAnalyticsSignalDefinition } from "@gram/client/react-query/analyticsSignalDefinition.js";
import { useRef, useState } from "react";
import {
  groupLabel,
  signalNumber,
  signalSeries,
  type SignalChartMetric,
} from "./explorationModel";

export function SignalResults({
  query,
  request,
  title,
  sensorName,
  onRetry,
  metric,
}: {
  query: {
    data?: AnalyticsQueryResult;
    isPending: boolean;
    isError: boolean;
    isFetching: boolean;
  };
  request: AnalyticsQueryPayload;
  title: string;
  sensorName: string;
  onRetry: () => void;
  metric: SignalChartMetric;
}): JSX.Element {
  const [definition, setDefinition] = useState<string | null>(null);
  const details = useRef<HTMLDetailsElement>(null);
  const metricLabel =
    metric === "match_share"
      ? "Share of measured answers"
      : "Matching messages";
  const result = query.data?.signalResult;
  const summary = result?.summary;
  const dateOptions: Intl.DateTimeFormatOptions = {
    month: "short",
    day: "numeric",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    timeZone: "UTC",
  };
  const window = `${request.from.toLocaleString("en-US", dateOptions)} – ${request.to.toLocaleString("en-US", dateOptions)} UTC`;
  if (query.isError)
    return (
      <InlineEmptyState
        className="min-h-[520px]"
        icon="triangle-alert"
        heading="This query couldn’t be completed"
        description="Try a shorter time range, an automatic interval, or fewer groups. Check that the selected manual run exists."
        action={<Button onClick={onRetry}>Try again</Button>}
      />
    );
  if (query.isPending || !result || !query.data)
    return <Skeleton className="h-[520px] w-full" />;
  if (!summary || summary.observed_outcomes === 0)
    return (
      <InlineEmptyState
        className="min-h-[520px]"
        icon="chart-line"
        heading="No measured activity in this range"
        description="Try a wider range or clear the attribute filter. An empty range does not mean the signal never occurred."
        action={
          <Button variant="secondary" onClick={onRetry}>
            Refresh results
          </Button>
        }
      />
    );
  const hashes = Array.isArray(summary.definition_hashes)
    ? summary.definition_hashes.map(String)
    : [];
  const columns = [
    {
      key: "group",
      header: "Group",
      render: (row: Record<string, unknown>) => (
        <span
          className="block max-w-72 truncate"
          title={groupLabel(row.group_values)}
        >
          {groupLabel(row.group_values)}
        </span>
      ),
    },
    {
      key: "matched",
      header: "Matching messages",
      render: (row: Record<string, unknown>) =>
        signalNumber(row.matched_messages),
    },
    {
      key: "answered",
      header: "Measured answers",
      render: (row: Record<string, unknown>) => signalNumber(row.answered),
    },
    {
      key: "share",
      header: "Match share",
      render: (row: Record<string, unknown>) =>
        signalNumber(row.match_share, true),
    },
    {
      key: "sessions",
      header: "Sessions with matches",
      render: (row: Record<string, unknown>) =>
        signalNumber(row.matched_sessions),
    },
  ];
  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 className="text-display-xs">{title}</h2>
          <p className="text-muted-foreground text-sm">
            {sensorName} · {window}
          </p>
        </div>
        <Button
          variant="secondary"
          size="sm"
          onClick={() =>
            downloadBlob(
              new Blob(
                [JSON.stringify({ request, result: query.data }, null, 2)],
                { type: "application/json" },
              ),
              "signal-measurement.json",
            )
          }
        >
          Export measurement
        </Button>
      </div>
      <StatRow
        className="flex-col divide-x-0 divide-y sm:flex-row sm:divide-x sm:divide-y-0"
        metrics={[
          {
            label: "Matching messages",
            value: signalNumber(summary.matched_messages),
            tone: "information",
            description: "Original captured messages",
          },
          {
            label: "Share of measured answers",
            value: signalNumber(summary.match_share, true),
            tone: "information",
            description: `${signalNumber(summary.answered)} measured answers`,
          },
          {
            label: "Sessions with matches",
            value: signalNumber(summary.matched_sessions),
            tone: "information",
            description: `${signalNumber(summary.session_breadth, true)} of evaluated sessions`,
          },
        ]}
      />
      <ChartCard
        title={`${metricLabel} · ${result.metadata.step} intervals`}
        chartId="signal-matches"
        expandedChart={null}
        onExpand={() => {}}
        expandable={false}
      >
        <ResultChart
          seriesSet={signalSeries(query.data, metric)}
          unit={metric === "match_share" ? "ratio" : "count"}
          chartType="line"
          grain={result.metadata.step as Grain}
          height={240}
          connectGaps={false}
          colorByIndex
          timeZone="UTC"
        />
        <p className="mt-3 text-sm text-muted-foreground">
          Gaps mean no measured answer. Zero means an answered negative.
          Triangles mark partial intervals at the edges of the selected window.
        </p>
      </ChartCard>
      {result.metadata.groupBy.length > 0 && (
        <div className="min-w-0 overflow-x-auto">
          <Table
            columns={columns}
            data={result.groups}
            rowKey={(row) => String(row.series_id)}
          />
        </div>
      )}
      {result.metadata.groupsTruncated && (
        <p className="text-sm text-muted-foreground">
          Showing the top five groups across the entire range. Headline counts
          include every group.
        </p>
      )}
      {Number(summary.definition_count) > 1 && (
        <div className="flex flex-wrap items-center gap-2 text-sm">
          <span>Multiple measured definitions contribute to this view.</span>
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => {
              if (details.current) {
                details.current.open = true;
                details.current.scrollIntoView({ block: "nearest" });
              }
            }}
          >
            Inspect definitions
          </Button>
        </div>
      )}
      <details ref={details} className="border-t pt-4 text-sm">
        <summary className="cursor-pointer font-medium">
          Measurement details
        </summary>
        <div className="mt-3 space-y-3 text-muted-foreground">
          <p>
            Coverage of all eligible messages is unknown. These rates describe
            measured answers only; sessions are distinct conversations and must
            not be added across groups.
          </p>
          <p>
            {signalNumber(summary.failed)} failed ·{" "}
            {signalNumber(summary.not_applicable)} not applicable ·{" "}
            {signalNumber(summary.draft)} draft ·{" "}
            {signalNumber(summary.unmeasured)} answered without the selected
            signal.
          </p>
          <p>
            Minimum probability: {request.signalQuery?.selection.threshold}.{" "}
            {request.signalQuery?.selection.minimumLevel !== undefined &&
              "Ordered matches use cumulative probability at or above this position in each measured definition. Inspect definitions before interpreting level meanings across edits."}
            {request.signalQuery?.selection.signalId &&
              "Exclusive matches use the chosen answer."}
          </p>
          <p>
            Latest available results, queried{" "}
            {result.metadata.queriedAt.toLocaleString("en-US", dateOptions)}{" "}
            UTC. Corrections can change a later query. Output uses{" "}
            {result.metadata.step} intervals; storage reads use{" "}
            {result.metadata.physicalResolution} resolution.
          </p>
          <p>
            {signalNumber(summary.definition_count)} contributing definitions.
            Classifier models:{" "}
            {String(summary.classifier_models ?? "Not recorded")}.
          </p>
          <div className="flex flex-wrap gap-2">
            {hashes.map((hash, i) => (
              <Button
                key={hash}
                variant="secondary"
                size="sm"
                onClick={() => setDefinition(hash)}
              >
                Inspect definition {i + 1}
              </Button>
            ))}
          </div>
          {Number(summary.definition_count) > hashes.length && (
            <p>
              Showing {hashes.length} of{" "}
              {signalNumber(summary.definition_count)} definitions. Narrow the
              time range to inspect the remaining definitions.
            </p>
          )}
        </div>
      </details>
      <Sheet
        open={definition !== null}
        onOpenChange={(open) => {
          if (!open) setDefinition(null);
        }}
      >
        <SheetContent className="sm:max-w-xl overflow-y-auto">
          <SheetHeader>
            <SheetTitle>Measured definition</SheetTitle>
            <SheetDescription>
              The preserved question and input rules used for these readings.
            </SheetDescription>
          </SheetHeader>
          {definition && (
            <Definition
              sensorId={request.signalQuery!.selection.sensorId}
              hash={definition}
            />
          )}
        </SheetContent>
      </Sheet>
    </>
  );
}

function Definition({
  sensorId,
  hash,
}: {
  sensorId: string;
  hash: string;
}): JSX.Element {
  const gramProject = useProjectSlugForRequests();
  const query = useAnalyticsSignalDefinition({
    gramProject,
    sensorId,
    definitionHash: hash,
  });
  if (query.isPending) return <Skeleton className="m-4 h-80" />;
  if (query.isError)
    return (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Definition unavailable"
        description="This historical definition may not have been captured."
        action={<Button onClick={() => void query.refetch()}>Try again</Button>}
      />
    );
  return (
    <pre className="p-4 text-xs whitespace-pre-wrap break-words">
      {JSON.stringify(query.data.definition, null, 2)}
    </pre>
  );
}
