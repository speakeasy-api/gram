import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { Page } from "@/components/page-layout";
import { StackedTimeSeriesPanel } from "@/components/stacked-time-series-panel";
import { Button } from "@/components/ui/Button";
import { MetricCard } from "@/components/ui/MetricCard";
import { MultiSelect } from "@/components/ui/MultiSelect";
import { Skeleton } from "@/components/ui/Skeleton";
import { type Column, Table } from "@/components/ui/Table";
import { CONTROL_HEIGHT } from "@/components/ui/Toolbar";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildGetSpendBreakdownQuery } from "@gram/client/react-query/getSpendBreakdown.js";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { RotateCcw } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { BillingCyclePicker } from "./billing-cycle-picker";
import {
  adaptSpendChart,
  formatScaledUsd,
  formatScaledUsdAxis,
  formatSpendRate,
  formatSpendUsage,
  formatSpendUsd,
  type SpendBreakdownData,
  type SpendProduct,
  type SpendProductID,
  sumSelectedCost,
  validateSpendBreakdown,
} from "./spend-breakdown-adapter";
import { meterPeriodDisplayRange, useMeterPeriod } from "./use-meter-period";

const PRODUCT_OPTIONS = [
  {
    value: "agent_session_storage",
    label: "Agent session storage",
    description: "Stored session tokens",
  },
  {
    value: "risk_content_scans",
    label: "Risk scanning",
    description: "Scanned tokens per scanner execution",
  },
  {
    value: "mcp_egress",
    label: "MCP gateway",
    description: "Egress body bytes only",
  },
] satisfies {
  value: SpendProductID;
  label: string;
  description: string;
}[];
const ALL_PRODUCT_IDS = PRODUCT_OPTIONS.map((option) => option.value);
const PRODUCT_ID_LOOKUP: Record<SpendProductID, true> = {
  agent_session_storage: true,
  risk_content_scans: true,
  mcp_egress: true,
};

const NUMERIC_CELL = "block w-full text-right tabular-nums";

const spendColumns: Column<SpendProduct>[] = [
  {
    key: "product",
    header: "Product",
    width: "2fr",
    render: (product) => <span className="font-medium">{product.label}</span>,
  },
  {
    key: "usage",
    header: "Usage",
    width: "1fr",
    render: (product) => (
      <span className={NUMERIC_CELL}>{formatSpendUsage(product)}</span>
    ),
  },
  {
    key: "rate",
    header: "Rate",
    width: "1fr",
    render: (product) => (
      <span className={NUMERIC_CELL}>{formatSpendRate(product)}</span>
    ),
  },
  {
    key: "cost",
    header: "Estimated cost",
    width: "1fr",
    render: (product) => (
      <span className={NUMERIC_CELL}>{formatSpendUsd(product.costUsd)}</span>
    ),
  },
];

