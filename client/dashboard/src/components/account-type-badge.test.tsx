import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { AccountTypeBadge } from "./account-type-badge";
import { TooltipProvider } from "@/components/ui/tooltip";

afterEach(cleanup);

describe("AccountTypeBadge", () => {
  it("renders a Personal badge", () => {
    render(<AccountTypeBadge accountType="personal" noTooltip />);
    const badge = screen.getByText("Personal");
    expect(badge).toBeTruthy();
    expect(badge.closest("[data-account-type='personal']")).toBeTruthy();
  });

  it("renders nothing for a team account type (team is the implied default)", () => {
    const { container } = render(
      <AccountTypeBadge accountType="team" noTooltip />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("renders nothing for an undefined account type", () => {
    const { container } = render(<AccountTypeBadge accountType={undefined} />);
    expect(container.firstChild).toBeNull();
  });

  it("renders nothing for an unrecognized account type", () => {
    const { container } = render(
      <AccountTypeBadge accountType="enterprise" noTooltip />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("still renders the label when wrapped in a tooltip", () => {
    render(
      <TooltipProvider>
        <AccountTypeBadge accountType="personal" />
      </TooltipProvider>,
    );
    expect(screen.getByText("Personal")).toBeTruthy();
  });
});
