import { cleanup, render, screen, fireEvent } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { BillingTabs } from "./billing-tabs";

const mocks = vi.hoisted(() => ({ visible: true, subscription: vi.fn() }));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ organization: { slug: "test-org" } }),
}));
vi.mock("./use-payment-method-visibility", () => ({
  usePaymentMethodVisibility: () => mocks.visible,
}));
vi.mock("./use-stripe-subscription", () => ({
  useStripeSubscription: (...args: unknown[]) => mocks.subscription(...args),
}));
vi.mock("@/lib/route-errors", () => ({
  isNotFoundError: (error: unknown) => error === "not-found",
}));
function Location() {
  return <output>{useLocation().pathname}</output>;
}
function show(
  path = "/test-org/billing",
  value: "overview" | "payment-method" = "overview",
) {
  render(
    <MemoryRouter initialEntries={[path]}>
      <BillingTabs value={value}>Billing content</BillingTabs>
      <Location />
    </MemoryRouter>,
  );
}
beforeEach(() => {
  mocks.visible = true;
  mocks.subscription.mockReturnValue({ data: undefined, error: null });
});
afterEach(cleanup);
it("links to setup without a subscription", () => {
  show();
  fireEvent.mouseDown(screen.getByRole("tab", { name: "Billing setup" }), {
    button: 0,
    ctrlKey: false,
  });
  expect(screen.getByRole("status").textContent).toBe(
    "/test-org/billing/setup",
  );
});
it("links directly to payment management with a subscription", () => {
  mocks.subscription.mockReturnValue({
    data: { status: "trialing" },
    error: null,
  });
  show();
  fireEvent.mouseDown(screen.getByRole("tab", { name: "Payment method" }), {
    button: 0,
    ctrlKey: false,
  });
  expect(screen.getByRole("status").textContent).toBe(
    "/test-org/billing/payment-method",
  );
});
it("uses setup when a confirmed absence supersedes cached subscription data", () => {
  mocks.subscription.mockReturnValue({
    data: { status: "active" },
    error: "not-found",
  });
  show();
  expect(screen.getByRole("tab", { name: "Billing setup" })).toBeTruthy();
});
it("labels the management route consistently before its query loads", () => {
  show("/test-org/billing/payment-method", "payment-method");
  expect(
    screen
      .getByRole("tab", { name: "Payment method" })
      .getAttribute("aria-selected"),
  ).toBe("true");
});
it("keeps payment navigation hidden for excluded organizations", () => {
  mocks.visible = false;
  show();
  expect(screen.queryByRole("tablist")).toBeNull();
  expect(mocks.subscription).toHaveBeenLastCalledWith({ enabled: false });
});
