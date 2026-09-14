import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TagInput } from ".";

function Harness({
  initial = [],
  onChange,
  separateOnSpace,
}: {
  initial?: string[];
  onChange?: (value: string[]) => void;
  separateOnSpace?: boolean;
}): JSX.Element {
  const [value, setValue] = useState(initial);
  return (
    <>
      <label htmlFor="tags">Tags</label>
      <TagInput
        id="tags"
        value={value}
        onChange={(next) => {
          setValue(next);
          onChange?.(next);
        }}
        placeholder="Add a tag"
        separateOnSpace={separateOnSpace}
      />
    </>
  );
}

describe("TagInput", () => {
  afterEach(cleanup);

  it("turns typed text into a tag on comma, Enter, or blur", () => {
    const onChange = vi.fn<(value: string[]) => void>();
    render(<Harness onChange={onChange} />);
    const input = screen.getByLabelText("Tags");

    fireEvent.change(input, { target: { value: "claude" } });
    fireEvent.keyDown(input, { key: "," });
    expect(screen.getByText("claude")).toBeDefined();

    fireEvent.change(input, { target: { value: "codex" } });
    fireEvent.keyDown(input, { key: "Enter" });
    fireEvent.change(input, { target: { value: "aider" } });
    fireEvent.blur(input);
    expect(onChange).toHaveBeenLastCalledWith(["claude", "codex", "aider"]);
    expect((input as HTMLInputElement).value).toBe("");
  });

  it("treats space as a separator only when asked", () => {
    const onChange = vi.fn<(value: string[]) => void>();
    render(<Harness onChange={onChange} />);
    const input = screen.getByLabelText("Tags");
    fireEvent.change(input, { target: { value: "LM" } });
    fireEvent.keyDown(input, { key: " " });
    expect(onChange).not.toHaveBeenCalled();
    cleanup();

    render(<Harness onChange={onChange} separateOnSpace />);
    const spaced = screen.getByLabelText("Tags");
    fireEvent.change(spaced, { target: { value: "claude" } });
    fireEvent.keyDown(spaced, { key: " " });
    expect(onChange).toHaveBeenLastCalledWith(["claude"]);
    fireEvent.paste(spaced, {
      clipboardData: { getData: () => "codex aider" },
    });
    expect(onChange).toHaveBeenLastCalledWith(["claude", "codex", "aider"]);
  });

  it("drops duplicates and ignores an empty comma", () => {
    const onChange = vi.fn<(value: string[]) => void>();
    render(<Harness initial={["claude"]} onChange={onChange} />);
    const input = screen.getByLabelText("Tags");

    fireEvent.keyDown(input, { key: "," });
    fireEvent.change(input, { target: { value: " claude " } });
    fireEvent.keyDown(input, { key: "," });
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getAllByText("claude")).toHaveLength(1);
  });

  it("splits pasted lists", () => {
    const onChange = vi.fn<(value: string[]) => void>();
    render(<Harness onChange={onChange} />);
    const input = screen.getByLabelText("Tags");

    fireEvent.paste(input, {
      clipboardData: { getData: () => "cursor, cursor helper\nwindsurf" },
    });
    expect(onChange).toHaveBeenLastCalledWith([
      "cursor",
      "cursor helper",
      "windsurf",
    ]);
  });

  it("removes tags from the chip button and with Backspace on an empty input", () => {
    const onChange = vi.fn<(value: string[]) => void>();
    render(<Harness initial={["claude", "codex"]} onChange={onChange} />);

    fireEvent.click(screen.getByRole("button", { name: "Remove claude" }));
    expect(onChange).toHaveBeenLastCalledWith(["codex"]);

    fireEvent.keyDown(screen.getByLabelText("Tags"), { key: "Backspace" });
    expect(onChange).toHaveBeenLastCalledWith([]);
  });
});
