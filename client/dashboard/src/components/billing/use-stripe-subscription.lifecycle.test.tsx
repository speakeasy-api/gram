import {
  QueryClient,
  QueryClientProvider,
  useQuery,
  type UseQueryOptions,
} from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { StrictMode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ fetchSubscription: vi.fn() }));

// Keep the real observer lifecycle; replace only the generated SDK transport.
vi.mock("@gram/client/react-query/getStripeSubscription.js", () => ({
  useGetStripeSubscription: (
    _request: unknown,
    _security: unknown,
    options: Omit<UseQueryOptions, "queryKey" | "queryFn">,
  ) =>
    useQuery({
      queryKey: ["stripe-subscription"],
      queryFn: mocks.fetchSubscription,
      ...options,
    }),
}));

import { useStripeSubscription } from "./use-stripe-subscription";

function Section({ name }: { name: string }) {
  const subscription = useStripeSubscription();
  return (
    <div>
      {name}
      <button onClick={() => void subscription.refetch()}>
        Refresh {name}
      </button>
    </div>
  );
}

// Billing moves keyed query consumers when a missing subscription makes payment
// the first section. React 19 StrictMode replays their effects on that move.
function BillingOrder() {
  const subscription = useStripeSubscription();
  const payment = (
    <div key="payment">
      <Section name="Payment" />
    </div>
  );
  const usage = <Section key="usage" name="Usage" />;
  return (
    <main data-status={subscription.status}>
      {subscription.isError ? [payment, usage] : [usage, payment]}
      <div>Billing notifications</div>
    </main>
  );
}

afterEach(cleanup);

describe("subscription observer lifecycle", () => {
  it("preserves a missing subscription when StrictMode consumers reconnect, while allowing explicit refresh", async () => {
    mocks.fetchSubscription
      .mockReset()
      .mockRejectedValue(
        Object.assign(new Error("subscription not found"), { statusCode: 404 }),
      );
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    // Re-subscription after an error is the trigger, whether caused by a new
    // surface mounting or development StrictMode reconnecting passive effects.
    await client.prefetchQuery({
      queryKey: ["stripe-subscription"],
      queryFn: mocks.fetchSubscription,
    });
    render(
      <StrictMode>
        <QueryClientProvider client={client}>
          <BillingOrder />
        </QueryClientProvider>
      </StrictMode>,
    );

    await waitFor(() =>
      expect(screen.getByRole("main").dataset.status).toBe("error"),
    );
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 100));
    });
    expect(mocks.fetchSubscription).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("main").firstElementChild?.textContent).toContain(
      "Payment",
    );
    expect(client.isFetching()).toBe(0);

    mocks.fetchSubscription.mockResolvedValue({ status: "active" });
    fireEvent.click(screen.getByRole("button", { name: "Refresh Payment" }));
    await waitFor(() =>
      expect(screen.getByRole("main").dataset.status).toBe("success"),
    );
    expect(screen.getByRole("main").firstElementChild?.textContent).toContain(
      "Usage",
    );
    client.clear();
  });
});
