import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { Page } from "@/components/page-layout";
import { StackedTimeSeriesPanel } from "@/components/stacked-time-series-panel";
import { Button } from "@/components/ui/Button";
import { MetricCard } from "@/components/ui/MetricCard";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Skeleton } from "@/components/ui/Skeleton";
import { useOrganization } from "@/contexts/Auth";
import { useGetMeterUsage } from "@gram/client/react-query/getMeterUsage.js";
import { useListProjects } from "@gram/client/react-query/listProjects.js";
import { RotateCcw } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { BillingCyclePicker } from "./billing-cycle-picker";
import { BreakdownPicker } from "./breakdown-picker";
import {
  METER_FAMILIES,
  meterBreakdownLabel,
  type MeterFamily,
  type MeterReadingKind,
} from "./meter-breakdown-options";
import {
  adaptMeterChart,
  formatDailyMeterRate,
  formatMeterAxis,
  formatMeterQuantity,
  type MeterUsageData,
} from "./meter-usage-adapter";
import { MeterUsageTable } from "./meter-usage-table";
import { useMeterPeriod } from "./use-meter-period";

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

const READING_KIND_OPTIONS = [
  { value: "usage", label: "Usage", tooltip: "Ordinary meter readings" },
  {
    value: "adjustment",
    label: "Adjustments",
    tooltip: "Separate signed correction readings",
  },
] satisfies { value: MeterReadingKind; label: string; tooltip: string }[];

function periodDisplayRange(period: { from: Date; to: Date }): {
  from: Date;
  to: Date;
} {
  // The picker displays local calendar dates; the reporting window is UTC.
  const calendarDate = (date: Date): Date =>
    new Date(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate());
  return {
    from: calendarDate(period.from),
    to: calendarDate(new Date(period.to.getTime() - 1)),
  };
}

export function MeterUsageSection(): JSX.Element {
  const [family, setFamily] = useState<MeterFamily>("agent_session_storage");
  const [readingKind, setReadingKind] = useState<MeterReadingKind>("usage");
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
    readingKind,
    ...(period ? { from: period.from, to: period.to } : {}),
  };
  const query = useGetMeterUsage(request, undefined, { throwOnError: false });
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
  const projectNames = useMemo(
    () =>
      new Map([
        ...organization.projects.map(
          (project) => [project.id, project.name] as const,
        ),
        ...(projectsQuery.data?.projects ?? []).map(
          (project) => [project.id, project.name] as const,
        ),
      ]),
    [organization.projects, projectsQuery.data],
  );
  const chart = useMemo(() => adaptMeterChart(data), [data]);

  const totalSeries = useMemo(
    () =>
      data
        ? {
            key: "__bucket_total__",
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
    const quantity = formatMeterQuantity(data.total, data.unit);
    const dailyRate = formatDailyMeterRate(
      data.total,
      data.unit,
      data.window.from,
      data.window.to,
      new Date(),
    );
    explorer = (
      <div key={periodState.viewNonce} className="space-y-4">
        {query.isError && (
          <div className="text-muted-foreground text-sm" role="alert">
            Couldn't refresh usage — showing the last loaded data.
          </div>
        )}
        <MetricCard.Group>
          <MetricCard
            label={
              readingKind === "adjustment" ? "Net adjustment" : "Total usage"
            }
            value={
              <span className="break-all tabular-nums" title={quantity}>
                {quantity}
              </span>
            }
            description={`${definition.label} · ${data.measurementMethod}`}
            tone={
              readingKind === "adjustment" && BigInt(data.total) < 0n
                ? "warning"
                : "information"
            }
            size="sm"
          />
          <MetricCard
            label={
              readingKind === "adjustment"
                ? "Average daily adjustment"
                : "Average daily usage"
            }
            value={
              <span className="break-all tabular-nums" title={dailyRate}>
                {dailyRate}
              </span>
            }
            description="Elapsed selected duration, capped at the current time"
            tone="neutral"
            size="sm"
          />
        </MetricCard.Group>
        <StackedTimeSeriesPanel
          title={`${definition.label} over time`}
          headerHint={`${definition.description} Click or drag the chart to drill into a date range.`}
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
          totalSeries={breakdown === "total" ? undefined : totalSeries}
          formatValue={(value) =>
            formatMeterQuantity(Math.trunc(value).toString(), data.unit)
          }
          formatExactValue={(value) => formatMeterQuantity(value, data.unit)}
          formatAxisValue={formatMeterAxis}
          emptyMessage="No meter readings recorded. Usage reflects the new metering system."
          loading={query.isFetching && !data}
          onSelectRange={periodState.selectChartRange}
        />
        <MeterUsageTable data={data} projectNames={projectNames} />
      </div>
    );
  }

  return (
    <Page.Section>
      <Page.Section.Title>Meter usage</Page.Section.Title>
      <Page.Section.Description>
        Explore independently metered storage, bandwidth, and risk-scanning
        volume. Usage and adjustments are reported separately and are not
        invoice estimates.
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
            <Page.Toolbar.Actions>
              <SegmentedControl
                value={readingKind}
                onChange={setReadingKind}
                options={READING_KIND_OPTIONS}
              />
            </Page.Toolbar.Actions>
          </Page.Toolbar.Row>
          <Page.Toolbar.Row>
            <Page.Toolbar.Leading>
              {period && (
                <div className="flex items-center gap-2">
                  <BillingCyclePicker
                    cycles={knownCycles}
                    selected={
                      periodState.customRange ? null : periodState.selectedCycle
                    }
                    onSelect={periodState.selectCycle}
                  />
                  <TimeRangePicker
                    preset={null}
                    customRange={periodDisplayRange(period)}
                    customRangeLabel={
                      periodState.customRange?.label ??
                      (periodState.customRange ? "Custom" : "Cycle")
                    }
                    availablePresets={[]}
                    onCustomRangeChange={periodState.setPickedRange}
                    onClearCustomRange={periodState.clearCustomRange}
                    className="bg-background py-1.5 text-sm"
                  />
                </div>
              )}
            </Page.Toolbar.Leading>
            <Page.Toolbar.Actions>
              <Button variant="secondary" size="sm" onClick={periodState.reset}>
                <RotateCcw className="size-3.5" />
                Reset
              </Button>
            </Page.Toolbar.Actions>
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
