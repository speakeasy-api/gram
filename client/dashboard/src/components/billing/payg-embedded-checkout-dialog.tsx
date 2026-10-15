import { Dialog } from "@/components/ui/Dialog";
import { useSession } from "@/contexts/Auth";
import {
  EmbeddedCheckout,
  EmbeddedCheckoutProvider,
} from "@stripe/react-stripe-js";
import { loadStripe, type Stripe } from "@stripe/stripe-js";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";

const stripePromises = new Map<string, Promise<Stripe | null>>();

function stripePromiseFor(publishableKey: string): Promise<Stripe | null> {
  const existing = stripePromises.get(publishableKey);
  if (existing) return existing;
  const created = loadStripe(publishableKey);
  stripePromises.set(publishableKey, created);
  return created;
}

/**
 * Mounts Stripe's embedded payment form on this page.
 *
 * Card payments stay here. Redirect-based payment methods leave for the
 * bank and come back to the billing page.
 */
export function PaygEmbeddedCheckoutDialog({
  clientSecret,
  publishableKey,
  onClose,
}: {
  clientSecret: string;
  publishableKey: string;
  onClose: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const { refetch } = useSession();
  const refetchSession = useRef(refetch);
  refetchSession.current = refetch;
  const [paid, setPaid] = useState(false);
  const fetchClientSecret = useCallback(
    async () => clientSecret,
    [clientSecret],
  );

  // Conversion lands on a webhook, so one refetch at completion usually still
  // sees the expired trial. A few more lets the trial-ended gate release
  // without a reload.
  useEffect(() => {
    if (!paid) return;
    const timers = [0, 1000, 2000, 4000].map((delay) =>
      window.setTimeout(() => {
        void refetchSession.current();
      }, delay),
    );
    return () => {
      for (const timer of timers) window.clearTimeout(timer);
    };
  }, [paid]);

  return (
    <Dialog
      open
      modal={false}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <Dialog.Content
        className="max-h-[90vh] max-w-2xl overflow-y-auto"
        onOpenAutoFocus={(event) => event.preventDefault()}
        onInteractOutside={(event) => event.preventDefault()}
      >
        <Dialog.Header>
          <Dialog.Title>Add a payment method</Dialog.Title>
          <Dialog.Description>
            Pay as you go starts after the card is confirmed.
          </Dialog.Description>
        </Dialog.Header>
        {paid ? (
          <p className="text-sm">
            Payment method saved. Your plan updates in a moment.
          </p>
        ) : (
          <EmbeddedCheckoutProvider
            stripe={stripePromiseFor(publishableKey)}
            options={{
              fetchClientSecret,
              onComplete: () => {
                void queryClient.invalidateQueries({
                  queryKey: ["@gram/client", "usage", "getStripeSubscription"],
                });
                setPaid(true);
              },
            }}
          >
            <EmbeddedCheckout />
          </EmbeddedCheckoutProvider>
        )}
      </Dialog.Content>
    </Dialog>
  );
}
