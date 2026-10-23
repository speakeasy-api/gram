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

it("uses separate column dividers in the header row above aligned summary and payment columns", () => {
  show();
  const pricing = screen.getByRole("heading", { name: "Pricing", level: 4 });
  const payment = screen.getByRole("heading", {
    name: "Payment details",
    level: 4,
  });
  const pricingCell = pricing.parentElement!;
  const paymentCell = payment.parentElement!;
  const headerRow = pricingCell.parentElement!;
  const summaryCell = screen.getByRole("region", {
    name: "Order summary",
  }).parentElement!;
  const formCell = screen.getByRole("button", {
    name: "Confirm",
  }).parentElement!;
  const bodyRow = summaryCell.parentElement!;
  expect(Array.from(headerRow.children)).toEqual([pricingCell, paymentCell]);
  expect(Array.from(bodyRow.children)).toEqual([summaryCell, formCell]);
  expect(Array.from(headerRow.parentElement!.children)).toEqual([
    headerRow,
    bodyRow,
  ]);
  expect(headerRow.classList.contains("lg:border-b")).toBe(false);
  for (const cell of [pricingCell, paymentCell]) {
    expect(cell.classList.contains("border-b")).toBe(true);
    expect(cell.classList.contains("border-border")).toBe(true);
  }
  for (const row of [headerRow, bodyRow]) {
    expect(row.classList.contains("contents")).toBe(true);
    expect(row.classList.contains("lg:grid")).toBe(true);
    expect(row.classList.contains("lg:grid-cols-2")).toBe(true);
    expect(row.classList.contains("lg:gap-10")).toBe(true);
    expect(row.classList.contains("lg:items-start")).toBe(true);
  }
  for (const cell of [paymentCell, formCell]) {
    expect(cell.classList.contains("max-w-lg")).toBe(true);
    expect(cell.classList.contains("lg:justify-self-end")).toBe(true);
  }
  // On mobile, display: contents lets summary precede the payment heading/form.
  for (const [index, cell] of [
    pricingCell,
    summaryCell,
    paymentCell,
    formCell,
  ].entries()) {
    expect(cell.classList.contains(`order-${index + 1}`)).toBe(true);
  }
  expect(payment.classList.contains("text-display-sm")).toBe(false);
  expect(headerRow.querySelector('[class*="lg:pb-7"]')).toBeNull();
  expect(
    screen.getAllByRole("heading", { name: "Payment details" }),
  ).toHaveLength(1);
});

it("reserves broad summary and payment shapes beneath aligned headers while checkout loads", () => {
  render(<PaygCheckoutSkeleton />);
  const grid = screen
    .getByRole("status", { name: "Loading checkout" })
    .querySelector('[aria-hidden="true"]')!;
  const [headerRow, bodyRow] = Array.from(grid.children);
  expect(grid.children).toHaveLength(2);
  expect(headerRow!.classList.contains("lg:border-b")).toBe(false);
  for (const cell of Array.from(headerRow!.children)) {
    expect(cell.classList.contains("border-b")).toBe(true);
  }
  for (const row of [headerRow!, bodyRow!]) {
    expect(row.children).toHaveLength(2);
    expect(row.classList.contains("contents")).toBe(true);
    expect(row.classList.contains("lg:grid-cols-2")).toBe(true);
    expect(row.classList.contains("lg:gap-10")).toBe(true);
    expect(row.lastElementChild!.classList.contains("max-w-lg")).toBe(true);
    expect(
      row.lastElementChild!.classList.contains("lg:justify-self-end"),
    ).toBe(true);
  }
  const [summary, payment] = Array.from(bodyRow!.children);
  // Reserve whole sections rather than reproducing product or billing text lines.
  for (const height of ["h-12", "h-28", "h-8"]) {
    expect(summary!.querySelector(`.${height}.w-full`)).not.toBeNull();
  }
  for (const height of ["h-11", "h-36", "h-16"]) {
    expect(payment!.querySelector(`.${height}.w-full`)).not.toBeNull();
  }
  expect(headerRow!.classList.contains("pb-6")).toBe(true);
  for (const cell of Array.from(headerRow!.children)) {
    expect(cell.classList.contains("pb-2")).toBe(true);
  }
  expect(headerRow!.querySelectorAll(".h-7")).toHaveLength(3);
  expect(grid.querySelector('[class*="lg:mb-15"]')).toBeNull();
});
