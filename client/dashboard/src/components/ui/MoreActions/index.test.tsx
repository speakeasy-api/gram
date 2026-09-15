import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MoreActions } from ".";

afterEach(cleanup);

describe("MoreActions", () => {
  it("restores trigger focus when an ordinary menu close finishes", async () => {
    render(<MoreActions actions={[{ label: "Inspect", onClick: () => {} }]} />);
    const trigger = screen.getByRole("button", { name: "Open menu" });
    trigger.focus();
    fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false });

    const item = screen.getByRole("menuitem", { name: "Inspect" });
    item.focus();
    fireEvent.keyDown(item, { key: "Escape" });

    await waitFor(() => expect(document.activeElement).toBe(trigger));
  });

  it("waits to restore focus until loading and disabled are both false", () => {
    const { rerender } = render(
      <MoreActions
        actions={[{ label: "Inspect", onClick: () => {} }]}
        triggerLoading
        triggerDisabled
      />,
    );
    const trigger = screen.getByRole("button", {
      name: "Action in progress",
    });
    const focus = vi.spyOn(trigger, "focus");

    rerender(
      <MoreActions
        actions={[{ label: "Inspect", onClick: () => {} }]}
        triggerLoading={false}
        triggerDisabled
      />,
    );

    expect(focus).not.toHaveBeenCalled();

    rerender(
      <MoreActions
        actions={[{ label: "Inspect", onClick: () => {} }]}
        triggerLoading={false}
        triggerDisabled={false}
      />,
    );

    expect(focus).toHaveBeenCalledTimes(1);
  });

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
