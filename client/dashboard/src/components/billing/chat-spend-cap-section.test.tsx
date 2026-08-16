import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render as rtlRender,
  screen,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { ProductTier } from "@/hooks/useProductTier";

const mocks = vi.hoisted(() => ({
  productTier: vi.fn(),
  session: vi.fn(),
  query: vi.fn(),
  refetch: vi.fn(),
  mutation: vi.fn(),
  mutate: vi.fn(),
  reset: vi.fn(),
  hasAnyScope: vi.fn(),
  invalidate: vi.fn(),
  subscription: vi.fn(),
  subscriptionRefetch: vi.fn(),
}));

vi.mock("@/hooks/useProductTier", () => ({
  useProductTier: () => mocks.productTier() as ProductTier,
}));

vi.mock("@/contexts/Auth", () => ({
  useSession: () => mocks.session(),
}));

vi.mock("@gram/client/react-query/getCreditUsage.js", () => ({
  // The hook options are part of what's under test — the section has to opt out
  // of the shared throwOnError — so the arguments are forwarded, not dropped.
  useGetCreditUsage: (...args: unknown[]) => mocks.query(...args),
  invalidateAllGetCreditUsage: mocks.invalidate,
}));

vi.mock("@gram/client/react-query/setSpendCap.js", () => ({
  useSetSpendCapMutation: (options?: { onSuccess?: () => void }) =>
    mocks.mutation(options),
}));

// The shared subscription read is mocked at the wrapper: what the cap does
// with the live Stripe state is this file's subject, and the wrapper's own
// query options are covered by `use-stripe-subscription.test.ts`.
vi.mock("@/components/billing/use-stripe-subscription", () => ({
  useStripeSubscription: () => mocks.subscription(),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: Scope) => mocks.hasAnyScope([scope]) as boolean,
    hasAnyScope: (scopes: Scope[]) => mocks.hasAnyScope(scopes) as boolean,
    hasAllScopes: (scopes: Scope[]) => mocks.hasAnyScope(scopes) as boolean,
    isLoading: false,
  }),
}));

// Page chrome isn't what's under test; render it as plain boxes so a failure
// here can only mean the section.
vi.mock("@/components/page-layout", () => {
  const Section = ({ children }: { children: ReactNode }) => <>{children}</>;
  Section.Title = ({ children }: { children: ReactNode }) => (
    <h2>{children}</h2>
  );
  Section.Description = ({ children }: { children: ReactNode }) => (
    <p>{children}</p>
  );
  Section.Body = ({ children }: { children: ReactNode }) => <>{children}</>;
  return { Page: { Section } };
});

import { CHAT_SPEND_CAP_ANCHOR } from "./chat-spend-cap";
import { ChatSpendCapSection } from "./chat-spend-cap-section";

const DAY = 24 * 60 * 60 * 1000;

type CreditUsage = { creditsUsed: number; monthlyCredits: number };

type QueryState = {
  data?: CreditUsage | undefined;
  isError?: boolean;
  isFetching?: boolean;
};

/**
 * The credit usage query the cap is read from. No `data` is a query that never
 * resolved — the loading state, or a load that failed outright.
 */
function queryState({
  data,
  isError = false,
  isFetching = false,
}: QueryState = {}) {
  mocks.query.mockReturnValue({
    data,
    isError,
    isFetching,
    refetch: mocks.refetch,
  });
}

/** A loaded cap of `monthlyCredits`, as the query reports it. */
function loadedCap(monthlyCredits: number) {
  queryState({ data: { creditsUsed: 12, monthlyCredits } });
}

// Midday UTC so the formatted day can't slide either side of the date line in
// whichever time zone the tests happen to run in.
const TRIAL_END = new Date("2026-08-20T12:00:00.000Z");

type Subscription = {
  status: string;
  trialEnd?: Date;
  cancelAtPeriodEnd?: boolean;
};

