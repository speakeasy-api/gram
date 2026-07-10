import { Page } from "@/components/page-layout";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useOrganization } from "@/contexts/Auth";
import { isAttributionDim } from "@/pages/costs/taxonomy";
import { Dimension } from "@gram/client/models/components/queryfilter.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import {
  invalidateAllGetTokensUnderManagement,
  useGetTokensUnderManagement,
} from "@gram/client/react-query/getTokensUnderManagement.js";
import { useListProjects } from "@gram/client/react-query/listProjects.js";
import { useSetBillingMetadataMutation } from "@gram/client/react-query/setBillingMetadata.js";
import { Button, Input, Stack } from "@/components/ui/moonshine";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Info, RotateCcw } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { BillingCyclePicker } from "./billing-cycle-picker";
import {
  type BillingPeriod,
  cycleKey,
  cyclesFromTum,
  formatCycleName,
  periodDisplayRange,
  periodFromCycle,
} from "./billing-cycles";
import { stackModeFor } from "./breakdown-options";
import { BreakdownPicker } from "./breakdown-picker";
import { TokenUsagePanel } from "./token-usage-panel";
import { TumDetailsTable } from "./tum-details-table";
import { riskPointsQuery, tumBreakdownQuery } from "./tum-queries";
import { TumUsageCard } from "./tum-usage-card";

// Org-wide token breakdown for one billing cycle: stacked daily tokens by a
// selectable dimension, by token type, or by risk involvement — one unified
// picker drives all three. Reads the same analytics aggregates as the costs
// explorer (telemetry.query), scoped to the cycle. No data-availability
// pruning of the dimension list: the availability probe
// (telemetry.listAttributeKeys) is project-scoped and this page is org-level,
// so it would filter against the wrong project — dimensions without data
// simply chart as "(unset)".
function TumTokenBreakdown({
  period,
  projectId,
  onSelectRange,
}: {
  period: BillingPeriod;
  projectId: string | null;
  // Bar-click drill-down: narrows the page's period to the clicked bucket.
  onSelectRange: (start: Date, end: Date) => void;
}): JSX.Element {
  const client = useGramContext();
  const organization = useOrganization();
  // The picker's selection, plus the last-picked dimension so switching to
  // token type or risk and back doesn't lose the grouping.
  const [breakdown, setBreakdown] = useState<string>(Dimension.DivisionName);
  const [dimension, setDimension] = useState<Dimension>(Dimension.DivisionName);
  const stackBy = stackModeFor(breakdown);

  const scope = { client, orgId: organization.id, period, projectId };
  const { data, isFetching } = useQuery(tumBreakdownQuery(scope, dimension));
  // Shared with the details table's risk rows (same key — one request).
  const { data: riskData } = useQuery(riskPointsQuery(scope));

  // Attribution cuts hide the "" (not-applicable) group, same as the costs
  // page — but only in the grouped view. The total / token-type / risk
  // stackings sum the whole series and must keep the unattributed slice, or
  // they'd undercount whenever the last-picked dimension was an attribution
  // cut.
  const series = useMemo(() => {
    const ts = data?.timeseries ?? [];
    return stackBy === "group" && isAttributionDim(dimension)
      ? ts.filter((s) => s.groupValue !== "")
      : ts;
  }, [data, dimension, stackBy]);

  const riskPoints = riskData?.points ?? null;

  const breakdownPicker = (
    <BreakdownPicker
      value={breakdown}
      showRisk={riskPoints != null}
      onChange={(value) => {
        setBreakdown(value);
        // Only actual dimensions feed the query's group_by; the special modes
        // (total / token type / risk) keep the last-picked dimension.
        if (stackModeFor(value) === "group") {
          setDimension(value as Dimension);
        }
      }}
    />
  );

  return (
    <TokenUsagePanel
      series={series}
      stackBy={stackBy}
      breakdownPicker={breakdownPicker}
      riskPoints={riskPoints}
      loading={isFetching && !data}
      onSelectRange={onSelectRange}
    />
  );
}

// All-projects sentinel for the project filter (Radix Select rejects "").
const ALL_PROJECTS = "__all__";

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
  const { data: projectsData } = useListProjects(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  const projects = projectsData?.projects ?? [];

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

  // Optional project scope for the chart and details table. The usage card
  // stays org-wide — the TUM contract is an organization-level number.
  const [projectId, setProjectId] = useState<string | null>(null);

  // Bumped by the Reset button; remounting the breakdown clears its internal
  // view state too (breakdown pick, granularity, cumulative, hidden series).
  const [viewNonce, setViewNonce] = useState(0);
  const handleReset = (): void => {
    setSelectedKey(null);
    setCustomRange(null);
    setProjectId(null);
    setViewNonce((n) => n + 1);
  };

  const monthlyLimit = tum?.monthlyTokenLimit ?? null;

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
        The volume of agent traffic the platform has processed, stored, and run
        security analysis on each billing cycle, measured in tokens.
      </Page.Section.Description>
      <Page.Section.Body>
        {tum && period ? (
          <Stack gap={3} className="mb-6">
            <Stack direction="horizontal" align="center" gap={1}>
              <Type variant="body" className="font-medium">
                Tokens Under Management
              </Type>
              <SimpleTooltip tooltip="Counts tokens from agent sessions the platform has stored chats or tool calls for during the selected billing cycle. Compared against your contracted monthly allowance.">
                <Info className="text-muted-foreground h-4 w-4" />
              </SimpleTooltip>
              <div className="ml-auto flex items-center gap-2">
                <Select
                  value={projectId ?? ALL_PROJECTS}
                  onValueChange={(value) =>
                    setProjectId(value === ALL_PROJECTS ? null : value)
                  }
                >
                  <SelectTrigger className="bg-background h-auto w-auto gap-1.5 py-1.5 text-sm">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={ALL_PROJECTS}>All projects</SelectItem>
                    {projects.map((p) => (
                      <SelectItem key={p.id} value={p.id}>
                        {p.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
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
                <Button variant="secondary" size="sm" onClick={handleReset}>
                  <Button.LeftIcon>
                    <RotateCcw className="size-3.5" />
                  </Button.LeftIcon>
                  <Button.Text>Reset</Button.Text>
                </Button>
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
                projectId={projectId}
                onSelectRange={handleBarSelect}
              />
            </div>
            <div className="mt-4">
              <TumDetailsTable
                key={viewNonce}
                period={period}
                projectId={projectId}
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
              onChange={(e) => setTokenLimit(e.target.value)}
            />
          </Stack>
          <Stack gap={2}>
            <Label htmlFor="tum-alert-email">Alert email</Label>
            <Input
              id="tum-alert-email"
              type="email"
              placeholder="billing-alerts@customer.com"
              value={alertEmail}
              onChange={(e) => setAlertEmail(e.target.value)}
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
              onChange={(e) => setAnchorDay(e.target.value)}
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
