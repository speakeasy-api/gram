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
  it("preserves the default target and spacing", () => {
    render(<MoreActions actions={[]} />);
    const trigger = screen.getByRole("button", { name: "Open menu" });
    expect(trigger.className).toContain("h-8 w-8");
    expect(trigger.className).toContain("mx-[-4px]");
    expect(trigger.style.marginInlineEnd).toBe("");
  });

  it.each([
    ["default", 32],
    ["compact", 24],
  ] as const)(
    "aligns %s dots at the edge without shrinking the target",
    (size, pixels) => {
      render(<MoreActions actions={[]} size={size} align="end" />);
      const trigger = screen.getByRole("button", { name: "Open menu" });
      expect(trigger.className).toContain(
        size === "compact" ? "h-6 w-6" : "h-8 w-8",
      );
      expect(trigger.className).not.toContain("mx-[-4px]");
      expect(parseFloat(trigger.style.marginInlineEnd)).toBeCloseTo(
        -(pixels / 2 - (2 * 14) / 24),
      );
      expect(trigger.style.transform).toBe("");
    },
  );

  it("keeps labelled triggers unchanged by icon-only geometry", () => {
    render(
      <MoreActions
        actions={[]}
        triggerLabel="Actions"
        size="compact"
        align="end"
      />,
    );
    const trigger = screen.getByRole("button", { name: "Actions" });
    expect(trigger.className).toContain("h-8");
    expect(trigger.style.marginInlineEnd).toBe("");
  });

  it("keeps compact edge triggers keyboard accessible", async () => {
    const user = userEvent.setup();
    const onClick = vi.fn<() => void>();
    render(
      <MoreActions
        actions={[{ label: "Inspect", onClick }]}
        size="compact"
        align="end"
        triggerAriaLabel="Task actions"
      />,
    );
    const trigger = screen.getByRole("button", { name: "Task actions" });
    await user.tab();
    expect(document.activeElement).toBe(trigger);
    await user.keyboard("{Enter}");
    expect(
      await screen.findByRole("menuitem", { name: "Inspect" }),
    ).not.toBeNull();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(document.activeElement).toBe(trigger));
    expect(onClick).not.toHaveBeenCalled();
  });

  it("disables compact edge triggers during loading", () => {
    render(
      <MoreActions actions={[]} size="compact" align="end" triggerLoading />,
    );
    const trigger = screen.getByRole("button", { name: "Action in progress" });
    expect(trigger.hasAttribute("disabled")).toBe(true);
    expect(trigger.getAttribute("aria-busy")).toBe("true");
  });

  it("renders a separator before marked actions", () => {
    render(
      <MoreActions
        actions={[
          { label: "Inspect", onClick: () => {} },
          { label: "Change status", onClick: () => {}, separatorBefore: true },
        ]}
      />,
    );
    fireEvent.pointerDown(screen.getByRole("button", { name: "Open menu" }), {
      button: 0,
      ctrlKey: false,
    });

    const separator = screen.getByRole("separator");
    const markedAction = screen.getByRole("menuitem", {
      name: "Change status",
    });
    expect(markedAction.previousElementSibling).toBe(separator);
  });

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
