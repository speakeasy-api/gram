import { lazy, Suspense } from "react";
import { PaygCheckoutSkeleton } from "@/components/billing/payg-checkout-skeleton";
import type { PaygCheckoutSession } from "@/components/billing/use-start-payg-checkout";

const TrialEmbeddedCheckout = lazy(() =>
  import("./trial-embedded-checkout").then((mod) => ({
    default: mod.TrialEmbeddedCheckout,
  })),
);

export function TrialCheckoutMount({
  session,
  onComplete,
}: {
  session: PaygCheckoutSession;
  onComplete: () => void;
}): JSX.Element {
  return (
    <Suspense fallback={<PaygCheckoutSkeleton />}>
      <TrialEmbeddedCheckout
        key={`${session.publishableKey}:${session.clientSecret}`}
        clientSecret={session.clientSecret}
        publishableKey={session.publishableKey}
        onComplete={onComplete}
      />
    </Suspense>
  );
}
