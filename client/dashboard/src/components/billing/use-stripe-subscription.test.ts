import { describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ query: vi.fn() }));

vi.mock("@gram/client/react-query/getStripeSubscription.js", () => ({
  useGetStripeSubscription: (...args: unknown[]) => mocks.query(...args),
}));

import { useStripeSubscription } from "./use-stripe-subscription";

describe("useStripeSubscription", () => {
  // The shared query client throws everything but a 401/403 to the app error
  // boundary, which would take the whole billing page down whenever Stripe is
  // unreachable. Every billing surface handles its failures inline, so the
  // opt-out belongs to the shared read rather than to each call site.
  it("keeps a Stripe outage out of the app error boundary", () => {
    mocks.query.mockReturnValue({ data: undefined });

    useStripeSubscription();

    const [request, security, options] = mocks.query.mock.calls.at(-1) as [
      unknown,
      unknown,
      { throwOnError?: boolean },
    ];
    expect(options.throwOnError).toBe(false);
    // No request or security overrides: the session travels on the client, and
    // passing anything here would fork the query key React Query dedupes on.
    expect(request).toBeUndefined();
    expect(security).toBeUndefined();
  });
});
