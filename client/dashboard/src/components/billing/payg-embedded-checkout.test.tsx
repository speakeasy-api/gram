import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { useState, type ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { PaygEmbeddedCheckout } from "./payg-embedded-checkout";
import { PaygCheckoutSkeleton } from "./payg-checkout-skeleton";
import { PaygCheckoutMount } from "./payg-checkout-mount";

const mocks = vi.hoisted(() => ({
  confirm: vi.fn(),
  formOptions: vi.fn(),
  complete: vi.fn<() => void>(),
  event: {},
}));
vi.mock("@/components/ui/hooks/useConfig", () => ({
  useConfig: () => ({ theme: "light" }),
}));
vi.mock("@stripe/stripe-js", () => ({
  loadStripe: () => Promise.resolve(null),
}));
vi.mock("./payg-checkout-summary", () => ({
  PaygCheckoutHeader: () => <h4>Pricing</h4>,
  PaygCheckoutSummary: () => (
    <section aria-label="Order summary">Price items</section>
  ),
}));
vi.mock("@stripe/react-stripe-js/checkout", () => ({
  CheckoutFormProvider: ({ children }: { children: ReactNode }) => {
    const [details, setDetails] = useState("");
    return (
      <>
        <input
          aria-label="Stripe payment details"
          value={details}
          onChange={(event) => setDetails(event.target.value)}
        />
        {children}
      </>
    );
  },
  useCheckoutForm: () => ({
    type: "success",
    checkout: { confirm: mocks.confirm },
  }),
  CheckoutForm: ({
    onConfirm,
    options,
  }: {
    onConfirm: (event: object) => void;
    options: unknown;
  }) => {
    mocks.formOptions(options);
    return <button onClick={() => onConfirm(mocks.event)}>Confirm</button>;
  },
}));
function show() {
  render(
    <PaygEmbeddedCheckout
      clientSecret="test-secret"
      publishableKey="test-key"
      onComplete={mocks.complete}
    />,
  );
}
beforeEach(() => vi.resetAllMocks());
afterEach(cleanup);
it("passes the form event to Stripe and starts backend verification on success", async () => {
  mocks.confirm.mockResolvedValue({ type: "success" });
  show();
  fireEvent.click(screen.getByText("Confirm"));
  await waitFor(() => expect(mocks.complete).toHaveBeenCalledOnce());
  expect(mocks.confirm).toHaveBeenCalledWith({
    formConfirmEvent: mocks.event,
    redirect: "if_required",
  });
});
it("shows Stripe errors without completing and allows retry", async () => {
  mocks.confirm.mockResolvedValue({
    type: "error",
    error: { message: "Card declined" },
  });
  show();
  fireEvent.click(screen.getByText("Confirm"));
  expect((await screen.findByRole("alert")).textContent).toBe("Card declined");
  expect(mocks.complete).not.toHaveBeenCalled();
  mocks.confirm.mockResolvedValue({ type: "success" });
  fireEvent.click(screen.getByText("Confirm"));
  await waitFor(() => expect(mocks.complete).toHaveBeenCalledOnce());
});
it("catches initialization exceptions without reporting completion", async () => {
  mocks.confirm.mockRejectedValue(new Error("internal failure"));
  show();
  fireEvent.click(screen.getByText("Confirm"));
  expect((await screen.findByRole("alert")).textContent).toContain(
    "Couldn't confirm",
  );
  expect(mocks.complete).not.toHaveBeenCalled();
});
it("ignores duplicate confirmation events while a request is pending", () => {
  mocks.confirm.mockReturnValue(new Promise(() => {}));
  show();
  fireEvent.click(screen.getByText("Confirm"));
  fireEvent.click(screen.getByText("Confirm"));
  expect(mocks.confirm).toHaveBeenCalledOnce();
});

it("keeps manual promotion entry enabled in the embedded form", () => {
  show();
  expect(mocks.formOptions).toHaveBeenCalledWith(
    expect.objectContaining({
      features: expect.objectContaining({ promotionCodeCollection: "auto" }),
    }),
  );
});

it("preserves Stripe provider state for the same session and resets it for a replacement", async () => {
  const props = {
    session: {
      clientSecret: "cs_test_first",
      publishableKey: "pk_test_example",
    },
    onComplete: mocks.complete,
  };
  const view = render(<PaygCheckoutMount {...props} />);
  const details = await screen.findByRole("textbox", {
    name: "Stripe payment details",
  });
  fireEvent.change(details, { target: { value: "entered details" } });
  view.rerender(<PaygCheckoutMount {...props} />);
  expect(screen.getByDisplayValue("entered details")).toBeTruthy();
  view.rerender(
    <PaygCheckoutMount
      {...props}
      session={{ ...props.session, clientSecret: "cs_test_replacement" }}
    />,
  );
  expect(screen.queryByDisplayValue("entered details")).toBeNull();
  expect(
    screen.getByRole("textbox", { name: "Stripe payment details" }),
  ).toBeTruthy();
});

it("shows pricing, order summary, and payment details", () => {
  show();
  expect(screen.getByRole("heading", { name: "Pricing" })).toBeTruthy();
  expect(screen.getByRole("heading", { name: "Payment details" })).toBeTruthy();
  expect(screen.getByRole("region", { name: "Order summary" })).toBeTruthy();
  expect(screen.getByRole("button", { name: "Confirm" })).toBeTruthy();
});

it("announces checkout loading", () => {
  render(<PaygCheckoutSkeleton />);
  expect(screen.getByRole("status", { name: "Loading checkout" })).toBeTruthy();
});
