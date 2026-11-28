import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { TrialEmbeddedCheckout } from "./trial-embedded-checkout";

const mocks = vi.hoisted(() => ({
  loadStripe: vi.fn<(key: string) => Promise<object | null>>(),
  providerOptions: vi.fn(),
}));

vi.mock("@stripe/stripe-js", () => ({
  loadStripe: (key: string) => mocks.loadStripe(key),
}));

vi.mock("@stripe/react-stripe-js", () => ({
  EmbeddedCheckoutProvider: ({
    children,
    options,
  }: {
    children: ReactNode;
    options: { clientSecret: string; onComplete: () => void };
  }) => {
    mocks.providerOptions(options);
    return (
      <div>
        {children}
        <button onClick={() => options.onComplete()}>Finish</button>
      </div>
    );
  },
  EmbeddedCheckout: () => <div>Stripe card form</div>,
}));

let keyCounter = 0;
function uniqueKey(): string {
  keyCounter += 1;
  return `pk_test_trial_${keyCounter}`;
}

beforeEach(() => {
  mocks.loadStripe.mockReset();
  mocks.providerOptions.mockReset();
});

afterEach(cleanup);

it("mounts the embedded Checkout and reports completion", async () => {
  mocks.loadStripe.mockResolvedValue({});
  const onComplete = vi.fn<() => void>();
  render(
    <TrialEmbeddedCheckout
      clientSecret="cs_secret"
      publishableKey={uniqueKey()}
      onComplete={onComplete}
    />,
  );

  expect(await screen.findByText("Stripe card form")).toBeTruthy();
  expect(mocks.providerOptions).toHaveBeenCalledWith(
    expect.objectContaining({ clientSecret: "cs_secret" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Finish" }));
  expect(onComplete).toHaveBeenCalledTimes(1);
});

it("shows a retry when Stripe.js fails to load and loads it again", async () => {
  const key = uniqueKey();
  mocks.loadStripe
    .mockRejectedValueOnce(new Error("blocked"))
    .mockResolvedValue({});
  render(
    <TrialEmbeddedCheckout
      clientSecret="cs_secret"
      publishableKey={key}
      onComplete={vi.fn<() => void>()}
    />,
  );

  expect(await screen.findByRole("alert")).toBeTruthy();
  expect(screen.getByText(/couldn't load/)).toBeTruthy();

  fireEvent.click(screen.getByRole("button", { name: "Try again" }));

  await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
  expect(screen.getByText("Stripe card form")).toBeTruthy();
  expect(mocks.loadStripe).toHaveBeenCalledTimes(2);
});
