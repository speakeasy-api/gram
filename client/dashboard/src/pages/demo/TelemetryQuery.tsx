// Throwaway demo/testing page for the generic org-scoped analytics
// `telemetry.query` API. No polish — just enough surface to exercise every
// payload knob (group_by, filters, sort_by, top_n, granularity) and inspect
// the grouped table + per-group timeseries it returns.
import { Page } from "@/components/page-layout";
import { useGramContext } from "@gram/client/react-query";
import { telemetryQuery } from "@gram/client/funcs/telemetryQuery";
import {
  Dimension,
  GroupBy,
  QueryPayloadSortBy,
  type QueryFilter,
  type QueryMeasures,
  type QueryResult,
} from "@gram/client/models/components";
import { unwrapAsync } from "@gram/client/types/fp";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@speakeasy-api/moonshine";
import { useMemo, useState } from "react";
import {
  CategoryScale,
  Chart as ChartJS,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip,
} from "chart.js";
import { Line } from "react-chartjs-2";

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
);

const DIMENSIONS = Object.values(Dimension);
const GROUP_BY_OPTIONS = Object.values(GroupBy);
const SORT_BY_OPTIONS = Object.values(QueryPayloadSortBy);

// The measures available in QueryMeasures. Keyed so we can both plot a chosen
// one on the chart and render every one in the table.
const MEASURE_KEYS: Array<keyof QueryMeasures> = [
  "totalCost",
  "totalTokens",
  "totalInputTokens",
  "totalOutputTokens",
  "cacheReadInputTokens",
  "cacheCreationInputTokens",
  "totalToolCalls",
  "totalChats",
];

const GRANULARITY_OPTIONS = [
  { label: "auto (default)", value: "" },
  { label: "1 hour", value: "3600" },
  { label: "6 hours", value: "21600" },
  { label: "1 day", value: "86400" },
  { label: "1 week", value: "604800" },
];

// A handful of distinct line colors; cycled by series index.
const SERIES_COLORS = [
  "#6366f1",
  "#ec4899",
  "#22c55e",
  "#f59e0b",
  "#06b6d4",
  "#ef4444",
  "#8b5cf6",
  "#14b8a6",
  "#eab308",
  "#f97316",
  "#64748b",
];

