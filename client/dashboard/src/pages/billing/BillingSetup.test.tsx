import { resetPaygCheckoutLocks } from "@/components/billing/payg-checkout-lock";
import { queryKeyGetStripeSubscription } from "@gram/client/react-query/getStripeSubscription.js";
import { queryKeyGetStripeCheckoutStatus } from "@gram/client/react-query/getStripeCheckoutStatus.js";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import type { ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { BillingSetup, BillingPaymentMethod } from "./BillingSetup";

const mocks = vi.hoisted(() => ({
  accountType: "payg",
  trial: vi.fn(),
  admin: true,
  whitelisted: true,
  hasActiveSubscription: false,
  fetchSubscription: vi.fn(),
  createCheckout: vi.fn(),
  fetchCheckoutStatus: vi.fn(),
  sessionRefetch: vi.fn(),
  confirm: vi.fn(),
  capture: vi.fn(),
  org: "test-org",
}));
// Only the SDK transport is stubbed; generated queries/mutations and checkout locks remain real.
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/funcs/usageGetStripeSubscription.js", () => ({
  usageGetStripeSubscription: (...args: unknown[]) =>
    mocks.fetchSubscription(...args),
}));
vi.mock("@gram/client/funcs/usageGetStripeCheckoutStatus.js", () => ({
  usageGetStripeCheckoutStatus: (...args: unknown[]) =>
    mocks.fetchCheckoutStatus(...args),
}));
vi.mock("@gram/client/funcs/usageCreateStripeCheckout.js", () => ({
  usageCreateStripeCheckout: (...args: unknown[]) =>
    mocks.createCheckout(...args),
}));
vi.mock("@stripe/stripe-js", () => ({
  loadStripe: () => Promise.resolve(null),
}));
vi.mock("@stripe/react-stripe-js/checkout", () => ({
  CheckoutFormProvider: ({ children }: { children: ReactNode }) => children,
  CheckoutForm: ({ onConfirm }: { onConfirm: (event: unknown) => void }) => (
    <button onClick={() => onConfirm({})}>Complete Stripe</button>
  ),
  useCheckoutForm: () => ({
    type: "success",
    checkout: {
      confirm: mocks.confirm,
      currency: "usd",
      lineItems: [],
      total: { total: { amount: "$0.00" } },
    },
  }),
}));
vi.mock("@/components/page-templates", () => ({
  SettingsPage: ({
    children,
    title,
    description,
    area,
  }: {
    children: ReactNode;
    title: ReactNode;
    description: ReactNode;
    area: ReactNode;
  }) => (
    <div>
      <span>{area}</span>
      <h1>{title}</h1>
      <p>{description}</p>
      {children}
    </div>
  ),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("@/components/billing/billing-email-section", () => ({
  BillingEmailSection: () => (
    <section data-testid="notifications">Notifications</section>
  ),
}));
vi.mock("@gram/client/react-query/getUsageTiers.js", () => ({
  useGetUsageTiers: () => ({
    data: {
      payg: {
        tumPricePerMillionUsd: "0.35",
        riskScanPricePerMillionUsd: "0.99",
        mcpEgressPricePerGibUsd: "20",
        includedBullets: ["Other inference billed at provider cost"],
      },
    },
    isLoading: false,
    isError: false,
  }),
}));
vi.mock("@/components/billing/payg-plan-section", () => ({
  PaygPlanSection: () => <section data-testid="payment">Payment</section>,
}));
vi.mock("@/components/ui/hooks/useConfig", () => ({
  useConfig: () => ({ theme: "light" }),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: mocks.capture }),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: "enabled" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => mocks.admin }),
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({
    activeOrganizationId: mocks.org,
    organization: { slug: mocks.org },
    rawGramAccountType: mocks.accountType,
    whitelisted: mocks.whitelisted,
    hasActiveSubscription: mocks.hasActiveSubscription,
    trial: mocks.trial(),
    refetch: mocks.sessionRefetch,
  }),
}));
const missing = { ok: false, error: { statusCode: 404 } };
const checkoutSession = {
  clientSecret: "cs_test_example",
  publishableKey: "pk_test_example",
};
const subscriptionKey = queryKeyGetStripeSubscription({});
const returnKey = queryKeyGetStripeCheckoutStatus({
  sessionId: "cs_test_return",
});
const returnedPath = "/test-org/billing/setup?session_id=cs_test_return";
function success<T>(value: T) {
  return { ok: true, value };
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
let client: QueryClient;
function Destination() {
  const location = useLocation();
  return (
    <p>
      {location.state?.billingSetupComplete
        ? "Confirmed success"
        : "Payment details"}
    </p>
  );
}
function Harness({ path = "/test-org/billing/setup" }: { path?: string }) {
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/:org/billing" element={<p>Billing overview</p>} />
          <Route path="/:org/billing/setup" element={<BillingSetup />} />
          <Route
            path="/:org/billing/payment-method"
            element={<Destination />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
}
beforeEach(() => {
  vi.resetAllMocks();
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  mocks.accountType = "payg";
  mocks.admin = true;
  mocks.whitelisted = true;
  mocks.hasActiveSubscription = false;
  mocks.org = "test-org";
  mocks.fetchSubscription.mockResolvedValue(missing);
  mocks.createCheckout.mockResolvedValue(success(checkoutSession));
  mocks.fetchCheckoutStatus.mockResolvedValue(
    success({
      status: "complete",
      paymentStatus: "unpaid",
      billingReady: false,
    }),
  );
  mocks.confirm.mockResolvedValue({ type: "success" });
});
afterEach(() => {
  cleanup();
  client.clear();
  resetPaygCheckoutLocks();
});
it("starts checkout only after a fresh server 404 and never duplicates it on rerender", async () => {
  const subscription = deferred<typeof missing>();
  mocks.fetchSubscription.mockReturnValue(subscription.promise);
  const view = render(<Harness />);
  await waitFor(() => expect(mocks.fetchSubscription).toHaveBeenCalledOnce());
  expect(mocks.createCheckout).not.toHaveBeenCalled();
  await act(async () => subscription.resolve(missing));
  expect(await screen.findByText("Complete Stripe")).toBeTruthy();
  view.rerender(<Harness />);
  expect(mocks.createCheckout).toHaveBeenCalledOnce();
});
it.each(["active", "trialing", "past_due", "incomplete"])(
  "redirects existing %s subscriptions without duplicate checkout",
  async (status) => {
    mocks.fetchSubscription.mockResolvedValue(success({ status }));
    render(<Harness />);
    expect(await screen.findByText("Payment details")).toBeTruthy();
    expect(mocks.createCheckout).not.toHaveBeenCalled();
  },
);
it("fails closed on subscription errors and retries through the API", async () => {
  mocks.fetchSubscription.mockResolvedValueOnce({
    ok: false,
    error: new Error("unavailable"),
  });
  render(<Harness />);
  expect((await screen.findByRole("alert")).textContent).toContain(
    "No checkout was started",
  );
  expect(mocks.createCheckout).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(await screen.findByText("Complete Stripe")).toBeTruthy();
  expect(mocks.fetchSubscription).toHaveBeenCalledTimes(2);
  expect(mocks.createCheckout).toHaveBeenCalledOnce();
});
it("does not start checkout for a viewer without billing authorization", () => {
  mocks.admin = false;
  render(<Harness />);
  expect(screen.getByText(/Billing setup is not available/)).toBeTruthy();
  expect(screen.queryByRole("heading", { name: "Pricing" })).toBeNull();
  expect(mocks.fetchSubscription).not.toHaveBeenCalled();
  expect(mocks.createCheckout).not.toHaveBeenCalled();
});
it("waits for backend subscription confirmation after Stripe completes", async () => {
  render(<Harness />);
  fireEvent.click(await screen.findByText("Complete Stripe"));
  expect(await screen.findByText("Confirming billing setup")).toBeTruthy();
  expect(mocks.confirm).toHaveBeenCalledOnce();
  expect(screen.queryByText("Confirmed success")).toBeNull();
  const subscription =
    deferred<ReturnType<typeof success<{ status: string }>>>();
  mocks.fetchSubscription.mockReturnValue(subscription.promise);
  fireEvent.click(screen.getByText("Check again"));
  expect(screen.queryByText("Confirmed success")).toBeNull();
  await act(async () => subscription.resolve(success({ status: "trialing" })));
  expect(await screen.findByText("Confirmed success")).toBeTruthy();
  expect(mocks.sessionRefetch).toHaveBeenCalled();
  expect(mocks.createCheckout).toHaveBeenCalledOnce();
});
it("waits for session authorization even after a paid return and active subscription", async () => {
  mocks.whitelisted = false;
  mocks.fetchCheckoutStatus.mockResolvedValue(
    success({ status: "complete", paymentStatus: "paid", billingReady: true }),
  );
  mocks.fetchSubscription.mockResolvedValue(success({ status: "active" }));
  const view = render(<Harness path={returnedPath} />);
  await waitFor(() =>
    expect(client.getQueryState(subscriptionKey)?.status).toBe("success"),
  );
  expect(screen.queryByText("Confirmed success")).toBeNull();
  mocks.whitelisted = true;
  view.rerender(<Harness path={returnedPath} />);
  expect(await screen.findByText("Confirmed success")).toBeTruthy();
  expect(mocks.createCheckout).not.toHaveBeenCalled();
});
it("discards confirmation state when the active organization changes", async () => {
  const view = render(<Harness />);
  fireEvent.click(await screen.findByText("Complete Stripe"));
  expect(await screen.findByText("Confirming billing setup")).toBeTruthy();
  const subscription = deferred<typeof missing>();
  mocks.fetchSubscription.mockReturnValue(subscription.promise);
  mocks.org = "another-org";
  view.rerender(<Harness />);
  expect(screen.queryByText("Confirming billing setup")).toBeNull();
  expect(screen.queryByText("Complete Stripe")).toBeNull();
  expect(mocks.createCheckout).toHaveBeenCalledOnce();
  await act(async () => subscription.resolve(missing));
  expect(await screen.findByText("Complete Stripe")).toBeTruthy();
  expect(mocks.createCheckout).toHaveBeenCalledTimes(2);
});
it("rechecks a cached 404 before starting checkout on a new mount", async () => {
  // Populate the generated cache key shared with the billing overview.
  const first = render(<Harness path={returnedPath} />);
  await waitFor(() =>
    expect(client.getQueryState(subscriptionKey)?.status).toBe("error"),
  );
  first.unmount();
  const subscription = deferred<typeof missing>();
  mocks.fetchSubscription.mockReturnValue(subscription.promise);
  render(<Harness />);
  await waitFor(() => expect(mocks.fetchSubscription).toHaveBeenCalledTimes(2));
  expect(mocks.createCheckout).not.toHaveBeenCalled();
  await act(async () => subscription.resolve(missing));
  expect(await screen.findByText("Complete Stripe")).toBeTruthy();
  expect(mocks.createCheckout).toHaveBeenCalledOnce();
});
it("waits for a previous mount's pending checkout to settle before starting again", async () => {
  const checkout =
    deferred<ReturnType<typeof success<typeof checkoutSession>>>();
  mocks.createCheckout.mockReturnValueOnce(checkout.promise);
  const first = render(<Harness />);
  await waitFor(() => expect(mocks.createCheckout).toHaveBeenCalledOnce());
  first.unmount();
  render(<Harness />);
  await waitFor(() => expect(mocks.fetchSubscription).toHaveBeenCalledTimes(2));
  expect(screen.queryByText("Complete Stripe")).toBeNull();
  expect(mocks.createCheckout).toHaveBeenCalledOnce();
  await act(async () => checkout.resolve(success(checkoutSession)));
  expect(await screen.findByText("Complete Stripe")).toBeTruthy();
  expect(mocks.createCheckout).toHaveBeenCalledTimes(2);
});
it("offers retry when checkout creation fails without automatically duplicating it", async () => {
  mocks.createCheckout.mockResolvedValueOnce({
    ok: false,
    error: new Error("unavailable"),
  });
  render(<Harness />);
  expect(
    await screen.findByText("Couldn't start checkout. Try again."),
  ).toBeTruthy();
  expect(mocks.createCheckout).toHaveBeenCalledOnce();
  fireEvent.click(screen.getByRole("button", { name: "Retry checkout" }));
  expect(await screen.findByText("Complete Stripe")).toBeTruthy();
  expect(mocks.createCheckout).toHaveBeenCalledTimes(2);
});
it.each(["open", "expired"])(
  "resumes checkout after a freshly verified %s return",
  async (status) => {
    const returned = deferred<
      ReturnType<
        typeof success<{
          status: string;
          paymentStatus: string;
          billingReady: boolean;
        }>
      >
    >();
    mocks.fetchCheckoutStatus.mockReturnValue(returned.promise);
    render(<Harness path={returnedPath} />);
    await waitFor(() =>
      expect(mocks.fetchCheckoutStatus).toHaveBeenCalledOnce(),
    );
    expect(screen.getByText("Confirming billing setup")).toBeTruthy();
    expect(mocks.createCheckout).not.toHaveBeenCalled();
    expect(mocks.fetchCheckoutStatus.mock.calls[0]?.[1]).toEqual({
      sessionId: "cs_test_return",
    });
    await act(async () =>
      returned.resolve(
        success({ status, paymentStatus: "unpaid", billingReady: false }),
      ),
    );
    expect(await screen.findByText("Complete Stripe")).toBeTruthy();
    expect(screen.queryByText("Confirming billing setup")).toBeNull();
    expect(mocks.createCheckout).toHaveBeenCalledOnce();
  },
);
it("keeps a completed unpaid checkout pending despite an active subscription", async () => {
  mocks.fetchSubscription.mockResolvedValue(success({ status: "active" }));
  render(<Harness path={returnedPath} />);
  expect(
    await screen.findByText(/Your payment is still processing/),
  ).toBeTruthy();
  await waitFor(() =>
    expect(client.getQueryState(subscriptionKey)?.status).toBe("success"),
  );
  expect(screen.getByText("Confirming billing setup")).toBeTruthy();
  expect(screen.queryByText("Confirmed success")).toBeNull();
  expect(mocks.createCheckout).not.toHaveBeenCalled();
});
it.each(["paid", "no_payment_required", "unpaid"])(
  "finishes a %s return only after fresh backend verification",
  async (paymentStatus) => {
    client.setQueryData(subscriptionKey, { status: "active" });
    const subscription =
      deferred<ReturnType<typeof success<{ status: string }>>>();
    mocks.fetchSubscription.mockReturnValue(subscription.promise);
    mocks.fetchCheckoutStatus.mockResolvedValue(
      success({ status: "complete", paymentStatus, billingReady: true }),
    );
    render(<Harness path={returnedPath} />);
    await waitFor(() =>
      expect(client.getQueryState(returnKey)?.status).toBe("success"),
    );
    expect(screen.queryByText("Confirmed success")).toBeNull();
    expect(screen.queryByText(/Your payment is still processing/)).toBeNull();
    await act(async () =>
      subscription.resolve(success({ status: "trialing" })),
    );
    expect(await screen.findByText("Confirmed success")).toBeTruthy();
    expect(mocks.createCheckout).not.toHaveBeenCalled();
  },
);
it("does not activate billing from a return URL without a persisted subscription", async () => {
  mocks.fetchCheckoutStatus.mockResolvedValue(
    success({
      status: "complete",
      paymentStatus: "unpaid",
      billingReady: true,
    }),
  );
  render(<Harness path={returnedPath} />);
  await waitFor(() => {
    expect(client.getQueryState(returnKey)?.status).toBe("success");
    expect(client.getQueryState(subscriptionKey)?.status).toBe("error");
  });
  expect(screen.getByText("Confirming billing setup")).toBeTruthy();
  expect(screen.queryByText("Confirmed success")).toBeNull();
  expect(screen.queryByText(/Your payment is still processing/)).toBeNull();
  expect(mocks.createCheckout).not.toHaveBeenCalled();
});
it("fails closed on unverified return status and retries through the API", async () => {
  mocks.fetchCheckoutStatus.mockResolvedValue({
    ok: false,
    error: new Error("unavailable"),
  });
  vi.useFakeTimers();
  try {
    render(<Harness path={returnedPath} />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(8_000);
    });
  } finally {
    vi.useRealTimers();
  }
  expect(screen.getByText(/Could not verify/)).toBeTruthy();
  expect(mocks.fetchCheckoutStatus).toHaveBeenCalledTimes(4);
  expect(mocks.createCheckout).not.toHaveBeenCalled();
  mocks.fetchCheckoutStatus.mockResolvedValue(
    success({
      status: "expired",
      paymentStatus: "unpaid",
      billingReady: false,
    }),
  );
  fireEvent.click(screen.getByText("Check again"));
  expect(await screen.findByText("Complete Stripe")).toBeTruthy();
  expect(mocks.fetchCheckoutStatus).toHaveBeenCalledTimes(5);
  expect(mocks.createCheckout).toHaveBeenCalledOnce();
});
it("does not trust cached open return status before a fresh read", async () => {
  client.setQueryData(returnKey, {
    status: "open",
    paymentStatus: "unpaid",
    billingReady: false,
  });
  const returned = deferred<
    ReturnType<
      typeof success<{
        status: string;
        paymentStatus: string;
        billingReady: boolean;
      }>
    >
  >();
  mocks.fetchCheckoutStatus.mockReturnValue(returned.promise);
  render(<Harness path={returnedPath} />);
  await waitFor(() => expect(mocks.fetchCheckoutStatus).toHaveBeenCalledOnce());
  expect(mocks.createCheckout).not.toHaveBeenCalled();
  await act(async () =>
    returned.resolve(
      success({
        status: "complete",
        paymentStatus: "unpaid",
        billingReady: false,
      }),
    ),
  );
  expect(
    await screen.findByText(/Your payment is still processing/),
  ).toBeTruthy();
  expect(mocks.createCheckout).not.toHaveBeenCalled();
});
it("allows checkout during an active trial on a contracted account", async () => {
  mocks.accountType = "enterprise";
  mocks.trial.mockReturnValue({
    startedAt: new Date(Date.now() - 86_400_000),
    endsAt: new Date(Date.now() + 86_400_000),
  });
  render(<Harness />);
  expect(await screen.findByText("Complete Stripe")).toBeTruthy();
  expect(mocks.createCheckout).toHaveBeenCalledOnce();
});
it.each(["free", "base"])(
  "allows unsubscribed %s partner signups without a trial",
  async (accountType) => {
    mocks.accountType = accountType;
    render(<Harness />);
    expect(await screen.findByText("Complete Stripe")).toBeTruthy();
    expect(mocks.createCheckout).toHaveBeenCalledOnce();
  },
);
it.each(["free", "base"])(
  "excludes already subscribed %s organizations",
  (accountType) => {
    mocks.accountType = accountType;
    mocks.hasActiveSubscription = true;
    render(<Harness />);
    expect(screen.getByText(/Billing setup is not available/)).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "Pricing" })).toBeNull();
    expect(mocks.createCheckout).not.toHaveBeenCalled();
  },
);
it("shows the setup heading and organization-scoped Overview tab", async () => {
  mocks.org = "another-org";
  render(<Harness path="/another-org/billing/setup" />);
  expect(screen.getByRole("heading", { name: "Billing setup" })).toBeTruthy();
  expect(screen.queryByText("$0.35 per million tokens")).toBeNull();
  expect(
    screen.queryByText("Other inference billed at provider cost"),
  ).toBeNull();
  expect(screen.getByText("Organization")).toBeTruthy();
  expect(
    screen.getByText("Add a payment method to activate pay as you go."),
  ).toBeTruthy();
  fireEvent.mouseDown(await screen.findByRole("tab", { name: "Overview" }), {
    button: 0,
    ctrlKey: false,
  });
  expect(screen.getByText("Billing overview")).toBeTruthy();
});
it("shows the selected Payment method tab on payment management", async () => {
  mocks.fetchSubscription.mockResolvedValue(success({ status: "active" }));
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/test-org/billing/payment-method"]}>
        <BillingPaymentMethod />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(
    (await screen.findByRole("tab", { name: "Payment method" })).getAttribute(
      "data-state",
    ),
  ).toBe("active");
  expect(screen.getByRole("heading", { name: "Pricing" })).toBeTruthy();
  expect(screen.getByText("$0.35")).toBeTruthy();
  expect(screen.getByText("per million tokens")).toBeTruthy();
  expect(screen.getByText("$0.99")).toBeTruthy();
  expect(screen.getByText("per million tokens scanned")).toBeTruthy();
  expect(screen.getByText("$20.00")).toBeTruthy();
  expect(screen.getByText("per GiB of egress")).toBeTruthy();
  expect(mocks.createCheckout).not.toHaveBeenCalled();
  expect(
    screen.getByText("Other inference billed at provider cost"),
  ).toBeTruthy();
});

