import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  mutation: vi.fn(),
  mutate: vi.fn(),
  reset: vi.fn(),
  invalidate: vi.fn(),
}));

vi.mock("@gram/client/react-query/cancelStripeSubscription.js", () => ({
  useCancelStripeSubscriptionMutation: (options?: { onSuccess?: () => void }) =>
    mocks.mutation(options),
}));

vi.mock("@gram/client/react-query/getStripeSubscription.js", () => ({
  invalidateAllGetStripeSubscription: mocks.invalidate,
}));

import { CancelPaygDialog } from "./cancel-payg-dialog";

// Midday UTC so the formatted day can't slide either side of the date line in
// whichever time zone the tests happen to run in.
const PERIOD_END = new Date("2026-09-01T12:00:00.000Z");

type MutationState = {
  isPending?: boolean;
  isError?: boolean;
};

function mutationState({
  isPending = false,
  isError = false,
}: MutationState = {}) {
  mocks.mutation.mockImplementation(() => ({
    mutate: mocks.mutate,
    reset: mocks.reset,
    isPending,
    isError,
  }));
}

/** Resolves the cancellation the way React Query would. */
function resolves() {
  mocks.mutate.mockImplementation(
    (_variables: unknown, callbacks?: { onSuccess?: () => void }) => {
      callbacks?.onSuccess?.();
    },
  );
}

/** The dialog reaches for the query client to invalidate after a cancellation. */
function renderDialog(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>{node}</QueryClientProvider>,
  );
}

const trigger = () =>
  screen.getByRole("button", { name: /^cancel pay as you go$/i });

const confirm = () =>
  screen.queryByRole("button", { name: /^cancel subscription$/i });

const keep = () => screen.getByRole("button", { name: /keep pay as you go/i });

const dialog = () => screen.queryByRole("dialog");

function openDialog() {
  fireEvent.click(trigger());
}

