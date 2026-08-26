import { AXIS, TOOLTIP, withAlpha } from "@/components/chart/palette";
import {
  useIsDarkTheme,
  useSeriesColors,
} from "@/components/chart/useSeriesColors";
import { Icon } from "@/components/ui/Icon";
import { MetricCard } from "@/components/ui/MetricCard";
import { Skeleton } from "@/components/ui/Skeleton";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import type { ExploreMetaResult } from "@gram/client/models/components/exploremetaresult.js";
import type { ExploreQueryResult } from "@gram/client/models/components/explorequeryresult.js";
import type { ExploreRow } from "@gram/client/models/components/explorerow.js";
import {
  BarElement,
  CategoryScale,
  Chart as ChartJS,
  Filler,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip,
  type ChartDataset,
  type ChartOptions,
} from "chart.js";
import { useMemo } from "react";
import { Chart } from "react-chartjs-2";
import {
  calculationDisplayLabel,
  calculationUnits,
  dimensionDisplayLabel,
  formatMeasureValue,
  unitForCalculation,
  type ChartType,
} from "./exploreModel";

ChartJS.register(
  CategoryScale,
  LinearScale,
  BarElement,
  LineElement,
  PointElement,
  Filler,
  Tooltip,
  Legend,
);

/** Label for one row's group tuple; empty tuples read as "all". */
function tupleLabel(row: ExploreRow): string {
  if (row.group.length === 0) return "all";
  return row.group.map((v) => (v === "" ? "(none)" : v)).join(" · ");
}

/**
 * Axis tick label for a raw ISO bucket, Grafana-style. Sub-daily
 * granularities render local-midnight buckets as dates ("Aug 13") and all
 * other buckets as 24h times ("08:00"); daily-or-coarser granularities are
 * date-only.
 */
function formatBucketTick(iso: string, granularitySeconds: number): string {
  const date = new Date(iso);
  const isLocalMidnight = date.getHours() === 0 && date.getMinutes() === 0;
  if (granularitySeconds >= 86400 || isLocalMidnight) {
    return date.toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
    });
  }
  return date.toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

/** Full bucket timestamp for tooltip titles, since axis ticks are sparse. */
function formatBucketFull(iso: string, granularitySeconds: number): string {
  const date = new Date(iso);
  if (granularitySeconds >= 86400) {
    return date.toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
    });
  }
  return date.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

/**
 * Renders one query's results in the selected shape, including loading,
 * error, and empty states.
 */
export function ExploreResultsBody({
  result,
  meta,
  chartType,
  loading,
  errorMessage,
  height,
}: {
  result: ExploreQueryResult | undefined;
  meta: ExploreMetaResult | undefined;
  chartType: ChartType;
  loading: boolean;
  errorMessage: string | null;
  height: number;
}): JSX.Element {
  if (loading) {
    return (
      <div style={{ height }}>
        <Skeleton className="h-full w-full" />
      </div>
    );
  }
  if (errorMessage) {
    return (
      <div
        role="alert"
        className="text-muted-foreground flex flex-col items-center justify-center gap-2 px-8 text-center text-sm"
        style={{ height }}
      >
        <Icon name="triangle-alert" className="size-5" />
        <span>{errorMessage}</span>
      </div>
    );
  }
  if (!result || result.rows.length === 0) {
    return (
      <div
        className="text-muted-foreground flex flex-col items-center justify-center gap-2 text-sm"
        style={{ height }}
      >
        <Icon name="chart-scatter" className="size-5" />
        <span>No data found for this time duration.</span>
      </div>
    );
  }
  if (
    chartType !== "table" &&
    chartType !== "number" &&
    calculationUnits(meta, result.dataset, result.calculations).length > 1
  ) {
    return (
      <div
        role="alert"
        className="text-muted-foreground flex flex-col items-center justify-center gap-2 px-8 text-center text-sm"
        style={{ height }}
      >
        <Icon name="triangle-alert" className="size-5" />
        <span>
          Charts cannot combine calculations with different units. Choose
          calculations with the same unit or use a table.
        </span>
      </div>
    );
  }
  switch (chartType) {
    case "table":
      return <ResultTable result={result} meta={meta} maxHeight={height} />;
    case "number":
      return <ResultNumbers result={result} meta={meta} />;
    case "line":
    case "area":
    case "bar":
      return (
        <ResultTimeseries
          result={result}
          meta={meta}
          chartType={chartType}
          height={height}
        />
      );
  }
}

