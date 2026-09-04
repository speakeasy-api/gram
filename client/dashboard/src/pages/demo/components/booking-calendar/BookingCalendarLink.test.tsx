import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { BookingCalendarLink } from "./BookingCalendarLink";

const mocks = vi.hoisted(() => ({
  session: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useSession: () => mocks.session(),
}));

vi.mock("@/hooks/useTrialNow", () => ({
  useTrialNow: () => new Date("2026-08-10T00:00:00.000Z"),
}));

vi.mock("./BookingCalendarModal", () => ({
  BookingCalendarModal: ({
    open,
    eventLabel,
    formDefaults,
    telemetrySource,
    footer,
  }: {
    open: boolean;
    eventLabel?: string;
    formDefaults?: Record<string, string | undefined>;
    telemetrySource?: string;
    footer?: React.ReactNode;
  }) =>
    open ? (
      <div role="dialog">
        <span>{eventLabel}</span>
        <span>{formDefaults?.source}</span>
        <span>{formDefaults?.notes}</span>
        <span>{telemetrySource}</span>
        {footer}
      </div>
    ) : null,
}));

afterEach(cleanup);

beforeEach(() => {
  mocks.session.mockReturnValue({
    trial: {
      startedAt: new Date("2026-08-05T00:00:00.000Z"),
      endsAt: new Date("2026-08-19T00:00:00.000Z"),
    },
  });
});

it("opens a prefilled upgrade calendar without navigating", () => {
  render(<BookingCalendarLink>Talk to us</BookingCalendarLink>);

  const trigger = screen.getByRole("button", { name: "Talk to us" });
  expect(trigger.getAttribute("type")).toBe("button");
  expect(trigger.getAttribute("aria-haspopup")).toBe("dialog");
  expect(trigger.getAttribute("aria-expanded")).toBe("false");
  fireEvent.click(trigger);
  expect(trigger.getAttribute("aria-expanded")).toBe("true");

  expect(screen.getByRole("dialog")).toBeTruthy();
  expect(screen.getByText("Upgrade Trial — 30 min")).toBeTruthy();
  expect(screen.getByText("Trial: Active")).toBeTruthy();
  expect(screen.getByText("Upgrade trial")).toBeTruthy();
  expect(screen.getByText("trial_upgrade")).toBeTruthy();
  expect(
    screen
      .getByRole("link", { name: "Email sales@speakeasy.com" })
      .getAttribute("href"),
  ).toBe("mailto:sales@speakeasy.com");
});

it("uses caller-specific booking context", () => {
  render(
    <BookingCalendarLink
      eventLabel="Inference caps — 30 min"
      formDefaults={{
        source: "Dashboard: Inference caps",
        notes: "Request inference cap above $10,000",
      }}
      telemetrySource="inference_cap"
    >
      Talk to us
    </BookingCalendarLink>,
  );

  fireEvent.click(screen.getByRole("button", { name: "Talk to us" }));

  expect(screen.getByText("Inference caps — 30 min")).toBeTruthy();
  expect(screen.getByText("Dashboard: Inference caps")).toBeTruthy();
  expect(screen.getByText("Request inference cap above $10,000")).toBeTruthy();
  expect(screen.getByText("inference_cap")).toBeTruthy();
});
