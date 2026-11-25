import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it } from "vitest";

import { TooltipProvider } from "@/components/ui/Tooltip";
import { KillswitchUserStatusIcon } from "./KillswitchUserStatusIcon";

const ACCESS_HREF = "/org/projects/p/identities/user%3Auser-1/access";

function renderIcon(ui: JSX.Element) {
  return render(
    <TooltipProvider>
      <MemoryRouter>{ui}</MemoryRouter>
    </TooltipProvider>,
  );
}

afterEach(cleanup);

describe("KillswitchUserStatusIcon", () => {
  it("marks each state without spending a roster row on its label", () => {
    renderIcon(
      <div>
        <KillswitchUserStatusIcon
          href={ACCESS_HREF}
          badge={{
            userId: "user-1",
            affected: true,
            affectedNow: true,
            scheduled: true,
          }}
        />
        <KillswitchUserStatusIcon
          href={ACCESS_HREF}
          badge={{
            userId: "user-2",
            affected: true,
            affectedNow: false,
            scheduled: true,
          }}
        />
        <KillswitchUserStatusIcon href={ACCESS_HREF} unavailable />
      </div>,
    );

    const links = screen.getAllByRole("link");
    expect(links.map((link) => link.getAttribute("aria-label"))).toEqual([
      "Killswitch active; open this person's access",
      "Killswitch scheduled; open this person's access",
      "Killswitch status unavailable; open this person's access",
    ]);
    // The mark carries no text of its own; the state is in the hover copy.
    expect(links.every((link) => link.textContent === "")).toBe(true);
    expect(
      links.every((link) => link.getAttribute("href") === ACCESS_HREF),
    ).toBe(true);
  });

  it("says what the state means on hover, not just what it is called", async () => {
    const user = userEvent.setup();
    renderIcon(
      <KillswitchUserStatusIcon
        href={ACCESS_HREF}
        badge={{
          userId: "user-1",
          affected: true,
          affectedNow: true,
          scheduled: false,
        }}
      />,
    );

    await user.hover(screen.getByRole("link"));
    await waitFor(() =>
      expect(
        screen.getAllByText(/A capability is turned off for this person/),
      ).not.toHaveLength(0),
    );
  });

  it("still marks the row for a reader who cannot open the identity page", () => {
    renderIcon(
      <KillswitchUserStatusIcon
        href={null}
        badge={{
          userId: "user-1",
          affected: true,
          affectedNow: true,
          scheduled: false,
        }}
      />,
    );

    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.getByRole("img", { name: "Killswitch active" })).toBeTruthy();
  });

  it("renders nothing for someone under no killswitch", () => {
    const view = renderIcon(
      <KillswitchUserStatusIcon
        href={ACCESS_HREF}
        badge={{
          userId: "user-1",
          affected: false,
          affectedNow: false,
          scheduled: false,
        }}
      />,
    );
    expect(view.container.textContent).toBe("");
    expect(screen.queryByRole("link")).toBeNull();
  });
});
