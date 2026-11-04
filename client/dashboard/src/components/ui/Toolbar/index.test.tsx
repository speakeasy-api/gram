import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { useRef, useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Page } from "@/components/page-layout";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

// Echoes each onChange back only when flush() runs, like a URL-backed parent
// whose router navigation commits after later keystrokes.
function LaggyParent({ onFlush }: { onFlush: (flush: () => void) => void }) {
  const [value, setValue] = useState("");
  const queue = useRef<string[]>([]).current;
  onFlush(() => {
    const next = queue.shift();
    if (next !== undefined) setValue(next);
  });
  return <Page.Toolbar.Search value={value} onChange={(v) => queue.push(v)} />;
}

describe("Page.Toolbar.Search", () => {
  it("keeps keystrokes typed before a late echo arrives", () => {
    vi.useFakeTimers();
    let flush = () => {};
    render(<LaggyParent onFlush={(f) => (flush = f)} />);
    const input = screen.getByRole<HTMLInputElement>("textbox");

    fireEvent.change(input, { target: { value: "l" } });
    act(() => vi.runAllTimers());
    fireEvent.change(input, { target: { value: "li" } });
    act(() => vi.runAllTimers());

    act(() => flush());
    expect(input.value).toBe("li");
  });

  it("adopts a value changed outside the input", () => {
    const { rerender } = render(
      <Page.Toolbar.Search value="abc" onChange={() => {}} />,
    );
    rerender(<Page.Toolbar.Search value="" onChange={() => {}} />);
    expect(screen.getByRole<HTMLInputElement>("textbox").value).toBe("");
  });
});
