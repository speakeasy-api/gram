import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithApp } from "@/test/harness";
import { dayOf, calendarDate, trialEndDay } from "@/lib/trialDates";
import { fmtDateShort } from "@/lib/utils";
import { TrialDaysDialog } from "./OrganizationActions";

afterEach(cleanup);

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
  expect(screen.getByRole("button", { name: "Ends on" }).textContent).toContain(
    fmtDateShort("2090-05-15T00:00:00Z"),
  );
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(onSubmit).toHaveBeenCalledWith(dayOf(calendarDate(anchor)));
});
