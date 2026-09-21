import type { ComponentProps } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import { XaaBulkConfirmBar } from "./XaaBulkConfirmBar";
import type { XaaConfirmHandler } from "./XaaConfirmFields";

type BarProps = ComponentProps<typeof XaaBulkConfirmBar>;

function renderBar(overrides: Partial<BarProps> = {}) {
  const onConfirm = vi.fn<XaaConfirmHandler>();
  const props: BarProps = {
    selectedCount: 1,
    applications: [],
    pending: false,
    onConfirm,
    ...overrides,
  };
  const view = render(<XaaBulkConfirmBar {...props} />);
  return {
    onConfirm,
    update: (next: Partial<BarProps>) =>
      view.rerender(<XaaBulkConfirmBar {...props} {...next} />),
  };
}

const issuerInput = () => screen.getByLabelText("Issuer URL");
const submit = (name: string) => screen.getByRole("button", { name });

afterEach(cleanup);

describe("XaaBulkConfirmBar", () => {
  it("blocks confirmation until a valid audience is entered", () => {
    const { onConfirm } = renderBar({ selectedCount: 3 });
    expect(screen.getByText("Confirm 3 selected servers")).toBeTruthy();
    const button = submit("Confirm 3 servers");
    expect(button.hasAttribute("disabled")).toBe(true);

    fireEvent.change(issuerInput(), {
      target: { value: "http://auth.example.com" },
    });
    expect(issuerInput().getAttribute("aria-invalid")).toBe("true");
    expect(button.hasAttribute("disabled")).toBe(true);
    expect(screen.getByRole("alert").textContent).toContain(
      "Enter the HTTPS Issuer URL",
    );
    fireEvent.keyDown(issuerInput(), { key: "Enter" });
    expect(onConfirm).not.toHaveBeenCalled();

    fireEvent.change(issuerInput(), {
      target: { value: "https://auth.example.com" },
    });
    expect(issuerInput().getAttribute("aria-invalid")).toBe("false");
    expect(button.hasAttribute("disabled")).toBe(false);

    fireEvent.click(button);
    expect(onConfirm).toHaveBeenCalledWith(
      "https://auth.example.com",
      undefined,
    );
  });

  it("guards Enter while pending and freezes both inputs", () => {
    const { onConfirm, update } = renderBar();
    fireEvent.change(issuerInput(), {
      target: { value: "https://auth.example.com" },
    });
    fireEvent.keyDown(issuerInput(), { key: "Enter" });
    expect(onConfirm).toHaveBeenCalledTimes(1);
    update({ pending: true });
    expect(issuerInput().hasAttribute("disabled")).toBe(true);
    expect(screen.getByRole("combobox").hasAttribute("disabled")).toBe(true);
    fireEvent.keyDown(issuerInput(), { key: "Enter" });
    expect(onConfirm).toHaveBeenCalledTimes(1);
    update({ selectedCount: 0 });
    fireEvent.keyDown(issuerInput(), { key: "Enter" });
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("blocks a removed app instance until the administrator chooses again", () => {
    const { onConfirm, update } = renderBar({
      applications: [{ id: "app", label: "Resource app" }],
    });
    fireEvent.change(issuerInput(), {
      target: { value: "https://issuer.example.com" },
    });
    fireEvent.keyDown(screen.getByRole("combobox"), { key: "ArrowDown" });
    fireEvent.click(screen.getByRole("option", { name: "Resource app" }));
    update({ applications: [] });
    expect(screen.getByRole("alert").textContent).toContain(
      "no longer available",
    );
    expect(submit("Confirm 1 server").hasAttribute("disabled")).toBe(true);
    fireEvent.keyDown(issuerInput(), { key: "Enter" });
    expect(onConfirm).not.toHaveBeenCalled();
    fireEvent.keyDown(screen.getByRole("combobox"), { key: "ArrowDown" });
    fireEvent.click(screen.getByRole("option", { name: "Not recorded" }));
    fireEvent.click(submit("Confirm 1 server"));
    expect(onConfirm).toHaveBeenCalledWith(
      "https://issuer.example.com",
      undefined,
    );
  });
});
