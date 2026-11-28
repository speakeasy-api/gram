import {
  EmbeddedCheckout,
  EmbeddedCheckoutProvider,
} from "@stripe/react-stripe-js";
import { useRef, useState } from "react";
import { stripePromiseFor } from "@/components/billing/stripe-loader";

/**
 * Stripe's hosted embedded page for saving the card that starts a trial. The
 * session's appearance is fixed when it is created, so only the client secret
 * and the completion callback are passed here. Stripe rejects option changes
 * after mount, so the options object is created once and the callback is read
 * through a ref.
 */
export function TrialEmbeddedCheckout({
  clientSecret,
  publishableKey,
  onComplete,
}: {
  clientSecret: string;
  publishableKey: string;
  onComplete: () => void;
}): JSX.Element {
  const completeRef = useRef(onComplete);
  completeRef.current = onComplete;
  const [options] = useState(() => ({
    clientSecret,
    onComplete: () => completeRef.current(),
  }));
  return (
    <EmbeddedCheckoutProvider
      stripe={stripePromiseFor(publishableKey)}
      options={options}
    >
      <EmbeddedCheckout className="w-full" />
    </EmbeddedCheckoutProvider>
  );
}