// Default range: last 7 days, rounded to the hour, as `datetime-local` strings.
function toLocalInputValue(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(
    d.getHours(),
  )}:${pad(d.getMinutes())}`;
}

interface CommittedParams {
  from: string;
  to: string;
  groupBy: string;
  sortBy: string;
  topN: number;
  granularitySeconds: string;
  filters: QueryFilter[];
}

function formatMeasure(key: keyof QueryMeasures, value: number): string {
  if (key === "totalCost") {
    return `$${value.toLocaleString(undefined, {
      minimumFractionDigits: 2,
      maximumFractionDigits: 4,
    })}`;
  }
  return value.toLocaleString();
}

// Renders the per-group distinct values of every non-group-by dimension. Keys
// with no values (the attribute was unset for this group) are skipped.
function DimensionValues({
  values,
}: {
  values: { [k: string]: string[] };
}): React.JSX.Element {
  const nonEmpty = Object.entries(values)
    .filter(([, vs]) => vs.length > 0)
    .sort(([a], [b]) => a.localeCompare(b));
  if (nonEmpty.length === 0) {
    return <span className="text-muted-foreground">—</span>;
  }
  return (
    <div className="flex flex-col gap-1">
      {nonEmpty.map(([dim, vs]) => (
        <div key={dim} className="text-xs">
          <span className="text-muted-foreground">{dim}: </span>
          <span>{vs.join(", ")}</span>
        </div>
      ))}
    </div>
  );
}

export default function TelemetryQueryDemo(): React.JSX.Element {
  const now = useMemo(() => new Date(), []);
  const weekAgo = useMemo(
    () => new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000),
    [now],
  );

  // Draft form state.
  const [from, setFrom] = useState(toLocalInputValue(weekAgo));
  const [to, setTo] = useState(toLocalInputValue(now));
  const [groupBy, setGroupBy] = useState<string>("");
  const [sortBy, setSortBy] = useState<string>("total_cost");
  const [topN, setTopN] = useState<number>(10);
  const [granularitySeconds, setGranularitySeconds] = useState<string>("");
  const [filters, setFilters] = useState<QueryFilter[]>([]);
  const [chartMeasure, setChartMeasure] =
    useState<keyof QueryMeasures>("totalCost");

  // Filter draft inputs.
  const [filterDim, setFilterDim] = useState<string>(DIMENSIONS[0] ?? "");
  const [filterValues, setFilterValues] = useState<string>("");

  // Committed params drive the query (so editing the form doesn't auto-fire).
  const [committed, setCommitted] = useState<CommittedParams | null>(null);

  const client = useGramContext();

  const {
    data: result,
    isFetching,
    isError,
    error,
  } = useQuery<QueryResult>({
    queryKey: ["telemetry-query-demo", committed],
    enabled: committed !== null,
    queryFn: () => {
      const c = committed!;
      return unwrapAsync(
        telemetryQuery(client, {
          queryPayload: {
            from: new Date(c.from),
            to: new Date(c.to),
            groupBy: c.groupBy ? (c.groupBy as GroupBy) : undefined,
            sortBy: c.sortBy ? (c.sortBy as QueryPayloadSortBy) : undefined,
            topN: c.topN,
            granularitySeconds: c.granularitySeconds
              ? Number(c.granularitySeconds)
              : undefined,
            filters: c.filters.length ? c.filters : undefined,
          },
        }),
      );
    },
  });

  const runQuery = () => {
    setCommitted({
      from,
      to,
      groupBy,
      sortBy,
      topN,
      granularitySeconds,
      filters,
    });
  };

  const addFilter = () => {
    const values = filterValues
      .split(",")
      .map((v) => v.trim())
      .filter(Boolean);
    if (!filterDim || values.length === 0) return;
    setFilters((prev) => [
      ...prev,
      { dimension: filterDim as Dimension, values },
    ]);
    setFilterValues("");
  };

  const removeFilter = (i: number) => {
    setFilters((prev) => prev.filter((_, idx) => idx !== i));
  };

  // Build chart data from the timeseries for the chosen measure. All series
  // share gap-filled buckets, so the first series' bucket times define labels.
  const chartData = useMemo(() => {
    if (!result || result.timeseries.length === 0) return null;
    const labels = result.timeseries[0]!.points.map((p) => {
      const ms = Number(BigInt(p.bucketTimeUnixNano) / 1_000_000n);
      return new Date(ms).toLocaleString(undefined, {
        month: "short",
        day: "numeric",
        hour: "2-digit",
      });
    });
    const datasets = result.timeseries.map((series, i) => {
      const color = SERIES_COLORS[i % SERIES_COLORS.length]!;
      return {
        label: series.groupValue || "(all)",
        data: series.points.map((p) => p.measures[chartMeasure]),
        borderColor: color,
        backgroundColor: color,
        tension: 0.2,
        pointRadius: 1,
      };
    });
    return { labels, datasets };
  }, [result, chartMeasure]);

  return (
    <Page>
      <Page.Header>
        <Page.Header.Title>telemetry.query demo</Page.Header.Title>
      </Page.Header>
      <Page.Body>
        <p className="text-muted-foreground mb-4 text-sm">
          Exercise the generic org-scoped analytics query. Org-scoped: spans
          every project in your org.
        </p>
        {/* --- Controls --- */}
        <div className="border-border bg-card flex flex-col gap-4 rounded-lg border p-4">
          <div className="flex flex-wrap gap-4">
            <label className="flex flex-col text-sm">
              <span className="text-muted-foreground mb-1">From</span>
              <input
                type="datetime-local"
                value={from}
                onChange={(e) => setFrom(e.target.value)}
                className="border-border bg-background rounded border px-2 py-1"
              />
            </label>
            <label className="flex flex-col text-sm">
              <span className="text-muted-foreground mb-1">To</span>
              <input
                type="datetime-local"
                value={to}
                onChange={(e) => setTo(e.target.value)}
                className="border-border bg-background rounded border px-2 py-1"
              />
            </label>
            <label className="flex flex-col text-sm">
              <span className="text-muted-foreground mb-1">Group by</span>
              <select
                value={groupBy}
                onChange={(e) => setGroupBy(e.target.value)}
                className="border-border bg-background rounded border px-2 py-1"
              >
                <option value="">(none — single aggregate)</option>
                {GROUP_BY_OPTIONS.map((d) => (
                  <option key={d} value={d}>
                    {d}
                  </option>
                ))}
              </select>
            </label>
            <label className="flex flex-col text-sm">
              <span className="text-muted-foreground mb-1">Sort by</span>
              <select
                value={sortBy}
                onChange={(e) => setSortBy(e.target.value)}
                className="border-border bg-background rounded border px-2 py-1"
              >
                {SORT_BY_OPTIONS.map((m) => (
                  <option key={m} value={m}>
                    {m}
                  </option>
                ))}
              </select>
            </label>
            <label className="flex flex-col text-sm">
              <span className="text-muted-foreground mb-1">Top N</span>
              <input
                type="number"
                min={1}
                value={topN}
                onChange={(e) => setTopN(Number(e.target.value))}
                className="border-border bg-background w-24 rounded border px-2 py-1"
              />
            </label>
            <label className="flex flex-col text-sm">
              <span className="text-muted-foreground mb-1">Granularity</span>
              <select
                value={granularitySeconds}
                onChange={(e) => setGranularitySeconds(e.target.value)}
                className="border-border bg-background rounded border px-2 py-1"
              >
                {GRANULARITY_OPTIONS.map((g) => (
                  <option key={g.value} value={g.value}>
                    {g.label}
                  </option>
                ))}
              </select>
            </label>
          </div>

          {/* --- Filters --- */}
          <div className="flex flex-col gap-2">
            <span className="text-muted-foreground text-sm">
              Filters (all ANDed)
            </span>
            <div className="flex flex-wrap items-end gap-2">
              <select
                value={filterDim}
                onChange={(e) => setFilterDim(e.target.value)}
                className="border-border bg-background rounded border px-2 py-1 text-sm"
              >
                {DIMENSIONS.map((d) => (
                  <option key={d} value={d}>
                    {d}
                  </option>
                ))}
              </select>
              <input
                type="text"
                value={filterValues}
                placeholder="value1, value2 (IN)"
                onChange={(e) => setFilterValues(e.target.value)}
                className="border-border bg-background min-w-64 rounded border px-2 py-1 text-sm"
              />
              <Button variant="secondary" size="sm" onClick={addFilter}>
                Add filter
              </Button>
            </div>
            {filters.length > 0 && (
              <div className="flex flex-wrap gap-2">
                {filters.map((f, i) => (
                  <span
                    key={`${f.dimension}-${i}`}
                    className="border-border bg-background flex items-center gap-2 rounded border px-2 py-1 text-xs"
                  >
                    <code>
                      {f.dimension} IN [{f.values.join(", ")}]
                    </code>
                    <button
                      type="button"
                      onClick={() => removeFilter(i)}
                      className="text-muted-foreground hover:text-foreground"
                      aria-label="Remove filter"
                    >
                      ✕
                    </button>
                  </span>
                ))}
              </div>
            )}
          </div>

          <div>
            <Button onClick={runQuery} disabled={isFetching}>
              {isFetching ? "Running…" : "Run query"}
            </Button>
          </div>
        </div>

        {/* --- Results --- */}
        {isError && (
          <div className="border-destructive text-destructive mt-4 rounded-lg border p-4 text-sm">
            {(error as Error)?.message ?? "Query failed"}
          </div>
        )}

        {result && (
          <div className="mt-4 flex flex-col gap-4">
            <div className="text-muted-foreground text-sm">
              group_by: <code>{result.groupBy || "(none)"}</code> ·
              interval_seconds: <code>{result.intervalSeconds}</code> ·{" "}
              {result.table.length} group(s)
            </div>

            {/* Timeseries chart */}
            <div className="border-border bg-card rounded-lg border p-4">
              <div className="mb-4 flex items-center justify-between">
                <h3 className="font-semibold">Timeseries</h3>
                <label className="flex items-center gap-2 text-sm">
                  <span className="text-muted-foreground">Measure</span>
                  <select
                    value={chartMeasure}
                    onChange={(e) =>
                      setChartMeasure(e.target.value as keyof QueryMeasures)
                    }
                    className="border-border bg-background rounded border px-2 py-1"
                  >
                    {MEASURE_KEYS.map((m) => (
                      <option key={m} value={m}>
                        {m}
                      </option>
                    ))}
                  </select>
                </label>
              </div>
              {chartData ? (
                <Line
                  data={chartData}
                  options={{
                    responsive: true,
                    interaction: { mode: "index", intersect: false },
                    plugins: {
                      legend: { display: result.timeseries.length > 1 },
                    },
                    scales: { y: { beginAtZero: true } },
                  }}
                />
              ) : (
                <p className="text-muted-foreground text-sm">No data.</p>
              )}
            </div>

            {/* Grouped table */}
            <div className="border-border bg-card overflow-x-auto rounded-lg border p-4">
              <h3 className="mb-4 font-semibold">Grouped table</h3>
              <table className="w-full text-left text-sm">
                <thead className="text-muted-foreground border-border border-b">
                  <tr>
                    <th className="py-2 pr-4">group</th>
                    {MEASURE_KEYS.map((m) => (
                      <th key={m} className="py-2 pr-4 text-right">
                        {m}
                      </th>
                    ))}
                    <th className="py-2 pr-4">members (per dimension)</th>
                  </tr>
                </thead>
                <tbody>
                  {result.table.map((row, i) => (
                    <tr
                      key={`${row.groupValue}-${i}`}
                      className="border-border/50 border-b align-top"
                    >
                      <td className="py-2 pr-4 font-medium">
                        {row.groupValue || "(all)"}
                      </td>
                      {MEASURE_KEYS.map((m) => (
                        <td
                          key={m}
                          className="py-2 pr-4 text-right tabular-nums"
                        >
                          {formatMeasure(m, row.measures[m])}
                        </td>
                      ))}
                      <td className="py-2 pr-4">
                        <DimensionValues values={row.dimensionValues} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {/* Raw JSON */}
            <details className="border-border bg-card rounded-lg border p-4">
              <summary className="cursor-pointer font-semibold">
                Raw response JSON
              </summary>
              <pre className="mt-2 overflow-x-auto text-xs">
                {JSON.stringify(result, null, 2)}
              </pre>
            </details>
          </div>
        )}
      </Page.Body>
    </Page>
  );
}
