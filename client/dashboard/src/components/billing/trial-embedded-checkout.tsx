import {
  EmbeddedCheckout,
  EmbeddedCheckoutProvider,
} from "@stripe/react-stripe-js";
import { useEffect, useRef, useState } from "react";
import { stripePromiseFor } from "@/components/billing/stripe-loader";
import { Button } from "@/components/ui/Button";

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
  const [attempt, setAttempt] = useState(0);
  const [stripePromise, setStripePromise] = useState(() =>
    stripePromiseFor(publishableKey),
  );
  const [loadFailed, setLoadFailed] = useState(false);

  useEffect(() => {
    let current = true;
    stripePromise.then(
      (stripe) => {
        if (current && !stripe) setLoadFailed(true);
      },
      () => {
        if (current) setLoadFailed(true);
      },
    );
    return () => {
      current = false;
    };
  }, [stripePromise]);

  if (loadFailed) {
    return (
      <div role="alert" className="flex flex-col items-start gap-3">
        <p>
          The card form couldn't load. Check your connection, then select{" "}
          <strong>Try again</strong>.
        </p>
        <Button
          variant="secondary"
          onClick={() => {
            setLoadFailed(false);
            setStripePromise(stripePromiseFor(publishableKey));
            setAttempt((value) => value + 1);
          }}
        >
          Try again
        </Button>
      </div>
    );
  }

  return (
    <EmbeddedCheckoutProvider
      key={attempt}
      stripe={stripePromise}
      options={options}
    >
      <EmbeddedCheckout className="w-full" />
    </EmbeddedCheckoutProvider>
  );
}