describe("CancelPaygDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mutationState();
    mocks.mutate.mockImplementation(() => {});
  });

  afterEach(cleanup);

  // Scheduling the cancellation is what takes the organization's access away,
  // so it is never one click away.
  it("cancels nothing until the confirmation is given", () => {
    renderDialog(<CancelPaygDialog endsOn={PERIOD_END} />);

    expect(dialog()).toBeNull();
    expect(mocks.mutate).not.toHaveBeenCalled();

    openDialog();

    expect(dialog()).not.toBeNull();
    expect(mocks.mutate).not.toHaveBeenCalled();
  });

  // The three things the organization is agreeing to. A confirmation that
  // leaves any of them out is the defect this covers.
  it("spells out what canceling does", () => {
    renderDialog(<CancelPaygDialog endsOn={PERIOD_END} />);
    openDialog();

    const description = dialog()!.textContent ?? "";
    expect(description).toMatch(
      /service continues through the current billing period/i,
    );
    expect(description).toMatch(/September 1, 2026/);
    expect(description).toMatch(/final invoice/i);
    expect(description).toMatch(/access to this organization is revoked/i);
  });

  // Canceling a trial stops it from ever converting, so no pay-as-you-go
  // period is ever billed. The paid copy's promise of a final invoice would
  // have the customer waiting on a charge that never arrives.
  describe("while Stripe is still trialing", () => {
    it("spells out that nothing will be billed", () => {
      renderDialog(<CancelPaygDialog endsOn={PERIOD_END} trialing />);
      openDialog();

      const description = dialog()!.textContent ?? "";
      expect(description).toMatch(/your trial continues through/i);
      expect(description).toMatch(/September 1, 2026/);
      expect(description).toMatch(/pay as you go never starts/i);
      expect(description).toMatch(/won't be invoiced/i);
      expect(description).toMatch(/access to this organization is revoked/i);
    });

    it("promises no final invoice", () => {
      renderDialog(<CancelPaygDialog endsOn={PERIOD_END} trialing />);
      openDialog();

      expect(dialog()!.textContent ?? "").not.toMatch(/final invoice/i);
    });

    it("keeps the copy readable without a trial end", () => {
      renderDialog(<CancelPaygDialog endsOn={null} trialing />);
      openDialog();

      const description = dialog()!.textContent ?? "";
      expect(description).toMatch(
        /trial continues through the end of the trial period/i,
      );
      expect(description).not.toMatch(/invalid date/i);
      expect(description).not.toMatch(/final invoice/i);
    });

    it("cancels through the same mutation", () => {
      resolves();
      renderDialog(<CancelPaygDialog endsOn={PERIOD_END} trialing />);
      openDialog();

      fireEvent.click(confirm()!);

      expect(mocks.mutate).toHaveBeenCalledTimes(1);
      expect(dialog()).toBeNull();
    });
  });

  // Stripe doesn't always hand back a usable date, and "Invalid Date" beside a
  // cancellation is worse than a sentence that names no date at all.
  it("keeps the copy readable without a period end", () => {
    renderDialog(<CancelPaygDialog endsOn={null} />);
    openDialog();

    const description = dialog()!.textContent ?? "";
    expect(description).toMatch(
      /service continues through the current billing period\./i,
    );
    expect(description).not.toMatch(/invalid date/i);
    expect(description).toMatch(/final invoice/i);
  });

  it("schedules the cancellation once confirmed", () => {
    resolves();
    renderDialog(<CancelPaygDialog endsOn={PERIOD_END} />);
    openDialog();

    fireEvent.click(confirm()!);

    expect(mocks.mutate).toHaveBeenCalledTimes(1);
  });

  it("closes itself once the cancellation lands", () => {
    resolves();
    renderDialog(<CancelPaygDialog endsOn={PERIOD_END} />);
    openDialog();

    fireEvent.click(confirm()!);

    expect(dialog()).toBeNull();
  });

  // Every billing surface reads the subscription off one key, so the whole key
  // is refreshed — the plan state, and anything gating on it, has to see the
  // scheduled cancellation.
  it("refreshes the cached subscription after canceling", () => {
    renderDialog(<CancelPaygDialog endsOn={PERIOD_END} />);

    const options = mocks.mutation.mock.calls.at(-1)?.[0] as {
      onSuccess: () => void;
    };
    options.onSuccess();

    expect(mocks.invalidate).toHaveBeenCalledTimes(1);
  });

  it("keeps the dialog open and reports a failed cancellation", () => {
    mutationState({ isError: true });
    renderDialog(<CancelPaygDialog endsOn={PERIOD_END} />);
    openDialog();

    fireEvent.click(confirm()!);

    expect(dialog()).not.toBeNull();
    expect(screen.getByRole("alert").textContent).toMatch(
      /couldn't cancel the subscription/i,
    );
  });

  it("disables both answers while the cancellation is in flight", () => {
    mutationState({ isPending: true });
    renderDialog(<CancelPaygDialog endsOn={PERIOD_END} />);
    openDialog();

    expect(confirm()).toBeNull();
    const pending = screen.getByRole("button", { name: /canceling/i });
    expect(pending.hasAttribute("disabled")).toBe(true);
    expect(keep().hasAttribute("disabled")).toBe(true);
  });

  it("backs out without canceling anything", () => {
    renderDialog(<CancelPaygDialog endsOn={PERIOD_END} />);
    openDialog();

    fireEvent.click(keep());

    expect(dialog()).toBeNull();
    expect(mocks.mutate).not.toHaveBeenCalled();
  });

  // A failure left over from the last attempt would otherwise greet the admin
  // the next time they open the dialog.
  it("clears a stale failure when the dialog is dismissed", () => {
    mutationState({ isError: true });
    renderDialog(<CancelPaygDialog endsOn={PERIOD_END} />);
    openDialog();

    fireEvent.click(keep());

    expect(mocks.reset).toHaveBeenCalled();
  });
});