it.each([
  ["needs setup", true, "Billing setup destination"],
  ["unavailable", false, "Billing overview destination"],
])(
  "redirects payment management when billing is %s",
  async (_state, admin, destination) => {
    mocks.admin = admin;
    mocks.fetchSubscription.mockResolvedValue(missing);
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={["/test-org/billing/payment-method"]}>
          <Routes>
            <Route
              path="/:org/billing/payment-method"
              element={<BillingPaymentMethod />}
            />
            <Route
              path="/:org/billing/setup"
              element={<p>Billing setup destination</p>}
            />
            <Route
              path="/:org/billing"
              element={<p>Billing overview destination</p>}
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(await screen.findByText(destination)).toBeTruthy();
    expect(mocks.createCheckout).not.toHaveBeenCalled();
  },
);

it("bounds all confirmation polling and lets the user restart it", async () => {
  vi.useFakeTimers();
  try {
    render(<Harness path={returnedPath} />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(61_000);
    });
    expect(screen.getByText(/Automatic checks have paused/)).toBeTruthy();
    const counts = [
      mocks.fetchSubscription.mock.calls.length,
      mocks.fetchCheckoutStatus.mock.calls.length,
      mocks.sessionRefetch.mock.calls.length,
    ];
    await act(async () => {
      await vi.advanceTimersByTimeAsync(20_000);
    });
    expect([
      mocks.fetchSubscription.mock.calls.length,
      mocks.fetchCheckoutStatus.mock.calls.length,
      mocks.sessionRefetch.mock.calls.length,
    ]).toEqual(counts);
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Check again" }));
    });
    expect(mocks.sessionRefetch.mock.calls.length).toBeGreaterThan(counts[2]!);
    expect(screen.queryByText(/Automatic checks have paused/)).toBeNull();
  } finally {
    vi.useRealTimers();
  }
});

