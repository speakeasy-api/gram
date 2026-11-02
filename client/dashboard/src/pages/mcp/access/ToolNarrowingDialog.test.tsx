import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ComponentProps } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ToolNarrowingDialog } from "./ToolNarrowingDialog";

afterEach(cleanup);

const catalog = [
  { name: "read_records", annotations: [] },
  { name: "search_records", annotations: [] },
];

describe("ToolNarrowingDialog", () => {
  it("toggles tool selection once by label and Space and saves tool names", async () => {
    const user = userEvent.setup();
    const onSave =
      vi.fn<ComponentProps<typeof ToolNarrowingDialog>["onSave"]>();
    render(
      <ToolNarrowingDialog
        serverId="server-a"
        serverName="Example server"
        catalog={catalog}
        tools={["read_records"]}
        dispositions={[]}
        pending={false}
        onSave={onSave}
        onClose={vi.fn<() => void>()}
      />,
    );
    const dialog = screen.getByRole("dialog");
    const checkbox = within(dialog).getByRole("checkbox", {
      name: "search_records",
    });
    const done = within(dialog).getByRole("button", { name: "Done" });
    expect(dialog.querySelector("button button")).toBeNull();
    expect(checkbox.getAttribute("aria-checked")).toBe("false");

    await user.click(within(dialog).getByText("search_records"));
    expect(checkbox.getAttribute("aria-checked")).toBe("true");
    expect(document.activeElement).toBe(checkbox);
    await user.click(done);
    expect(onSave).toHaveBeenCalledTimes(1);
    expect(onSave).toHaveBeenLastCalledWith({
      tools: ["read_records", "search_records"],
      dispositions: [],
    });

    checkbox.focus();
    await user.keyboard(" ");
    expect(checkbox.getAttribute("aria-checked")).toBe("false");
    expect(document.activeElement).toBe(checkbox);
    await user.click(done);
    expect(onSave).toHaveBeenCalledTimes(2);
    expect(onSave).toHaveBeenLastCalledWith({
      tools: ["read_records"],
      dispositions: [],
    });
  });

  it("disables saving while pending", async () => {
    const user = userEvent.setup();
    const onSave =
      vi.fn<ComponentProps<typeof ToolNarrowingDialog>["onSave"]>();
    render(
      <ToolNarrowingDialog
        serverId="server-a"
        catalog={catalog}
        tools={["read_records"]}
        dispositions={[]}
        pending
        onSave={onSave}
        onClose={vi.fn<() => void>()}
      />,
    );
    const done = screen.getByRole("button", { name: "Done" });
    expect(done.hasAttribute("disabled")).toBe(true);
    await user.click(done);
    expect(onSave).not.toHaveBeenCalled();
  });
});
