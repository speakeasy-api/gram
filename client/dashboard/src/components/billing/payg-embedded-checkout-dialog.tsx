import { Dialog } from "@/components/ui/Dialog";
import {
  EmbeddedCheckout,
  EmbeddedCheckoutProvider,
} from "@stripe/react-stripe-js";
import { loadStripe, type Stripe } from "@stripe/stripe-js";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useState } from "react";

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
  const [paid, setPaid] = useState(false);
  const fetchClientSecret = useCallback(
    async () => clientSecret,
    [clientSecret],
  );

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
            Payment received. Your plan updates in a moment.
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
