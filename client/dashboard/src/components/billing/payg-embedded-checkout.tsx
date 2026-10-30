import {
  PaygCheckoutHeader,
  PaygCheckoutSummary,
} from "@/components/billing/payg-checkout-summary";
import { PaygCheckoutSkeleton } from "@/components/billing/payg-checkout-skeleton";
import {
  CheckoutForm,
  CheckoutFormProvider,
  useCheckoutForm,
} from "@stripe/react-stripe-js/checkout";
import { useRef, useState } from "react";
import {
  loadStripe,
  type Stripe,
  type StripeCheckoutFormConfirmEvent,
} from "@stripe/stripe-js";
import { Heading } from "@/components/ui/Heading";
import { useConfig } from "@/components/ui/hooks/useConfig";

const stripePromises = new Map<string, Promise<Stripe | null>>();

function stripePromiseFor(publishableKey: string): Promise<Stripe | null> {
  const existing = stripePromises.get(publishableKey);
  if (existing) return existing;
  const created = loadStripe(publishableKey);
  stripePromises.set(publishableKey, created);
  return created;
}

/** Embedded checkout confirms cards inline; redirect methods use the return URL. */
export function PaygEmbeddedCheckout({
  clientSecret,
  publishableKey,
  onComplete,
}: {
  clientSecret: string;
  publishableKey: string;
  onComplete: () => void;
}): JSX.Element {
  const { theme } = useConfig();
  return (
    <CheckoutFormProvider
      stripe={stripePromiseFor(publishableKey)}
      options={{
        clientSecret,
        appearance: {
          theme: theme === "dark" ? "night" : "stripe",
          variables: {
            colorPrimary: theme === "dark" ? "#ffffff" : "#303030",
            colorBackground: theme === "dark" ? "#111111" : "#ffffff",
            colorText: theme === "dark" ? "#eeeeee" : "#303030",
            fontFamily: "Inter, sans-serif",
            borderRadius: "0px",
          },
        },
      }}
    >
      <BillingCheckoutForm onComplete={onComplete} />
    </CheckoutFormProvider>
  );
}

// Stripe documents amountSummary, but the installed SDK types do not include it yet.
const checkoutFormOptions = {
  layout: "expanded" as const,
  features: {
    promotionCodeCollection: "auto" as const,
    amountSummary: { expanded: "auto" as const },
  },
};

function BillingCheckoutForm({ onComplete }: { onComplete: () => void }) {
  const checkout = useCheckoutForm();
  const confirming = useRef(false);
  const [confirmationError, setConfirmationError] = useState<string | null>(
    null,
  );

  const onConfirm = async (event: StripeCheckoutFormConfirmEvent) => {
    if (checkout.type !== "success" || confirming.current) return;
    confirming.current = true;
    setConfirmationError(null);
    try {
      const result = await checkout.checkout.confirm({
        formConfirmEvent: event,
        redirect: "if_required",
      });
      if (result.type === "success") {
        // The parent verifies backend subscription state before declaring success.
        onComplete();
      } else {
        setConfirmationError(result.error.message);
      }
    } catch {
      setConfirmationError(
        "Couldn't confirm your payment method. Please try again.",
      );
    } finally {
      confirming.current = false;
    }
  };
  if (checkout.type === "loading") return <PaygCheckoutSkeleton />;
  if (checkout.type === "error")
    return <p role="alert">{checkout.error.message}</p>;
  return (
    <div className="grid w-full grid-cols-1 gap-8 lg:block">
      <div className="contents lg:grid lg:grid-cols-2 lg:items-start lg:gap-10 pb-6">
        <div className="order-1 min-w-0 border-b border-border pb-2">
          <PaygCheckoutHeader />
        </div>
        <div className="order-3 w-full max-w-lg border-b border-border pb-2 lg:justify-self-end">
          <Heading
            variant="h4"
            className="flex font-semibold items-center text-xl"
          >
            Payment details
          </Heading>
        </div>
      </div>
      <div className="contents lg:grid lg:grid-cols-2 lg:items-start lg:gap-10">
        <div className="order-2 min-w-0">
          <PaygCheckoutSummary checkout={checkout.checkout} />
        </div>
        <div className="order-4 w-full max-w-lg space-y-8 lg:justify-self-end">
          <CheckoutForm options={checkoutFormOptions} onConfirm={onConfirm} />
          {confirmationError && (
            <p role="alert" className="text-sm text-destructive">
              {confirmationError}
            </p>
          )}
        </div>
      </div>
    </div>
  );
}
