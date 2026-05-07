import { Page } from "@/components/page-layout";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { Button, cn } from "@speakeasy-api/moonshine";
import { useCallback, useState } from "react";

export const TopUpCTA = () => {
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
      <Button onClick={handleClick} disabled={busy}>
        TOP UP CREDITS
      </Button>
    </Page.Section.CTA>
  );
};

export const UsageProgress = ({
  value,
  included,
  overageIncrement,
  noMax,
}: {
  value: number;
  included: number;
  overageIncrement: number;
  noMax?: boolean;
}) => {
  if (noMax) {
    included = Math.max(1, value * 1.5);
  }

  const anyOverage = value > included;
  const overageMax = anyOverage
    ? Math.ceil((value - included + 1) / overageIncrement) * overageIncrement
    : 0;
  const totalMax = included + overageMax;

  const includedWidth = (included / totalMax) * 100;
  const overageWidth = (overageMax / totalMax) * 100;

  const includedProgress = (
    <div
      className={cn(
        "bg-muted relative h-4 overflow-hidden rounded-md dark:bg-neutral-800",
        anyOverage && "rounded-r-none",
      )}
      style={{ width: `${includedWidth}%` }}
    >
      <div
        className="bg-success-default h-full transition-all duration-300"
        style={{
          width: `${Math.min((value / included) * 100, 100)}%`,
        }}
      />
    </div>
  );

  const overageProgress = anyOverage ? (
    <div
      className="bg-muted relative h-4 overflow-hidden rounded-r-md dark:bg-neutral-800"
      style={{ width: `${overageWidth}%` }}
    >
      <div
        className="bg-warning-default h-full transition-all duration-300"
        style={{
          width: `${Math.min(((value - included) / overageMax) * 100, 100)}%`,
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
      <div
        className="text-muted-foreground absolute top-6 text-xs whitespace-nowrap"
        style={{ right: `${101 - includedWidth}%` }}
      >
        {anyOverage
          ? `Included: ${included.toLocaleString()}`
          : `${value.toLocaleString()} / ${
              noMax ? "No limit" : included.toLocaleString()
            }`}
      </div>

      {anyOverage && (
        <>
          <div
            className="absolute top-0 h-8 w-[2px] bg-neutral-600"
            style={{ left: `${includedWidth}%` }}
          />
          <div
            className="text-muted-foreground absolute top-6 text-xs whitespace-nowrap"
            style={{ left: `${includedWidth + 1}%` }}
          >
            Extra: {(value - included).toLocaleString()}
          </div>

          {Array.from(
            { length: Math.floor((value - included) / overageIncrement) },
            (_, index) => {
              const incrementPosition =
                includedWidth +
                (((index + 1) * overageIncrement) / totalMax) * 100;
              return (
                <div
                  key={index}
                  className="absolute top-0 h-5 w-[2px] bg-neutral-600"
                  style={{ left: `${incrementPosition}%` }}
                />
              );
            },
          )}
        </>
      )}
    </div>
  );
};
