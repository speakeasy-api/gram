import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { handleError, toError } from "@/lib/errors";
import { useGetTokensUnderManagement } from "@gram/client/react-query/getTokensUnderManagement.js";
import { useMemo } from "react";
import { ErrorBoundary } from "react-error-boundary";
import { BillingCyclePicker } from "./billing-cycle-picker";
import {
  type BillingCycle,
  billedDaysFromCycles,
  cyclesFromTum,
  overageDaysFromBilled,
  periodDisplayRange,
  resolvePeriodFigures,
} from "./billing-cycles";
import { PaygCycleEstimate } from "./payg-cycle-estimate";
import { PeriodUsageCard } from "./tum-usage-card";
import { useBillingPeriod } from "./use-billing-period";

export function BillingPositionSection(): JSX.Element {
  const query = useGetTokensUnderManagement(undefined, undefined, {
    throwOnError: false,
  });
  const cycles = useMemo(
    () => (query.data ? cyclesFromTum(query.data) : []),
    [query.data],
  );

  return (
    <Page.Section>
      <Page.Section.Title>Billing position</Page.Section.Title>
      <Page.Section.Description>
        Contract allowances and invoice estimates use the billing system of
        record, independently of the meter usage explorer below.
      </Page.Section.Description>
      <Page.Section.Body>
        <div className="space-y-4">
          <ErrorBoundary
            onError={(error) => handleError(toError(error), { silent: true })}
            fallbackRender={() => (
              <Text muted small role="alert">
                Couldn't load the current invoice estimate. Contract position
                and meter usage remain available.
              </Text>
            )}
          >
            <PaygCycleEstimate />
          </ErrorBoundary>
          {query.isError && (
            <div className="flex items-center gap-3" role="alert">
              <Text muted small>
                {query.data
                  ? "Couldn't refresh billing position — showing the last loaded data."
                  : "Couldn't load billing position. Meter usage remains available below."}
              </Text>
              <Button
                variant="secondary"
                size="sm"
                disabled={query.isFetching}
                onClick={() => void query.refetch()}
              >
                Retry
              </Button>
            </div>
          )}
          {query.data ? (
            <ContractPosition
              cycles={cycles}
              monthlyLimit={query.data.monthlyTokenLimit ?? null}
            />
          ) : !query.isError ? (
            <Skeleton className="h-40 w-full" />
          ) : null}
        </div>
      </Page.Section.Body>
    </Page.Section>
  );
}

function ContractPosition({
  cycles,
  monthlyLimit,
}: {
  cycles: BillingCycle[];
  monthlyLimit: number | null;
}): JSX.Element | null {
  const selection = useBillingPeriod(cycles);
  const { period } = selection;
  const windows = useMemo(
    () =>
      cycles
        .map((cycle) => ({ from: cycle.start, to: cycle.end }))
        .toReversed(),
    [cycles],
  );
  const billedDays = useMemo(() => billedDaysFromCycles(cycles), [cycles]);
  const overageDays = useMemo(
    () =>
      monthlyLimit == null
        ? null
        : overageDaysFromBilled(cycles, billedDays, monthlyLimit),
    [cycles, billedDays, monthlyLimit],
  );
  const figures = useMemo(
    () =>
      period == null
        ? null
        : resolvePeriodFigures(period, billedDays, overageDays, monthlyLimit),
    [period, billedDays, overageDays, monthlyLimit],
  );
  if (period == null || figures == null) return null;

  return (
    <div className="space-y-3">
      <Page.Toolbar>
        <Page.Toolbar.Leading>
          <BillingCyclePicker
            cycles={windows}
            selected={
              selection.customRange || !selection.selectedCycle
                ? null
                : {
                    from: selection.selectedCycle.start,
                    to: selection.selectedCycle.end,
                  }
            }
            onSelect={(window) => {
              const cycle = cycles.find(
                (candidate) =>
                  candidate.start.getTime() === window.from.getTime(),
              );
              if (cycle) selection.selectCycle(cycle);
            }}
          />
          <TimeRangePicker
            preset={null}
            customRange={periodDisplayRange(period)}
            customRangeLabel={
              selection.customRange
                ? (selection.customRange.label ?? "Custom")
                : "Cycle"
            }
            availablePresets={[]}
            onCustomRangeChange={selection.setPickedRange}
            onClearCustomRange={selection.clearCustomRange}
          />
        </Page.Toolbar.Leading>
        <Page.Toolbar.Actions>
          <Button variant="secondary" size="sm" onClick={selection.reset}>
            Reset billing period
          </Button>
        </Page.Toolbar.Actions>
      </Page.Toolbar>
      <PeriodUsageCard
        period={period}
        cycles={cycles}
        figures={figures}
        limit={monthlyLimit}
      />
    </div>
  );
}
