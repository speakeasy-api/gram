import {
  useOtherSeriesColor,
  useSeriesColors,
} from "@/components/chart/useSeriesColors";
import { type Column, Table } from "@/components/ui/Table";
import { meterBreakdownLabel } from "./meter-breakdown-options";
import {
  type MeterUsageData,
  formatMeterQuantity,
  meterSeriesIdentity,
  meterSeriesLabel,
} from "./meter-usage-adapter";

const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

type MeterTableRow = {
  identity: string;
  label: string;
  total: string;
  color: string;
  mono: boolean;
};

export function MeterUsageTable({
  data,
  projectSlugs,
}: {
  data: MeterUsageData;
  projectSlugs: ReadonlyMap<string, string>;
}): JSX.Element {
  const colors = useSeriesColors();
  const remainderColor = useOtherSeriesColor();
  const rows: MeterTableRow[] = data.breakdown.series.map((series, index) => {
    const label = meterSeriesLabel(
      series,
      data.breakdown.dimension,
      projectSlugs,
    );
    return {
      identity: meterSeriesIdentity(series),
      label,
      total: series.total,
      color:
        series.kind === "remainder"
          ? remainderColor
          : colors[
              (index + (data.breakdown.dimension === "total" ? 0 : 1)) %
                colors.length
            ]!,
      mono:
        data.breakdown.dimension === "project" &&
        series.key !== null &&
        UUID_RE.test(label),
    };
  });
  const columns: Column<MeterTableRow>[] = [
    {
      key: "label",
      header: "Value",
      render: (row) => (
        <div className="flex min-w-0 items-center gap-3">
          <span
            className="size-2 shrink-0 rounded-full"
            style={{ backgroundColor: row.color }}
          />
          <span
            className={row.mono ? "truncate font-mono text-xs" : "truncate"}
            title={row.label}
          >
            {row.mono ? `${row.label.slice(0, 8)}…` : row.label}
          </span>
        </div>
      ),
    },
    {
      key: "total",
      header: "Usage",
      width: "220px",
      render: (row) => (
        <span
          className="block w-full text-right tabular-nums"
          title={formatMeterQuantity(row.total, data.unit, "standard")}
        >
          {formatMeterQuantity(row.total, data.unit)}
        </span>
      ),
    },
  ];

  return (
    <div className="border-border border">
      <div className="border-border flex items-baseline justify-between border-b px-4 py-3">
        <span className="font-medium">Cumulative breakdown</span>
        <span className="text-muted-foreground text-sm">
          By {meterBreakdownLabel(data.family, data.breakdown.dimension)}
        </span>
      </div>
      <Table
        columns={columns}
        data={rows}
        rowKey={(row) => row.identity}
        noResultsMessage="No meter readings recorded."
      />
    </div>
  );
}
