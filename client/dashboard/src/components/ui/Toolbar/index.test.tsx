import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Page } from "@/components/page-layout";

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

// A parent that renders a new value only when told to, like a URL-backed
// parent whose router commits after later keystrokes.
const parent = {
  sent: [] as string[],
  setValue: (_: string) => {},
};

function LaggyParent({ debounceMs }: { debounceMs?: number }) {
  const [value, setValue] = useState("");
  parent.setValue = setValue;
  return (
    <Page.Toolbar.Search
      value={value}
      debounceMs={debounceMs}
      onChange={(v) => {
        parent.sent.push(v);
      }}
    />
  );
}

function setup(debounceMs?: number) {
  parent.sent = [];
  render(<LaggyParent debounceMs={debounceMs} />);
  const input = screen.getByRole<HTMLInputElement>("textbox");
  const type = (text: string, { flush = true } = {}) => {
    fireEvent.change(input, { target: { value: text } });
    if (flush) {
      act(() => {
        vi.runAllTimers();
      });
    }
  };
  const show = (value: string) => {
    act(() => {
      parent.setValue(value);
    });
    act(() => {
      vi.runAllTimers();
    });
  };
  return { input, type, show };
}

describe("Page.Toolbar.Search", () => {
  it("keeps keystrokes typed before a late echo arrives", () => {
    const { input, type, show } = setup();
    type("l");
    type("li");
    show("l");
    expect(input.value).toBe("li");
    show("li");
    expect(input.value).toBe("li");
    expect(parent.sent).toEqual(["l", "li"]);
  });

  it("adopts an external change after a skipped echo", () => {
    const { input, type, show } = setup();
    type("a");
    type("");
    type("a");
    show("a");
    show("");
    expect(input.value).toBe("");
    expect(parent.sent).toEqual(["a", "", "a"]);
  });

  it("adopts an external change that supersedes a pending echo", () => {
    const { input, type, show } = setup();
    type("new");
    show("external");
    expect(input.value).toBe("external");
    show("again");
    expect(input.value).toBe("again");
    expect(parent.sent).toEqual(["new"]);
  });

  it("drops unsent keystrokes when an external change lands", () => {
    const { input, type, show } = setup(150);
    type("new");
    type("newer", { flush: false });
    show("external");
    expect(input.value).toBe("external");
    expect(parent.sent).toEqual(["new"]);
  });

  it("does not swallow an external change after repeating a value", () => {
    const { input, type, show } = setup();
    show("q");
    type("");
    type("a");
    type("");
    show("");
    type("a");
    show("a");
    show("");
    expect(input.value).toBe("");
  });

  it("adopts a value changed outside the input", () => {
    const { input, show } = setup();
    show("abc");
    expect(input.value).toBe("abc");
  });
});
