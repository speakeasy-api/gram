import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { UserSearchInput } from "./UserSearchInput";
afterEach(cleanup);
function setup(initial = "") {
  const changed = vi.fn<(value: string) => void>();
  function Controlled() {
    const [value, setValue] = useState(initial);
    return (
      <UserSearchInput
        value={value}
        onChange={(next) => {
          changed(next);
          setValue(next);
        }}
      />
    );
  }
  render(<Controlled />);
  return changed;
}
const input = () => screen.getByRole("textbox", { name: "Search users" });
const edit = (field: string, value: string) =>
  screen.getByRole("button", { name: `Edit ${field} filter: ${value}` });
const remove = (field: string, value: string) =>
  screen.getByRole("button", { name: `Remove ${field} filter: ${value}` });
const type = (text: string, target = input()) =>
  fireEvent.change(target, { target: { value: text } });
const key = (key: string, target = input(), extra = {}) =>
  fireEvent.keyDown(target, { key, ...extra });
describe("UserSearchInput", () => {
  it("tokenizes external filters without a trailing delimiter and associates help", () => {
    setup("email:example.invalid");
    expect(edit("email", "example.invalid")).toBeTruthy();
    expect(input().getAttribute("aria-describedby")).toBe(
      screen.getByRole("status").id,
    );
    fireEvent.click(screen.getByRole("button", { name: "Search help" }));
    expect(screen.getByRole("dialog").textContent).toContain("name:");
  });
  it.each([" ", "Enter"])(
    "commits complete filters on %s but leaves bare text editable",
    (delimiter) => {
      setup();
      type("email:example.invalid");
      key(delimiter);
      expect(edit("email", "example.invalid")).toBeTruthy();
      type("ordinary");
      key("Enter");
      expect((input() as HTMLInputElement).value).toBe("ordinary");
    },
  );
  it("waits for a closing quote and does not consume quoted spaces", () => {
    setup();
    type('org:"Example');
    expect(fireEvent.keyDown(input(), { key: " " })).toBe(true);
    expect(screen.queryByRole("button", { name: /Edit org/ })).toBeNull();
    type('org:"Example Studio"');
    key("Enter");
    expect(edit("org", "Example Studio")).toBeTruthy();
  });
  it.each(["org:", 'org:"unfinished', "unknown:value", "OR"])(
    "keeps invalid/incomplete %s editable with an accessible hint",
    (value) => {
      setup();
      type(value);
      key("Enter");
      expect((input() as HTMLInputElement).value).toBe(value);
      expect(input().getAttribute("aria-invalid")).toBe("true");
      expect(screen.getByRole("status").textContent).not.toBe("");
      expect(screen.queryByRole("button", { name: /^Edit / })).toBeNull();
    },
  );
  it("keeps own echoes in edit mode and Escape restores the original ordered term", () => {
    const changed = setup("email:first.invalid org:Studio");
    fireEvent.click(edit("email", "first.invalid"));
    const editing = screen.getByDisplayValue("email:first.invalid");
    expect(document.activeElement).toBe(editing);
    type("email:second.invalid", editing);
    expect(screen.queryByRole("button", { name: /Edit email/ })).toBeNull();
    key("Escape", editing);
    expect(edit("email", "first.invalid")).toBeTruthy();
    expect(changed).toHaveBeenLastCalledWith("email:first.invalid org:Studio");
  });
  it("uses native buttons for keyboard editing and returns focus after removal", () => {
    setup("email:example.invalid");
    expect(edit("email", "example.invalid").tagName).toBe("BUTTON");
    fireEvent.click(edit("email", "example.invalid"));
    key("Enter", screen.getByDisplayValue("email:example.invalid"));
    fireEvent.click(remove("email", "example.invalid"));
    expect(document.activeElement).toBe(input());
  });
  it("selects the final pill on Backspace before removing it", () => {
    const changed = setup("email:example.invalid org:Studio");
    key("Backspace");
    expect(document.activeElement).toBe(edit("org", "Studio"));
    expect(screen.getByRole("status").textContent).toContain(
      "Backspace to remove",
    );
    expect(changed).not.toHaveBeenCalled();
    key("Backspace", document.activeElement as HTMLElement);
    expect(screen.queryByRole("button", { name: /Edit org/ })).toBeNull();
    expect(edit("email", "example.invalid")).toBeTruthy();
    expect(document.activeElement).toBe(input());
  });
  it("ignores commit and deletion during composition", () => {
    setup();
    fireEvent.compositionStart(input());
    type("org:Studio");
    key("Enter");
    expect(screen.queryByRole("button", { name: /Edit org/ })).toBeNull();
    fireEvent.compositionEnd(input());
    key("Enter");
    expect(edit("org", "Studio")).toBeTruthy();
    fireEvent.compositionStart(input());
    key("Backspace");
    expect(edit("org", "Studio")).toBeTruthy();
    expect(document.activeElement).not.toBe(edit("org", "Studio"));
  });
  it("preserves paste without requiring a trailing space", () => {
    const changed = setup();
    fireEvent.paste(input(), {
      clipboardData: {
        getData: () => 'org:"Example Studio" email:example.invalid',
      },
    });
    expect(edit("org", "Example Studio")).toBeTruthy();
    expect(edit("email", "example.invalid")).toBeTruthy();
    expect(changed).toHaveBeenLastCalledWith(
      'org:"Example Studio" email:example.invalid',
    );
  });
  it("undoes/redoes removal and token editing while leaving ordinary text undo native", () => {
    setup("org:Studio");
    fireEvent.click(remove("org", "Studio"));
    key("z", input(), { ctrlKey: true });
    expect(edit("org", "Studio")).toBeTruthy();
    key("z", input(), { metaKey: true, shiftKey: true });
    expect(screen.queryByRole("button", { name: /Edit org/ })).toBeNull();
    key("z", input(), { metaKey: true });
    fireEvent.click(edit("org", "Studio"));
    const editing = screen.getByDisplayValue("org:Studio");
    type("org:Other", editing);
    key("Enter", editing);
    key("z", input(), { ctrlKey: true });
    expect(edit("org", "Studio")).toBeTruthy();
    type("ordinary text");
    expect(fireEvent.keyDown(input(), { key: "z", ctrlKey: true })).toBe(true);
  });
  it("preserves bare text and filter order when editing the middle filter", () => {
    const changed = setup(
      'first org:"Example Studio" middle email:example.invalid last',
    );
    expect(screen.getByDisplayValue("first")).toBeTruthy();
    expect(screen.getByDisplayValue("middle")).toBeTruthy();
    fireEvent.click(edit("org", "Example Studio"));
    const editing = screen.getByDisplayValue('org:"Example Studio"');
    type('org:"Other Studio"', editing);
    key("Enter", editing);
    expect(changed).toHaveBeenLastCalledWith(
      'first org:"Other Studio" middle email:example.invalid last',
    );
  });
  it("undoes token creation and redoes it, without consuming native text undo", () => {
    setup();
    type("org:Studio");
    key("Enter");
    key("z", input(), { metaKey: true });
    expect(screen.getByDisplayValue("org:Studio")).toBeTruthy();
    key("y", input(), { ctrlKey: true });
    expect(edit("org", "Studio")).toBeTruthy();
    type("next");
    expect(fireEvent.keyDown(input(), { key: "z", metaKey: true })).toBe(true);
  });
  it("does not tokenize at a caret inside a value or during native composition", () => {
    setup();
    type("org:Studio");
    (input() as HTMLInputElement).setSelectionRange(5, 5);
    expect(fireEvent.keyDown(input(), { key: " " })).toBe(true);
    expect(
      fireEvent.keyDown(input(), { key: "Enter", isComposing: true }),
    ).toBe(true);
    expect(screen.queryByRole("button", { name: /Edit org/ })).toBeNull();
  });
  it("preserves minimal escaping when editing and committing a quoted literal", () => {
    const changed = setup('org:"OR"');
    fireEvent.click(edit("org", "OR"));
    expect(screen.getByDisplayValue('org:"OR"')).toBeTruthy();
    key("Enter", screen.getByDisplayValue('org:"OR"'));
    expect(edit("org", "OR")).toBeTruthy();
    expect(changed).not.toHaveBeenCalled();
  });
  it("replaces only the selected text on paste and lets invalid paste stay native", () => {
    const changed = setup("prefix suffix");
    (input() as HTMLInputElement).setSelectionRange(7, 13);
    fireEvent.paste(input(), {
      clipboardData: { getData: () => "org:Studio" },
    });
    expect(changed).toHaveBeenLastCalledWith("prefix org:Studio");
    expect(edit("org", "Studio")).toBeTruthy();
    expect(
      fireEvent.paste(input(), {
        clipboardData: { getData: () => 'org:"unfinished' },
      }),
    ).toBe(true);
  });
  it("allows native text undo to return to a transformation boundary", () => {
    setup("org:Studio");
    fireEvent.click(remove("org", "Studio"));
    type("ordinary");
    expect(fireEvent.keyDown(input(), { key: "z", ctrlKey: true })).toBe(true);
    type(""); // The input event a native text undo would deliver.
    key("z", input(), { ctrlKey: true });
    expect(edit("org", "Studio")).toBeTruthy();
  });
  it("canceling an edit does not add a no-op undo entry", () => {
    setup("org:Studio");
    fireEvent.click(edit("org", "Studio"));
    type("org:Other", screen.getByDisplayValue("org:Studio"));
    key("Escape", screen.getByDisplayValue("org:Other"));
    expect(fireEvent.keyDown(input(), { key: "z", ctrlKey: true })).toBe(true);
  });
  it("uses parser spans rather than JavaScript trim for literal text", () => {
    const changed = setup("\uFEFF org:Studio");
    expect(
      (
        screen.getByRole("textbox", {
          name: "Search text before filter 1",
        }) as HTMLInputElement
      ).value,
    ).toBe("\uFEFF");
    fireEvent.click(remove("org", "Studio"));
    expect(changed).toHaveBeenLastCalledWith("\uFEFF");
  });
  it("keeps the trailing native input mounted across token transformations", () => {
    setup();
    const nativeInput = input();
    type("org:Studio");
    key("Enter");
    expect(input()).toBe(nativeInput);
    fireEvent.click(remove("org", "Studio"));
    expect(input()).toBe(nativeInput);
    key("z", input(), { metaKey: true });
    expect(input()).toBe(nativeInput);
  });
  it("restores external values and clears transformation history", () => {
    const changed = vi.fn<(value: string) => void>();
    const { rerender } = render(
      <UserSearchInput value="org:Studio" onChange={changed} />,
    );
    fireEvent.click(remove("org", "Studio"));
    rerender(
      <UserSearchInput
        value="email:restored.invalid"
        onChange={changed}
        error="Server rejected this search"
      />,
    );
    expect(edit("email", "restored.invalid")).toBeTruthy();
    expect(screen.getByRole("status").textContent).toBe(
      "Server rejected this search",
    );
    expect(fireEvent.keyDown(input(), { key: "z", ctrlKey: true })).toBe(true);
    expect(changed).toHaveBeenCalledTimes(1);
  });
});
