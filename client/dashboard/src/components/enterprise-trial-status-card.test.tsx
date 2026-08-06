import { cleanup, render, screen } from "@testing-library/react";
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

    expect(screen.getByText("TRIAL")).toBeTruthy();
    expect(screen.getByText("Day 1/14")).toBeTruthy();
    expect(screen.getByText("14 days left")).toBeTruthy();
    expect(
      screen
        .getByRole("progressbar", { name: "Day 1 of 14" })
        .getAttribute("aria-valuenow"),
    ).toBe("0");
  });

  it("uses singular copy when one day remains", () => {
    vi.setSystemTime(new Date("2026-08-18T00:00:00.000Z"));

    render(<EnterpriseTrialStatusCard />);

    expect(screen.getByText("1 day left")).toBeTruthy();
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

  it("opens the Sales conversation safely in a new tab", () => {
    render(<EnterpriseTrialStatusCard />);

    const salesLink = screen.getByRole("link", { name: "Talk to Sales" });
    expect(salesLink.getAttribute("href")).toBe(
      "https://www.speakeasy.com/talk-to-us",
    );
    expect(salesLink.getAttribute("target")).toBe("_blank");
    expect(salesLink.getAttribute("rel")).toBe("noopener noreferrer");
  });
});
