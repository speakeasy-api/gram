import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import { TooltipProvider } from "@/components/ui/Tooltip";
import type { RowSelection } from "@/hooks/useRowSelection";
import type { OktaResourceConnectionServer } from "@gram/client/models/components/oktaresourceconnectionserver.js";
import { XaaReadinessTable } from "./XaaReadinessTable";
import { confirmedRow } from "./xaaTestRows";

const selection: RowSelection<OktaResourceConnectionServer> = {
  selectedIds: new Set(),
  selectedCount: 0,
  isSelected: () => false,
  toggle: () => {},
  toggleAll: () => {},
  clear: () => {},
  allState: false,
  selectedItems: [],
};

function renderTable(rows: OktaResourceConnectionServer[]): void {
  render(
    <TooltipProvider>
      <XaaReadinessTable
        rows={rows}
        selection={selection}
        locked={false}
        fallbackDeepLink={undefined}
        emptyMessage="No servers"
        onSelectionChange={() => {}}
        onReview={() => {}}
        onClear={() => {}}
      />
    </TooltipProvider>,
  );
}

afterEach(cleanup);

describe("XaaReadinessTable", () => {
  it("shows refusals on their own full-width line under the server's row", () => {
    const refused = confirmedRow(0, {
      state: "verified",
      recentRefusals: [
        { refusedBy: "service", people: 2, lastRefusedAt: new Date() },
      ],
    });
    renderTable([refused, confirmedRow(1)]);

    const notes = screen.getByRole("row", {
      name: "Recent refusals for Server 0",
    });
    expect(
      within(notes).getByText(/^The service refused 2 people in the last day/),
    ).toBeTruthy();
    const serverRow = screen
      .getByText("Server 0")
      .closest("tr") as HTMLTableRowElement;
    expect(serverRow).not.toBe(notes);
    expect(serverRow.nextElementSibling).toBe(notes);
    expect(within(serverRow).queryByText(/refused/)).toBeNull();
    expect(
      screen.queryByRole("row", { name: "Recent refusals for Server 1" }),
    ).toBeNull();
  });
});
