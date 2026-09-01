import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi, beforeEach } from "vitest";
import {
  CAL_DEMO_LINK,
  CAL_DEMO_NAMESPACE,
  CAL_DEMO_URL,
  CAL_EMBED_TIMEOUT_MS,
  SALES_EMAIL,
} from "./demo-booking";

type MockSession = {
  user: { email: string; displayName?: string };
  organization?: { name: string };
} | null;
const { captureMock, sessionHolder, cal } = vi.hoisted(() => ({
  captureMock: vi.fn(),
  sessionHolder: {
    current: {
      user: { email: "jane@acme.com", displayName: "Jane Smith" },
      organization: { name: "Acme Inc" },
    } as MockSession,
  },
  // Stands in for Cal's global instruction queue: it records what it was told
  // to do, holds the event listeners the component registers, and can be made
  // to fail the way the real one does.
  cal: (() => {
    const state = {
      /** Instructions received, in order, as `[name, argument]` pairs. */
      calls: [] as [string, unknown][],
      /** Namespaces `getCalApi` was asked for. */
      namespaces: [] as (string | undefined)[],
      /** Makes `getCalApi` reject, as it does when embed.js never loads. */
      apiError: null as Error | null,
      /** Makes the "ui" instruction throw, as Cal's `doInIframe` does. */
      uiThrows: false,
      listeners: new Map<string, (event: unknown) => void>(),

      api: (name: string, arg?: unknown) => {
        state.calls.push([name, arg]);
        const { action, callback } = (arg ?? {}) as {
          action?: string;
          callback?: (event: unknown) => void;
        };
        if (name === "on" && action && callback) {
          state.listeners.set(action, callback);
        }
        if (name === "off" && action) state.listeners.delete(action);
        if (name === "ui" && state.uiThrows) {
          throw new Error(
            "iframe doesn't exist. `createIframe` must be called before `doInIframe`",
          );
        }
      },

      /** Fires one of Cal's embed events at whatever the component registered. */
      emit(action: string, detail: unknown = {}) {
        state.listeners.get(action)?.({ detail });
      },

      countOf(name: string) {
        return state.calls.filter(([called]) => called === name).length;
      },

      argOf(name: string) {
        return state.calls.find(([called]) => called === name)?.[1];
      },

      reset() {
        state.calls = [];
        state.namespaces = [];
        state.apiError = null;
        state.uiThrows = false;
        state.listeners.clear();
      },
    };
    return state;
  })(),
}));

// Replace the Cal embed with a probe that exposes the props it received.
vi.mock("@calcom/embed-react", () => ({
  default: ({
    calLink,
    namespace,
    config,
  }: {
    calLink: string;
    namespace?: string;
    config?: Record<string, string | undefined>;
  }) => (
    <div
      data-testid="cal-embed"
      data-cal-link={calLink}
      data-cal-namespace={namespace ?? ""}
      data-cal-name={config?.name ?? ""}
      data-cal-email={config?.email ?? ""}
      // Keyed by the booking question's identifier on the Cal event, which is
      // what the embed actually matches on.
      data-cal-company={config?.["Company-Name"] ?? ""}
      data-cal-source={config?.source ?? ""}
      data-cal-notes={config?.notes ?? ""}
    />
  ),
  getCalApi: (options?: { namespace?: string }) => {
    cal.namespaces.push(options?.namespace);
    return cal.apiError
      ? Promise.reject(cal.apiError)
      : Promise.resolve(cal.api);
  },
}));

vi.mock("@/contexts/Auth", () => ({
  useSessionData: () => ({ session: sessionHolder.current }),
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: captureMock }),
}));

// The panel does its own gating (flag, org:admin, walled-off organization);
// here only its placement relative to the calendar matters.
vi.mock("@/components/billing/lockout-payg-checkout-panel", () => ({
  LockoutPaygCheckoutPanel: () => <div data-testid="payg-panel" />,
}));

import { DemoBookingFlow } from "./DemoBookingFlow";

/** Renders, then settles the promise `getCalApi` returns. */
async function renderConfigured(ui: React.ReactElement = <DemoBookingFlow />) {
  render(ui);
  await waitFor(() => expect(cal.countOf("on")).toBe(2));
}