export function SpendBreakdownSection(): JSX.Element | null {
  const client = useGramContext();
  const [selectedProductIds, setSelectedProductIds] =
    useState<SpendProductID[]>(ALL_PRODUCT_IDS);
  const [knownCycles, setKnownCycles] = useState<{ from: Date; to: Date }[]>(
    [],
  );
  const periodState = useMeterPeriod(knownCycles);
  const period = periodState.period;
  const query = useQuery({
    ...buildGetSpendBreakdownQuery(
      client,
      periodState.requestPeriod ?? undefined,
    ),
    throwOnError: false,
    placeholderData: keepPreviousData,
    select: validateSpendBreakdown,
  });
  const data: SpendBreakdownData | undefined = query.data;

  useEffect(() => {
    if (!data || data.billingCycles.length === 0) return;
    setKnownCycles(data.billingCycles);
  }, [data]);

  const selectedProducts = useMemo(
    () => new Set(selectedProductIds),
    [selectedProductIds],
  );
  const chart = useMemo(
    () => adaptSpendChart(data, selectedProducts),
    [data, selectedProducts],
  );
  const tableProducts = useMemo(
    () =>
      data?.products.filter((product) => selectedProducts.has(product.id)) ??
      [],
    [data, selectedProducts],
  );

  if (data?.availability === "unsupported_plan") return null;
  const selectedTotal = data
    ? sumSelectedCost(data.products, selectedProducts)
    : "0";

  const selectProducts = (values: string[]): void => {
    setSelectedProductIds(
      values.filter(
        (value): value is SpendProductID =>
          PRODUCT_ID_LOOKUP[value as SpendProductID] === true,
      ),
    );
  };

  let emptyMessage = "No estimated metered-product spend in this period.";
  if (selectedProductIds.length === 0) {
    emptyMessage = "Select a product to see its estimated spend.";
  }

  let content: JSX.Element;
  if (!data && query.isError) {
    content = (
      <div className="border-border border p-6">
        <div className="flex flex-wrap items-center gap-3" role="alert">
          <span className="text-muted-foreground text-sm">
            Couldn't load estimated spend.
          </span>
          <Button
            size="sm"
            variant="secondary"
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            {query.isFetching ? "RETRYING..." : "RETRY"}
          </Button>
        </div>
      </div>
    );
  } else if (!data) {
    content = <Skeleton className="h-[520px] w-full" />;
  } else {
    content = (
      <div
        key={periodState.viewNonce}
        className="space-y-4"
        aria-busy={query.isFetching}
      >
        {query.isPlaceholderData && (
          <div className="text-muted-foreground text-sm" role="status">
            Loading selected spend — showing the previous selection.
          </div>
        )}
        {query.isError && (
          <div className="text-muted-foreground text-sm" role="alert">
            Couldn't refresh estimated spend — showing the last loaded data.
          </div>
        )}
        <MetricCard.Group>
          <MetricCard
            label="Selected estimated cost"
            value={
              <span className="tabular-nums">
                {formatSpendUsd(selectedTotal)}
              </span>
            }
            description="Current PAYG list prices"
            tone="information"
            size="sm"
          />
        </MetricCard.Group>
        <StackedTimeSeriesPanel
          title="Estimated cost over time"
          headerHint="Daily estimated cost at current PAYG list prices. Weekly and monthly views sum the daily estimates exactly. An asterisk marks the in-progress bucket."
          bucketsMs={chart.bucketsMs}
          bucketEndsMs={chart.bucketEndsMs}
          stacks={chart.stacks}
          formatValue={(value) =>
            formatScaledUsdAxis(value, chart.decimalScale)
          }
          formatExactValue={(value) =>
            formatScaledUsd(value, chart.decimalScale)
          }
          formatAxisValue={(value) =>
            formatScaledUsdAxis(value, chart.decimalScale)
          }
          emptyMessage={emptyMessage}
          loading={query.isFetching && !data}
          onSelectRange={periodState.selectChartRange}
          inProgressAtMs={chart.queriedAtMs}
          tooltipMode="index"
          tooltipTotalLabel="Total"
        />
        <Table
          columns={spendColumns}
          data={tableProducts}
          rowKey={(product) => product.id}
          noResultsMessage="Select at least one product to see its estimate."
        />
      </div>
    );
  }

  return (
    <Page.Section>
      <Page.Section.Title area="">Spend by product</Page.Section.Title>
      <Page.Section.Description>
        Estimated metered-product cost at current PAYG list prices, not an
        invoice. Inference is excluded.
      </Page.Section.Description>
      <Page.Section.Body>
        <Page.Toolbar>
          <Page.Toolbar.Row>
            <Page.Toolbar.Leading>
              <MultiSelect
                options={PRODUCT_OPTIONS}
                value={selectedProductIds}
                onValueChange={selectProducts}
                placeholder="Select products"
                aria-label="Products"
                searchable={false}
                autoSize
                responsive={{ mobile: { maxCount: 1, compactMode: true } }}
                singleLine
                className={CONTROL_HEIGHT}
              />
            </Page.Toolbar.Leading>
          </Page.Toolbar.Row>
          <Page.Toolbar.Row>
            <Page.Toolbar.Leading>
              {period && (
                <div className="flex flex-wrap items-center gap-2">
                  <BillingCyclePicker
                    cycles={knownCycles}
                    selected={
                      periodState.customRange ? null : periodState.selectedCycle
                    }
                    onSelect={periodState.selectCycle}
                  />
                  <TimeRangePicker
                    preset={null}
                    customRange={meterPeriodDisplayRange(period)}
                    customRangeLabel={
                      periodState.customRange ? "Custom" : "Cycle"
                    }
                    availablePresets={[]}
                    timezone="UTC"
                    onCustomRangeChange={periodState.setPickedRange}
                    onClearCustomRange={periodState.clearCustomRange}
                    className={CONTROL_HEIGHT}
                  />
                </div>
              )}
              <Button
                variant="secondary"
                className={CONTROL_HEIGHT}
                onClick={periodState.reset}
              >
                <RotateCcw className="size-4" />
                Reset
              </Button>
            </Page.Toolbar.Leading>
            <Page.Toolbar.Refresh
              onRefresh={() => void query.refetch()}
              isRefreshing={query.isFetching}
            />
          </Page.Toolbar.Row>
        </Page.Toolbar>
        <div className="mt-4">{content}</div>
        <details className="text-muted-foreground mt-4 text-sm">
          <summary className="cursor-pointer">
            How estimates are calculated
          </summary>
          <p className="mt-2">
            Current PAYG list prices apply to all selected dates, including
            historical ranges. Risk scanning counts tokens for each scanner
            execution; MCP gateway counts egress only. Ordinary usage summaries
            may include duplicate deliveries. Credits, discounts, taxes,
            inference, and billing adjustments are excluded. Today’s usage is
            still in progress.
          </p>
        </details>
      </Page.Section.Body>
    </Page.Section>
  );
}
