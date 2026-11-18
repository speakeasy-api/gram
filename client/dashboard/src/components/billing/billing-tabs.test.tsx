import { cleanup, render, screen, fireEvent } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { BillingTabs } from "./billing-tabs";

const mocks = vi.hoisted(() => ({ status: "needs-setup" }));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ organization: { slug: "test-org" } }),
}));
vi.mock("./use-billing-state", () => ({
  useBillingState: () => ({ status: mocks.status }),
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
  mocks.status = "needs-setup";
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
  mocks.status = "manage";
  show();
  fireEvent.mouseDown(screen.getByRole("tab", { name: "Payment method" }), {
    button: 0,
    ctrlKey: false,
  });
  expect(screen.getByRole("status").textContent).toBe(
    "/test-org/billing/payment-method",
  );
});
it("selects the payment tab on the management route", () => {
  mocks.status = "manage";
  show("/test-org/billing/payment-method", "payment-method");
  expect(
    screen
      .getByRole("tab", { name: "Payment method" })
      .getAttribute("aria-selected"),
  ).toBe("true");
});
it.each(["unavailable", "loading", "error"])(
  "hides navigation when billing is %s",
  (status) => {
    mocks.status = status;
    show("/test-org/billing/payment-method", "payment-method");
    expect(screen.queryByRole("tablist")).toBeNull();
    expect(screen.getByText("Billing content")).toBeTruthy();
  },
);
