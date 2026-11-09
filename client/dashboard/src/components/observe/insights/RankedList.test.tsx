import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RankedList, type RankedRow } from "./RankedList";

const ROW_HEIGHT = 32;

// happy-dom lays nothing out, so the list's height is what each test says
// and a row is always ROW_HEIGHT tall; resizing calls the observer by hand.
let listHeight = 0;
let observers: (() => void)[] = [];

class FakeResizeObserver {
  private readonly callback: () => void;
  constructor(callback: () => void) {
    this.callback = callback;
  }
  observe() {
    observers.push(this.callback);
  }
  disconnect() {
    observers = observers.filter((callback) => callback !== this.callback);
  }
}

function resizeTo(height: number) {
  listHeight = height;
  act(() => observers.forEach((callback) => callback()));
}

function rowsOf(count: number): RankedRow[] {
  return Array.from({ length: count }, (_, index) => ({
    id: `tool-${index}`,
    label: `tool-${index}`,
    value: count - index,
  }));
}

function renderList(props: { rows: RankedRow[]; fill?: boolean }) {
  return render(
    <MemoryRouter>
      <RankedList color="#000" {...props} />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  observers = [];
  listHeight = 0;
  vi.stubGlobal("ResizeObserver", FakeResizeObserver);
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockImplementation(
    function (this: HTMLElement) {
      return this.tagName === "UL" ? listHeight : 0;
    },
  );
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(
    () => ({ height: ROW_HEIGHT }) as DOMRect,
  );
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("RankedList", () => {
  it("shows five rows before Show more when it does not fill its card", () => {
    listHeight = ROW_HEIGHT * 12;
    renderList({ rows: rowsOf(20) });
    expect(screen.getAllByRole("listitem")).toHaveLength(5);
    expect(screen.getByText("5 of 20")).toBeTruthy();
  });

  it("shows as many rows as the card's height holds when it fills it", () => {
    listHeight = ROW_HEIGHT * 8 + 10;
    renderList({ rows: rowsOf(20), fill: true });
    expect(screen.getAllByRole("listitem")).toHaveLength(8);
    expect(screen.getByText("8 of 20")).toBeTruthy();
  });

  it("follows the card as it is resized", () => {
    listHeight = ROW_HEIGHT * 4;
    renderList({ rows: rowsOf(20), fill: true });
    expect(screen.getAllByRole("listitem")).toHaveLength(4);
    resizeTo(ROW_HEIGHT * 10);
    expect(screen.getAllByRole("listitem")).toHaveLength(10);
    expect(screen.getByText("10 of 20")).toBeTruthy();
  });

  it("drops Show more when every row fits", () => {
    listHeight = ROW_HEIGHT * 10;
    renderList({ rows: rowsOf(3), fill: true });
    expect(screen.getAllByRole("listitem")).toHaveLength(3);
    expect(screen.queryByText(/ of 3$/)).toBeNull();
  });

  it("reveals more rows past what fits", () => {
    listHeight = ROW_HEIGHT * 6;
    renderList({ rows: rowsOf(30), fill: true });
    fireEvent.click(screen.getByRole("button", { name: "Show 10 more" }));
    expect(screen.getAllByRole("listitem")).toHaveLength(16);
    expect(screen.getByText("16 of 30")).toBeTruthy();
  });

  it("shows at least one row in a card too short for any", () => {
    listHeight = 10;
    renderList({ rows: rowsOf(5), fill: true });
    expect(screen.getAllByRole("listitem")).toHaveLength(1);
  });
});
