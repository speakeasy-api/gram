import { Page } from "@/components/page-layout";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { Button, cn } from "@speakeasy-api/moonshine";
import { useCallback, useState } from "react";
import { formatBillingQuantity, type BillingUnit } from "./billing-format";

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
        TOP UP CHAT CREDITS
      </Button>
    </Page.Section.CTA>
  );
};

export const UsageProgress = ({
  value,
  included,
  overageIncrement,
  noMax,
  unit,
}: {
  value: number;
  included: number;
  overageIncrement?: number;
  noMax?: boolean;
  unit: BillingUnit;
}): JSX.Element => {
  const effectiveIncluded = noMax ? Math.max(1, value * 1.5) : included;

  const anyOverage = value > effectiveIncluded;
  const additional = Math.max(0, value - effectiveIncluded);
  let overageMax = 0;
  if (anyOverage) {
    overageMax = overageIncrement
      ? Math.ceil(additional / overageIncrement) * overageIncrement
      : additional;
  }
  const totalMax = Math.max(1, effectiveIncluded + overageMax);

  const includedWidth = (effectiveIncluded / totalMax) * 100;
  const overageWidth = (overageMax / totalMax) * 100;
  const includedProgressWidth =
    effectiveIncluded > 0
      ? Math.min((value / effectiveIncluded) * 100, 100)
      : 0;

  const includedProgress = (
    <div
      className={cn(
        "bg-muted relative h-4 overflow-hidden rounded-md",
        anyOverage && "rounded-r-none",
      )}
      style={{ width: `${includedWidth}%` }}
    >
      <div
        className="bg-success-default h-full transition-all duration-300"
        style={{
          width: `${includedProgressWidth}%`,
        }}
      />
    </div>
  );

  const overageProgress = anyOverage ? (
    <div
      className="bg-muted relative h-4 overflow-hidden rounded-r-md"
      style={{ width: `${overageWidth}%` }}
    >
      <div
        className="bg-warning-default h-full transition-all duration-300"
        style={{
          width: `${Math.min((additional / overageMax) * 100, 100)}%`,
        }}
      />
    </div>
  ) : null;

  return (
    <div className="relative">
      <div className="flex w-full">
        {includedProgress}
        {overageProgress}
      </div>
      <div className="text-muted-foreground mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs">
        <span>Consumed: {formatBillingQuantity(value, unit)}</span>
        <span>
          Included: {noMax ? "No limit" : formatBillingQuantity(included, unit)}
        </span>
        {anyOverage ? (
          <span>Additional: {formatBillingQuantity(additional, unit)}</span>
        ) : null}
      </div>

      {anyOverage ? (
        <>
          <div
            className="bg-border absolute top-0 h-4 w-[2px]"
            style={{ left: `${includedWidth}%` }}
          />

          {overageIncrement
            ? Array.from(
                { length: Math.floor(additional / overageIncrement) },
                (_, index) => {
                  const incrementPosition =
                    includedWidth +
                    (((index + 1) * overageIncrement) / totalMax) * 100;
                  return (
                    <div
                      key={index}
                      className="bg-border absolute top-0 h-4 w-[2px]"
                      style={{ left: `${incrementPosition}%` }}
                    />
                  );
                },
              )
            : null}
        </>
      ) : null}
    </div>
  );
};
