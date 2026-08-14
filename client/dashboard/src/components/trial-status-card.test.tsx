import {
  act,
  cleanup,
  render as rtlRender,
  screen,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { emptySession } from "@/contexts/Auth";
import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";

const mocks = vi.hoisted(() => ({
  useSession: vi.fn(),
  flagResult: vi.fn(),
  hasScope: vi.fn(),
}));

vi.mock("@/contexts/Auth", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/contexts/Auth")>();
  return {
    ...actual,
    useSession: mocks.useSession,
  };
});

// The embedded self-serve CTA brings its own gates; stub what it reads so the
// card's own states stay the subject of these tests.
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => mocks.flagResult() as FeatureFlagResult,
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => mocks.hasScope() as boolean }),
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: vi.fn() }),
}));

vi.mock("@gram/client/react-query/createStripeCheckout.js", () => ({
  useCreateStripeCheckoutMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));

import { TrialStatusCard } from "./trial-status-card";

// The card links to the in-app upgrade gate, so Link needs router context.
const render = (ui: React.ReactElement) =>
  rtlRender(ui, { wrapper: MemoryRouter });

const activeTrial = {
  startedAt: new Date("2026-08-05T00:00:00.000Z"),
  endsAt: new Date("2026-08-19T00:00:00.000Z"),
};

describe("TrialStatusCard", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-05T00:00:00.000Z"));
    mocks.useSession.mockReturnValue({ trial: activeTrial });
    // Off by default so the existing states are asserted without the CTA.
    mocks.flagResult.mockReturnValue({ status: "disabled" });
    mocks.hasScope.mockReturnValue(true);
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it("defaults the empty session trial to null", () => {
    expect(emptySession.trial).toBeNull();
  });

  it("renders the active trial status", () => {
    render(<TrialStatusCard />);

    expect(screen.getByText("Trial")).toBeTruthy();
    expect(screen.getByText("Day 1/14")).toBeTruthy();
    expect(screen.getByText("14 days left")).toBeTruthy();
    const progressBar = screen.getByRole("progressbar", {
      name: "Day 1 of 14",
    });
    expect(progressBar.getAttribute("aria-valuenow")).toBe("0");
    expect(progressBar.firstElementChild?.getAttribute("style")).toBe(
      "width: 0%;",
    );
  });

  it("shows one day left on the final trial day", () => {
    vi.setSystemTime(new Date("2026-08-18T00:00:00.000Z"));

    render(<TrialStatusCard />);

    expect(screen.getByText("1 day left")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Talk to sales about upgrading" }),
    ).toBeTruthy();
  });

  it("renders nothing without a trial", () => {
    mocks.useSession.mockReturnValue({ trial: null });

    const { container } = render(<TrialStatusCard />);

    expect(container.firstChild).toBeNull();
  });

  it("renders the ended state once the trial has expired", () => {
    vi.setSystemTime(new Date("2026-08-19T00:00:00.000Z"));

    render(<TrialStatusCard />);

    expect(screen.getByText("Your trial has ended")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Talk to sales about upgrading" }),
    ).toBeTruthy();
    const progressBar = screen.getByRole("progressbar", {
      name: "Trial ended",
    });
    expect(progressBar.getAttribute("aria-valuenow")).toBe("100");
    expect(progressBar.firstElementChild?.getAttribute("style")).toBe(
      "width: 100%;",
    );
  });

  it.each([
    ["start", new Date("invalid"), activeTrial.endsAt],
    ["end", activeTrial.startedAt, new Date("invalid")],
  ])(
    "renders nothing when the trial %s date is invalid",
    (_, startedAt, endsAt) => {
      mocks.useSession.mockReturnValue({
        trial: { startedAt, endsAt },
      });

      const { container } = render(<TrialStatusCard />);

      expect(container.firstChild).toBeNull();
    },
  );

  it("renders nothing when a trial date is missing", () => {
    mocks.useSession.mockReturnValue({
      trial: { startedAt: undefined, endsAt: activeTrial.endsAt },
    });

    const { container } = render(<TrialStatusCard />);

    expect(container.firstChild).toBeNull();
  });

  it("shows the ended state when the trial expires without a parent rerender", async () => {
    vi.setSystemTime(new Date("2026-08-18T23:59:59.999Z"));

    render(<TrialStatusCard />);

    expect(screen.getByText("1 day left")).toBeTruthy();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });

    expect(screen.getByText("Your trial has ended")).toBeTruthy();
    expect(
      screen.getByRole("progressbar", { name: "Trial ended" }),
    ).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Talk to sales about upgrading" }),
    ).toBeTruthy();
  });

  it("updates the displayed day at a day boundary without a parent rerender", async () => {
    render(<TrialStatusCard />);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(24 * 60 * 60 * 1000);
    });

    expect(screen.getByText("Day 2/14")).toBeTruthy();
    expect(screen.getByText("13 days left")).toBeTruthy();
  });

  it("does not recreate the timer on an unrelated rerender", () => {
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");
    const { rerender } = render(<TrialStatusCard />);
    setTimeoutSpy.mockClear();

    rerender(<TrialStatusCard />);

    expect(setTimeoutSpy).not.toHaveBeenCalled();
    setTimeoutSpy.mockRestore();
  });

  it("visually advances the elapsed-time bar", () => {
    vi.setSystemTime(new Date("2026-08-07T00:00:00.000Z"));

    render(<TrialStatusCard />);

    expect(
      screen
        .getByRole("progressbar", { name: "Day 3 of 14" })
        .firstElementChild?.getAttribute("style"),
    ).toBe("width: 14.29%;");
  });

  it("changes the brand color as the trial progresses", () => {
    const colorByDay = [
      [1, "bg-(--color-base-black)"],
      [5, "bg-(--color-brand-c)"],
      [9, "bg-(--color-brand-ruby)"],
      [13, "bg-(--color-brand-swift)"],
    ] as const;

    for (const [day, colorClass] of colorByDay) {
      vi.setSystemTime(
        new Date(
          activeTrial.startedAt.getTime() + (day - 1) * 24 * 60 * 60 * 1000,
        ),
      );
      const { unmount } = render(<TrialStatusCard />);

      expect(
        screen
          .getByRole("progressbar")
          .firstElementChild?.classList.contains(colorClass),
      ).toBe(true);

      unmount();
    }
  });

  it("hides the card when the sidebar is collapsed", () => {
    const { container } = render(<TrialStatusCard />);

    expect(
      container.firstElementChild?.classList.contains(
        "group-data-[collapsible=icon]:hidden",
      ),
    ).toBe(true);
  });

  it("offers self-serve checkout beside the sales link during the trial", () => {
    mocks.flagResult.mockReturnValue({ status: "enabled" });

    render(<TrialStatusCard />);

    expect(
      screen.getByRole("button", { name: /start pay as you go/i }),
    ).toBeTruthy();
    // Sales stays available; checkout is an addition, not a replacement.
    expect(screen.getByRole("link", { name: "Talk to sales" })).toBeTruthy();
  });

  it("keeps only the sales link for a member", () => {
    mocks.flagResult.mockReturnValue({ status: "enabled" });
    mocks.hasScope.mockReturnValue(false);

    render(<TrialStatusCard />);

    expect(
      screen.queryByRole("button", { name: /start pay as you go/i }),
    ).toBeNull();
    expect(screen.getByRole("link", { name: "Talk to sales" })).toBeTruthy();
  });

  it("keeps only the sales link once the trial has ended", () => {
    mocks.flagResult.mockReturnValue({ status: "enabled" });
    vi.setSystemTime(new Date("2026-08-19T00:00:00.000Z"));

    render(<TrialStatusCard />);

    expect(
      screen.queryByRole("button", { name: /start pay as you go/i }),
    ).toBeNull();
    expect(
      screen.getByRole("link", { name: "Talk to sales about upgrading" }),
    ).toBeTruthy();
  });

  it("sends the Sales conversation to the in-app upgrade gate", () => {
    render(<TrialStatusCard />);

    // In-app rather than the marketing site: the gate prefills the booking
    // form from the session, so it stays in the same tab.
    const salesLink = screen.getByRole("link", { name: "Talk to sales" });
    expect(salesLink.getAttribute("href")).toBe("/talk-to-us");
    expect(salesLink.getAttribute("target")).toBeNull();
  });
});
