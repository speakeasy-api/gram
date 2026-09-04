import { ChartCard } from "@/components/chart/ChartCard";
import { TOOLTIP } from "@/components/chart/palette";
import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
import { useSeriesColors } from "@/components/chart/useSeriesColors";
import { type FilterChip } from "@/components/observe/ObserveFilterBar";
import { HOOK_SOURCE_FILTER_PATH } from "@/components/observe/observeTargetFilters";
import {
  type ConsumptionGroupBy,
  type ConsumptionMeasure,
  CONSUMPTION_GROUP_OPTIONS,
  buildConsumptionFilters,
  consumptionBarRows,
  consumptionRowLabel,
  consumptionTimeSeriesStacks,
  hasConsumptionActivity,
  rankedConsumptionRows,
  sortByForMeasure,
  sumConsumptionRows,
} from "@/components/observe/toolCallConsumption";
import { StackedTimeSeriesPanel } from "@/components/stacked-time-series-panel";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Skeleton, SkeletonTable } from "@/components/ui/Skeleton";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { formatCompact } from "@/lib/format";
import { formatPlatform } from "@/lib/formatPlatform";
import { formatCost } from "@/lib/money";
import { telemetryQuery } from "@gram/client/funcs/telemetryQuery";
import { Dimension } from "@gram/client/models/components/queryfilter.js";
import { type GroupBy } from "@gram/client/models/components/querypayload.js";
import { type QueryResult } from "@gram/client/models/components/queryresult.js";
import { type QueryRow } from "@gram/client/models/components/queryrow.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import {
  BarElement,
  BarController,
  CategoryScale,
  Chart as ChartJS,
  type ChartOptions,
  LinearScale,
  Tooltip,
} from "chart.js";
import { Bar } from "react-chartjs-2";
import { useMemo, useState } from "react";

ChartJS.register(
  CategoryScale,
  LinearScale,
  BarElement,
  BarController,
  Tooltip,
);

const CONSUMPTION_TOP_N = 20;

type ConsumptionTableRow = {
  key: string;
  label: string;
  groupValue: string;
  toolCalls: number;
  inputTokens: number;
  outputTokens: number;
  tokens: number;
  cost: number;
};

function formatCount(value: number): string {
  return value.toLocaleString();
}

