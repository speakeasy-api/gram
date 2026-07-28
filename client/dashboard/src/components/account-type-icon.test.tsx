import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { AccountTypeIcon } from "./account-type-icon";
import { TooltipProvider } from "@/components/ui/tooltip";

afterEach(cleanup);

describe("AccountTypeIcon", () => {
  it("labels a personal account with the single-person glyph", () => {
    render(<AccountTypeIcon accountType="personal" noTooltip />);
    expect(screen.getByLabelText("Personal account")).toBeTruthy();
  });

  it("labels a team account with the group glyph", () => {
    render(<AccountTypeIcon accountType="team" noTooltip />);
    expect(screen.getByLabelText("Team account")).toBeTruthy();
  });

  it("falls back to a neutral owner glyph when unclassified", () => {
    render(<AccountTypeIcon accountType={undefined} />);
    expect(screen.getByLabelText("Account owner")).toBeTruthy();
  });

  it("treats an unrecognized account type as unclassified", () => {
    render(<AccountTypeIcon accountType="enterprise" />);
    expect(screen.getByLabelText("Account owner")).toBeTruthy();
  });

  it("still renders the glyph when wrapped in a tooltip", () => {
    render(
      <TooltipProvider>
        <AccountTypeIcon accountType="team" />
      </TooltipProvider>,
    );
    expect(screen.getByLabelText("Team account")).toBeTruthy();
  });
});
