import { lazy, Suspense } from "react";
import type { PaygCheckoutSession } from "@/components/billing/use-start-payg-checkout";

const PaygEmbeddedCheckoutDialog = lazy(() =>
  import("./payg-embedded-checkout-dialog").then((mod) => ({
    default: mod.PaygEmbeddedCheckoutDialog,
  })),
);

export function PaygCheckoutMount({
  session,
  onClose,
}: {
  session: PaygCheckoutSession | null;
  onClose: () => void;
}): JSX.Element | null {
  if (!session) return null;

  return (
    <Suspense fallback={null}>
      <PaygEmbeddedCheckoutDialog
        clientSecret={session.clientSecret}
        publishableKey={session.publishableKey}
        onClose={onClose}
      />
    </Suspense>
  );
}