it("retries transient checkout status failures with backoff", async () => {
  mocks.fetchCheckoutStatus.mockResolvedValueOnce({
    ok: false,
    error: { statusCode: 503 },
  });
  vi.useFakeTimers();
  try {
    render(<Harness path={returnedPath} />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(mocks.fetchCheckoutStatus).toHaveBeenCalledOnce();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(mocks.fetchCheckoutStatus).toHaveBeenCalledTimes(2);
  } finally {
    vi.useRealTimers();
  }
});

it("does not retry missing or foreign returned checkout sessions", async () => {
  mocks.fetchCheckoutStatus.mockResolvedValue(missing);
  vi.useFakeTimers();
  try {
    render(<Harness path={returnedPath} />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });
    expect(mocks.fetchCheckoutStatus).toHaveBeenCalledOnce();
    expect(screen.queryByRole("button", { name: "Check again" })).toBeNull();
    expect(
      screen.getByRole("button", { name: "Return to billing setup" }),
    ).toBeTruthy();
  } finally {
    vi.useRealTimers();
  }
});

it.each([59_500, 61_000])(
  "does not retry a status failure at %i ms beyond the retry budget",
  async (elapsed) => {
    const response = deferred<{ ok: false; error: { statusCode: number } }>();
    mocks.fetchCheckoutStatus.mockReturnValue(response.promise);
    vi.useFakeTimers();
    try {
      render(<Harness path={returnedPath} />);
      await act(async () => {
        await vi.advanceTimersByTimeAsync(elapsed);
        response.resolve({ ok: false, error: { statusCode: 503 } });
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(20_000);
      });
      expect(mocks.fetchCheckoutStatus).toHaveBeenCalledOnce();
      expect(screen.getByText(/Automatic checks have paused/)).toBeTruthy();
    } finally {
      vi.useRealTimers();
    }
  },
);

