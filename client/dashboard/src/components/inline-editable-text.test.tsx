import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { InlineEditableText } from "./inline-editable-text";

afterEach(cleanup);

function renderEditor({
  value = "GitHub MCP",
  onSubmit = vi.fn().mockResolvedValue(true),
}: {
  value?: string;
  onSubmit?: (value: string) => boolean | Promise<boolean>;
} = {}) {
  return render(
    <InlineEditableText
      value={value}
      onSubmit={onSubmit}
      inputLabel="Server name"
      editTitle="Rename server"
      maxLength={255}
      inputClassName="text-lg"
    >
      <span>{value}</span>
    </InlineEditableText>,
  );
}

describe("InlineEditableText", () => {
  it("exposes the visible value and enters an accessible focused input", () => {
    renderEditor();

    const button = screen.getByRole("button", { name: "GitHub MCP" });
    expect(button.getAttribute("title")).toBe("Rename server");
    fireEvent.click(button);

    const input = screen.getByRole("textbox", { name: "Server name" });
    expect(input).toBe(document.activeElement);
    expect(input.getAttribute("maxlength")).toBe("255");
  });

  it("normalizes and submits once on Enter through blur", async () => {
    const onSubmit = vi.fn().mockResolvedValue(true);
    renderEditor({ onSubmit });
    fireEvent.click(screen.getByRole("button", { name: "GitHub MCP" }));
    const input = screen.getByRole("textbox", { name: "Server name" });
    fireEvent.change(input, { target: { value: "  Engineering  " } });
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("Engineering"));
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  it("cancels with Escape without submitting", async () => {
    const onSubmit = vi.fn().mockResolvedValue(true);
    renderEditor({ onSubmit });
    fireEvent.click(screen.getByRole("button", { name: "GitHub MCP" }));
    const input = screen.getByRole("textbox", { name: "Server name" });
    fireEvent.change(input, { target: { value: "Canceled" } });
    fireEvent.keyDown(input, { key: "Escape" });

    await waitFor(() => expect(screen.queryByRole("textbox")).toBeNull());
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("skips an unchanged normalized value", async () => {
    const onSubmit = vi.fn().mockResolvedValue(true);
    renderEditor({ onSubmit });
    fireEvent.click(screen.getByRole("button", { name: "GitHub MCP" }));
    const input = screen.getByRole("textbox", { name: "Server name" });
    fireEvent.change(input, { target: { value: "  GitHub MCP  " } });
    fireEvent.blur(input);

    await waitFor(() => expect(screen.queryByRole("textbox")).toBeNull());
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("submits an empty normalized value", async () => {
    const onSubmit = vi.fn().mockResolvedValue(true);
    renderEditor({ onSubmit });
    fireEvent.click(screen.getByRole("button", { name: "GitHub MCP" }));
    const input = screen.getByRole("textbox", { name: "Server name" });
    fireEvent.change(input, { target: { value: "   " } });
    fireEvent.blur(input);

    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith(""));
  });

  it("retains the draft when submission returns false", async () => {
    renderEditor({ onSubmit: vi.fn().mockResolvedValue(false) });
    fireEvent.click(screen.getByRole("button", { name: "GitHub MCP" }));
    const input = screen.getByRole("textbox", { name: "Server name" });
    fireEvent.change(input, { target: { value: "Engineering" } });
    fireEvent.blur(input);

    await waitFor(() =>
      expect(input.getAttribute("value")).toBe("Engineering"),
    );
    expect(screen.getByRole("textbox", { name: "Server name" })).toBeTruthy();
  });

  it("prevents duplicate submissions while pending", async () => {
    let resolve!: (accepted: boolean) => void;
    const onSubmit = vi.fn().mockReturnValue(
      new Promise<boolean>((done) => {
        resolve = done;
      }),
    );
    renderEditor({ onSubmit });
    fireEvent.click(screen.getByRole("button", { name: "GitHub MCP" }));
    const input = screen.getByRole("textbox", { name: "Server name" });
    fireEvent.change(input, { target: { value: "Engineering" } });
    fireEvent.blur(input);
    fireEvent.blur(input);

    expect(onSubmit).toHaveBeenCalledTimes(1);
    resolve(true);
    await waitFor(() => expect(screen.queryByRole("textbox")).toBeNull());
  });

  it("uses the latest authoritative value for later edits", () => {
    const { rerender } = renderEditor();
    rerender(
      <InlineEditableText
        value="Updated MCP"
        onSubmit={vi.fn().mockResolvedValue(true)}
        inputLabel="Server name"
        editTitle="Rename server"
      >
        <span>Updated MCP</span>
      </InlineEditableText>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Updated MCP" }));
    expect(screen.getByRole("textbox").getAttribute("value")).toBe(
      "Updated MCP",
    );
  });
});
