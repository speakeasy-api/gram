import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ConfirmTrafficStep } from "./confirm-traffic-step";

const mocks = vi.hoisted(() => ({
  query: {
    data: undefined as
      | undefined
      | { events: Array<Record<string, unknown>>; latestUnixNano: string },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  },
}));

vi.mock("@gram/client/react-query/verifyOnboardingHooksSetup.js", () => ({
  useVerifyOnboardingHooksSetup: () => mocks.query,
}));

vi.mock("motion/react", () => ({
  AnimatePresence: ({ children }: { children: React.ReactNode }) => children,
  motion: { div: "div" },
}));

afterEach(cleanup);
beforeEach(() => {
  mocks.query.data = undefined;
  mocks.query.isLoading = false;
  mocks.query.isError = false;
  mocks.query.refetch.mockReset();
});

describe("ConfirmTrafficStep", () => {
  it("requires a received event before continuing", () => {
    const onComplete = vi.fn();
    const view = render(
      <ConfirmTrafficStep
        onComplete={() => void onComplete()}
        onBack={() => {}}
      />,
    );

    expect(
      screen.getByRole("button", { name: "Continue" }).hasAttribute("disabled"),
    ).toBe(true);

    mocks.query.data = {
      events: [
        {
          source: "cursor",
          timeUnixNano: String(BigInt(Date.now()) * 1_000_000n),
          eventName: "tool call",
        },
      ],
      latestUnixNano: String(BigInt(Date.now()) * 1_000_000n),
    };
    view.rerender(
      <ConfirmTrafficStep
        onComplete={() => void onComplete()}
        onBack={() => {}}
      />,
    );

    expect(
      screen.getByRole("button", { name: "Continue" }).hasAttribute("disabled"),
    ).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(onComplete).toHaveBeenCalledOnce();
  });

  it("shows polling failures and retries them", () => {
    mocks.query.isError = true;
    render(<ConfirmTrafficStep onComplete={() => {}} onBack={() => {}} />);

    expect(screen.getByRole("alert").textContent).toContain(
      "We couldn't check for traffic. Try again.",
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mocks.query.refetch).toHaveBeenCalledOnce();
  });
});
