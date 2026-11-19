import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithApp } from "@/test/harness";
import { dayOf, calendarDate, trialEndDay } from "@/lib/trialDates";
import { fmtDateShort } from "@/lib/utils";
import { TrialDaysDialog } from "./OrganizationActions";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

it("initializes the picker on the current end date and submits an absolute UTC day", async () => {
  const anchor = trialEndDay("2090-05-15T13:00:00Z")!;
  const onSubmit = vi.fn<(day: number) => void>();
  await renderWithApp(
    <TrialDaysDialog
      bounds={{ min: 1, max: 365 }}
      range={{ anchor, earliest: anchor - 10 }}
      title="Change end date"
      description="Choose a future date."
      submitLabel="Save"
      pendingLabel="Saving..."
      failureLead="Could not change end date"
      pending={false}
      failure={null}
      onCancel={() => {}}
      onCloseAutoFocus={() => {}}
      onSubmit={onSubmit}
    />,
  );
  expect(screen.getByLabelText("Ends on").textContent).toContain(
    fmtDateShort("2090-05-15T00:00:00Z"),
  );
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(onSubmit).toHaveBeenCalledWith(dayOf(calendarDate(anchor)));
});

it("disables the elapsed date when an open calendar crosses UTC midnight", async () => {
  vi.useFakeTimers({ toFake: ["Date", "setTimeout", "clearTimeout"] });
  vi.setSystemTime(new Date("2090-05-01T23:59:59Z"));
  const anchor = trialEndDay("2090-05-15T00:00:00Z")!;
  render(
    <TrialDaysDialog
      range={{ anchor, earliest: anchor - 13 }}
      title="Change end date"
      description="Choose a future date."
      submitLabel="Save"
      pendingLabel="Saving..."
      failureLead="Could not change end date"
      pending={false}
      failure={null}
      onCancel={() => {}}
      onCloseAutoFocus={() => {}}
      onSubmit={() => {}}
    />,
  );
  fireEvent.click(screen.getByLabelText("Ends on"));
  const tomorrow = () =>
    document.querySelector<HTMLButtonElement>(
      "td[data-day='2090-05-02'] button",
    );
  expect(tomorrow()?.disabled).toBe(false);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(tomorrow()?.disabled).toBe(true);
});
