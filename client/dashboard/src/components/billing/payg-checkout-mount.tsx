import { lazy, Suspense } from "react";
import type { PaygCheckoutSession } from "@/components/billing/use-start-payg-checkout";
import { PaygCheckoutSkeleton } from "@/components/billing/payg-checkout-skeleton";

const PaygEmbeddedCheckout = lazy(() =>
  import("./payg-embedded-checkout").then((mod) => ({
    default: mod.PaygEmbeddedCheckout,
  })),
);

export function PaygCheckoutMount({
  session,
  onComplete,
}: {
  session: PaygCheckoutSession | null;
  onComplete: () => void;
}): JSX.Element | null {
  if (!session) return null;

  return (
    <Suspense fallback={<PaygCheckoutSkeleton />}>
      <PaygEmbeddedCheckout
        key={`${session.publishableKey}:${session.clientSecret}`}
        clientSecret={session.clientSecret}
        publishableKey={session.publishableKey}
        onComplete={onComplete}
      />
    </Suspense>
  );
}
