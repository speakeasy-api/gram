import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";
import { MoreActions } from "./index";

afterEach(cleanup);

describe("MoreActions", () => {
  it("names row triggers and returns keyboard focus after closing", async () => {
    const user = userEvent.setup();
    render(
      <MoreActions
        triggerAriaLabel="Actions for Alex Morgan"
        actions={[{ label: "New killswitch…", onClick: () => {} }]}
      />,
    );

    const trigger = screen.getByRole("button", {
      name: "Actions for Alex Morgan",
    });
    trigger.focus();
    await user.keyboard("{Enter}");
    expect(
      await screen.findByRole("menuitem", { name: "New killswitch…" }),
    ).not.toBeNull();
    await user.keyboard("{Escape}");
    expect(document.activeElement).toBe(trigger);
  });
});
