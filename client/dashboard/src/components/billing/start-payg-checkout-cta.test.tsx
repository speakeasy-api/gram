import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { StartPaygCheckoutCTA } from "./start-payg-checkout-cta";
const mocks = vi.hoisted(() => ({ eligible: true, slug: "test-org" }));
vi.mock("./payg-checkout-access", () => ({
  usePaygCheckoutAccess: () => ({ eligible: mocks.eligible }),
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ organization: { slug: mocks.slug } }),
}));
function Location() {
  return <p>{useLocation().pathname}</p>;
}
afterEach(cleanup);
beforeEach(() => {
  mocks.eligible = true;
  mocks.slug = "test-org";
});
it("navigates to the organization's setup page without opening a modal", () => {
  render(
    <MemoryRouter>
      <StartPaygCheckoutCTA label="Add payment method" />
      <Location />
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Add payment method" }));
  expect(screen.getByText("/test-org/billing/setup")).toBeTruthy();
  expect(screen.queryByRole("dialog")).toBeNull();
});
it("preserves eligibility restrictions", () => {
  mocks.eligible = false;
  render(
    <MemoryRouter>
      <StartPaygCheckoutCTA />
    </MemoryRouter>,
  );
  expect(screen.queryByRole("button")).toBeNull();
});
it("uses the newly selected organization", () => {
  const view = render(
    <MemoryRouter>
      <StartPaygCheckoutCTA />
      <Location />
    </MemoryRouter>,
  );
  mocks.slug = "another-org";
  view.rerender(
    <MemoryRouter>
      <StartPaygCheckoutCTA />
      <Location />
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("button"));
  expect(screen.getByText("/another-org/billing/setup")).toBeTruthy();
});
