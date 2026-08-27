import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useGetTokensUnderManagement } from "@gram/client/react-query/getTokensUnderManagement.js";
import { useListProjects } from "@gram/client/react-query/listProjects.js";
import { RotateCcw } from "lucide-react";
import { type ReactNode, useMemo } from "react";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { BillingCyclePicker } from "./billing-cycle-picker";
import {
  type BillingCycle,
  billedDaysFromCycles,
  cyclesFromTum,
  cyclesHaveUsage,
  overageDaysFromBilled,
  periodDisplayRange,
  resolvePeriodFigures,
} from "./billing-cycles";
import { TumDefinitionTooltip } from "./tum-definition";
import { TumDetailsTable } from "./tum-details-table";
import { TumTokenBreakdown } from "./tum-token-breakdown";
import { PeriodUsageCard } from "./tum-usage-card";
import { useBillingPeriod } from "./use-billing-period";

// The tokens-under-management usage section: period controls (cycle picker,
// range picker, reset), the usage card, the token breakdown chart, and the
// per-metric details table — all scoped to one effective period resolved by
// useBillingPeriod, and all reading the billed daily series derived in the
// body so every surface reports the same numbers. Shared by the enterprise and
// pay-as-you-go views; `estimate` is the PAYG invoice estimate rendered at the
// head of the section, so both tiers read their bill off one usage surface.
export const TumUsageSection = ({
  estimate,
}: {
  estimate?: ReactNode;
}): JSX.Element => {
  const { data: tum } = useGetTokensUnderManagement();
  const cycles = useMemo(() => (tum ? cyclesFromTum(tum) : []), [tum]);

  let body: ReactNode;
  if (!tum) {
    body = (
      <div className="space-y-4">
        <Skeleton className="h-4 w-1/3" />
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-40 w-full" />
      </div>
    );
  } else if (!cyclesHaveUsage(cycles)) {
    // A brand-new organization gets an explicit empty state instead of a
    // zeroed card over an empty chart — which reads as a reporting failure
    // rather than as "nothing yet".
    body = (
      <InlineEmptyState
        icon="chart-column"
        heading="No usage recorded yet"
        description="Once your first sessions come in, usage appears here with a per-cycle breakdown."
      />
    );
  } else {
    body = (
      <TumUsageBody
        cycles={cycles}
        monthlyLimit={tum.monthlyTokenLimit ?? null}
      />
    );
  }

  return (
    <Page.Section>
      <Page.Section.Title>Usage</Page.Section.Title>
      <Page.Section.Description>
        The volume of agent traffic the platform observes from your users'
        sessions each billing cycle, measured in tokens. Cache reads are
        excluded, as is inference the platform runs itself.
      </Page.Section.Description>
      <Page.Section.Body>
        <Stack gap={8}>
          {estimate}
          {body}
        </Stack>
      </Page.Section.Body>
    </Page.Section>
  );
};

// The loaded usage explorer for one organization's billing cycles.
function TumUsageBody({
  cycles,
  // The contracted monthly allowance; null when the org has no contracted cap.
  monthlyLimit,
}: {
  cycles: BillingCycle[];
  monthlyLimit: number | null;
}): JSX.Element | null {
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

  const {
    period,
    selectedCycle,
    customRange,
    viewNonce,
    selectCycle,
    setPickedRange,
    clearCustomRange,
    selectBarRange,
    reset,
  } = useBillingPeriod(cycles);

  // The billed per-day series every surface (card, chart headline, details
  // table) reads, and the per-day overage attribution derived from it.
  const billedDays = useMemo(() => billedDaysFromCycles(cycles), [cycles]);
  const overageDays = useMemo(
    () =>
      monthlyLimit == null
        ? null
        : overageDaysFromBilled(cycles, billedDays, monthlyLimit),
    [cycles, billedDays, monthlyLimit],
  );

  // The billed answers for the effective period, resolved once and handed to
  // the card, the chart headline, and the details table — their agreement is
  // structural, not three parallel recomputations.
  const figures = useMemo(
    () =>
      period == null
        ? null
        : resolvePeriodFigures(period, billedDays, overageDays, monthlyLimit),
    [period, billedDays, overageDays, monthlyLimit],
  );

  // cyclesFromTum always yields the active cycle, so a loaded section always
  // has a period — this narrows the types rather than handling a real state.
  if (period == null || figures == null) return null;

  return (
    <Stack gap={3}>
      <Stack direction="horizontal" align="center" gap={1}>
        <Text variant="body" className="font-medium">
          Tokens Under Management
        </Text>
        <TumDefinitionTooltip />
        <div className="ml-auto flex items-center gap-2">
          <BillingCyclePicker
            cycles={cycles}
            selected={customRange ? null : selectedCycle}
            onSelect={selectCycle}
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
            onCustomRangeChange={setPickedRange}
            onClearCustomRange={clearCustomRange}
            className="bg-background py-1.5 text-sm"
          />
          <button
            type="button"
            onClick={reset}
            className="border-border hover:bg-muted text-muted-foreground hover:text-foreground inline-flex items-center gap-1.5 border px-2.5 py-2 text-sm transition-colors"
          >
            <RotateCcw className="size-3.5" />
            Reset
          </button>
        </div>
      </Stack>
      <PeriodUsageCard
        period={period}
        cycles={cycles}
        figures={figures}
        limit={monthlyLimit}
      />
      <div className="mt-8">
        <TumTokenBreakdown
          key={viewNonce}
          period={period}
          projectNames={projectNames}
          billedDays={billedDays}
          figures={figures}
          onSelectRange={selectBarRange}
        />
      </div>
      <div className="mt-4">
        <TumDetailsTable
          key={viewNonce}
          period={period}
          projectNames={projectNames}
          limit={monthlyLimit}
          billedDays={billedDays}
          overageDays={overageDays}
          figures={figures}
        />
      </div>
    </Stack>
  );
}
