import { inferenceSpendLabel } from "@/components/billing/inference-caps";
import {
  formatExactUsd,
  formatRecordedThrough,
} from "@/components/billing/payg-billing-estimate";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import type { InferenceSpendCapKeyType } from "@gram/client/models/components/inferencespendcap.js";
import type { InferenceSpendMonth } from "@gram/client/models/components/inferencespendmonth.js";
import { useGetInferenceSpendHistory } from "@gram/client/react-query/getInferenceSpendHistory.js";

const MISSING_FIGURE = "—";

const KEY_COLUMNS: readonly InferenceSpendCapKeyType[] = ["chat", "internal"];

const monthFormat = new Intl.DateTimeFormat("en-US", {
  month: "long",
  year: "numeric",
  timeZone: "UTC",
});

/**
 * Calendar-month inference spend recorded going forward from durable daily
 * collection. Months before tracking began are omitted rather than rebuilt
 * from upstream history, which is not reliable across key rotations.
 */
export function InferenceSpendHistorySection(): JSX.Element {
  return (
    <Page.Section>
      {/* Secondary section below Usage / Billing: suppress the area eyebrow. */}
      <Page.Section.Title area="">Inference spend</Page.Section.Title>
      <Page.Section.Description>
        Monthly spend on the inference Gram runs for this organization.
        Customer-facing inference is assistants and the other AI-powered
        dashboard experiences. Security inference is the automated analysis
        Gram runs over this organization's traffic. OpenRouter caps reset on
        the first of the month, so these figures follow the calendar rather
        than the billing cycle. History is recorded going forward from
        completed days.
      </Page.Section.Description>
      <Page.Section.Body>
        <InferenceSpendHistoryTable />
      </Page.Section.Body>
    </Page.Section>
  );
}

function InferenceSpendHistoryTable(): JSX.Element {
  const { data, isError, refetch, isFetching } = useGetInferenceSpendHistory(
    undefined,
    undefined,
    { throwOnError: false },
  );

  if (data) {
    return (
      <Stack gap={3}>
        <Table
          columns={monthColumns}
          data={data.months}
          rowKey={(month) => month.monthStart.toISOString()}
          noResultsMessage={
            <Text>No monthly inference spend recorded yet.</Text>
          }
        />
        {isError && (
          <Text muted small role="alert">
            Couldn't refresh inference spend history, so the amounts shown may
            be out of date.
          </Text>
        )}
      </Stack>
    );
  }

  if (isError) {
    return (
      <Stack direction="horizontal" align="center" gap={3}>
        <Text muted small role="alert">
          Couldn't load inference spend history.
        </Text>
        <Button
          variant="secondary"
          size="sm"
          disabled={isFetching}
          onClick={() => void refetch()}
        >
          {isFetching ? "RETRYING..." : "RETRY"}
        </Button>
      </Stack>
    );
  }

  return (
    <Stack gap={2}>
      <Skeleton className="h-4 w-48" />
      <Skeleton className="h-10 w-full" />
      <Skeleton className="h-10 w-full" />
    </Stack>
  );
}

const monthColumns: Column<InferenceSpendMonth>[] = [
  {
    key: "month",
    header: "Month",
    width: "220px",
    render: (month) => <MonthCell month={month} />,
  },
  ...KEY_COLUMNS.map((keyType): Column<InferenceSpendMonth> => ({
    key: keyType,
    header: inferenceSpendLabel(keyType),
    render: (month) => <SpendCell amount={spendForKey(month, keyType)} />,
  })),
  {
    key: "total",
    header: "Total",
    render: (month) => <SpendCell amount={month.spendUsd} />,
  },
];

function MonthCell({ month }: { month: InferenceSpendMonth }): JSX.Element {
  const recordedThrough = formatRecordedThrough(month.recordedThrough);
  const subtitle = monthSubtitle(month.current, recordedThrough);

  return (
    <Stack gap={0.5}>
      <Text>
        {monthFormat.format(month.monthStart)}
        {month.current ? " (current)" : ""}
      </Text>
      {subtitle !== null && (
        <Text muted small>
          {subtitle}
        </Text>
      )}
    </Stack>
  );
}

function monthSubtitle(
  current: boolean,
  recordedThrough: string | null,
): string | null {
  if (recordedThrough !== null) {
    if (current) {
      return `Completed days through ${recordedThrough}; today isn't counted yet.`;
    }
    return `Completed days through ${recordedThrough}.`;
  }
  if (current) {
    return "No completed day has been recorded in this month yet.";
  }
  return null;
}

function SpendCell({ amount }: { amount: string | null }): JSX.Element {
  return (
    <Text className="tabular-nums">
      {formatExactUsd(amount) ?? MISSING_FIGURE}
    </Text>
  );
}

function spendForKey(
  month: InferenceSpendMonth,
  keyType: InferenceSpendCapKeyType,
): string | null {
  return (
    month.keySpend.find((entry) => entry.keyType === keyType)?.spendUsd ?? null
  );
}