function ResultTimeseries({
  result,
  meta,
  chartType,
  height,
}: {
  result: ExploreQueryResult;
  meta: ExploreMetaResult | undefined;
  chartType: ChartType;
  height: number;
}): JSX.Element {
  const seriesColors = useSeriesColors();
  const isDark = useIsDarkTheme();

  const { labels, datasets, primaryUnit } = useMemo(() => {
    const buckets = [...new Set(result.rows.map((r) => r.bucket))].sort();
    const bucketIndex = new Map(buckets.map((b, i) => [b, i]));

    // One series per (group tuple × calculation). Single-calculation queries
    // label series by tuple; multi-calculation queries include the field label.
    const seriesKeys: string[] = [];
    const seriesValues = new Map<string, (number | null)[]>();
    for (const row of result.rows) {
      for (const calculation of result.calculations) {
        const value = row.values[calculation];
        if (value === undefined) continue;
        const tuple = tupleLabel(row);
        let label: string;
        if (result.calculations.length === 1) {
          label = tuple;
        } else if (row.group.length === 0) {
          label = calculationDisplayLabel(meta, result.dataset, calculation);
        } else {
          label = `${calculationDisplayLabel(meta, result.dataset, calculation)} · ${tuple}`;
        }
        if (!seriesValues.has(label)) {
          seriesKeys.push(label);
          seriesValues.set(
            label,
            buckets.map(() => null),
          );
        }
        const idx = bucketIndex.get(row.bucket);
        if (idx !== undefined) seriesValues.get(label)![idx] = value;
      }
    }

    const line = chartType !== "bar";
    const built = seriesKeys.map((label, i) => {
      const color = seriesColors[i % seriesColors.length]!;
      const base = {
        label,
        data: seriesValues.get(label)!,
        backgroundColor: chartType === "area" ? withAlpha(color, 0.2) : color,
        borderColor: color,
      };
      if (line) {
        return {
          ...base,
          type: "line",
          borderWidth: 1.5,
          pointRadius: 0,
          fill: chartType === "area",
          spanGaps: true,
        } as ChartDataset<"bar" | "line", (number | null)[]>;
      }
      return { ...base, type: "bar" } as ChartDataset<
        "bar" | "line",
        (number | null)[]
      >;
    });

    // Raw ISO buckets stay as labels; the x scale's ticks callback and the
    // tooltip title format them for display.
    return {
      labels: buckets,
      datasets: built,
      primaryUnit: unitForCalculation(
        meta,
        result.dataset,
        result.calculations[0] ?? "",
      ),
    };
  }, [result, meta, chartType, seriesColors]);

  const granularitySeconds = result.granularitySeconds;

  const options = useMemo<ChartOptions<"line" | "bar">>(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: "index", intersect: false },
      plugins: {
        legend: {
          display: datasets.length > 1,
          position: "bottom",
          labels: { color: AXIS.label, boxWidth: 10, boxHeight: 10 },
        },
        tooltip: {
          ...TOOLTIP,
          callbacks: {
            title: (items) => {
              const iso = items[0]?.label;
              return iso ? formatBucketFull(iso, granularitySeconds) : "";
            },
            label: (ctx) =>
              `${ctx.dataset.label}: ${formatMeasureValue(Number(ctx.parsed.y ?? 0), primaryUnit)}`,
          },
        },
      },
      scales: {
        x: {
          grid: { display: false },
          ticks: {
            color: AXIS.label,
            autoSkip: true,
            maxRotation: 0,
            maxTicksLimit: 10,
            callback: (value) => {
              const iso = labels[Number(value)];
              return iso === undefined
                ? ""
                : formatBucketTick(iso, granularitySeconds);
            },
          },
        },
        y: {
          beginAtZero: true,
          grid: { color: isDark ? AXIS.gridDark : AXIS.grid },
          ticks: {
            color: AXIS.label,
            callback: (value) => formatMeasureValue(Number(value), primaryUnit),
          },
        },
      },
    }),
    [datasets.length, primaryUnit, isDark, labels, granularitySeconds],
  );

  return (
    <div style={{ height }}>
      <Chart
        type={chartType === "bar" ? "bar" : "line"}
        data={{ labels, datasets }}
        options={options}
      />
    </div>
  );
}

function ResultTable({
  result,
  meta,
  maxHeight,
}: {
  result: ExploreQueryResult;
  meta: ExploreMetaResult | undefined;
  maxHeight: number;
}): JSX.Element {
  const columns: Column<ExploreRow>[] = [
    ...result.groupBy.map((dim, i) => ({
      key: `g_${dim}`,
      header: dimensionDisplayLabel(meta, result.dataset, dim),
      render: (row: ExploreRow) => (
        <Text className="font-medium">{row.group[i] || "(none)"}</Text>
      ),
    })),
    ...result.calculations.map((calculation) => ({
      key: `c_${calculation}`,
      header: calculationDisplayLabel(meta, result.dataset, calculation),
      render: (row: ExploreRow) => {
        const value = row.values[calculation];
        return (
          <Text mono>
            {value === undefined
              ? "—"
              : formatMeasureValue(
                  value,
                  unitForCalculation(meta, result.dataset, calculation),
                )}
          </Text>
        );
      },
    })),
  ];
  return (
    <div className="overflow-y-auto" style={{ maxHeight }}>
      <Table
        columns={columns}
        data={result.rows}
        rowKey={(row) => row.group.join("\x1f") || "all"}
        noResultsMessage={<Text>No results</Text>}
      />
    </div>
  );
}

function ResultNumbers({
  result,
  meta,
}: {
  result: ExploreQueryResult;
  meta: ExploreMetaResult | undefined;
}): JSX.Element {
  const row = result.rows[0];
  return (
    <MetricCard.Group>
      {result.calculations.map((calculation) => {
        const value = row?.values[calculation];
        return (
          <MetricCard
            key={calculation}
            label={calculationDisplayLabel(meta, result.dataset, calculation)}
            value={
              value === undefined
                ? "—"
                : formatMeasureValue(
                    value,
                    unitForCalculation(meta, result.dataset, calculation),
                  )
            }
            tone="information"
          />
        );
      })}
    </MetricCard.Group>
  );
}
