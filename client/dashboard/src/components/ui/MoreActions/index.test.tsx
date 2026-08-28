import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MoreActions } from ".";

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
});
