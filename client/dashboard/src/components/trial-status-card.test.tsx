import {
  act,
  cleanup,
  render as rtlRender,
  screen,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { emptySession } from "@/contexts/Auth";

const mocks = vi.hoisted(() => ({
  useSession: vi.fn(),
}));

vi.mock("@/contexts/Auth", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/contexts/Auth")>();
  return {
    ...actual,
    useSession: mocks.useSession,
  };
});

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
  });

  it("renders nothing without a trial", () => {
    mocks.useSession.mockReturnValue({ trial: null });

    const { container } = render(<TrialStatusCard />);

    expect(container.firstChild).toBeNull();
  });

  it("renders nothing once the trial has expired", () => {
    vi.setSystemTime(new Date("2026-08-19T00:00:00.000Z"));

    const { container } = render(<TrialStatusCard />);

    expect(container.firstChild).toBeNull();
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

  it("removes the card when the trial expires without a parent rerender", async () => {
    vi.setSystemTime(new Date("2026-08-18T23:59:59.999Z"));

    const { container } = render(<TrialStatusCard />);

    expect(screen.getByText("1 day left")).toBeTruthy();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });

    expect(container.firstChild).toBeNull();
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
      [1, "bg-[var(--color-base-black)]"],
      [5, "bg-[var(--color-brand-c)]"],
      [9, "bg-[var(--color-brand-ruby)]"],
      [13, "bg-[var(--color-brand-swift)]"],
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

  it("sends the Sales conversation to the in-app upgrade gate", () => {
    render(<TrialStatusCard />);

    // In-app rather than the marketing site: the gate prefills the booking
    // form from the session, so it stays in the same tab.
    const salesLink = screen.getByRole("link", { name: "Talk to sales" });
    expect(salesLink.getAttribute("href")).toBe("/talk-to-us");
    expect(salesLink.getAttribute("target")).toBeNull();
  });
});
