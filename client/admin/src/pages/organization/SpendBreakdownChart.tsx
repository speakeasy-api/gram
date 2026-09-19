import { useMemo, type JSX } from "react";
import { barY, defineChart, stack } from "@tanstack/charts";
import { Chart } from "@tanstack/charts/react";
import { scaleBand } from "@tanstack/charts/scales/band";
import { scaleLinear } from "@tanstack/charts/scales/linear";
import { tooltip } from "@tanstack/charts/tooltip";

import { inclusiveEnd } from "./billingUsageSearch";
import {
  meterAxisTicks,
  meterDateLabel,
  type MeterGranularity,
} from "./meterUsageUtils";
import {
  SPEND_PRODUCT_COLOR,
  formatScaledUsdAxis,
  formatSpendUsd,
  spendChartData,
  sumSpendCosts,
  type AdminSpendBreakdown,
  type SpendProductID,
} from "./spendBreakdownUtils";

export function SpendBreakdownChart({
  data,
  selectedProductIDs,
  granularity,
  cumulative,
}: {
  data: AdminSpendBreakdown;
  selectedProductIDs: ReadonlySet<SpendProductID>;
  granularity: MeterGranularity;
  cumulative: boolean;
}): JSX.Element {
  const definition = useMemo(() => {
    const chart = spendChartData(
      data,
      selectedProductIDs,
      granularity,
      cumulative,
    );
    const yTicks = meterAxisTicks(chart.maximum);
    const mark = barY(chart.points, {
      x: "from",
      y: "scaledCost",
      z: "productId",
      fill: (point) => SPEND_PRODUCT_COLOR[point.productId],
      layout: stack({ order: data.products.map((product) => product.id) }),
      inset: 1,
    });
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
          scale: scaleLinear().domain([0, yTicks.at(-1) ?? chart.maximum]),
          grid: true,
          axis: {
            ticks: {
              values: yTicks,
              format: (value) =>
                formatScaledUsdAxis(Number(value), chart.scale),
            },
          },
        },
      },
      tooltip: {
        use: tooltip,
        className:
          "rounded-md border border-border bg-popover px-3 py-2 text-sm text-popover-foreground shadow-md",
        content: (focused) => {
          const points = focused.map((entry) => entry.datum);
          const first = points[0];
          if (!first) return { rows: [] };
          const start = first.from.slice(0, 10);
          const end = inclusiveEnd(first.to);
          const title = `${start}${start === end ? "" : ` – ${end}`} (UTC)${first.inProgress ? " *" : ""}`;
          const rows = points.map((point) => ({
            label: point.productLabel,
            value: formatSpendUsd(point.exactCostUsd),
          }));
          if (points.length > 1) {
            rows.push({
              label: "Total",
              value: formatSpendUsd(
                sumSpendCosts(points.map((point) => point.exactCostUsd)),
              ),
            });
          }
          return { title, rows };
        },
      },
    });
  }, [cumulative, data, granularity, selectedProductIDs]);

  return (
    <Chart
      definition={definition}
      height={320}
      ariaLabel={`${cumulative ? "Cumulative" : granularity} estimated spend in USD by product. Use arrow keys to inspect exact values.`}
    />
  );
}
