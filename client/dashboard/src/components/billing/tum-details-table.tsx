import { telemetryQuery } from "@gram/client/funcs/telemetryQuery";
import { telemetryQueryMessageTokenStats } from "@gram/client/funcs/telemetryQueryMessageTokenStats";
import { type QueryMeasures } from "@gram/client/models/components/querymeasures.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { Skeleton } from "@/components/ui/skeleton";
import { type BillingCycle } from "./billing-cycles";

// Vercel-style usage details for the selected billing cycle: one row per
// metric with a colored dot, a mini sparkline of the daily series, and the
// cycle total — grouped into scannable sections under the token usage chart.

const compactNumber = new Intl.NumberFormat("en-US", {
  notation: "compact",
  maximumFractionDigits: 2,
});

type DetailRow = {
  label: string;
  color: string;
  series: number[];
  total: number;
};

type DetailGroup = {
  heading: string;
  rows: DetailRow[];
};

// Minimal inline sparkline — a normalized polyline of the daily series.
function Sparkline({
  series,
  color,
}: {
  series: number[];
  color: string;
}): JSX.Element {
  const width = 120;
  const height = 24;
  const pad = 2;
  const max = Math.max(...series, 1);
  const step = series.length > 1 ? (width - pad * 2) / (series.length - 1) : 0;
  const points = series
    .map((v, i) => {
      const x = pad + i * step;
      const y = height - pad - (v / max) * (height - pad * 2);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");
  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      aria-hidden="true"
    >
      <polyline
        points={points}
        fill="none"
        stroke={color}
        strokeWidth="1.5"
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}

function DetailRowItem({ row }: { row: DetailRow }): JSX.Element {
  return (
    <div className="flex items-center gap-3 px-4 py-2.5">
      <span
        className="size-2 shrink-0 rounded-full"
        style={{ backgroundColor: row.color }}
      />
      <span className="min-w-0 flex-1 truncate text-sm">{row.label}</span>
      <span className="text-muted-foreground shrink-0">
        <Sparkline series={row.series} color={row.color} />
      </span>
      <span
        className="w-24 shrink-0 text-right text-sm tabular-nums"
        title={row.total.toLocaleString()}
      >
        {compactNumber.format(row.total)}
      </span>
    </div>
  );
}

/**
 * Per-metric usage details for one billing cycle, rendered under the token
 * usage chart. Token-type and activity rows come from the org-wide analytics
 * aggregates; the message rows come from Postgres per-message token counts
 * (telemetry.queryMessageTokenStats).
 */
export function TumDetailsTable({
  cycle,
}: {
  cycle: BillingCycle;
}): JSX.Element {
  const client = useGramContext();
  const from = cycle.start;
  const to = cycle.end;

  // Ungrouped slice: one series carrying every measure per day, plus true
  // totals (distinct session counts can't be summed from grouped rows). The
  // generated hooks key their cache on gramSession only, so drive useQuery
  // directly with payload-encoding keys.
  const { data: usageData, isFetching: usageFetching } = useQuery({
    queryKey: ["tum-details", from.toISOString(), to.toISOString()],
    throwOnError: false,
    queryFn: () =>
      unwrapAsync(
        telemetryQuery(client, {
          queryPayload: {
            from,
            to,
            topN: 1,
            granularitySeconds: 86400,
          },
        }),
      ),
  });
  const { data: messageStats, isFetching: statsFetching } = useQuery({
    queryKey: ["tum-details-messages", from.toISOString(), to.toISOString()],
    throwOnError: false,
    queryFn: () =>
      unwrapAsync(
        telemetryQueryMessageTokenStats(client, {
          // The generator dedupes structurally identical payload schemas, so
          // this request reuses the risk-tokens payload shape/name.
          queryRiskTokensPayload: { from, to },
        }),
      ),
  });

  const groups = useMemo<DetailGroup[]>(() => {
    const points = usageData?.timeseries?.[0]?.points ?? [];
    const totals = usageData?.table?.[0]?.measures;
    const measureRow = (
      label: string,
      color: string,
      value: (m: QueryMeasures) => number,
    ): DetailRow => ({
      label,
      color,
      series: points.map((p) => value(p.measures)),
      total: totals ? value(totals) : 0,
    });

    const statsPoints = messageStats?.points ?? [];
    const statsRow = (
      label: string,
      color: string,
      value: (p: {
        riskyMessageTokens: number;
        toolMessageTokens: number;
      }) => number,
    ): DetailRow => ({
      label,
      color,
      series: statsPoints.map((p) => value(p)),
      total: statsPoints.reduce((sum, p) => sum + value(p), 0),
    });

    return [
      {
        heading: "Tokens",
        rows: [
          measureRow("Input tokens", "#60a5fa", (m) => m.totalInputTokens),
          measureRow("Output tokens", "#34d399", (m) => m.totalOutputTokens),
          measureRow(
            "Cache read tokens",
            "#f97316",
            (m) => m.cacheReadInputTokens,
          ),
          measureRow(
            "Cache write tokens",
            "#a78bfa",
            (m) => m.cacheCreationInputTokens,
          ),
        ],
      },
      {
        heading: "Activity",
        rows: [
          measureRow("Agent sessions", "#38bdf8", (m) => m.totalChats),
          measureRow("Tool calls", "#4ade80", (m) => m.totalToolCalls),
        ],
      },
      {
        heading: "Messages",
        rows: [
          statsRow(
            "Tokens in messages with risk findings",
            "#fb7185",
            (p) => p.riskyMessageTokens,
          ),
          statsRow(
            "Tokens from tool call messages",
            "#94a3b8",
            (p) => p.toolMessageTokens,
          ),
        ],
      },
    ];
  }, [usageData, messageStats]);

  const loading =
    (usageFetching && !usageData) || (statsFetching && !messageStats);

  return (
    <div className="border-border overflow-hidden rounded-lg border">
      <div className="text-muted-foreground flex items-center px-4 py-2.5 text-xs font-medium">
        <span className="flex-1">Metric</span>
        <span className="w-24 text-right">Usage</span>
      </div>
      {loading ? (
        <div className="space-y-3 p-4">
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-2/3" />
        </div>
      ) : (
        groups.map((group) => (
          <div key={group.heading}>
            <div className="bg-muted/50 text-muted-foreground border-border border-t px-4 py-1.5 text-xs font-medium">
              {group.heading}
            </div>
            <div className="divide-border divide-y">
              {group.rows.map((row) => (
                <DetailRowItem key={row.label} row={row} />
              ))}
            </div>
          </div>
        ))
      )}
    </div>
  );
}