it("starts a fresh confirmation window after a late open status resumes checkout", async () => {
  const response = deferred<
    ReturnType<
      typeof success<{
        status: string;
        paymentStatus: string;
        billingReady: boolean;
      }>
    >
  >();
  mocks.fetchCheckoutStatus.mockReturnValue(response.promise);
  vi.useFakeTimers();
  try {
    render(<Harness path={returnedPath} />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(61_000);
    });
    expect(screen.getByText(/Automatic checks have paused/)).toBeTruthy();
    await act(async () => {
      response.resolve(
        success({
          status: "open",
          paymentStatus: "unpaid",
          billingReady: false,
        }),
      );
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10);
    });
    expect(screen.getByText("Complete Stripe")).toBeTruthy();
    const calls = mocks.sessionRefetch.mock.calls.length;
    await act(async () => {
      fireEvent.click(screen.getByText("Complete Stripe"));
    });
    expect(screen.queryByText(/Automatic checks have paused/)).toBeNull();
    expect(mocks.sessionRefetch.mock.calls.length).toBeGreaterThan(calls);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(59_000);
    });
    expect(screen.queryByText(/Automatic checks have paused/)).toBeNull();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });
    expect(screen.getByText(/Automatic checks have paused/)).toBeTruthy();
  } finally {
    vi.useRealTimers();
  }
});
