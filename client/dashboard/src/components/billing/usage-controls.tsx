import { Page } from "@/components/page-layout";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { Button } from "@speakeasy-api/moonshine";
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
        TOP UP CHAT CREDITS
      </Button>
    </Page.Section.CTA>
  );
};
