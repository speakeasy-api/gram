import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";
import type { Scope } from "@gram/client/models/components/rolegrant.js";

const mocks = vi.hoisted(() => ({
  flagResult: vi.fn(),
  hasScope: vi.fn(),
  session: vi.fn(),
  mutate: vi.fn(),
  isPending: vi.fn(),
  capture: vi.fn(),
  hookOptions: vi.fn(),
}));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => mocks.flagResult() as FeatureFlagResult,
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: (scope: Scope) => mocks.hasScope(scope) }),
}));

vi.mock("@/contexts/Auth", () => ({
  useSession: () => mocks.session(),
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: mocks.capture }),
}));

vi.mock("@gram/client/react-query/createStripeCheckout.js", () => ({
  useCreateStripeCheckoutMutation: (options?: CheckoutHookOptions) => {
    mocks.hookOptions(options);
    return {
      mutate: mocks.mutate,
      isPending: mocks.isPending() as boolean,
    };
  },
}));

import {
  isPaygCheckoutLocked,
  resetPaygCheckoutLocks,
} from "./payg-checkout-lock";
import { StartPaygCheckoutCTA } from "./start-payg-checkout-cta";

/** The callbacks React Query drops when the calling mount goes away. */
type MutateCallbacks = {
  onSuccess: (link: string) => void;
  onError: (error: unknown) => void;
};

/** The callbacks React Query keeps on the mutation itself. */
type CheckoutHookOptions = {
  onMutate?: () => unknown;
  onSettled?: (
    data: string | undefined,
    error: unknown,
    variables: unknown,
    context: unknown,
  ) => void;
};

/**
 * The hook options from the most recent render. React Query hands a pending
 * mutation the options of whatever rendered last, so settling deliberately
 * reads the newest ones — only the context is pinned to the dispatch.
 */
function hookOptions(): CheckoutHookOptions | undefined {
  return mocks.hookOptions.mock.calls.at(-1)?.[0] as
    | CheckoutHookOptions
    | undefined;
}

/** The context React Query captures once, when the mutation starts. */
let dispatchedContext: unknown;

/** Leaves the mutation in flight, the way a request in progress would. */
function stayPending() {
  mocks.mutate.mockImplementation(() => {
    dispatchedContext = hookOptions()?.onMutate?.();
  });
}

/** Runs the mutation-level settle callback, which outlives any single mount. */
function settle(context: unknown) {
  hookOptions()?.onSettled?.(undefined, null, {}, context);
}

const DAY = 24 * 60 * 60 * 1000;
const CHECKOUT_URL = "https://checkout.stripe.test/c/pay/session";
const ORGANIZATION_ID = "org-under-test";
const OTHER_ORGANIZATION_ID = "org-switched-to";

/** Points the session at `organizationId` with a trial that is still running. */
function sessionFor(organizationId: string) {
  mocks.session.mockReturnValue({
    trial: activeTrial(),
    activeOrganizationId: organizationId,
  });
}

const activeTrial = () => ({
  startedAt: new Date(Date.now() - 3 * DAY),
  endsAt: new Date(Date.now() + 11 * DAY),
});

/** Resolves the mutation with `link` the way React Query would. */
function resolveWith(link: string) {
  mocks.mutate.mockImplementation(
    (_variables: unknown, callbacks: MutateCallbacks) => {
      const context = hookOptions()?.onMutate?.();
      callbacks.onSuccess(link);
      settle(context);
    },
  );
}

function rejectWith(error: unknown) {
  mocks.mutate.mockImplementation(
    (_variables: unknown, callbacks: MutateCallbacks) => {
      const context = hookOptions()?.onMutate?.();
      callbacks.onError(error);
      settle(context);
    },
  );
}

const cta = () =>
  screen.queryByRole("button", { name: /start pay as you go/i });

const ctas = () =>
  screen.getAllByRole("button", { name: /start pay as you go/i });

const alert = () => screen.queryByRole("alert");