beforeEach(() => {
  captureMock.mockClear();
  cal.reset();
  sessionHolder.current = {
    user: { email: "jane@acme.com", displayName: "Jane Smith" },
    organization: { name: "Acme Inc" },
  };
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("DemoBookingFlow", () => {
  it("embeds the demo calendar directly with no intermediate form", () => {
    render(<DemoBookingFlow />);
    const embed = screen.getByTestId("cal-embed");
    expect(embed.getAttribute("data-cal-link")).toBe(CAL_DEMO_LINK);
  });

  // Checkout is the shortcut past the gate, so it has to read before the
  // calendar rather than under it.
  it("places the checkout panel above the calendar", () => {
    render(<DemoBookingFlow />);
    const panel = screen.getByTestId("payg-panel");
    const embed = screen.getByTestId("cal-embed");
    expect(
      panel.compareDocumentPosition(embed) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
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

  it("keeps session identity when a caller tries to override it", () => {
    render(
      <DemoBookingFlow
        formDefaults={{
          email: "spoof@example.test",
          name: "Someone Else",
          "Company-Name": "Other Co",
        }}
      />,
    );
    const embed = screen.getByTestId("cal-embed");
    expect(embed.getAttribute("data-cal-email")).toBe("jane@acme.com");
    expect(embed.getAttribute("data-cal-name")).toBe("Jane Smith");
    expect(embed.getAttribute("data-cal-company")).toBe("Acme Inc");
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
    await renderConfigured();

    const uiConfig = cal.argOf("ui") as {
      hideEventTypeDetails?: boolean;
      cssVarsPerTheme?: Record<string, unknown>;
    };
    expect(uiConfig.hideEventTypeDetails).toBe(true);
    expect(Object.keys(uiConfig.cssVarsPerTheme ?? {})).toEqual([
      "light",
      "dark",
    ]);
  });

  // The regression this file exists to catch. "ui" resolves to Cal's
  // `doInIframe`, which throws when it lands on an instance with no iframe of
  // its own — so the instruction and the embed have to name the same namespace
  // rather than both falling back to the shared default.
  it("issues the ui instruction on the namespace the embed uses", async () => {
    await renderConfigured();

    expect(cal.namespaces).toEqual([CAL_DEMO_NAMESPACE]);
    expect(
      screen.getByTestId("cal-embed").getAttribute("data-cal-namespace"),
    ).toBe(CAL_DEMO_NAMESPACE);
  });

  // Cal replays a queued "ui" against the iframe, but only if the early
  // attempt was queued rather than dropped by a stale instance.
  it("reapplies the branding once Cal reports the iframe ready", async () => {
    await renderConfigured();
    expect(cal.countOf("ui")).toBe(1);

    act(() => cal.emit("linkReady"));

    expect(cal.countOf("ui")).toBe(2);
  });

  it("keeps the calendar when the ui instruction throws", async () => {
    cal.uiThrows = true;
    await renderConfigured();

    act(() => cal.emit("linkReady"));

    expect(screen.getByTestId("cal-embed")).toBeTruthy();
    expect(captureMock).toHaveBeenCalledWith(
      "demo_booking_embed_loaded",
      expect.objectContaining({ cal_link: CAL_DEMO_LINK }),
    );
  });

  it("covers the embed until Cal reports the iframe ready", async () => {
    await renderConfigured();
    expect(screen.getByText(/Loading calendar/)).toBeTruthy();

    act(() => cal.emit("linkReady"));

    expect(screen.queryByText(/Loading calendar/)).toBeNull();
  });

  it("offers a direct booking link when the embed never becomes ready", async () => {
    vi.useFakeTimers();
    render(<DemoBookingFlow />);

    await act(() => vi.advanceTimersByTimeAsync(CAL_EMBED_TIMEOUT_MS));

    expect(screen.queryByTestId("cal-embed")).toBeNull();
    expect(
      screen.getByRole("link", { name: "Book a demo" }).getAttribute("href"),
    ).toBe(CAL_DEMO_URL);
    expect(
      screen
        .getByRole("link", { name: `Email ${SALES_EMAIL}` })
        .getAttribute("href"),
    ).toBe(`mailto:${SALES_EMAIL}`);
    expect(captureMock).toHaveBeenCalledWith(
      "demo_booking_embed_failed",
      expect.objectContaining({ reason: "timeout" }),
    );
  });

  it("falls back as soon as Cal reports the link failed", async () => {
    await renderConfigured();

    act(() => cal.emit("linkFailed", { data: { code: "404" } }));

    expect(screen.queryByTestId("cal-embed")).toBeNull();
    expect(screen.getByRole("link", { name: "Book a demo" })).toBeTruthy();
    expect(captureMock).toHaveBeenCalledWith(
      "demo_booking_embed_failed",
      expect.objectContaining({ reason: "link_failed", code: "404" }),
    );
  });

  it("falls back when the embed API never loads", async () => {
    cal.apiError = new Error("embed.js blocked");
    render(<DemoBookingFlow />);

    expect(
      await screen.findByRole("link", { name: "Book a demo" }),
    ).toBeTruthy();
    expect(captureMock).toHaveBeenCalledWith(
      "demo_booking_embed_failed",
      expect.objectContaining({ reason: "api_error" }),
    );
  });

  it("stops listening once the flow unmounts", async () => {
    await renderConfigured();

    cleanup();

    expect(cal.countOf("off")).toBe(2);
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
