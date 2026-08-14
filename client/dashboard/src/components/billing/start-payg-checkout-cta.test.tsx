import { cleanup, fireEvent, render, screen } from "@testing-library/react";
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
  useCreateStripeCheckoutMutation: () => ({
    mutate: mocks.mutate,
    isPending: mocks.isPending() as boolean,
  }),
}));

import { StartPaygCheckoutCTA } from "./start-payg-checkout-cta";

type MutateCallbacks = {
  onSuccess: (link: string) => void;
  onError: (error: unknown) => void;
  onSettled: () => void;
};

const DAY = 24 * 60 * 60 * 1000;
const CHECKOUT_URL = "https://checkout.stripe.test/c/pay/session";

const activeTrial = () => ({
  startedAt: new Date(Date.now() - 3 * DAY),
  endsAt: new Date(Date.now() + 11 * DAY),
});

/** Resolves the mutation with `link` the way React Query would. */
function resolveWith(link: string) {
  mocks.mutate.mockImplementation(
    (_variables: unknown, callbacks: MutateCallbacks) => {
      callbacks.onSuccess(link);
      callbacks.onSettled();
    },
  );
}

function rejectWith(error: unknown) {
  mocks.mutate.mockImplementation(
    (_variables: unknown, callbacks: MutateCallbacks) => {
      callbacks.onError(error);
      callbacks.onSettled();
    },
  );
}

const cta = () =>
  screen.queryByRole("button", { name: /start pay as you go/i });

const alert = () => screen.queryByRole("alert");

describe("StartPaygCheckoutCTA", () => {
  const assign = vi.fn();

  beforeEach(() => {
    // The hoisted mocks are shared across tests; call counts have to start
    // from zero or the single-flight assertions read stale clicks.
    vi.clearAllMocks();
    mocks.flagResult.mockReturnValue({ status: "enabled" });
    mocks.hasScope.mockReturnValue(true);
    mocks.session.mockReturnValue({ trial: activeTrial() });
    mocks.isPending.mockReturnValue(false);
    mocks.mutate.mockImplementation(() => {});
    assign.mockReset();
    vi.spyOn(window.location, "assign").mockImplementation((url) => {
      assign(url);
    });
  });

  afterEach(() => {
    cleanup();
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
    mocks.mutate.mockImplementation(() => {});
    render(<StartPaygCheckoutCTA />);

    const button = cta()!;
    fireEvent.click(button);
    fireEvent.click(button);

    expect(mocks.mutate).toHaveBeenCalledTimes(1);
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
});
