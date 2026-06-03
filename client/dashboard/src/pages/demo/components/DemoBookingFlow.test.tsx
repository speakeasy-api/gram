import { cleanup, render, screen, fireEvent } from "@testing-library/react";
import { afterEach, describe, expect, it, vi, beforeEach } from "vitest";

type MockSession = { user: { email: string; displayName?: string } } | null;
const { captureMock, sessionHolder } = vi.hoisted(() => ({
  captureMock: vi.fn(),
  sessionHolder: {
    current: {
      user: { email: "jane@acme.com", displayName: "Jane Smith" },
    } as MockSession,
  },
}));

// moonshine's bundle can't resolve lucide dynamic icons in tests — render Button
// as a plain <button>.
vi.mock("@speakeasy-api/moonshine", () => ({
  Button: ({
    children,
    type,
    onClick,
  }: {
    children: React.ReactNode;
    type?: "button" | "submit";
    onClick?: () => void;
    variant?: string;
    className?: string;
  }) => (
    <button type={type} onClick={onClick}>
      {children}
    </button>
  ),
}));

// Replace the Cal embed with a probe that exposes the calLink it received.
vi.mock("@calcom/embed-react", () => ({
  default: ({ calLink }: { calLink: string }) => (
    <div data-testid="cal-embed" data-cal-link={calLink} />
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
  it("prefills name and email from the session", () => {
    render(<DemoBookingFlow />);
    expect(
      (screen.getByLabelText(/First name/i) as HTMLInputElement).value,
    ).toBe("Jane");
    expect(
      (screen.getByLabelText(/Last name/i) as HTMLInputElement).value,
    ).toBe("Smith");
    expect(
      (screen.getByLabelText(/Work email/i) as HTMLInputElement).value,
    ).toBe("jane@acme.com");
  });

  it("blocks submission and shows an error when a required field is empty", () => {
    render(<DemoBookingFlow />);
    fireEvent.click(
      screen.getByRole("button", { name: /Continue to booking/i }),
    );
    expect(screen.getByText("This field is required")).toBeTruthy();
    expect(captureMock).not.toHaveBeenCalled();
    expect(screen.queryByTestId("cal-embed")).toBeNull();
  });

  it("advances to the Cal embed with an encoded link and fires demo_form_submitted", () => {
    render(<DemoBookingFlow />);
    fireEvent.change(screen.getByLabelText(/How did you hear about us/i), {
      target: { value: "Google" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: /Continue to booking/i }),
    );

    expect(captureMock).toHaveBeenCalledWith("demo_form_submitted", {
      product: "AI Control Plane",
    });
    const embed = screen.getByTestId("cal-embed");
    const link = embed.getAttribute("data-cal-link") ?? "";
    expect(link).toContain("first-name=Jane");
    expect(link).toContain("heard-about-us=Google");
    expect(link).toContain("interested-products=AI%20Control%20Plane");
  });

  it("fires booked_demo on a Cal bookingSuccessful message", () => {
    render(<DemoBookingFlow />);
    fireEvent.change(screen.getByLabelText(/How did you hear about us/i), {
      target: { value: "Google" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: /Continue to booking/i }),
    );
    captureMock.mockClear();

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
        email: "jane@acme.com",
        product: "AI Control Plane",
      }),
    );
  });

  it("returns to the form when Back is clicked", () => {
    render(<DemoBookingFlow />);
    fireEvent.change(screen.getByLabelText(/How did you hear about us/i), {
      target: { value: "Google" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: /Continue to booking/i }),
    );
    fireEvent.click(screen.getByRole("button", { name: /Back/i }));
    expect(screen.getByText("Talk to our experts")).toBeTruthy();
    expect(screen.queryByTestId("cal-embed")).toBeNull();
  });

  it("backfills empty fields when the session arrives after mount", () => {
    sessionHolder.current = null; // session still loading at mount
    const { rerender } = render(<DemoBookingFlow />);
    expect(
      (screen.getByLabelText(/Work email/i) as HTMLInputElement).value,
    ).toBe("");
    expect(
      (screen.getByLabelText(/First name/i) as HTMLInputElement).value,
    ).toBe("");

    sessionHolder.current = {
      user: { email: "jane@acme.com", displayName: "Jane Smith" },
    };
    rerender(<DemoBookingFlow />);

    expect(
      (screen.getByLabelText(/Work email/i) as HTMLInputElement).value,
    ).toBe("jane@acme.com");
    expect(
      (screen.getByLabelText(/First name/i) as HTMLInputElement).value,
    ).toBe("Jane");
    expect(
      (screen.getByLabelText(/Last name/i) as HTMLInputElement).value,
    ).toBe("Smith");
  });

  it("does not overwrite fields the user already edited when the session arrives", () => {
    sessionHolder.current = null;
    const { rerender } = render(<DemoBookingFlow />);
    fireEvent.change(screen.getByLabelText(/First name/i), {
      target: { value: "Custom" },
    });

    sessionHolder.current = {
      user: { email: "jane@acme.com", displayName: "Jane Smith" },
    };
    rerender(<DemoBookingFlow />);

    // user's edit is preserved...
    expect(
      (screen.getByLabelText(/First name/i) as HTMLInputElement).value,
    ).toBe("Custom");
    // ...while still-empty fields are backfilled
    expect(
      (screen.getByLabelText(/Work email/i) as HTMLInputElement).value,
    ).toBe("jane@acme.com");
  });
});
