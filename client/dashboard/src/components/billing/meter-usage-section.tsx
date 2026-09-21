import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { Page } from "@/components/page-layout";
import { StackedTimeSeriesPanel } from "@/components/stacked-time-series-panel";
import { Button } from "@/components/ui/Button";
import { MetricCard } from "@/components/ui/MetricCard";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Skeleton } from "@/components/ui/Skeleton";
import { CONTROL_HEIGHT } from "@/components/ui/Toolbar";
import { useOrganization } from "@/contexts/Auth";
import { useGetMeterUsage } from "@gram/client/react-query/getMeterUsage.js";
import { useListProjects } from "@gram/client/react-query/listProjects.js";
import { keepPreviousData } from "@tanstack/react-query";
import { RotateCcw } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { BillingCyclePicker } from "./billing-cycle-picker";
import { BreakdownPicker } from "./breakdown-picker";
import {
  METER_FAMILIES,
  meterBreakdownLabel,
  type MeterFamily,
} from "./meter-breakdown-options";
import {
  adaptMeterChart,
  formatDailyMeterRate,
  formatMeterAxis,
  formatMeterQuantity,
  type MeterUsageData,
} from "./meter-usage-adapter";
import { MeterUsageTable } from "./meter-usage-table";
import { meterPeriodDisplayRange, useMeterPeriod } from "./use-meter-period";

const FAMILY_OPTIONS = [
  {
    value: "agent_session_storage",
    label: "Storage",
    tooltip: "Stored-message workload",
  },
  {
    value: "mcp_bandwidth",
    label: "Bandwidth",
    tooltip: "MCP ingress and egress body bytes",
  },
  {
    value: "risk_content_scans",
    label: "Risk scans",
    tooltip: "Content volume processed by risk scanners",
  },
] satisfies { value: MeterFamily; label: string; tooltip: string }[];