export function InsightsToolCallConsumption({
  from,
  to,
  hookSources,
  accountType,
  addFilter,
  onRangeSelect,
}: {
  from: Date;
  to: Date;
  hookSources: string[];
  accountType: string;
  addFilter: (chip: FilterChip) => void;
  onRangeSelect?: (from: Date, to: Date) => void;
}): JSX.Element | null {
  const project = useProject();
  const client = useGramContext();
  const seriesColors = useSeriesColors();
  const [groupBy, setGroupBy] = useState<ConsumptionGroupBy>(
    Dimension.HookSource,
  );
  const [measure, setMeasure] = useState<ConsumptionMeasure>("tokens");
  const [expandedChart, setExpandedChart] = useState<string | null>(null);

  const filters = useMemo(
    () => buildConsumptionFilters(project.id, hookSources, accountType),
    [project.id, hookSources, accountType],
  );

  const query = useQuery({
    queryKey: [
      "tool-call-consumption",
      project.id,
      from.toISOString(),
      to.toISOString(),
      hookSources,
      accountType,
      groupBy,
      measure,
    ],
    queryFn: () =>
      unwrapAsync(
        telemetryQuery(client, {
          queryPayload: {
            from,
            to,
            groupBy: groupBy as GroupBy,
            sortBy: sortByForMeasure(measure),
            topN: CONSUMPTION_TOP_N,
            filters,
          },
        }),
      ),
    enabled: !!project.id,
    placeholderData: keepPreviousData,
    throwOnError: false,
  });

  const data = query.data;
  const totals = useMemo(
    () => sumConsumptionRows(data?.table ?? []),
    [data?.table],
  );
  const hasActivity = hasConsumptionActivity(data?.table ?? []);
  const groupLabel =
    CONSUMPTION_GROUP_OPTIONS.find((option) => option.value === groupBy)
      ?.label ?? "Group";

  const barRows = useMemo(
    () => consumptionBarRows(data?.table ?? [], groupBy, measure),
    [data?.table, groupBy, measure],
  );
  const tableRows = useMemo(
    () => toTableRows(data?.table ?? [], groupBy),
    [data?.table, groupBy],
  );
  const { bucketsMs, stacks } = useMemo(
    () =>
      consumptionTimeSeriesStacks(
        data?.timeseries ?? [],
        groupBy,
        measure,
        rollupGroupValue(data),
      ),
    [data, groupBy, measure],
  );

  const columns = useMemo<Column<ConsumptionTableRow>[]>(
    () => [
      {
        key: "label",
        header: groupLabel,
        sortable: true,
        sortValue: (row) => row.label,
        render: (row) => (
          <Text as="span" className="font-medium">
            {row.label}
          </Text>
        ),
      },
      {
        key: "toolCalls",
        header: "Calls",
        width: "90px",
        sortable: true,
        sortValue: (row) => row.toolCalls,
        render: (row) => <Text as="span">{formatCount(row.toolCalls)}</Text>,
      },
      {
        key: "inputTokens",
        header: "Input",
        width: "100px",
        sortable: true,
        sortValue: (row) => row.inputTokens,
        render: (row) => <Text as="span">{formatCount(row.inputTokens)}</Text>,
      },
      {
        key: "outputTokens",
        header: "Output",
        width: "100px",
        sortable: true,
        sortValue: (row) => row.outputTokens,
        render: (row) => <Text as="span">{formatCount(row.outputTokens)}</Text>,
      },
      {
        key: "tokens",
        header: "Tokens",
        width: "100px",
        sortable: true,
        sortValue: (row) => row.tokens,
        render: (row) => <Text as="span">{formatCount(row.tokens)}</Text>,
      },
      {
        key: "cost",
        header: "Cost",
        width: "100px",
        sortable: true,
        sortValue: (row) => row.cost,
        render: (row) => <Text as="span">{formatCost(row.cost)}</Text>,
      },
    ],
    [groupLabel],
  );

  const handleBarClick = (groupValue: string) => {
    if (
      groupBy !== Dimension.HookSource ||
      !groupValue ||
      groupValue === "Other"
    ) {
      return;
    }
    addFilter({
      display: formatPlatform(groupValue),
      filters: [groupValue],
      path: HOOK_SOURCE_FILTER_PATH,
    });
  };

  if (query.isError && !data) {
    return null;
  }

  if (!query.isPending && !hasActivity) {
    return null;
  }

  const measureNoun = measure === "calls" ? "tool calls" : "tokens";
  const barTitle = `${measure === "calls" ? "Tool calls" : "Tokens"} by ${groupLabel}`;

  return (
    <section className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-1">
          <p className="text-eyebrow">Token consumption</p>
          <Text muted small>
            Agent-turn tokens attributed to the tool that ran on that turn —
            grouped by agent harness, MCP server, or MCP tool.
          </Text>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <SegmentedControl
            value={groupBy}
            onChange={setGroupBy}
            options={CONSUMPTION_GROUP_OPTIONS.map((option) => ({
              value: option.value,
              label: option.label,
            }))}
          />
          <SegmentedControl
            value={measure}
            onChange={setMeasure}
            options={[
              {
                value: "calls",
                label: "Calls",
                tooltip: "Rank by completed tool calls",
              },
              {
                value: "tokens",
                label: "Tokens",
                tooltip: "Rank by attributed token consumption",
              },
            ]}
          />
        </div>
      </div>

      <StatTileGroup>
        {query.isPending && !data ? (
          Array.from({ length: 5 }).map((_, index) => (
            <Skeleton key={index} className="h-[104px] flex-1" />
          ))
        ) : (
          <>
            <StatTile
              title="Tool Calls"
              value={totals.toolCalls}
              tone="information"
              icon="wrench"
            />
            <StatTile
              title="Input Tokens"
              value={totals.inputTokens}
              tone="information"
              icon="log-in"
            />
            <StatTile
              title="Output Tokens"
              value={totals.outputTokens}
              tone="information"
              icon="log-out"
            />
            <StatTile
              title="Total Tokens"
              value={totals.tokens}
              tone="information"
              icon="coins"
            />
            <StatTile
              title="Cost"
              value={totals.cost}
              format="currency"
              tone="neutral"
              icon="credit-card"
            />
          </>
        )}
      </StatTileGroup>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <ChartCard
          title={barTitle}
          chartId="tool-call-consumption-bar"
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
          loading={query.isPending && !data}
          error={query.isError}
          hasData={barRows.length > 0}
        >
          {barRows.length === 0 ? (
            <div className="text-muted-foreground flex h-24 items-center justify-center text-sm">
              No {measureNoun} in this period
            </div>
          ) : (
            <ConsumptionBarChart
              rows={barRows}
              color={seriesColors[0]!}
              measure={measure}
              onSelect={handleBarClick}
            />
          )}
        </ChartCard>

        <div className="border-border bg-card min-h-0 border">
          {query.isPending && !data ? (
            <div className="p-4">
              <SkeletonTable />
            </div>
          ) : (
            <Table
              columns={columns}
              data={tableRows}
              rowKey={(row) => row.key}
              className="max-h-[360px] overflow-y-auto"
              noResultsMessage={<Text muted>No matching groups</Text>}
            />
          )}
        </div>
      </div>

      <StackedTimeSeriesPanel
        title={`${measure === "calls" ? "Tool calls" : "Tokens"} over time`}
        headerHint={`Attributed ${measureNoun} over time, stacked by ${groupLabel.toLowerCase()}. Click or drag on the chart to zoom to a period.`}
        bucketsMs={bucketsMs}
        stacks={query.isError ? [] : stacks}
        formatValue={
          measure === "calls"
            ? (value) => `${formatCount(value)} calls`
            : (value) => `${formatCount(value)} tokens`
        }
        formatAxisValue={formatCompact}
        emptyMessage={
          query.isError
            ? "Failed to load token consumption."
            : `No ${measureNoun} in this range.`
        }
        loading={query.isPending && !data}
        onSelectRange={onRangeSelect}
      />
    </section>
  );
}

