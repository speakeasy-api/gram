import {
  isPaygCheckoutLocked,
  setPaygCheckoutLocked,
  usePaygCheckoutLocked,
} from "@/components/billing/payg-checkout-lock";
import { useTelemetry } from "@/contexts/Telemetry";
import { useCreateStripeCheckoutMutation } from "@gram/client/react-query/createStripeCheckout.js";
import { useCallback, useState } from "react";

export const PAYG_CHECKOUT_ERROR_MESSAGE =
  "Couldn't start checkout. Try again.";

export type PaygCheckoutSession = {
  clientSecret: string;
  publishableKey: string;
};

function usableCheckoutSession(
  session: PaygCheckoutSession | null | undefined,
): session is PaygCheckoutSession {
  if (!session) return false;
  return (
    /^cs_(test|live)_/.test(session.clientSecret) &&
    /^pk_(test|live)_/.test(session.publishableKey)
  );
}

export function useStartPaygCheckout(activeOrganizationId: string): {
  startCheckout: () => void;
  dismissCheckout: () => void;
  isPending: boolean;
  error: string | null;
  session: PaygCheckoutSession | null;
} {
  const telemetry = useTelemetry();
  const checkoutMutation = useCreateStripeCheckoutMutation({
    onMutate: () => activeOrganizationId,
    onSettled: (_data, _error, _variables, context) => {
      if (typeof context === "string") {
        setPaygCheckoutLocked(context, false);
      }
    },
  });
  const [error, setError] = useState<string | null>(null);
  const [session, setSession] = useState<PaygCheckoutSession | null>(null);
  const locked = usePaygCheckoutLocked(activeOrganizationId);

  const startCheckout = useCallback(() => {
    if (isPaygCheckoutLocked(activeOrganizationId)) return;
    setPaygCheckoutLocked(activeOrganizationId, true);
    setError(null);

    checkoutMutation.mutate(
      {},
      {
        onSuccess: (created) => {
          if (!usableCheckoutSession(created)) {
            telemetry.capture("payg_checkout_error", {
              error: "unusable checkout session",
            });
            setError(PAYG_CHECKOUT_ERROR_MESSAGE);
            return;
          }
          setSession(created);
        },
        onError: (checkoutError: unknown) => {
          telemetry.capture("payg_checkout_error", {
            error:
              checkoutError instanceof Error
                ? checkoutError.message
                : "unknown",
          });
          setError(PAYG_CHECKOUT_ERROR_MESSAGE);
        },
      },
    );
  }, [activeOrganizationId, checkoutMutation, telemetry]);

  const dismissCheckout = useCallback(() => {
    setSession(null);
  }, []);

  return {
    startCheckout,
    dismissCheckout,
    isPending: locked || checkoutMutation.isPending,
    error,
    session,
  };
}
