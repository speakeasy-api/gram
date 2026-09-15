import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  mutation: vi.fn(),
  mutate: vi.fn(),
  invalidate: vi.fn(),
}));

vi.mock("@gram/client/react-query/resumeStripeSubscription.js", () => ({
  useResumeStripeSubscriptionMutation: (options?: { onSettled?: () => void }) =>
    mocks.mutation(options),
}));

vi.mock("@gram/client/react-query/getStripeSubscription.js", () => ({
  invalidateAllGetStripeSubscription: mocks.invalidate,
}));

import { ResumePaygButton } from "./resume-payg-button";

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
    isPending,
    isError,
  }));
}

/** Runs the mutation-level `onSettled` React Query fires on either outcome. */
function settle() {
  const options = mocks.mutation.mock.calls.at(-1)?.[0] as {
    onSettled?: () => void;
  };
  options?.onSettled?.();
}

/** The button reaches for the query client to invalidate after resuming. */
function renderButton(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>{node}</QueryClientProvider>,
  );
}

const button = () =>
  screen.queryByRole("button", { name: /^resume pay as you go$/i });

describe("ResumePaygButton", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mutationState();
    mocks.mutate.mockImplementation(() => {
      settle();
    });
  });

  afterEach(cleanup);

  // Resuming restores the plan the organization already had, so it goes
  // through on one click — the destructive direction is the one that asks.
  it("clears the scheduled cancellation without a confirmation", () => {
    renderButton(<ResumePaygButton />);

    fireEvent.click(button()!);

    expect(mocks.mutate).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("refreshes the cached subscription after resuming", () => {
    renderButton(<ResumePaygButton />);

    fireEvent.click(button()!);

    expect(mocks.invalidate).toHaveBeenCalledTimes(1);
  });

  // Stripe can clear the scheduled cancellation and the call still fail
  // afterwards, so a failure is no proof the subscription is untouched.
  // Leaving the stale copy cached would show a plan state Stripe has already
  // moved on from.
  it("refreshes the cached subscription after a failed resume", () => {
    mutationState({ isError: true });
    renderButton(<ResumePaygButton />);

    fireEvent.click(button()!);

    expect(mocks.invalidate).toHaveBeenCalledTimes(1);
  });

  it("reports a failed resume and leaves the button usable", () => {
    mutationState({ isError: true });
    renderButton(<ResumePaygButton />);

    expect(screen.getByRole("alert").textContent).toMatch(
      /couldn't resume the subscription/i,
    );

    fireEvent.click(button()!);

    expect(mocks.mutate).toHaveBeenCalledTimes(1);
  });

  it("disables the button while the resume is in flight", () => {
    mutationState({ isPending: true });
    renderButton(<ResumePaygButton />);

    expect(button()).toBeNull();
    const pending = screen.getByRole("button", { name: /resuming/i });
    expect(pending.hasAttribute("disabled")).toBe(true);
  });
});