function toTableRows(
  rows: QueryRow[],
  groupBy: ConsumptionGroupBy,
): ConsumptionTableRow[] {
  return rankedConsumptionRows(rows, groupBy, "tokens").map((row) => ({
    key: row.groupValue || "(unset)",
    label: consumptionRowLabel(groupBy, row.groupValue),
    groupValue: row.groupValue,
    toolCalls: row.measures.totalToolCalls ?? 0,
    inputTokens: row.measures.totalInputTokens ?? 0,
    outputTokens: row.measures.totalOutputTokens ?? 0,
    tokens: row.measures.totalTokens ?? 0,
    cost: row.measures.totalCost ?? 0,
  }));
}

function rollupGroupValue(data: QueryResult | undefined): string | undefined {
  const rows = data?.table ?? [];
  return rows.length >= CONSUMPTION_TOP_N
    ? rows[rows.length - 1]?.groupValue
    : undefined;
}

function ConsumptionBarChart({
  rows,
  color,
  measure,
  onSelect,
}: {
  rows: { label: string; value: number; groupValue: string }[];
  color: string;
  measure: ConsumptionMeasure;
  onSelect: (groupValue: string) => void;
}): JSX.Element {
  const height = Math.max(120, rows.length * 32 + 40);
  const options = useMemo<ChartOptions<"bar">>(
    () => ({
      indexAxis: "y",
      responsive: true,
      maintainAspectRatio: false,
      onClick(_, elements) {
        if (!elements.length) return;
        const index = elements[0]!.index;
        const groupValue = rows[index]?.groupValue;
        if (groupValue) onSelect(groupValue);
      },
      onHover(event, elements) {
        const el = event.native?.target as HTMLElement | null;
        if (el) el.style.cursor = elements.length ? "pointer" : "default";
      },
      plugins: {
        legend: { display: false },
        tooltip: {
          ...TOOLTIP,
          cornerRadius: 0,
          callbacks: {
            label: (item) =>
              measure === "calls"
                ? ` ${formatCount(item.parsed.x ?? 0)} calls`
                : ` ${formatCount(item.parsed.x ?? 0)} tokens`,
          },
        },
      },
      scales: {
        x: {
          grid: { color: "#e5e5e5" },
          ticks: {
            color: "#A3A3A3",
            callback: (value) => formatCompact(Number(value)),
          },
        },
        y: {
          grid: { display: false },
          ticks: { color: "#A3A3A3", font: { size: 12 } },
        },
      },
    }),
    [measure, onSelect, rows],
  );

  return (
    <div style={{ height }}>
      <Bar
        data={{
          labels: rows.map((row) => row.label),
          datasets: [
            {
              data: rows.map((row) => row.value),
              backgroundColor: color,
              barThickness: 18,
              borderRadius: 0,
              borderSkipped: false,
            },
          ],
        }}
        options={options}
      />
    </div>
  );
}
