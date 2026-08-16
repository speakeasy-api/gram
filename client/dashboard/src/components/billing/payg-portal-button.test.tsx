import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  mutate: vi.fn(),
  isPending: vi.fn(),
  capture: vi.fn(),
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: mocks.capture }),
}));

vi.mock("@gram/client/react-query/createStripePortalSession.js", () => ({
  useCreateStripePortalSessionMutation: () => ({
    mutate: mocks.mutate,
    isPending: mocks.isPending() as boolean,
  }),
}));

import { PaygPortalButton } from "./payg-portal-button";

const PORTAL_URL = "https://billing.stripe.test/p/session/live_abc123";

type MutateCallbacks = {
  onSuccess: (link: string) => void;
  onError: (error: unknown) => void;
};

function resolveWith(link: string) {
  mocks.mutate.mockImplementation(
    (_variables: unknown, callbacks: MutateCallbacks) => {
      callbacks.onSuccess(link);
    },
  );
}

function rejectWith(error: unknown) {
  mocks.mutate.mockImplementation(
    (_variables: unknown, callbacks: MutateCallbacks) => {
      callbacks.onError(error);
    },
  );
}

const button = () =>
  screen.queryByRole("button", {
    name: /manage payment method and invoices/i,
  });

const alert = () => screen.queryByRole("alert");

describe("PaygPortalButton", () => {
  const assign = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
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

  // The portal link is single-use and short-lived, so one minted at render
  // would already be spent by the time anyone pressed the button.
  it("only mints a portal session on click", () => {
    render(<PaygPortalButton />);

    expect(button()).not.toBeNull();
    expect(mocks.mutate).not.toHaveBeenCalled();
  });

  it("hands off to the portal in the same tab", () => {
    resolveWith(PORTAL_URL);
    render(<PaygPortalButton />);

    fireEvent.click(button()!);

    expect(mocks.mutate).toHaveBeenCalledTimes(1);
    expect(assign).toHaveBeenCalledWith(PORTAL_URL);
    expect(alert()).toBeNull();
  });

  // A link the dashboard would refuse to navigate to is a failure, not a
  // navigation: the URL crosses a trust boundary on its way here.
  it.each([
    ["an unsafe scheme", "javascript:alert(1)"],
    ["a protocol-relative link", "//evil.test/portal"],
    ["a relative link", "/billing/portal"],
    ["an empty link", ""],
  ])("stays in-page and reports %s", (_name, link) => {
    resolveWith(link);
    render(<PaygPortalButton />);

    fireEvent.click(button()!);

    expect(assign).not.toHaveBeenCalled();
    expect(alert()?.textContent).toMatch(/couldn't open the billing portal/i);
    expect(mocks.capture).toHaveBeenCalledWith("stripe_portal_error", {
      error: "unusable portal link",
    });
  });

  it("reports an RPC failure without leaving the page", () => {
    rejectWith(new Error("stripe unavailable"));
    render(<PaygPortalButton />);

    fireEvent.click(button()!);

    expect(assign).not.toHaveBeenCalled();
    expect(alert()?.textContent).toMatch(/couldn't open the billing portal/i);
    expect(mocks.capture).toHaveBeenCalledWith("stripe_portal_error", {
      error: "stripe unavailable",
    });
  });

  it("retries after a failure and clears the error", () => {
    rejectWith(new Error("stripe unavailable"));
    render(<PaygPortalButton />);

    fireEvent.click(button()!);
    expect(alert()).not.toBeNull();

    resolveWith(PORTAL_URL);
    fireEvent.click(button()!);

    expect(mocks.mutate).toHaveBeenCalledTimes(2);
    expect(assign).toHaveBeenCalledWith(PORTAL_URL);
    expect(alert()).toBeNull();
  });

  it("disables the button while the session is being minted", () => {
    mocks.isPending.mockReturnValue(true);

    render(<PaygPortalButton />);

    expect(button()).toBeNull();
    const pending = screen.getByRole("button", { name: /opening/i });
    expect(pending.hasAttribute("disabled")).toBe(true);
  });
});
