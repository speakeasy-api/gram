import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import TrialEnded from "./TrialEnded";

const mocks = vi.hoisted(() => ({
  session: vi.fn(),
  openBookingCalendar: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useSessionData: mocks.session,
}));

vi.mock("@/hooks/useTrialNow", () => ({
  useTrialNow: () => new Date("2026-08-20T00:00:00.000Z"),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ auth: { logout: vi.fn() } }),
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: vi.fn() }),
}));

vi.mock("@/components/billing/use-start-payg-checkout", () => ({
  useStartPaygCheckout: () => ({
    startCheckout: vi.fn(),
    isPending: false,
    error: null,
  }),
}));

vi.mock("./components/booking-calendar/useBookingCalendarModal", () => ({
  useBookingCalendarModal: () => ({
    openBookingCalendar: mocks.openBookingCalendar,
    modalProps: { open: false, onOpenChange: vi.fn() },
  }),
}));

vi.mock("./components/booking-calendar/BookingCalendarModal", () => ({
  BookingCalendarModal: () => null,
}));

afterEach(cleanup);

it("redirects when the session has no expired trial", () => {
  mocks.session.mockReturnValue({ session: { trial: null } });

  render(
    <MemoryRouter initialEntries={["/trial-ended"]}>
      <Routes>
        <Route path="/" element={<div>Workspace</div>} />
        <Route path="/trial-ended" element={<TrialEnded />} />
      </Routes>
    </MemoryRouter>,
  );

  expect(screen.getByText("Workspace")).toBeDefined();
});

it("opens sales with expired-trial context", () => {
  mocks.session.mockReturnValue({
    session: {
      activeOrganizationId: "org-1",
      user: { email: "person@example.com" },
      organization: { name: "Example", slug: "example" },
      trial: {
        startedAt: new Date("2026-08-01T00:00:00.000Z"),
        endsAt: new Date("2026-08-15T00:00:00.000Z"),
      },
    },
  });

  render(
    <MemoryRouter initialEntries={["/trial-ended"]}>
      <Routes>
        <Route path="/trial-ended" element={<TrialEnded />} />
      </Routes>
    </MemoryRouter>,
  );

  const salesOption = screen.getByRole("radio", { name: "Contact sales" });
  expect(salesOption.getAttribute("aria-describedby")).not.toBeNull();
  fireEvent.click(salesOption);

  expect(mocks.openBookingCalendar).toHaveBeenCalledWith(
    expect.objectContaining({
      eventLabel: "Upgrade Trial — 30 min",
      formDefaults: {
        source: "Trial: Expired",
        notes: "Upgrade expired trial",
      },
      telemetrySource: "trial_upgrade",
    }),
  );
});
