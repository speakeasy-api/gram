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
const type = (text: string, target = input()) =>
  fireEvent.change(target, { target: { value: text } });
const key = (key: string, target = input(), extra = {}) =>
  fireEvent.keyDown(target, { key, ...extra });
describe("UserSearchInput", () => {
  it("keeps filter pills and typing inside one clickable search field", () => {
    setup("email:example.invalid org:Studio");
    const field = screen.getByRole("group", { name: "User search" });
    expect(field.contains(input())).toBe(true);
    expect(field.contains(edit("email", "example.invalid"))).toBe(true);
    expect(field.contains(edit("org", "Studio"))).toBe(true);
    expect(screen.queryByRole("button", { name: /Remove/ })).toBeNull();
    expect(
      field.contains(screen.getByRole("button", { name: "Search help" })),
    ).toBe(false);
    fireEvent.click(field);
    expect(document.activeElement).toBe(input());
  });
  it("clears the whole query from one X inside the field and refocuses the textbox", () => {
    function Clearable() {
      const [value, setValue] = useState("email:admin org:Studio text");
      return (
        <UserSearchInput
          value={value}
          onChange={setValue}
          onClear={() => setValue("")}
        />
      );
    }
    render(<Clearable />);
    const field = screen.getByRole("group", { name: "User search" });
    const clear = screen.getByRole("button", { name: "Clear search" });
    expect(field.contains(clear)).toBe(true);
    expect(clear.getAttribute("type")).toBe("button");
    fireEvent.click(clear);
    expect(screen.queryByRole("button", { name: /^Edit / })).toBeNull();
    expect(input()).toHaveProperty("value", "");
    expect(document.activeElement).toBe(input());
  });
  it("shows the placeholder only while the whole query is empty", () => {
    setup();
    expect(input().getAttribute("placeholder")).toBe("Search users…");
    type("email:admin");
    key("Enter");
    expect(input().hasAttribute("placeholder")).toBe(false);
    key("Backspace");
    expect(input().hasAttribute("placeholder")).toBe(true);
    type("");
    expect(input().getAttribute("placeholder")).toBe("Search users…");
  });
  it("sizes an editing pill to its text and lets only the trailing draft fill the field", () => {
    setup("first email:admin");
    fireEvent.click(edit("email", "admin"));
    const editing = screen.getByDisplayValue("email:admin");
    const bare = screen.getByDisplayValue("first");
    for (const text of [editing, bare]) {
      expect(text.hasAttribute("placeholder")).toBe(false);
      expect(text.hasAttribute("data-draft")).toBe(false);
      expect(text.getAttribute("size")).toBe(
        String((text as HTMLInputElement).value.length),
      );
    }
    expect(input().hasAttribute("placeholder")).toBe(false);
    expect(input().hasAttribute("data-draft")).toBe(true);
  });
  it("tokenizes external filters without a trailing delimiter and associates help", () => {
    setup("email:example.invalid");
    expect(edit("email", "example.invalid")).toBeTruthy();
    expect(input().getAttribute("aria-describedby")).toBe(
      screen.getByRole("status").id,
    );
    fireEvent.click(screen.getByRole("button", { name: "Search help" }));
    expect(screen.getByRole("dialog").textContent).toContain("name:");
  });
  it("reserves the help text's height under a validation error", () => {
    setup();
    const help = /^Use name:, email:, or org:/;
    expect(screen.getByRole("status").textContent).toMatch(help);
    type('"unclosed');
    const status = screen.getByRole("status");
    expect(status.textContent).not.toMatch(help);
    expect(input().getAttribute("aria-describedby")).toBe(status.id);
    // Hidden from assistive technology; it only holds the row's height.
    const spacer = status.parentElement!.querySelector('[aria-hidden="true"]');
    expect(spacer?.textContent).toMatch(help);
    expect(spacer?.classList.contains("invisible")).toBe(true);
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
  it("uses native buttons for keyboard editing without remove buttons", () => {
    setup("email:example.invalid");
    expect(edit("email", "example.invalid").tagName).toBe("BUTTON");
    expect(screen.queryByRole("button", { name: /^Remove / })).toBeNull();
    fireEvent.click(edit("email", "example.invalid"));
    key("Enter", screen.getByDisplayValue("email:example.invalid"));
    expect(edit("email", "example.invalid")).toBeTruthy();
  });
  it("backspaces from an empty draft into the adjacent pill's text", () => {
    const changed = setup("email:example.invalid org:Studio");
    expect(key("Backspace")).toBe(false);
    expect(screen.queryByRole("button", { name: /Edit org/ })).toBeNull();
    expect((input() as HTMLInputElement).value).toBe("org:Studi");
    expect(document.activeElement).toBe(input());
    expect((input() as HTMLInputElement).selectionStart).toBe(9);
    expect(changed).toHaveBeenLastCalledWith("email:example.invalid org:Studi");
    // Further deletions are native until the text is gone, then Backspace
    // enters the next pill the same way.
    type("org:St");
    expect(key("Backspace")).toBe(true);
    type("");
    expect(key("Backspace")).toBe(false);
    expect(screen.queryByRole("button", { name: /^Edit / })).toBeNull();
    expect((input() as HTMLInputElement).value).toBe("email:example.invali");
    expect(changed).toHaveBeenLastCalledWith("email:example.invali");
  });
  it("backspaces from the start of following text into the adjacent pill", () => {
    const changed = setup("first org:Studio middle email:example.invalid last");
    const middle = screen.getByDisplayValue("middle") as HTMLInputElement;
    middle.setSelectionRange(2, 2);
    expect(key("Backspace", middle)).toBe(true);
    middle.setSelectionRange(0, 0);
    expect(key("Backspace", middle)).toBe(false);
    const merged = screen.getByDisplayValue(
      "first org:Studi middle",
    ) as HTMLInputElement;
    expect(document.activeElement).toBe(merged);
    expect(merged.selectionStart).toBe("first org:Studi".length);
    expect(screen.queryByRole("button", { name: /Edit org/ })).toBeNull();
    expect(edit("email", "example.invalid")).toBeTruthy();
    expect((input() as HTMLInputElement).value).toBe("last");
    expect(changed).toHaveBeenLastCalledWith(
      "first org:Studi middle email:example.invalid last",
    );
  });
  it("backspaces into a quoted value's source text", () => {
    setup('org:"Example Studio"');
    key("Backspace");
    expect((input() as HTMLInputElement).value).toBe('org:"Example Studio');
  });
  it("undoes and redoes backspacing into a pill", () => {
    const changed = setup("email:example.invalid org:Studio");
    key("Backspace");
    key("z", input(), { ctrlKey: true });
    expect(edit("org", "Studio")).toBeTruthy();
    expect((input() as HTMLInputElement).value).toBe("");
    expect(changed).toHaveBeenLastCalledWith(
      "email:example.invalid org:Studio",
    );
    key("z", input(), { metaKey: true, shiftKey: true });
    expect(screen.queryByRole("button", { name: /Edit org/ })).toBeNull();
    expect((input() as HTMLInputElement).value).toBe("org:Studi");
    type("org:Stud");
    expect(fireEvent.keyDown(input(), { key: "z", ctrlKey: true })).toBe(true);
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
    expect((input() as HTMLInputElement).value).toBe("");
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
  it("undoes/redoes backspacing into a pill and token editing while leaving ordinary text undo native", () => {
    setup("org:Studio");
    key("Backspace");
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
    key("Backspace");
    type("org:Stu");
    expect(fireEvent.keyDown(input(), { key: "z", ctrlKey: true })).toBe(true);
    type("org:Studi"); // The input event a native text undo would deliver.
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
    key("Backspace");
    expect(changed).toHaveBeenLastCalledWith("\uFEFF org:Studi");
  });
  it("keeps the trailing native input mounted across token transformations", () => {
    setup();
    const nativeInput = input();
    type("org:Studio");
    key("Enter");
    expect(input()).toBe(nativeInput);
    key("Backspace");
    expect(input()).toBe(nativeInput);
    key("z", input(), { metaKey: true });
    expect(input()).toBe(nativeInput);
  });
  it("restores external values and clears transformation history", () => {
    const changed = vi.fn<(value: string) => void>();
    const { rerender } = render(
      <UserSearchInput value="org:Studio" onChange={changed} />,
    );
    key("Backspace");
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
  it("does not transform bare text when a later pill's absolute spans change", () => {
    setup("first org:Studio last");
    const first = screen.getByDisplayValue("first") as HTMLInputElement;
    first.focus();
    type("firstX", first);
    first.setSelectionRange(6, 6);
    expect(fireEvent.keyDown(first, { key: " " })).toBe(true);
    expect(document.activeElement).toBe(first);
    expect(first.selectionStart).toBe(6);
    expect(fireEvent.keyDown(first, { key: "z", ctrlKey: true })).toBe(true);
    type("firstX continued", first);
    expect(document.activeElement).toBe(first);
    expect(edit("org", "Studio")).toBeTruthy();
  });
  it("settles an unrelated edit by restoring it when backspacing into another pill", () => {
    const changed = setup("org:Studio name:Ada email:example.invalid");
    fireEvent.click(edit("org", "Studio"));
    type("org:Other", screen.getByDisplayValue("org:Studio"));
    key("Backspace");
    expect(edit("org", "Studio")).toBeTruthy();
    expect(edit("name", "Ada")).toBeTruthy();
    expect((input() as HTMLInputElement).value).toBe("email:example.invali");
    expect(changed).toHaveBeenLastCalledWith(
      "org:Studio name:Ada email:example.invali",
    );
    key("z", input(), { ctrlKey: true });
    expect(edit("email", "example.invalid")).toBeTruthy();
    expect(edit("org", "Studio")).toBeTruthy();
    expect(fireEvent.keyDown(input(), { key: "z", ctrlKey: true })).toBe(true);
  });
  it("merges adjacent edit text and enters the previous pill from an edit's start", () => {
    const changed = setup("org:Studio email:example.invalid");
    fireEvent.click(edit("email", "example.invalid"));
    const editing = screen.getByDisplayValue(
      "email:example.invalid",
    ) as HTMLInputElement;
    editing.setSelectionRange(0, 0);
    key("Backspace", editing);
    expect(screen.queryByRole("button", { name: /^Edit / })).toBeNull();
    expect((input() as HTMLInputElement).value).toBe(
      "org:Studi email:example.invalid",
    );
    expect((input() as HTMLInputElement).selectionStart).toBe(9);
    expect(changed).toHaveBeenLastCalledWith("org:Studi email:example.invalid");
    key("z", input(), { ctrlKey: true });
    expect(edit("org", "Studio")).toBeTruthy();
    expect(edit("email", "example.invalid")).toBeTruthy();
  });
  it.each(["org:Other", "org:"])(
    "settles %s before switching pills and preserves distinct undo",
    (draft) => {
      setup("org:Studio email:example.invalid");
      fireEvent.click(edit("org", "Studio"));
      type(draft, screen.getByDisplayValue("org:Studio"));
      fireEvent.click(edit("email", "example.invalid"));
      expect(edit("org", "Studio")).toBeTruthy();
      expect(screen.getByDisplayValue("email:example.invalid")).toBeTruthy();
      key("z", screen.getByDisplayValue("email:example.invalid"), {
        ctrlKey: true,
      });
      expect(edit("org", "Studio")).toBeTruthy();
      expect(edit("email", "example.invalid")).toBeTruthy();
      key("z", input(), { ctrlKey: true, shiftKey: true });
      type(
        "email:other.invalid",
        screen.getByDisplayValue("email:example.invalid"),
      );
      key("Escape", screen.getByDisplayValue("email:other.invalid"));
      expect(edit("org", "Studio")).toBeTruthy();
      expect(edit("email", "example.invalid")).toBeTruthy();
    },
  );
});
