import { loadStripe, type Stripe } from "@stripe/stripe-js";

const stripePromises = new Map<string, Promise<Stripe | null>>();

/**
 * Loads Stripe.js once per publishable key, shared by every embedded Checkout.
 * A failed load is not cached, so the next mount tries again.
 */
export function stripePromiseFor(
  publishableKey: string,
): Promise<Stripe | null> {
  const existing = stripePromises.get(publishableKey);
  if (existing) return existing;
  const created = loadStripe(publishableKey).then(
    (stripe) => {
      if (!stripe) stripePromises.delete(publishableKey);
      return stripe;
    },
    (error: unknown) => {
      stripePromises.delete(publishableKey);
      throw error;
    },
  );
  stripePromises.set(publishableKey, created);
  return created;
}
