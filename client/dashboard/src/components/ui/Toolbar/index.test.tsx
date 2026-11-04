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

function LaggyParent() {
  const [value, setValue] = useState("");
  parent.setValue = setValue;
  return (
    <Page.Toolbar.Search
      value={value}
      onChange={(v) => {
        parent.sent.push(v);
      }}
    />
  );
}

function setup() {
  parent.sent = [];
  render(<LaggyParent />);
  const input = screen.getByRole<HTMLInputElement>("textbox");
  const type = (text: string) => {
    fireEvent.change(input, { target: { value: text } });
    act(() => {
      vi.runAllTimers();
    });
  };
  const show = (value: string) => {
    act(() => {
      parent.setValue(value);
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

  it("adopts a value changed outside the input", () => {
    const { input, show } = setup();
    show("abc");
    expect(input.value).toBe("abc");
  });
});
