import { useMemo, type JSX } from "react";
import { barY, defineChart, lineY } from "@tanstack/charts";
import { Chart } from "@tanstack/charts/react";
import { scaleBand } from "@tanstack/charts/scales/band";
import { scaleLinear } from "@tanstack/charts/scales/linear";
import { tooltip } from "@tanstack/charts/tooltip";

import {
  formatMeterQuantity,
  meterDateLabel,
  meterPoints,
  type AdminMeterUsage,
  type MeterGranularity,
} from "./meterUsage";
import { inclusiveEnd } from "./billingUsageSearch";

export function MeterUsageChart({
  data,
  granularity,
  cumulative,
}: {
  data: AdminMeterUsage;
  granularity: MeterGranularity;
  cumulative: boolean;
}): JSX.Element {
  const definition = useMemo(() => {
    const points = meterPoints(data, granularity, cumulative);
    const options = { x: "from", y: "value" } as const;
    const mark = cumulative
      ? lineY(points, {
          ...options,
          stroke: "var(--primary)",
          strokeWidth: 2,
          points: true,
        })
      : barY(points, { ...options, fill: "var(--primary)" });
    return defineChart({
      marks: [mark],
      margin: { left: 84, right: 16, top: 16, bottom: 40 },
      theme: {
        foreground: "var(--foreground)",
        muted: "var(--muted-foreground)",
        grid: "var(--border)",
        background: "var(--card)",
      },
      scales: {
        x: {
          scale: () => scaleBand().padding(0.2),
          axis: { ticks: { format: (value) => meterDateLabel(String(value)) } },
        },
        y: {
          scale: scaleLinear().domain([
            0,
            Math.max(1, ...points.map((point) => point.value)),
          ]),
          nice: true,
          grid: true,
          axis: {
            ticks: {
              format: (value) =>
                formatMeterQuantity(
                  BigInt(Math.round(Number(value))).toString(),
                  data.unit,
                ),
            },
          },
        },
      },
      tooltip: {
        use: tooltip,
        className:
          "rounded-md border border-border bg-popover px-3 py-2 text-sm text-popover-foreground shadow-md",
        content: (focused) => {
          const point = focused[0]?.datum;
          if (!point) return { rows: [] };
          const end = inclusiveEnd(point.to);
          const start = point.from.slice(0, 10);
          return {
            title: `${start}${end === start ? "" : ` – ${end}`} (UTC)`,
            rows: [
              {
                label: cumulative ? "Cumulative total" : "Total",
                value: `${BigInt(point.total).toLocaleString("en-US")} ${data.unit === "bytes" ? "bytes" : "tokens"}`,
              },
            ],
          };
        },
      },
    });
  }, [data, granularity, cumulative]);

  return (
    <Chart
      definition={definition}
      height={320}
      ariaLabel={`${cumulative ? "Cumulative" : granularity} total usage in ${data.unit === "bytes" ? "bytes" : "tokens"}. Use arrow keys to inspect values.`}
    />
  );
}
