import { Page } from "@/components/page-layout";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useOrganization } from "@/contexts/Auth";
import { Dimension } from "@gram/client/models/components/queryfilter.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import {
  invalidateAllGetTokensUnderManagement,
  useGetTokensUnderManagement,
} from "@gram/client/react-query/getTokensUnderManagement.js";
import { useListProjects } from "@gram/client/react-query/listProjects.js";
import { useSetBillingMetadataMutation } from "@gram/client/react-query/setBillingMetadata.js";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Info, RotateCcw } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { BillingCyclePicker } from "./billing-cycle-picker";
import {
  type BilledDays,
  type BillingPeriod,
  bucketDateKey,
  cycleKey,
  cyclesFromTum,
  formatCycleName,
  periodDisplayRange,
  periodFromCycle,
} from "./billing-cycles";
import {
  BREAKDOWN_TOTAL,
  breakdownValueLabel,
  stackModeFor,
} from "./breakdown-options";
import { BreakdownPicker } from "./breakdown-picker";
import { type GroupSeries, TokenUsagePanel } from "./token-usage-panel";
import { TumDetailsTable } from "./tum-details-table";
import { tumDetailsQuery } from "./tum-queries";
import { TumUsageCard } from "./tum-usage-card";

// Org-wide token breakdown for one billing cycle: stacked daily tokens by a
// selectable dimension or by token type — one unified picker drives both.
// Everything renders from the billing details request shared with the
// details table (the server scopes it to the observed agent traffic, cache
// reads excluded), except the headline total, which prefers the billed
// per-day series the usage endpoint returns — the exact numbers on the
// usage card. No data-availability pruning of the dimension list:
// dimensions without data simply chart as "(unset)".
function TumTokenBreakdown({
  period,
  projectNames,
  billedDays,
  onSelectRange,
}: {
  period: BillingPeriod;
  // Project id → name, for labeling the Project breakdown's UUID values.
  projectNames: Map<string, string>;
  // The billed per-day series and the cycle windows it fully describes.
  billedDays: BilledDays;
  // Bar-click drill-down: narrows the page's period to the clicked bucket.
  onSelectRange: (start: Date, end: Date) => void;
}): JSX.Element {
  const client = useGramContext();
  const organization = useOrganization();
  // The picker's selection, plus the last-picked dimension so switching to
  // token type and back doesn't lose the grouping. Opens on the total view —
  // the billed series that matches the usage card exactly.
  const [breakdown, setBreakdown] = useState<string>(BREAKDOWN_TOTAL);
  const [dimension, setDimension] = useState<string>(Dimension.DivisionName);
  const stackBy = stackModeFor(breakdown);

  const scope = { client, orgId: organization.id, period };
  // Shared with the details table (same key — one request).
  const { data, isFetching } = useQuery(tumDetailsQuery(scope));

  const points = useMemo(() => data?.points ?? [], [data]);

  // The selected dimension's rows. "" rows are real observed traffic that
  // lacks the attribute — charted as "(unset)", same as the details table.
  const groups = useMemo<GroupSeries[]>(() => {
    const rows = data?.breakdowns.find((b) => b.key === dimension)?.rows ?? [];
    return rows.map((r) => ({
      label: breakdownValueLabel(dimension, r.value, projectNames),
      series: r.series,
    }));
  }, [data, dimension, projectNames]);

  // The billed series aligned to the points grid, used only when the billed
  // data COVERS every charted day — coverage, not positivity: a sealed
  // zero-token cycle is fully known (all zeros beat late-recomputed
  // telemetry), while a day outside every covered cycle window (e.g. a
  // synthesized active cycle without history) makes the whole view fall
  // back to the details totals rather than charting misleading zeros.
  const billedSeries = useMemo(() => {
    if (points.length === 0) return null;
    const series: number[] = [];
    for (const p of points) {
      const key = bucketDateKey(p.bucketTimeUnixNano);
      // Bucket dates are UTC midnights, so the key parses back to the
      // bucket's exact start instant.
      const ms = Date.parse(key);
      const coveredDay = billedDays.covered.some(
        (r) => ms >= r.start && ms < r.end,
      );
      if (!coveredDay) return null;
      series.push(billedDays.byDate.get(key) ?? 0);
    }
    return series;
  }, [points, billedDays]);

  const breakdownPicker = (
    <BreakdownPicker
      value={breakdown}
      onChange={(value) => {
        setBreakdown(value);
        // Only actual dimensions pick a breakdown; the special modes
        // (total / token type) keep the last-picked dimension.
        if (stackModeFor(value) === "group") {
          setDimension(value);
        }
      }}
    />
  );

  return (
    <TokenUsagePanel
      points={points}
      groups={groups}
      billedSeries={billedSeries}
      stackBy={stackBy}
      breakdownPicker={breakdownPicker}
      loading={isFetching && !data}
      onSelectRange={onSelectRange}
    />
  );
}

