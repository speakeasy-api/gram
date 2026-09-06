import { formatExactUsd } from "@/components/billing/payg-billing-estimate";
import { Page } from "@/components/page-layout";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useSession } from "@/contexts/Auth";
import { useTrialNow } from "@/hooks/useTrialNow";
import { getTrialLifecycleFromDates } from "@/lib/trial-status";
import type { TierLimits } from "@gram/client/models/components/tierlimits.js";
import { useGetUsageTiers } from "@gram/client/react-query/getUsageTiers.js";

/**
 * The server-owned PAYG rates, shown while the product trial that converts to
 * pay as you go is running: the metered price per million tokens under
 * management, plus the provider-cost pass-through lines. The product feature
 * list stays off this section — it describes the plan's contents, not what
 * the organization will be charged.
 */
export function PaygPriceList(): JSX.Element | null {
  const { trial } = useSession();
  const now = useTrialNow(trial);

  if (getTrialLifecycleFromDates(trial, now) !== "active") return null;

  return <PaygPriceListBody />;
}

function PaygPriceListBody(): JSX.Element | null {
  const { data, isLoading, isError } = useGetUsageTiers({
    throwOnError: false,
  });

  if (isError) return null;

  return (
    <Page.Section>
      <Page.Section.Title area="">Pay as you go pricing</Page.Section.Title>
      <Page.Section.Description>
        The rates this organization is billed once the trial converts to pay as
        you go.
      </Page.Section.Description>
      <Page.Section.Body>
        {isLoading || !data ? (
          <Skeleton className="h-16 w-full max-w-md" />
        ) : (
          <PaygRates payg={data.payg} />
        )}
      </Page.Section.Body>
    </Page.Section>
  );
}

function PaygRates({ payg }: { payg: TierLimits }): JSX.Element {
  const rate = formatExactUsd(payg.tumPricePerMillionUsd);
  const passThroughLines = payg.includedBullets ?? [];

  return (
    <Stack gap={4}>
      {rate !== null && (
        <Stack gap={1}>
          <Text className="font-medium">Tokens under management</Text>
          <Text muted>{rate} per million tokens</Text>
        </Stack>
      )}
      {passThroughLines.length > 0 && (
        <ul className="space-y-1">
          {passThroughLines.map((item) => (
            <li key={item}>
              <Text muted small>
                {item}
              </Text>
            </li>
          ))}
        </ul>
      )}
    </Stack>
  );
}
