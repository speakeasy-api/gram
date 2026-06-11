import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi, beforeEach } from "vitest";
import { CAL_DEMO_LINK } from "./demo-booking";

type MockSession = { user: { email: string; displayName?: string } } | null;
const { captureMock, sessionHolder } = vi.hoisted(() => ({
  captureMock: vi.fn(),
  sessionHolder: {
    current: {
      user: { email: "jane@acme.com", displayName: "Jane Smith" },
    } as MockSession,
  },
}));

// Replace the Cal embed with a probe that exposes the props it received.
vi.mock("@calcom/embed-react", () => ({
  default: ({
    calLink,
    config,
  }: {
    calLink: string;
    config?: Record<string, unknown>;
  }) => (
    <div
      data-testid="cal-embed"
      data-cal-link={calLink}
      data-cal-name={String(config?.name ?? "")}
      data-cal-email={String(config?.email ?? "")}
    />
  ),
}));

vi.mock("@/contexts/Auth", () => ({
  useSessionData: () => ({ session: sessionHolder.current }),
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: captureMock }),
}));

import { DemoBookingFlow } from "./DemoBookingFlow";

beforeEach(() => {
  captureMock.mockClear();
  sessionHolder.current = {
    user: { email: "jane@acme.com", displayName: "Jane Smith" },
  };
});

afterEach(cleanup);

describe("DemoBookingFlow", () => {
  it("embeds the demo calendar directly with no intermediate form", () => {
    render(<DemoBookingFlow />);
    const embed = screen.getByTestId("cal-embed");
    expect(embed.getAttribute("data-cal-link")).toBe(CAL_DEMO_LINK);
  });

  it("prefills name and email from the session", () => {
    render(<DemoBookingFlow />);
    const embed = screen.getByTestId("cal-embed");
    expect(embed.getAttribute("data-cal-name")).toBe("Jane Smith");
    expect(embed.getAttribute("data-cal-email")).toBe("jane@acme.com");
  });

  it("renders the embed even before the session resolves", () => {
    sessionHolder.current = null;
    render(<DemoBookingFlow />);
    const embed = screen.getByTestId("cal-embed");
    expect(embed.getAttribute("data-cal-link")).toBe(CAL_DEMO_LINK);
    expect(embed.getAttribute("data-cal-name")).toBe("");
    expect(embed.getAttribute("data-cal-email")).toBe("");
  });

  it("fires booked_demo on a Cal bookingSuccessful message", () => {
    render(<DemoBookingFlow />);

    window.dispatchEvent(
      new MessageEvent("message", {
        data: JSON.stringify({
          originator: "CAL",
          fullType: "CAL:bookingSuccessful",
        }),
      }),
    );

    expect(captureMock).toHaveBeenCalledWith(
      "booked_demo",
      expect.objectContaining({
        first_name: "Jane",
        last_name: "Smith",
        email: "jane@acme.com",
      }),
    );
  });

  it("ignores non-Cal postMessages", () => {
    render(<DemoBookingFlow />);

    window.dispatchEvent(
      new MessageEvent("message", {
        data: JSON.stringify({ originator: "OTHER", fullType: "x" }),
      }),
    );

    expect(captureMock).not.toHaveBeenCalled();
  });
});
