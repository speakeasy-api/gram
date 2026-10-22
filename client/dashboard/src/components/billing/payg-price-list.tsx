import { formatExactUsd } from "@/components/billing/payg-billing-estimate";
import { Badge } from "@/components/ui/Badge";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { TierLimits } from "@gram/client/models/components/tierlimits.js";
import { useGetUsageTiers } from "@gram/client/react-query/getUsageTiers.js";
import { Heading } from "../ui/Heading";

/** Server-owned rates for the guarded billing setup and payment subpages. */
export function PaygPriceList(): JSX.Element | null {
  const { data, isLoading, isError } = useGetUsageTiers({
    throwOnError: false,
  });

  if (isError) return null;

  return (
    <section className="min-w-0 space-y-6">
      <div className="flex items-center justify-between gap-4 border-b border-border pb-2">
        <Heading variant="h3">Pricing</Heading>
        <Badge variant="neutral" className="shrink-0">
          Pay as you go
        </Badge>
      </div>
      <div>
        {isLoading || !data ? (
          <Skeleton className="h-16 w-full max-w-md" />
        ) : (
          <PaygRates payg={data.payg} />
        )}
      </div>
    </section>
  );
}

function PaygRates({ payg }: { payg: TierLimits }): JSX.Element {
  const passThroughLines = payg.includedBullets ?? [];

  return (
    <Stack gap={4}>
      <ul className="divide-y divide-border/50 border-b border-border pb-2">
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
      </ul>
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
    <li className="flex items-start justify-between gap-4 py-3 first:pt-0">
      <h3 className="min-w-0 text-lg font-medium text-foreground">{label}</h3>
      <p className="shrink-0 text-right">
        <span className="block font-mono text-xl font-medium text-foreground tabular-nums">
          {rate}
        </span>{" "}
        <span className="block font-mono text-xs leading-5 text-muted">
          {unit}
        </span>
      </p>
    </li>
  );
}
