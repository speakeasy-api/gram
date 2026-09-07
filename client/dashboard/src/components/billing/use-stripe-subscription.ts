import { useGetStripeSubscription } from "@gram/client/react-query/getStripeSubscription.js";

/**
 * The organization's live Stripe subscription.
 *
 * Every billing surface reads the subscription through this hook so they share
 * one query key: React Query dedupes the request, and the plan section and the
 * inference cap controls can never disagree about what Stripe is reporting.
 *
 * The shared query client throws everything but a 401/403 to the app error
 * boundary, which would take the whole billing page down whenever Stripe is
 * unreachable. Billing surfaces handle their own failures inline, so the read
 * opts out here rather than at each call site.
 *
 * `options` is for callers that need more than the default read — a banner
 * outside the billing page has no user action to refresh it, so it polls. They
 * merge on top, so a caller could opt back into throwing; none does.
 */
export function useStripeSubscription(
  options?: Parameters<typeof useGetStripeSubscription>[2],
): ReturnType<typeof useGetStripeSubscription> {
  return useGetStripeSubscription(undefined, undefined, {
    throwOnError: false,
    ...options,
  });
}
