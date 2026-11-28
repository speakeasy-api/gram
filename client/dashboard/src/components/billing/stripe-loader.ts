import { loadStripe, type Stripe } from "@stripe/stripe-js";

const stripePromises = new Map<string, Promise<Stripe | null>>();

/** Loads Stripe.js once per publishable key, shared by every embedded Checkout. */
export function stripePromiseFor(
  publishableKey: string,
): Promise<Stripe | null> {
  const existing = stripePromises.get(publishableKey);
  if (existing) return existing;
  const created = loadStripe(publishableKey);
  stripePromises.set(publishableKey, created);
  return created;
}
