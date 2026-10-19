import { formatExactUsd } from "@/components/billing/payg-billing-estimate";
import { Page } from "@/components/page-layout";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { TierLimits } from "@gram/client/models/components/tierlimits.js";
import { useGetUsageTiers } from "@gram/client/react-query/getUsageTiers.js";

/** Server-owned rates for the guarded billing setup and payment subpages. */
export function PaygPriceList(): JSX.Element | null {
  const { data, isLoading, isError } = useGetUsageTiers({
    throwOnError: false,
  });

  if (isError) return null;

  return (
    <Page.Section>
      <Page.Section.Title area="">Pay as you go pricing</Page.Section.Title>
      <Page.Section.Description>
        Pay-as-you-go rates. During a trial, billing starts when the trial
        converts.
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
  const passThroughLines = payg.includedBullets ?? [];

  return (
    <Stack gap={4}>
      <MeteredRate
        label="Agent session storage"
        price={payg.tumPricePerMillionUsd}
        unit="per million tokens"
      />
      <MeteredRate
        label="Risk scanning"
        price={payg.riskScanPricePerMillionUsd}
        unit="per million tokens scanned"
      />
      <MeteredRate
        label="MCP gateway"
        price={payg.mcpEgressPricePerGibUsd}
        unit="per GiB of egress"
      />
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

function MeteredRate({
  label,
  price,
  unit,
}: {
  label: string;
  price: string | undefined;
  unit: string;
}): JSX.Element | null {
  const rate = formatExactUsd(price);
  if (rate === null) return null;

  return (
    <Stack gap={1}>
      <Text className="font-medium">{label}</Text>
      <Text muted>
        {rate} {unit}
      </Text>
    </Stack>
  );
}
