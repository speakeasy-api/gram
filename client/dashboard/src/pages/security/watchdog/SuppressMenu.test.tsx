import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SuppressMenu } from "./SuppressMenu";

afterEach(cleanup);

describe("SuppressMenu", () => {
  it("opens and fires the two actions", async () => {
    const user = userEvent.setup();
    const onSuppressOnce = vi.fn<() => void>();
    const onCreateRule = vi.fn<() => void>();

    render(
      <SuppressMenu
        variant="secondary"
        onSuppressOnce={onSuppressOnce}
        onCreateRule={onCreateRule}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Suppress" }));
    await user.click(
      await screen.findByRole("menuitem", { name: "Suppress Once" }),
    );
    expect(onSuppressOnce).toHaveBeenCalledTimes(1);

    await user.click(screen.getByRole("button", { name: "Suppress" }));
    await user.click(
      await screen.findByRole("menuitem", { name: "Create Rule" }),
    );
    expect(onCreateRule).toHaveBeenCalledTimes(1);
  });

  it("disables the trigger while busy", () => {
    render(
      <SuppressMenu
        variant="secondary"
        busy
        onSuppressOnce={vi.fn<() => void>()}
        onCreateRule={vi.fn<() => void>()}
      />,
    );
    const trigger = screen.getByRole("button", { name: "Suppress" });
    expect(trigger.hasAttribute("disabled")).toBe(true);
  });
});
