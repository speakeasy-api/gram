import {
  isPaygCheckoutLocked,
  setPaygCheckoutLocked,
  usePaygCheckoutLocked,
} from "@/components/billing/payg-checkout-lock";
import { useTelemetry } from "@/contexts/Telemetry";
import { safeExternalHttpUrl } from "@/lib/safe-external-url";
import { useCreateStripeCheckoutMutation } from "@gram/client/react-query/createStripeCheckout.js";
import { useCallback, useState } from "react";

export const PAYG_CHECKOUT_ERROR_MESSAGE =
  "Couldn't start checkout. Try again.";

export function useStartPaygCheckout(activeOrganizationId: string): {
  startCheckout: () => void;
  isPending: boolean;
  error: string | null;
} {
  const telemetry = useTelemetry();
  const checkout = useCreateStripeCheckoutMutation({
    onMutate: () => activeOrganizationId,
    onSettled: (_data, _error, _variables, context) => {
      if (typeof context === "string") {
        setPaygCheckoutLocked(context, false);
      }
    },
  });
  const [error, setError] = useState<string | null>(null);
  const locked = usePaygCheckoutLocked(activeOrganizationId);

  const startCheckout = useCallback(() => {
    if (isPaygCheckoutLocked(activeOrganizationId)) return;
    setPaygCheckoutLocked(activeOrganizationId, true);
    setError(null);

    checkout.mutate(
      {},
      {
        onSuccess: (link) => {
          const url = safeExternalHttpUrl(link);
          if (url === null) {
            telemetry.capture("payg_checkout_error", {
              error: "unusable checkout link",
            });
            setError(PAYG_CHECKOUT_ERROR_MESSAGE);
            return;
          }

          window.location.assign(url);
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
  }, [activeOrganizationId, checkout, telemetry]);

  return {
    startCheckout,
    isPending: locked || checkout.isPending,
    error,
  };
}