type SubscriptionState = {
  data?: Subscription | undefined;
  error?: unknown;
  isError?: boolean;
  isFetching?: boolean;
};

/**
 * The live Stripe subscription the cap gates on. No `data` is a read that never
 * resolved — the loading state, or a load that failed outright.
 */
function subscriptionState({
  data,
  error,
  isError = false,
  isFetching = false,
}: SubscriptionState = {}) {
  mocks.subscription.mockReturnValue({
    data,
    error,
    isError,
    isFetching,
    refetch: mocks.subscriptionRefetch,
  });
}

/** Shaped like the SDK's 404 rejection, which is what the branch keys on. */
function notFound(): Error {
  return Object.assign(new Error("subscription not found"), {
    statusCode: 404,
  });
}

/** Stripe has converted the trial and is billing the organization. */
function billingSubscription() {
  subscriptionState({ data: { status: "active" } });
}

type MutationState = {
  isPending?: boolean;
  isSuccess?: boolean;
  isError?: boolean;
};

function mutationState({
  isPending = false,
  isSuccess = false,
  isError = false,
}: MutationState = {}) {
  mocks.mutation.mockImplementation(() => ({
    mutate: mocks.mutate,
    reset: mocks.reset,
    isPending,
    isSuccess,
    isError,
  }));
}

/**
 * The section reaches for the query client to invalidate after a save, and the
 * form links to the in-app sales gate through the router.
 */
function render(ui: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrap = (node: ReactNode) => (
    <QueryClientProvider client={client}>
      <MemoryRouter>{node}</MemoryRouter>
    </QueryClientProvider>
  );
  const result = rtlRender(wrap(ui));
  // Re-wrapping keeps the provider at the root, so a rerender updates the
  // mounted section instead of remounting it under a new tree.
  return {
    ...result,
    rerender: (node: ReactNode) => result.rerender(wrap(node)),
  };
}

const field = () =>
  screen.queryByLabelText(/monthly chat spend cap/i) as HTMLInputElement | null;

const saveButton = () =>
  screen.queryByRole("button", { name: /save spend cap/i });

const heading = () =>
  screen.queryByRole("heading", { name: /chat spend cap/i });

/** The cap the save handler sent, unwrapped from the request envelope. */
function sentCap() {
  const variables = mocks.mutate.mock.calls.at(-1)?.[0] as {
    request: { spendCap: { monthlyCredits: number } };
  };
  return variables.request.spendCap.monthlyCredits;
}

