import { act, cleanup, render, screen } from "@testing-library/react";
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

import { EnterpriseTrialStatusCard } from "./enterprise-trial-status-card";

const activeTrial = {
  startedAt: new Date("2026-08-05T00:00:00.000Z"),
  endsAt: new Date("2026-08-19T00:00:00.000Z"),
};

describe("EnterpriseTrialStatusCard", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-05T00:00:00.000Z"));
    mocks.useSession.mockReturnValue({ enterpriseTrial: activeTrial });
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it("defaults the empty session enterprise trial to null", () => {
    expect(emptySession.enterpriseTrial).toBeNull();
  });

  it("renders the active trial status", () => {
    render(<EnterpriseTrialStatusCard />);

    expect(screen.getByText("Trial")).toBeTruthy();
    expect(screen.getByText("Day 1/14")).toBeTruthy();
    expect(screen.getByText("13 days left")).toBeTruthy();
    const progressBar = screen.getByRole("progressbar", {
      name: "Day 1 of 14",
    });
    expect(progressBar.getAttribute("aria-valuenow")).toBe("7.14");
    expect(progressBar.firstElementChild?.getAttribute("style")).toBe(
      "width: 7.14%;",
    );
  });

  it("shows zero days left on the final trial day", () => {
    vi.setSystemTime(new Date("2026-08-18T00:00:00.000Z"));

    render(<EnterpriseTrialStatusCard />);

    expect(screen.getByText("0 days left")).toBeTruthy();
  });

  it("renders nothing without an enterprise trial", () => {
    mocks.useSession.mockReturnValue({ enterpriseTrial: null });

    const { container } = render(<EnterpriseTrialStatusCard />);

    expect(container.firstChild).toBeNull();
  });

  it("renders nothing once the enterprise trial has expired", () => {
    vi.setSystemTime(new Date("2026-08-19T00:00:00.000Z"));

    const { container } = render(<EnterpriseTrialStatusCard />);

    expect(container.firstChild).toBeNull();
  });

  it("removes the card when the trial expires without a parent rerender", async () => {
    vi.setSystemTime(new Date("2026-08-18T23:59:59.999Z"));

    const { container } = render(<EnterpriseTrialStatusCard />);

    expect(screen.getByText("0 days left")).toBeTruthy();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });

    expect(container.firstChild).toBeNull();
  });

  it("updates the displayed day at a day boundary without a parent rerender", async () => {
    render(<EnterpriseTrialStatusCard />);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(24 * 60 * 60 * 1000);
    });

    expect(screen.getByText("Day 2/14")).toBeTruthy();
    expect(screen.getByText("12 days left")).toBeTruthy();
  });

  it("visually advances the elapsed-time bar", () => {
    vi.setSystemTime(new Date("2026-08-07T00:00:00.000Z"));

    render(<EnterpriseTrialStatusCard />);

    expect(
      screen
        .getByRole("progressbar", { name: "Day 3 of 14" })
        .firstElementChild?.getAttribute("style"),
    ).toBe("width: 21.43%;");
  });

  it("hides the card when the sidebar is collapsed", () => {
    const { container } = render(<EnterpriseTrialStatusCard />);

    expect(
      container.firstElementChild?.classList.contains(
        "group-data-[collapsible=icon]:hidden",
      ),
    ).toBe(true);
  });

  it("opens the Sales conversation safely in a new tab", () => {
    render(<EnterpriseTrialStatusCard />);

    const salesLink = screen.getByRole("link", { name: "Talk to sales" });
    expect(salesLink.getAttribute("href")).toBe(
      "https://www.speakeasy.com/talk-to-us",
    );
    expect(salesLink.getAttribute("target")).toBe("_blank");
    expect(salesLink.getAttribute("rel")).toBe("noopener noreferrer");
  });
});