export function MeterUsageSection(): JSX.Element {
  const [family, setFamily] = useState<MeterFamily>("agent_session_storage");
  const [breakdownByFamily, setBreakdownByFamily] = useState<
    Record<MeterFamily, string>
  >({
    agent_session_storage:
      METER_FAMILIES.agent_session_storage.defaultBreakdown,
    mcp_bandwidth: METER_FAMILIES.mcp_bandwidth.defaultBreakdown,
    risk_content_scans: METER_FAMILIES.risk_content_scans.defaultBreakdown,
  });
  const breakdown = breakdownByFamily[family];
  const definition = METER_FAMILIES[family];
  const [knownCycles, setKnownCycles] = useState<{ from: Date; to: Date }[]>(
    [],
  );
  const periodState = useMeterPeriod(knownCycles);
  const period = periodState.period;

  const request = {
    family,
    breakdown,
    ...periodState.requestPeriod,
  };
  const query = useGetMeterUsage(request, undefined, {
    throwOnError: false,
    placeholderData: keepPreviousData,
  });
  const data: MeterUsageData | undefined = query.data;
  useEffect(() => {
    if (!data || data.billingCycles.length === 0) return;
    setKnownCycles(data.billingCycles);
  }, [data]);

  const organization = useOrganization();
  const projectsQuery = useListProjects(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  const projectSlugs = useMemo(() => {
    const slugs = new Map<string, string>();
    for (const projects of [
      organization.projects,
      projectsQuery.data?.projects ?? [],
    ]) {
      for (const project of projects) {
        if (project.slug) slugs.set(project.id, project.slug);
      }
    }
    return slugs;
  }, [organization.projects, projectsQuery.data]);
  const chart = useMemo(
    () => adaptMeterChart(data, projectSlugs),
    [data, projectSlugs],
  );

  const totalSeries = useMemo(
    () =>
      data
        ? {
            key: `${data.family}:${data.breakdown.dimension}:__bucket_total__`,
            label: "Total",
            series: data.buckets.map((bucket) => Number(bucket.total)),
            exactSeries: data.buckets.map((bucket) => bucket.total),
          }
        : undefined,
    [data],
  );
  const selectBreakdown = (value: string): void => {
    setBreakdownByFamily((current) => ({ ...current, [family]: value }));
  };

  let explorer: JSX.Element;
  if (!data && query.isError) {
    explorer = (
      <div className="border-border border p-6">
        <div className="flex items-center gap-3" role="alert">
          <span className="text-muted-foreground text-sm">
            Couldn't load meter usage.
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
    explorer = <Skeleton className="h-[480px] w-full" />;
  } else {
    const dataDefinition = METER_FAMILIES[data.family];
    const quantity = formatMeterQuantity(data.total, data.unit);
    const now = new Date();
    const dailyRate = formatDailyMeterRate(
      data.total,
      data.unit,
      data.window.from,
      data.window.to,
      now,
    );
    const exactDailyRate = formatDailyMeterRate(
      data.total,
      data.unit,
      data.window.from,
      data.window.to,
      now,
      "standard",
    );
    explorer = (
      <div
        key={periodState.viewNonce}
        className="space-y-4"
        aria-busy={query.isFetching}
      >
        {query.isPlaceholderData && (
          <div className="text-muted-foreground text-sm" role="status">
            Loading selected usage — showing the previous selection.
          </div>
        )}
        {query.isError && (
          <div className="text-muted-foreground text-sm" role="alert">
            Couldn't refresh usage — showing the last loaded data.
          </div>
        )}
        <MetricCard.Group>
          <MetricCard
            label="Total usage"
            value={
              <span
                className="break-all tabular-nums"
                title={formatMeterQuantity(data.total, data.unit, "standard")}
              >
                {quantity}
              </span>
            }
            description={dataDefinition.label}
            tone="information"
            size="sm"
          />
          <MetricCard
            label="Average daily usage"
            value={
              <span className="break-all tabular-nums" title={exactDailyRate}>
                {dailyRate}
              </span>
            }
            description="Elapsed selected duration, capped at the current time"
            tone="neutral"
            size="sm"
          />
        </MetricCard.Group>
        <StackedTimeSeriesPanel
          title={`${dataDefinition.label} over time`}
          headerHint={`${dataDefinition.description} Click or drag the chart to drill into a date range.`}
          bucketsMs={chart.bucketsMs}
          bucketEndsMs={chart.bucketEndsMs}
          stacks={chart.stacks}
          headerControls={
            <BreakdownPicker
              value={breakdown}
              groups={definition.groups}
              label={meterBreakdownLabel(family, breakdown)}
              onChange={selectBreakdown}
            />
          }
          totalSeries={
            data.breakdown.dimension === "total" ? undefined : totalSeries
          }
          formatValue={(value) => formatMeterAxis(value, data.unit)}
          formatExactValue={(value) =>
            formatMeterQuantity(value, data.unit, "standard")
          }
          formatAxisValue={(value) => formatMeterAxis(value, data.unit)}
          emptyMessage="No meter readings recorded. Usage reflects the new metering system."
          loading={query.isFetching && !data}
          onSelectRange={periodState.selectChartRange}
        />
        <MeterUsageTable data={data} projectSlugs={projectSlugs} />
      </div>
    );
  }

  return (
    <Page.Section>
      <Page.Section.Title area="">Usage</Page.Section.Title>
      <Page.Section.Description>
        Explore storage, bandwidth, and risk-scanning volume by UTC day. Today's
        totals update as readings arrive. These usage totals are not invoice
        estimates.
      </Page.Section.Description>
      <Page.Section.Body>
        <Page.Toolbar>
          <Page.Toolbar.Row>
            <Page.Toolbar.Leading>
              <SegmentedControl
                value={family}
                onChange={setFamily}
                options={FAMILY_OPTIONS}
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
        <div className="mt-4">{explorer}</div>
      </Page.Section.Body>
    </Page.Section>
  );
}