// The range picker's calendar hands back local midnights for both ends. The
// page's data is bucketed by UTC day (matching the billing-cycle boundaries),
// so a picked day means that UTC calendar day — otherwise a one-day pick
// spans two UTC buckets and the chart grows a phantom extra day. The last day
// is inclusive. Natural-language parses carry real times and pass through
// untouched.
function customRangeFromPicker(
  from: Date,
  to: Date,
): { start: Date; end: Date } {
  const isLocalMidnight = (d: Date) =>
    d.getHours() === 0 &&
    d.getMinutes() === 0 &&
    d.getSeconds() === 0 &&
    d.getMilliseconds() === 0;
  if (!isLocalMidnight(from) || !isLocalMidnight(to)) {
    return { start: from, end: to };
  }
  return {
    start: new Date(
      Date.UTC(from.getFullYear(), from.getMonth(), from.getDate()),
    ),
    end: new Date(Date.UTC(to.getFullYear(), to.getMonth(), to.getDate() + 1)),
  };
}

export const TumUsageSection = (): JSX.Element => {
  const { data: tum } = useGetTokensUnderManagement();
  const organization = useOrganization();
  // Projects are fetched only to label the Project breakdown's UUID values.
  const { data: projectsData } = useListProjects(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  const projectNames = useMemo(
    () =>
      new Map(
        (projectsData?.projects ?? []).map((p) => [p.id, p.name] as const),
      ),
    [projectsData],
  );

  // The selected billing cycle scopes the usage bar and the breakdown chart.
  // Derived (not synced) so the current cycle is the default once TUM loads.
  const cycles = useMemo(() => (tum ? cyclesFromTum(tum) : []), [tum]);
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const selectedCycle =
    cycles.find((c) => cycleKey(c) === selectedKey) ??
    cycles.find((c) => c.current) ??
    cycles[0] ??
    null;

  // A custom range (typed into the range picker or drilled into via a chart
  // bar click) overrides the cycle selection for the chart and details table.
  const [customRange, setCustomRange] = useState<{
    start: Date;
    end: Date;
    label?: string;
  } | null>(null);

  // Bumped by the Reset button; remounting the breakdown clears its internal
  // view state too (breakdown pick, granularity, cumulative, hidden series).
  const [viewNonce, setViewNonce] = useState(0);
  const handleReset = (): void => {
    setSelectedKey(null);
    setCustomRange(null);
    setViewNonce((n) => n + 1);
  };

  const monthlyLimit = tum?.monthlyTokenLimit ?? null;

  // Billed tokens per UTC day across every known cycle, for the chart's
  // headline total. The daily series is advisory — a finalized cycle serves
  // its sealed snapshot total while the days recompute live and can drift
  // (late telemetry) or expire (aggregate TTL) — so each cycle's days are
  // scaled to sum to its billed total, the number on the usage card. Same
  // normalization as the details table; cumulative rounding keeps the series
  // integral without losing the exact sum.
  //
  // `covered` records the cycle windows the billed data fully describes —
  // including zero-token cycles, where every day is a known zero (a sealed
  // zero total beats whatever late telemetry recomputed). A cycle with a
  // nonzero total but no daily shape (the synthesized active-cycle fallback)
  // stays uncovered, and the chart falls back to the details totals there.
  const billedDays = useMemo<BilledDays>(() => {
    const byDate = new Map<string, number>();
    const covered: { start: number; end: number }[] = [];
    for (const c of cycles) {
      const daysSum = c.days.reduce((sum, d) => sum + d.tokens, 0);
      if (daysSum === 0) {
        if (c.tokens === 0) {
          covered.push({ start: c.start.getTime(), end: c.end.getTime() });
        }
        continue;
      }
      covered.push({ start: c.start.getTime(), end: c.end.getTime() });
      const scale = c.tokens / daysSum;
      let acc = 0;
      let prevRounded = 0;
      for (const d of c.days) {
        acc += d.tokens * scale;
        const rounded = Math.round(acc);
        byDate.set(d.date, rounded - prevRounded);
        prevRounded = rounded;
      }
    }
    return { byDate, covered };
  }, [cycles]);

  // The effective period. A custom range that happens to match a cycle's
  // exact boundaries IS that cycle (billed normalization applies).
  const period: BillingPeriod | null = useMemo(() => {
    if (customRange) {
      const exact =
        cycles.find(
          (c) =>
            c.start.getTime() === customRange.start.getTime() &&
            c.end.getTime() === customRange.end.getTime(),
        ) ?? null;
      return {
        start: customRange.start,
        end: customRange.end,
        cycle: exact,
        label: customRange.label,
      };
    }
    return selectedCycle ? periodFromCycle(selectedCycle) : null;
  }, [customRange, cycles, selectedCycle]);

  // The usage card keeps showing the billing position of the cycle the
  // period sits inside — bar-click drill-downs never leave the viewed cycle.
  // A typed range spanning cycles has no single billing position; hide it.
  const cardCycle = useMemo(() => {
    if (!period) return null;
    return (
      period.cycle ??
      cycles.find(
        (c) =>
          c.start.getTime() <= period.start.getTime() &&
          period.end.getTime() <= c.end.getTime(),
      ) ??
      null
    );
  }, [period, cycles]);

  // Bar-click drill-down, clamped to the current period (week/month buckets
  // can overhang the period's edges).
  const handleBarSelect = (start: Date, end: Date): void => {
    if (!period) return;
    const s = Math.max(start.getTime(), period.start.getTime());
    const e = Math.min(end.getTime(), period.end.getTime());
    if (e <= s) return;
    setCustomRange({ start: new Date(s), end: new Date(e), label: undefined });
  };

  return (
    <Page.Section>
      <Page.Section.Title>Billing</Page.Section.Title>
      <Page.Section.Description>
        The volume of agent traffic the platform observes from your users'
        sessions each billing cycle, measured in tokens. Cache reads are
        excluded, as is inference the platform runs itself.
      </Page.Section.Description>
      <Page.Section.Body>
        {tum && period ? (
          <Stack gap={3} className="mb-6">
            <Stack direction="horizontal" align="center" gap={1}>
              <Type variant="body" className="font-medium">
                Tokens Under Management
              </Type>
              <SimpleTooltip tooltip="Counts the tokens observed in your users' agent sessions (input, output, and cache writes; cache reads excluded) during the selected billing cycle. Compared against your contracted monthly allowance.">
                <Info className="text-muted-foreground h-4 w-4" />
              </SimpleTooltip>
              <div className="ml-auto flex items-center gap-2">
                <BillingCyclePicker
                  cycles={cycles}
                  selected={customRange ? null : selectedCycle}
                  onSelect={(c) => {
                    setCustomRange(null);
                    setSelectedKey(cycleKey(c));
                  }}
                />
                {/* Always shows the effective window; typing a range (natural
                    language or calendar) narrows the section to it, clearing
                    returns to the selected cycle. */}
                <TimeRangePicker
                  preset={null}
                  customRange={periodDisplayRange(period)}
                  customRangeLabel={
                    customRange ? (customRange.label ?? "Custom") : "Cycle"
                  }
                  availablePresets={[]}
                  onCustomRangeChange={(from, to, label) =>
                    setCustomRange({
                      ...customRangeFromPicker(from, to),
                      label,
                    })
                  }
                  onClearCustomRange={() => setCustomRange(null)}
                  className="bg-background py-1.5 text-sm"
                />
                <button
                  type="button"
                  onClick={handleReset}
                  className="border-border hover:bg-muted text-muted-foreground hover:text-foreground inline-flex items-center gap-1.5 rounded-md border px-2.5 py-2 text-sm transition-colors"
                >
                  <RotateCcw className="size-3.5" />
                  Reset
                </button>
              </div>
            </Stack>
            {cardCycle && (
              <TumUsageCard
                tokens={cardCycle.tokens}
                limit={monthlyLimit}
                // On a custom range the card still shows the WHOLE containing
                // cycle's billing position — larger than the range's totals
                // below, so say which cycle these figures are for.
                label={
                  customRange
                    ? `${formatCycleName(cardCycle)} — full-cycle totals`
                    : formatCycleName(cardCycle)
                }
              />
            )}
            <div className="mt-8">
              <TumTokenBreakdown
                key={viewNonce}
                period={period}
                projectNames={projectNames}
                billedDays={billedDays}
                onSelectRange={handleBarSelect}
              />
            </div>
            <div className="mt-4">
              <TumDetailsTable
                key={viewNonce}
                period={period}
                projectNames={projectNames}
                limit={monthlyLimit}
              />
            </div>
          </Stack>
        ) : (
          <div className="space-y-4">
            <Skeleton className="h-4 w-1/3" />
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-40 w-full" />
          </div>
        )}
      </Page.Section.Body>
    </Page.Section>
  );
};

export const TumAdminSection = (): JSX.Element => {
  const queryClient = useQueryClient();
  const { data: tum } = useGetTokensUnderManagement();

  const [tokenLimit, setTokenLimit] = useState("");
  const [alertEmail, setAlertEmail] = useState("");
  const [anchorDay, setAnchorDay] = useState("1");

  // Prefill the form once the current contract terms load.
  useEffect(() => {
    if (!tum) return;
    setTokenLimit(tum.monthlyTokenLimit?.toString() ?? "");
    setAlertEmail(tum.alertEmail ?? "");
    setAnchorDay(tum.billingCycleAnchorDay.toString());
  }, [tum]);

  const mutation = useSetBillingMetadataMutation({
    onSuccess: () => {
      void invalidateAllGetTokensUnderManagement(queryClient);
    },
  });

  const parsedLimit = tokenLimit.trim() === "" ? undefined : Number(tokenLimit);
  const parsedAnchorDay = Number(anchorDay);
  const limitInvalid =
    parsedLimit !== undefined &&
    (!Number.isFinite(parsedLimit) || parsedLimit < 0);
  const anchorDayInvalid =
    !Number.isInteger(parsedAnchorDay) ||
    parsedAnchorDay < 1 ||
    parsedAnchorDay > 31;

  const handleSave = () => {
    mutation.mutate({
      request: {
        setBillingMetadataRequestBody: {
          monthlyTokenLimit: parsedLimit,
          alertEmail: alertEmail.trim() === "" ? undefined : alertEmail.trim(),
          billingCycleAnchorDay: parsedAnchorDay,
        },
      },
    });
  };

  return (
    <Page.Section>
      <Page.Section.Title>
        TUM Contract (PLATFORM ADMIN VIEW ONLY)
      </Page.Section.Title>
      <Page.Section.Description>
        Set this organization's contracted tokens under management terms.
        Customers never see this section or the alert email.
      </Page.Section.Description>
      <Page.Section.Body>
        <Stack gap={4} className="max-w-md">
          <Stack gap={2}>
            <Label htmlFor="tum-monthly-limit">
              Allowed TUM per month (tokens)
            </Label>
            <Input
              id="tum-monthly-limit"
              type="number"
              min={0}
              placeholder="Leave empty for no contracted limit"
              value={tokenLimit}
              onChange={setTokenLimit}
            />
          </Stack>
          <Stack gap={2}>
            <Label htmlFor="tum-alert-email">Alert email</Label>
            <Input
              id="tum-alert-email"
              type="email"
              placeholder="billing-alerts@customer.com"
              value={alertEmail}
              onChange={setAlertEmail}
            />
          </Stack>
          <Stack gap={2}>
            <Label htmlFor="tum-anchor-day">
              Billing cycle anchor day (1–31)
            </Label>
            <Input
              id="tum-anchor-day"
              type="number"
              min={1}
              max={31}
              value={anchorDay}
              onChange={setAnchorDay}
            />
          </Stack>
          <Stack direction="horizontal" align="center" gap={3}>
            <Button
              onClick={handleSave}
              disabled={mutation.isPending || limitInvalid || anchorDayInvalid}
            >
              {mutation.isPending ? "SAVING..." : "SAVE CONTRACT TERMS"}
            </Button>
            {mutation.isSuccess && !mutation.isPending && (
              <Type muted small>
                Saved.
              </Type>
            )}
            {mutation.isError && (
              <Type small className="text-destructive">
                Failed to save contract terms.
              </Type>
            )}
          </Stack>
        </Stack>
      </Page.Section.Body>
    </Page.Section>
  );
};
