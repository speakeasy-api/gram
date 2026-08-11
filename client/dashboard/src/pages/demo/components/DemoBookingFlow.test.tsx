import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi, beforeEach } from "vitest";
import { CAL_DEMO_LINK } from "./demo-booking";

type MockSession = {
  user: { email: string; displayName?: string };
  organization?: { name: string };
} | null;
const { captureMock, sessionHolder, calUiMock } = vi.hoisted(() => ({
  captureMock: vi.fn(),
  calUiMock: vi.fn(),
  sessionHolder: {
    current: {
      user: { email: "jane@acme.com", displayName: "Jane Smith" },
      organization: { name: "Acme Inc" },
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
    config?: Record<string, string | undefined>;
  }) => (
    <div
      data-testid="cal-embed"
      data-cal-link={calLink}
      data-cal-name={config?.name ?? ""}
      data-cal-email={config?.email ?? ""}
      // Keyed by the booking question's identifier on the Cal event, which is
      // what the embed actually matches on.
      data-cal-company={config?.["Company-Name"] ?? ""}
      data-cal-source={config?.source ?? ""}
      data-cal-notes={config?.notes ?? ""}
    />
  ),
  getCalApi: () => Promise.resolve(calUiMock),
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
  calUiMock.mockClear();
  sessionHolder.current = {
    user: { email: "jane@acme.com", displayName: "Jane Smith" },
    organization: { name: "Acme Inc" },
  };
});

afterEach(cleanup);

describe("DemoBookingFlow", () => {
  it("embeds the demo calendar directly with no intermediate form", () => {
    render(<DemoBookingFlow />);
    const embed = screen.getByTestId("cal-embed");
    expect(embed.getAttribute("data-cal-link")).toBe(CAL_DEMO_LINK);
  });

  it("prefills name, email, and company from the session", () => {
    render(<DemoBookingFlow />);
    const embed = screen.getByTestId("cal-embed");
    expect(embed.getAttribute("data-cal-name")).toBe("Jane Smith");
    expect(embed.getAttribute("data-cal-email")).toBe("jane@acme.com");
    expect(embed.getAttribute("data-cal-company")).toBe("Acme Inc");
  });

  it("leaves the form defaults blank when the caller sets none", () => {
    render(<DemoBookingFlow />);
    const embed = screen.getByTestId("cal-embed");
    expect(embed.getAttribute("data-cal-source")).toBe("");
    expect(embed.getAttribute("data-cal-notes")).toBe("");
  });

  it("passes the caller's form defaults through to the embed", () => {
    render(
      <DemoBookingFlow
        formDefaults={{ source: "Trial", notes: "Upgrade trial account" }}
      />,
    );
    const embed = screen.getByTestId("cal-embed");
    expect(embed.getAttribute("data-cal-source")).toBe("Trial");
    expect(embed.getAttribute("data-cal-notes")).toBe("Upgrade trial account");
  });

  it("renders the embed even before the session resolves", () => {
    sessionHolder.current = null;
    render(<DemoBookingFlow />);
    const embed = screen.getByTestId("cal-embed");
    expect(embed.getAttribute("data-cal-link")).toBe(CAL_DEMO_LINK);
    expect(embed.getAttribute("data-cal-name")).toBe("");
    expect(embed.getAttribute("data-cal-email")).toBe("");
    expect(embed.getAttribute("data-cal-company")).toBe("");
  });

  // hideEventTypeDetails and cssVarsPerTheme are UiConfig, not prefill config;
  // passing them via the `config` prop silently does nothing.
  it("brands the embed through the ui instruction, not the config prop", async () => {
    render(<DemoBookingFlow />);

    await waitFor(() => expect(calUiMock).toHaveBeenCalled());

    const [instruction, uiConfig] = calUiMock.mock.calls[0] as [
      string,
      {
        hideEventTypeDetails?: boolean;
        cssVarsPerTheme?: Record<string, unknown>;
      },
    ];
    expect(instruction).toBe("ui");
    expect(uiConfig.hideEventTypeDetails).toBe(true);
    expect(Object.keys(uiConfig.cssVarsPerTheme ?? {})).toEqual([
      "light",
      "dark",
    ]);
  });

  it("footnotes the details it handed the embed", () => {
    render(<DemoBookingFlow />);
    expect(
      screen.getByText(/jane@acme\.com · Acme Inc/, { exact: false }),
    ).toBeTruthy();
  });

  it("drops the footnote when the session has nothing to prefill", () => {
    sessionHolder.current = null;
    render(<DemoBookingFlow />);
    expect(screen.queryByText(/Details prefilled/)).toBeNull();
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
