import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
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
    footer,
  }: {
    open: boolean;
    eventLabel?: string;
    formDefaults?: Record<string, string | undefined>;
    footer?: React.ReactNode;
  }) =>
    open ? (
      <div role="dialog">
        <span>{eventLabel}</span>
        <span>{formDefaults?.source}</span>
        <span>{formDefaults?.notes}</span>
        {footer}
      </div>
    ) : null,
}));

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
  fireEvent.click(trigger);

  expect(screen.getByRole("dialog")).toBeTruthy();
  expect(screen.getByText("Upgrade Trial — 30 min")).toBeTruthy();
  expect(screen.getByText("Trial: Active")).toBeTruthy();
  expect(screen.getByText("Upgrade trial")).toBeTruthy();
  expect(
    screen
      .getByRole("link", { name: "Email sales@speakeasy.com" })
      .getAttribute("href"),
  ).toBe("mailto:sales@speakeasy.com");
});