describe("ChatSpendCapSection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.productTier.mockReturnValue("payg");
    mocks.session.mockReturnValue({ trial: null });
    mocks.hasAnyScope.mockReturnValue(true);
    loadedCap(250);
    billingSubscription();
    mutationState();
  });

  afterEach(cleanup);

  // The banner that reports a paused organization links straight at this
  // anchor, so it has to survive edits to the section that carries it.
  it("carries the anchor the paused-chat banner links to", () => {
    const { container } = render(<ChatSpendCapSection />);

    expect(container.querySelector(`#${CHAT_SPEND_CAP_ANCHOR}`)).not.toBeNull();
  });

  it("seeds the field from the cap the usage meters read", () => {
    render(<ChatSpendCapSection />);

    expect(field()!.value).toBe("250");
    expect(saveButton()).not.toBeNull();
  });

  it("sends the cap the admin entered", () => {
    render(<ChatSpendCapSection />);

    fireEvent.change(field()!, { target: { value: "1500" } });
    fireEvent.click(saveButton()!);

    expect(mocks.mutate).toHaveBeenCalledTimes(1);
    expect(sentCap()).toBe(1500);
  });

  it("lowers the cap as readily as it raises it", () => {
    render(<ChatSpendCapSection />);

    fireEvent.change(field()!, { target: { value: "1" } });
    fireEvent.click(saveButton()!);

    expect(sentCap()).toBe(1);
  });

  it("confirms a save and refreshes the cached credit usage", () => {
    mutationState({ isSuccess: true });

    render(<ChatSpendCapSection />);

    expect(screen.getByRole("status").textContent).toMatch(/saved/i);

    // The section — and the usage meter above it — read the cap back from the
    // credit usage query, so that key has to be invalidated or both would keep
    // showing the pre-save amount.
    const options = mocks.mutation.mock.calls.at(-1)?.[0] as {
      onSuccess: () => void;
    };
    options.onSuccess();
    expect(mocks.invalidate).toHaveBeenCalled();
  });

  it("disables saving while the save is in flight", () => {
    mutationState({ isPending: true });

    render(<ChatSpendCapSection />);

    // The button renames itself while the request is in flight, so the idle
    // name is gone by the time this queries for it.
    expect(saveButton()).toBeNull();
    const button = screen.getByRole("button", { name: /saving/i });
    expect(button.hasAttribute("disabled")).toBe(true);
  });

  it("reports a failed save and lets the admin retry it", () => {
    mutationState({ isError: true });

    render(<ChatSpendCapSection />);

    expect(screen.getByRole("alert").textContent).toMatch(
      /couldn't save the chat spend cap/i,
    );

    fireEvent.click(saveButton()!);

    expect(mocks.mutate).toHaveBeenCalledTimes(1);
    expect(sentCap()).toBe(250);
  });

  it("clears stale save feedback once the amount is edited", () => {
    mutationState({ isError: true });

    render(<ChatSpendCapSection />);

    fireEvent.change(field()!, { target: { value: "300" } });

    expect(mocks.reset).toHaveBeenCalled();
  });

  // An amount the API would reject has to be caught in the field, where it can
  // be corrected, rather than sent and bounced back as a transient-looking
  // failure the admin is invited to retry.
  it.each(["0", "10001", "12.5", ""])("refuses to send %s", (value) => {
    render(<ChatSpendCapSection />);

    fireEvent.change(field()!, { target: { value } });
    fireEvent.click(saveButton()!);

    expect(mocks.mutate).not.toHaveBeenCalled();
    expect(screen.getByRole("alert").textContent).toMatch(
      /between \$1 and \$10,000/i,
    );
  });

  it.each(["1", "10000"])("accepts the boundary amount %s", (value) => {
    render(<ChatSpendCapSection />);

    fireEvent.change(field()!, { target: { value } });
    fireEvent.click(saveButton()!);

    expect(sentCap()).toBe(Number(value));
  });

  it("recovers once an out-of-range amount is corrected", () => {
    render(<ChatSpendCapSection />);

    fireEvent.change(field()!, { target: { value: "20000" } });
    fireEvent.click(saveButton()!);
    expect(mocks.mutate).not.toHaveBeenCalled();

    fireEvent.change(field()!, { target: { value: "2000" } });
    fireEvent.click(saveButton()!);

    expect(mocks.mutate).toHaveBeenCalledTimes(1);
    expect(sentCap()).toBe(2000);
  });

  // $10,000 is the ceiling the endpoint enforces, so an admin who needs more
  // has to be pointed at a conversation rather than left retrying an amount
  // that will never be accepted. The gate is the in-app booking route, which
  // prefills from the session — not the marketing site.
  it("points an admin needing a larger cap at the in-app sales gate", () => {
    render(<ChatSpendCapSection />);

    expect(screen.getByText(/need a cap above \$10,000/i)).toBeTruthy();
    const salesLink = screen.getByRole("link", { name: "Talk to us" });
    expect(salesLink.getAttribute("href")).toBe("/talk-to-us");
    expect(salesLink.getAttribute("target")).toBeNull();
  });

  // The endpoint is admin-only, so a member gets the amount they're spending
  // under and no control that would fire a request bound to be refused.
  it("shows a member the cap without a way to change it", () => {
    mocks.hasAnyScope.mockReturnValue(false);

    render(<ChatSpendCapSection />);

    expect(screen.getByText("$250")).toBeTruthy();
    expect(field()).toBeNull();
    expect(saveButton()).toBeNull();
    // The form owns the mutation, so a member never even mounts it.
    expect(mocks.mutation).not.toHaveBeenCalled();
    expect(mocks.mutate).not.toHaveBeenCalled();
    expect(mocks.hasAnyScope).toHaveBeenCalledWith(["org:admin"]);
  });

  // The shared query client throws everything but a 401/403 to the app error
  // boundary, which would take the whole billing page down over this section
  // and leave the branches below unreachable. Handling the failure inline only
  // works if the query opts out.
  it("keeps a load failure out of the app error boundary", () => {
    render(<ChatSpendCapSection />);

    const options = mocks.query.mock.calls.at(-1)?.[2] as {
      throwOnError?: boolean;
    };
    expect(options.throwOnError).toBe(false);
  });

  it("explains a failed load instead of showing an empty field", () => {
    queryState({ isError: true });

    render(<ChatSpendCapSection />);

    expect(field()).toBeNull();
    expect(screen.getByRole("alert").textContent).toMatch(
      /couldn't load the chat spend cap/i,
    );
  });

  // Nothing was cached, so there is no cap on screen to work from. The failure
  // stays inside the section, so the way out has to be here too.
  it("retries a failed load in place", () => {
    queryState({ isError: true });

    render(<ChatSpendCapSection />);

    fireEvent.click(screen.getByRole("button", { name: /^retry$/i }));

    expect(mocks.refetch).toHaveBeenCalledTimes(1);
  });

  it("disables the retry while the reload is in flight", () => {
    queryState({ isError: true, isFetching: true });

    render(<ChatSpendCapSection />);

    const button = screen.getByRole("button", { name: /retrying/i });
    expect(button.hasAttribute("disabled")).toBe(true);
    expect(screen.queryByRole("button", { name: /^retry$/i })).toBeNull();
  });

  it("shows the form once a retried load succeeds", () => {
    queryState({ isError: true });

    const { rerender } = render(<ChatSpendCapSection />);

    loadedCap(250);
    rerender(<ChatSpendCapSection />);

    expect(field()!.value).toBe("250");
    expect(saveButton()).not.toBeNull();
  });

  // A refetch that fails leaves the last successful value in the cache, so the
  // query reports data and an error together. The form stays — taking it away
  // would discard an in-progress edit — and the stale amount is called out.
  it("keeps the form and reports the failure when a cached cap is held", () => {
    queryState({
      data: { creditsUsed: 12, monthlyCredits: 250 },
      isError: true,
    });

    render(<ChatSpendCapSection />);

    expect(field()!.value).toBe("250");
    expect(screen.getByRole("alert").textContent).toMatch(
      /couldn't refresh the chat spend cap/i,
    );
  });

  // A refetch (the mutation's own invalidation, a window refocus) resolving
  // mid-edit would otherwise replace what the admin is typing.
  it("keeps an in-progress edit through a background refetch", () => {
    const { rerender } = render(<ChatSpendCapSection />);

    fireEvent.change(field()!, { target: { value: "900" } });

    loadedCap(400);
    rerender(<ChatSpendCapSection />);

    expect(field()!.value).toBe("900");
  });

  // The cap can change under an idle page — another admin sets it, or this
  // section's own post-save invalidation lands — and an untouched field left
  // showing the old amount would save that stale amount straight back.
  it("takes up a cap that changed while the field was untouched", () => {
    const { rerender } = render(<ChatSpendCapSection />);
    expect(field()!.value).toBe("250");

    loadedCap(400);
    rerender(<ChatSpendCapSection />);

    expect(field()!.value).toBe("400");
    fireEvent.click(saveButton()!);
    expect(sentCap()).toBe(400);
  });

  // An edit is dirty against whatever the field was last seeded from, so the
  // admin who types over a synchronized cap keeps their amount too.
  it("keeps an edit made after a cap synchronized in", () => {
    const { rerender } = render(<ChatSpendCapSection />);

    loadedCap(400);
    rerender(<ChatSpendCapSection />);

    fireEvent.change(field()!, { target: { value: "900" } });
    loadedCap(500);
    rerender(<ChatSpendCapSection />);

    expect(field()!.value).toBe("900");
  });

  // Editing back to the amount on the server is not an edit to protect: the
  // field is pristine again and the next cap has to reach it.
  it("resumes synchronizing once an edit is reverted", () => {
    const { rerender } = render(<ChatSpendCapSection />);

    fireEvent.change(field()!, { target: { value: "900" } });
    fireEvent.change(field()!, { target: { value: "250" } });

    loadedCap(400);
    rerender(<ChatSpendCapSection />);

    expect(field()!.value).toBe("400");
  });

  // Trials run on the enterprise tier, but the account type can already read as
  // PAYG while one is still running. Either way there is no pay-as-you-go bill
  // yet, so an active trial outranks the tier: the cap is locked rather than
  // editable, and nothing about it can reach the endpoint.
  describe.each<ProductTier>(["enterprise", "payg"])(
    "during an active trial on the %s tier",
    (tier) => {
      beforeEach(() => {
        mocks.productTier.mockReturnValue(tier);
        mocks.session.mockReturnValue({
          trial: {
            startedAt: new Date(Date.now() - 2 * DAY),
            endsAt: new Date(Date.now() + 12 * DAY),
          },
        });
      });

      it("locks the cap and explains when it starts", () => {
        render(<ChatSpendCapSection />);

        expect(heading()).not.toBeNull();
        expect(field()!.disabled).toBe(true);
        expect(
          screen.getByText(/starts when pay as you go begins/i),
        ).toBeTruthy();
      });

      it("never reaches the cap endpoint", () => {
        render(<ChatSpendCapSection />);

        expect(saveButton()).toBeNull();
        expect(mocks.mutation).not.toHaveBeenCalled();
        expect(mocks.mutate).not.toHaveBeenCalled();
        // The locked state is self-contained copy — no cap to read yet, and
        // no subscription to ask about either.
        expect(mocks.query).not.toHaveBeenCalled();
        expect(mocks.subscription).not.toHaveBeenCalled();
      });
    },
  );

  // Checkout marks the product trial converted and drops it from the session
  // while Stripe can keep trialing the subscription for days. So a PAYG
  // organization with no product trial left is not necessarily being billed:
  // from here on the live Stripe status is what decides, and it fails closed.
  describe("once the product trial has been converted", () => {
    it("locks the cap while Stripe is still trialing", () => {
      subscriptionState({ data: { status: "trialing", trialEnd: TRIAL_END } });

      render(<ChatSpendCapSection />);

      expect(field()!.disabled).toBe(true);
      expect(saveButton()).toBeNull();
      expect(
        screen.getByText(
          /starts when pay as you go begins on August 20, 2026/i,
        ),
      ).toBeTruthy();
    });

    it("never reaches the cap endpoint while Stripe is trialing", () => {
      subscriptionState({ data: { status: "trialing" } });

      render(<ChatSpendCapSection />);

      expect(mocks.mutation).not.toHaveBeenCalled();
      expect(mocks.query).not.toHaveBeenCalled();
    });

    // A trial on its way out is still a trial: there is no pay-as-you-go bill
    // for a cap to apply to until it converts.
    it("keeps a trial that is set to cancel locked", () => {
      subscriptionState({
        data: {
          status: "trialing",
          trialEnd: TRIAL_END,
          cancelAtPeriodEnd: true,
        },
      });

      render(<ChatSpendCapSection />);

      expect(field()!.disabled).toBe(true);
    });

    it("opens the form once Stripe is billing", () => {
      render(<ChatSpendCapSection />);

      expect(field()!.disabled).toBe(false);
      expect(field()!.value).toBe("250");
      expect(saveButton()).not.toBeNull();
    });

    // Only a subscription Stripe is actually billing has a bill for a cap to
    // apply to. `past_due` counts: the bill exists and Stripe is retrying it.
    it.each(["active", "past_due"])(
      "opens the form for a %s subscription",
      (status) => {
        subscriptionState({ data: { status } });

        render(<ChatSpendCapSection />);

        expect(field()!.disabled).toBe(false);
        expect(saveButton()).not.toBeNull();
      },
    );

    // Service and billing both run to the end of the period, so a scheduled
    // cancellation leaves the cap editable for as long as it applies.
    it("keeps the form through a scheduled cancellation", () => {
      subscriptionState({
        data: { status: "active", cancelAtPeriodEnd: true },
      });

      render(<ChatSpendCapSection />);

      expect(field()!.disabled).toBe(false);
      expect(saveButton()).not.toBeNull();
    });

    // Every status with no bill behind it locks, rather than leaving an admin
    // to set a cap that applies to nothing.
    it.each([
      "canceled",
      "unpaid",
      "incomplete",
      "incomplete_expired",
      "paused",
    ])("locks the cap for a %s subscription", (status) => {
      subscriptionState({ data: { status } });

      render(<ChatSpendCapSection />);

      expect(field()!.disabled).toBe(true);
      expect(saveButton()).toBeNull();
      expect(screen.getByText(/subscription isn't billing/i)).toBeTruthy();
      // No bill to read a cap from, and nothing to write one to.
      expect(mocks.mutation).not.toHaveBeenCalled();
      expect(mocks.mutate).not.toHaveBeenCalled();
      expect(mocks.query).not.toHaveBeenCalled();
    });

    // Nothing is known yet, so nothing is editable yet.
    it("locks the cap while the subscription read is in flight", () => {
      subscriptionState();

      render(<ChatSpendCapSection />);

      expect(field()).toBeNull();
      expect(saveButton()).toBeNull();
      expect(mocks.query).not.toHaveBeenCalled();
    });

    // A refetch is in flight precisely when the state might be about to change
    // — after a conversion or a cancellation — but also for reasons that have
    // nothing to do with this admin, like a window refocus. Taking the form
    // away would discard an amount they typed and haven't saved, so the lock
    // stops the writing without touching what is on screen.
    it("locks the form rather than closing it during a background refetch", () => {
      subscriptionState({ data: { status: "active" }, isFetching: true });

      render(<ChatSpendCapSection />);

      expect(field()!.disabled).toBe(true);
      expect(saveButton()!.hasAttribute("disabled")).toBe(true);
      expect(screen.getByRole("status").textContent).toMatch(
        /checking your subscription/i,
      );
    });

    it("keeps an unsaved draft through a refetch and blocks saving it", () => {
      const { rerender } = render(<ChatSpendCapSection />);

      fireEvent.change(field()!, { target: { value: "900" } });

      subscriptionState({ data: { status: "active" }, isFetching: true });
      rerender(<ChatSpendCapSection />);

      // The draft is still there, still showing the admin's amount...
      expect(field()!.value).toBe("900");
      expect(field()!.disabled).toBe(true);

      // ...and nothing can reach the endpoint until the state is confirmed.
      const save = saveButton()!;
      expect(save.hasAttribute("disabled")).toBe(true);
      fireEvent.click(save);
      fireEvent.submit(save.closest("form")!);
      expect(mocks.mutate).not.toHaveBeenCalled();
    });

    // Saving invalidates the credit usage and the subscription both, so the
    // confirmation and the lock land in the same render. Two live regions at
    // once get announced in whichever order the screen reader picks, or talk
    // over each other, so exactly one has to be present.
    it("announces one status when a save and a refetch overlap", () => {
      mutationState({ isSuccess: true });
      subscriptionState({ data: { status: "active" }, isFetching: true });

      render(<ChatSpendCapSection />);

      const statuses = screen.getAllByRole("status");
      expect(statuses).toHaveLength(1);
      expect(statuses[0]!.textContent).toMatch(/saved/i);
      expect(screen.queryByText(/checking your subscription/i)).toBeNull();
      // The lock still holds even though it no longer speaks for itself.
      expect(field()!.disabled).toBe(true);
    });

    // A failed save outranks both: the amount didn't land, which is the thing
    // the admin has to hear.
    it("announces one message when a failed save and a refetch overlap", () => {
      mutationState({ isError: true });
      subscriptionState({ data: { status: "active" }, isFetching: true });

      render(<ChatSpendCapSection />);

      expect(screen.queryAllByRole("status")).toHaveLength(0);
      expect(screen.getByRole("alert").textContent).toMatch(
        /couldn't save the chat spend cap/i,
      );
      expect(screen.queryByText(/checking your subscription/i)).toBeNull();
    });

    it("saves the same draft once the refetch settles", () => {
      const { rerender } = render(<ChatSpendCapSection />);

      fireEvent.change(field()!, { target: { value: "900" } });
      subscriptionState({ data: { status: "active" }, isFetching: true });
      rerender(<ChatSpendCapSection />);

      billingSubscription();
      rerender(<ChatSpendCapSection />);

      expect(field()!.value).toBe("900");
      expect(field()!.disabled).toBe(false);
      expect(screen.queryByRole("status")).toBeNull();

      fireEvent.click(saveButton()!);

      expect(mocks.mutate).toHaveBeenCalledTimes(1);
      expect(sentCap()).toBe(900);
    });

    // A refetch that settles somewhere the cap no longer applies takes the form
    // with it, draft and all — there is nothing left to save it against.
    it.each(["trialing", "canceled"])(
      "closes the form when the refetch settles to %s",
      (status) => {
        const { rerender } = render(<ChatSpendCapSection />);

        fireEvent.change(field()!, { target: { value: "900" } });

        subscriptionState({ data: { status } });
        rerender(<ChatSpendCapSection />);

        expect(saveButton()).toBeNull();
        expect(field()!.disabled).toBe(true);
        expect(field()!.value).toBe("");
      },
    );

    // The pay-as-you-go tier predates Stripe, so an organization can hold it
    // without a Stripe subscription behind it. There is no cap to set and
    // nothing a recheck would find, so the copy has to say so and stop.
    describe("with no Stripe subscription behind the tier", () => {
      beforeEach(() => {
        subscriptionState({ isError: true, error: notFound() });
      });

      it("says why the cap can't be set", () => {
        render(<ChatSpendCapSection />);

        expect(heading()).not.toBeNull();
        expect(
          screen.getByText(/this organization has no stripe subscription/i),
        ).toBeTruthy();
        expect(
          screen.queryByText(/couldn't check your subscription/i),
        ).toBeNull();
      });

      it("keeps the cap locked and unwritable", () => {
        render(<ChatSpendCapSection />);

        expect(field()!.disabled).toBe(true);
        expect(saveButton()).toBeNull();
        expect(mocks.mutation).not.toHaveBeenCalled();
        expect(mocks.mutate).not.toHaveBeenCalled();
        expect(mocks.query).not.toHaveBeenCalled();
      });

      it("offers nothing to recheck", () => {
        render(<ChatSpendCapSection />);

        expect(screen.queryByRole("button", { name: /recheck/i })).toBeNull();
        expect(mocks.subscriptionRefetch).not.toHaveBeenCalled();
      });

      // The answer is definitive, so it outranks whatever the cache still
      // holds — and it stays fail-closed either way.
      it("outranks a cached subscription", () => {
        subscriptionState({
          data: { status: "active" },
          isError: true,
          error: notFound(),
        });

        render(<ChatSpendCapSection />);

        expect(field()!.disabled).toBe(true);
        expect(saveButton()).toBeNull();
        expect(
          screen.getByText(/this organization has no stripe subscription/i),
        ).toBeTruthy();
      });
    });

    it("locks the cap when the subscription can't be read", () => {
      subscriptionState({
        isError: true,
        error: new Error("stripe unavailable"),
      });

      render(<ChatSpendCapSection />);

      expect(field()).toBeNull();
      expect(saveButton()).toBeNull();
      expect(screen.getByRole("alert").textContent).toMatch(
        /couldn't check your subscription/i,
      );
    });

    // The cached copy goes stale across exactly the moment that matters — a
    // trial converting, or a subscription ending — so a read that is failing
    // right now must not re-open the form on the strength of it.
    it("keeps the cap locked when a failing read still holds a cached subscription", () => {
      subscriptionState({
        data: { status: "active" },
        isError: true,
        error: new Error("stripe unavailable"),
      });

      render(<ChatSpendCapSection />);

      expect(field()).toBeNull();
      expect(saveButton()).toBeNull();
      expect(mocks.mutation).not.toHaveBeenCalled();
      expect(screen.getByRole("alert").textContent).toMatch(
        /couldn't check your subscription/i,
      );
    });

    it("rechecks the subscription in place", () => {
      subscriptionState({
        isError: true,
        error: new Error("stripe unavailable"),
      });

      render(<ChatSpendCapSection />);

      fireEvent.click(screen.getByRole("button", { name: /^recheck$/i }));

      expect(mocks.subscriptionRefetch).toHaveBeenCalledTimes(1);
    });

    it("disables the recheck while it is in flight", () => {
      subscriptionState({
        isError: true,
        error: new Error("stripe unavailable"),
        isFetching: true,
      });

      render(<ChatSpendCapSection />);

      const button = screen.getByRole("button", { name: /rechecking/i });
      expect(button.hasAttribute("disabled")).toBe(true);
    });

    it("returns the form once a recheck succeeds", () => {
      subscriptionState({
        isError: true,
        error: new Error("stripe unavailable"),
      });

      const { rerender } = render(<ChatSpendCapSection />);
      expect(field()).toBeNull();

      billingSubscription();
      rerender(<ChatSpendCapSection />);

      expect(field()!.disabled).toBe(false);
      expect(saveButton()).not.toBeNull();
    });
  });

  // Only an *active* trial locks the cap. Once it is over, a PAYG org has the
  // bill the cap applies to and gets the form back.
  it("returns the form to pay as you go once the trial has ended", () => {
    mocks.session.mockReturnValue({
      trial: {
        startedAt: new Date(Date.now() - 20 * DAY),
        endsAt: new Date(Date.now() - 6 * DAY),
      },
    });

    render(<ChatSpendCapSection />);

    expect(field()!.disabled).toBe(false);
    expect(field()!.value).toBe("250");
    expect(saveButton()).not.toBeNull();
  });

  // Every other tier has no pay-as-you-go bill for a cap to apply to.
  it.each<ProductTier>([
    "enterprise",
    "base",
    "base_PAID",
    "__deprecated__pro",
  ])("renders nothing on the %s view without an active trial", (tier) => {
    mocks.productTier.mockReturnValue(tier);

    const { container } = render(<ChatSpendCapSection />);

    expect(container.innerHTML).toBe("");
    expect(mocks.query).not.toHaveBeenCalled();
  });

  it("renders nothing for enterprise once the trial has ended", () => {
    mocks.productTier.mockReturnValue("enterprise");
    mocks.session.mockReturnValue({
      trial: {
        startedAt: new Date(Date.now() - 20 * DAY),
        endsAt: new Date(Date.now() - 6 * DAY),
      },
    });

    const { container } = render(<ChatSpendCapSection />);

    expect(container.innerHTML).toBe("");
  });
});
