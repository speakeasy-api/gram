import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ActionPicker } from "./PolicyCenter";

afterEach(cleanup);

describe("ActionPicker", () => {
  it("limits MCP-scoped policies to flag and block", () => {
    const setAction = vi.fn();
    render(
      <ActionPicker formAction="warn" setFormAction={setAction} mcpScoped />,
    );

    const warn = screen.getByRole("radio", { name: /Warn & confirm/ });
    const block = screen.getByRole("radio", { name: /Deny the request/ });
    const quarantine = screen.getByRole("radio", {
      name: /Quarantine session/,
    });

    expect(warn.hasAttribute("disabled")).toBe(true);
    expect(quarantine.hasAttribute("disabled")).toBe(true);
    expect(block.getAttribute("aria-checked")).toBe("true");
    expect(
      screen.getAllByText("MCP-scoped policies support flag and block only."),
    ).toHaveLength(2);

    fireEvent.click(warn);
    expect(setAction).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("radio", { name: /Log for review/ }));
    expect(setAction).toHaveBeenCalledWith("flag");
  });
});