describe("StartPaygCheckoutCTA", () => {
  const assign = vi.fn();

  beforeEach(() => {
    // The hoisted mocks are shared across tests; call counts have to start
    // from zero or the single-flight assertions read stale clicks.
    vi.clearAllMocks();
    // The checkout lock is module state shared by every mount, so it outlives
    // unmount and has to be released between tests.
    resetPaygCheckoutLocks();
    mocks.flagResult.mockReturnValue({ status: "enabled" });
    mocks.hasScope.mockReturnValue(true);
    sessionFor(ORGANIZATION_ID);
    mocks.isPending.mockReturnValue(false);
    dispatchedContext = undefined;
    stayPending();
    assign.mockReset();
    vi.spyOn(window.location, "assign").mockImplementation((url) => {
      assign(url);
    });
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("offers checkout to an admin inside an active trial", () => {
    render(<StartPaygCheckoutCTA />);

    expect(cta()).not.toBeNull();
    expect(mocks.hasScope).toHaveBeenCalledWith("org:admin");
  });

  // Opt-in only: every unresolved flag state has to read as "off", so a
  // misconfigured or unreachable PostHog can never surface a payment flow.
  it.each([
    ["disabled", { status: "disabled" }],
    ["loading", { status: "loading" }],
    ["missing", { status: "missing" }],
    ["error", { status: "error" }],
  ])("renders nothing when the flag is %s", (_name, result) => {
    mocks.flagResult.mockReturnValue(result);

    render(<StartPaygCheckoutCTA />);

    expect(cta()).toBeNull();
  });

  it("renders nothing for a member without org:admin", () => {
    mocks.hasScope.mockReturnValue(false);

    render(<StartPaygCheckoutCTA />);

    expect(cta()).toBeNull();
  });

  it.each([
    ["there is no trial", null],
    [
      "the trial has ended",
      {
        startedAt: new Date(Date.now() - 20 * DAY),
        endsAt: new Date(Date.now() - 6 * DAY),
      },
    ],
    [
      "a trial date is unusable",
      { startedAt: new Date("invalid"), endsAt: new Date(Date.now() + DAY) },
    ],
  ])("renders nothing when %s", (_name, trial) => {
    mocks.session.mockReturnValue({ trial });

    render(<StartPaygCheckoutCTA />);

    expect(cta()).toBeNull();
  });

  // A billing page left open across the end of the trial would otherwise keep
  // offering a checkout the backend now rejects.
  it("stops offering checkout the moment the trial ends", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-18T23:59:59.999Z"));
    mocks.session.mockReturnValue({
      trial: {
        startedAt: new Date("2026-08-05T00:00:00.000Z"),
        endsAt: new Date("2026-08-19T00:00:00.000Z"),
      },
      activeOrganizationId: ORGANIZATION_ID,
    });

    render(<StartPaygCheckoutCTA />);
    expect(cta()).not.toBeNull();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });

    expect(cta()).toBeNull();
  });

  it("only calls the checkout endpoint on click", () => {
    render(<StartPaygCheckoutCTA />);

    expect(mocks.mutate).not.toHaveBeenCalled();
  });

  it("hands off to Stripe in the same tab on success", () => {
    resolveWith(CHECKOUT_URL);
    render(<StartPaygCheckoutCTA />);

    fireEvent.click(cta()!);

    expect(mocks.mutate).toHaveBeenCalledTimes(1);
    expect(assign).toHaveBeenCalledWith(CHECKOUT_URL);
    expect(alert()).toBeNull();
  });

  it("starts a single checkout for a rapid double click", () => {
    // The mutation stays in flight, which is when a second click would
    // otherwise create a second Stripe session.
    stayPending();
    render(<StartPaygCheckoutCTA />);

    const button = cta()!;
    fireEvent.click(button);
    fireEvent.click(button);

    expect(mocks.mutate).toHaveBeenCalledTimes(1);
  });

  // The billing page and the sidebar trial card mount the CTA at the same
  // time, each with its own mutation, so the single-flight guard has to be
  // shared or a click on one surface can open a second Stripe session while
  // the other's request is still in flight.
  it("starts a single checkout across two mounted instances", () => {
    stayPending();
    render(
      <>
        <StartPaygCheckoutCTA />
        <StartPaygCheckoutCTA />
      </>,
    );

    const [first, second] = ctas();
    fireEvent.click(first!);
    fireEvent.click(second!);

    expect(mocks.mutate).toHaveBeenCalledTimes(1);
    // Both surfaces reflect the in-flight checkout, not just the clicked one.
    expect(first!.hasAttribute("disabled")).toBe(true);
    expect(second!.hasAttribute("disabled")).toBe(true);
  });

  it("re-enables both instances once the checkout settles", () => {
    rejectWith(new Error("stripe unavailable"));
    render(
      <>
        <StartPaygCheckoutCTA />
        <StartPaygCheckoutCTA />
      </>,
    );

    const [first, second] = ctas();
    fireEvent.click(first!);

    expect(first!.hasAttribute("disabled")).toBe(false);
    expect(second!.hasAttribute("disabled")).toBe(false);
  });

  // React Query only runs the callbacks passed to `mutate` while the mount that
  // dispatched them still has listeners, so a lock released from there alone
  // would stay held forever once a navigation unmounts the CTA mid-checkout.
  it("releases the lock when the CTA unmounts before the checkout settles", () => {
    // The checkout stays in flight for as long as the CTA is mounted.
    stayPending();
    const { unmount } = render(<StartPaygCheckoutCTA />);

    fireEvent.click(cta()!);
    expect(mocks.mutate).toHaveBeenCalledTimes(1);

    unmount();
    // Only the mutation-level callback survives the unmount.
    settle(dispatchedContext);

    resolveWith(CHECKOUT_URL);
    render(<StartPaygCheckoutCTA />);

    const button = cta()!;
    expect(button.hasAttribute("disabled")).toBe(false);
    fireEvent.click(button);

    expect(mocks.mutate).toHaveBeenCalledTimes(2);
    expect(assign).toHaveBeenCalledWith(CHECKOUT_URL);
  });

  // React Query hands a pending mutation whatever options rendered last, so an
  // organization switch mid-checkout would release the lock on the organization
  // switched *to* and wedge the one that actually started the checkout. The
  // dispatching organization has to come back through the mutation context.
  it("releases the dispatching organization after an organization switch", () => {
    stayPending();
    const { rerender } = render(<StartPaygCheckoutCTA />);

    fireEvent.click(cta()!);
    expect(mocks.mutate).toHaveBeenCalledTimes(1);
    expect(isPaygCheckoutLocked(ORGANIZATION_ID)).toBe(true);

    // The user switches organizations while the checkout is still in flight.
    sessionFor(OTHER_ORGANIZATION_ID);
    rerender(<StartPaygCheckoutCTA />);

    act(() => {
      settle(dispatchedContext);
    });

    expect(isPaygCheckoutLocked(ORGANIZATION_ID)).toBe(false);
    // The organization switched to never started a checkout of its own.
    expect(isPaygCheckoutLocked(OTHER_ORGANIZATION_ID)).toBe(false);

    // Switching back finds the first organization usable, not wedged.
    resolveWith(CHECKOUT_URL);
    sessionFor(ORGANIZATION_ID);
    rerender(<StartPaygCheckoutCTA />);

    const button = cta()!;
    expect(button.hasAttribute("disabled")).toBe(false);
    fireEvent.click(button);

    expect(mocks.mutate).toHaveBeenCalledTimes(2);
    expect(assign).toHaveBeenCalledWith(CHECKOUT_URL);
  });

  it("disables the button while the mutation is pending", () => {
    mocks.isPending.mockReturnValue(true);

    render(<StartPaygCheckoutCTA />);

    expect(cta()!.hasAttribute("disabled")).toBe(true);
  });

  // A link the dashboard would refuse to open is a failure, not a navigation.
  it.each([
    ["an unsafe scheme", "javascript:alert(1)"],
    ["a protocol-relative link", "//evil.test/checkout"],
    ["an empty link", ""],
  ])("stays in-page and reports %s", (_name, link) => {
    resolveWith(link);
    render(<StartPaygCheckoutCTA />);

    fireEvent.click(cta()!);

    expect(assign).not.toHaveBeenCalled();
    expect(alert()?.textContent).toMatch(/couldn't start checkout/i);
    expect(mocks.capture).toHaveBeenCalledWith("payg_checkout_error", {
      error: "unusable checkout link",
    });
  });

  it("reports an RPC failure without leaving the page", () => {
    rejectWith(new Error("stripe unavailable"));
    render(<StartPaygCheckoutCTA />);

    fireEvent.click(cta()!);

    expect(assign).not.toHaveBeenCalled();
    expect(alert()?.textContent).toMatch(/couldn't start checkout/i);
    expect(mocks.capture).toHaveBeenCalledWith("payg_checkout_error", {
      error: "stripe unavailable",
    });
  });

  it("retries after a failure and clears the error", () => {
    rejectWith(new Error("stripe unavailable"));
    render(<StartPaygCheckoutCTA />);

    fireEvent.click(cta()!);
    expect(alert()).not.toBeNull();

    resolveWith(CHECKOUT_URL);
    fireEvent.click(cta()!);

    expect(mocks.mutate).toHaveBeenCalledTimes(2);
    expect(assign).toHaveBeenCalledWith(CHECKOUT_URL);
    expect(alert()).toBeNull();
  });

  // On the lockout gate the organization is walled off rather than trialing,
  // so eligibility turns on that instead of the trial's state.
  describe("gated eligibility", () => {
    beforeEach(() => {
      mocks.session.mockReturnValue({
        trial: null,
        whitelisted: false,
        activeOrganizationId: ORGANIZATION_ID,
      });
    });

    it("offers checkout to an admin of a walled-off organization", () => {
      render(<StartPaygCheckoutCTA eligibility="gated" />);

      expect(cta()).not.toBeNull();
    });

    it("renders nothing for an organization that is not walled off", () => {
      mocks.session.mockReturnValue({
        trial: activeTrial(),
        whitelisted: true,
        activeOrganizationId: ORGANIZATION_ID,
      });

      render(<StartPaygCheckoutCTA eligibility="gated" />);

      expect(cta()).toBeNull();
    });

    it("starts checkout through the same mutation", () => {
      resolveWith(CHECKOUT_URL);
      render(<StartPaygCheckoutCTA eligibility="gated" />);

      fireEvent.click(cta()!);

      expect(mocks.mutate).toHaveBeenCalledTimes(1);
      expect(assign).toHaveBeenCalledWith(CHECKOUT_URL);
    });

    // The in-app surfaces stay trial-only: a walled-off organization has no
    // billing page to reach.
    it("leaves the active-trial surfaces gated on the trial", () => {
      render(<StartPaygCheckoutCTA />);

      expect(cta()).toBeNull();
    });
  });
});
