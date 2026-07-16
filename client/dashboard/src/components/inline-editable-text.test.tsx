import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { FormEvent } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { InlineEditableText } from "./inline-editable-text";

afterEach(cleanup);

function renderEditor({
  value = "GitHub MCP",
  onSubmit = vi.fn().mockResolvedValue(true),
  editorClassName,
}: {
  value?: string;
  onSubmit?: (value: string) => boolean | Promise<boolean>;
  editorClassName?: string;
} = {}) {
  return render(
    <InlineEditableText
      value={value}
      onSubmit={onSubmit}
      inputLabel="Server name"
      editTitle="Rename server"
      maxLength={255}
      editorClassName={editorClassName}
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

  it("uses the shared card-style editor shell", () => {
    renderEditor({ editorClassName: "w-96" });
    fireEvent.click(screen.getByRole("button", { name: "GitHub MCP" }));

    const input = screen.getByRole("textbox", { name: "Server name" });
    const editor = input.closest('[data-slot="input-group"]');

    expect(editor).not.toBeNull();
    expect(editor?.classList).toContain("bg-card");
    expect(editor?.classList).toContain("border-border");
    expect(editor?.classList).toContain("h-10");
    expect(editor?.classList).toContain("rounded-md");
    expect(editor?.classList).toContain("shadow-none");
    expect(editor?.classList).toContain(
      "has-[[data-slot=input-group-control]:focus-visible]:ring-1",
    );
    expect(editor?.classList).toContain(
      "has-[[data-slot=input-group-control]:focus-visible]:ring-ring/30",
    );
    expect(editor?.classList).toContain("w-96");
  });

  it("shows the save action only for a normalized difference", () => {
    renderEditor();
    fireEvent.click(screen.getByRole("button", { name: "GitHub MCP" }));
    const input = screen.getByRole("textbox", { name: "Server name" });

    expect(screen.queryByRole("button", { name: "Save change" })).toBeNull();

    fireEvent.change(input, { target: { value: "  GitHub MCP  " } });
    expect(screen.queryByRole("button", { name: "Save change" })).toBeNull();

    fireEvent.change(input, { target: { value: "Engineering" } });
    const saveButton = screen.getByRole("button", { name: "Save change" });
    expect(saveButton.getAttribute("title")).toBe("Save change");
    expect(saveButton.querySelector(".lucide-check")).not.toBeNull();
  });

  it("keeps input focus on save mouse-down and submits once on click", async () => {
    const onSubmit = vi.fn().mockResolvedValue(true);
    renderEditor({ onSubmit });
    fireEvent.click(screen.getByRole("button", { name: "GitHub MCP" }));
    const input = screen.getByRole("textbox", { name: "Server name" });
    fireEvent.change(input, { target: { value: "Engineering" } });
    const saveButton = screen.getByRole("button", { name: "Save change" });

    expect(fireEvent.mouseDown(saveButton)).toBe(false);
    expect(document.activeElement).toBe(input);
    fireEvent.click(saveButton);

    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("Engineering"));
    expect(onSubmit).toHaveBeenCalledTimes(1);
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

  it("prevents Enter from submitting an enclosing form", async () => {
    const onSubmit = vi.fn().mockResolvedValue(true);
    const onFormSubmit = vi.fn((event: FormEvent) => event.preventDefault());
    render(
      <form onSubmit={onFormSubmit}>
        <InlineEditableText
          value="GitHub MCP"
          onSubmit={onSubmit}
          inputLabel="Server name"
          editTitle="Rename server"
        >
          <span>GitHub MCP</span>
        </InlineEditableText>
      </form>,
    );
    fireEvent.click(screen.getByRole("button", { name: "GitHub MCP" }));
    const input = screen.getByRole("textbox", { name: "Server name" });
    fireEvent.change(input, { target: { value: "Engineering" } });

    expect(fireEvent.keyDown(input, { key: "Enter" })).toBe(false);

    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("Engineering"));
    expect(onFormSubmit).not.toHaveBeenCalled();
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
    expect(screen.getByRole("button", { name: "Save change" })).toBeTruthy();
  });

  it("retains the draft and recovers when submission rejects", async () => {
    renderEditor({
      onSubmit: vi.fn().mockRejectedValue(new Error("save failed")),
    });
    fireEvent.click(screen.getByRole("button", { name: "GitHub MCP" }));
    const input = screen.getByRole("textbox", { name: "Server name" });
    fireEvent.change(input, { target: { value: "Engineering" } });
    fireEvent.blur(input);

    await waitFor(() => expect(input.hasAttribute("disabled")).toBe(false));
    expect(input.getAttribute("value")).toBe("Engineering");
    expect(screen.getByRole("textbox", { name: "Server name" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Save change" })).toBeTruthy();
  });

  it("disables the save action while submission is pending", async () => {
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
    const saveButton = screen.getByRole("button", { name: "Save change" });
    fireEvent.click(saveButton);

    await waitFor(() => expect(saveButton.hasAttribute("disabled")).toBe(true));
    resolve(false);
    await waitFor(() =>
      expect(saveButton.hasAttribute("disabled")).toBe(false),
    );
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
