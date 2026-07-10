import { Page } from "@/components/page-layout";
import { UsageMeter } from "@/components/ui/progress";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { Button } from "@/components/ui/moonshine";
import { useCallback, useState } from "react";

export const TopUpCTA = (): JSX.Element => {
  const client = useSdkClient();
  const telemetry = useTelemetry();
  const [busy, setBusy] = useState(false);

  const handleClick = useCallback(async () => {
    setBusy(true);
    try {
      const link = await client.usage.createTopUpCheckout();
      if (!link) {
        telemetry.capture("topup_checkout_error", { error: "empty link" });
        return;
      }
      window.open(link, "_blank");
    } catch (err) {
      telemetry.capture("topup_checkout_error", {
        error: err instanceof Error ? err.message : "unknown",
      });
    } finally {
      setBusy(false);
    }
  }, [client, telemetry]);

  return (
    <Page.Section.CTA>
      <Button onClick={() => void handleClick()} disabled={busy}>
        TOP UP CREDITS
      </Button>
    </Page.Section.CTA>
  );
};

export const UsageProgress = ({
  value,
  included,
  noMax,
}: {
  value: number;
  included: number;
  // Formerly drove per-block tick marks along the overage segment; the
  // unified UsageMeter track doesn't render them, but the prop stays so
  // existing callers (which pass a billing block size) don't need updates.
  overageIncrement: number;
  noMax?: boolean;
}): JSX.Element => {
  const effectiveIncluded = noMax ? Math.max(1, value * 1.5) : included;
  const overage = Math.max(0, value - effectiveIncluded);

  return (
    <UsageMeter
      used={value}
      included={effectiveIncluded}
      overageUsed={overage}
      labels={{
        primary:
          overage > 0
            ? `Included: ${effectiveIncluded.toLocaleString()}`
            : `${value.toLocaleString()} / ${
                noMax ? "No limit" : effectiveIncluded.toLocaleString()
              }`,
        secondary:
          overage > 0 ? `Extra: ${overage.toLocaleString()}` : undefined,
      }}
    />
  );
};
