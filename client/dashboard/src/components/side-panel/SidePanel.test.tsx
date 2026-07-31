import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitForElementToBeRemoved,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./panel-kinds", () => ({
  SidePanelKind: ({ descriptor }: { descriptor: { kind: string } }) => (
    <div data-testid="panel-body">rendered {descriptor.kind}</div>
  ),
}));

import { SidePanelProvider, SidePanelSurface } from "./SidePanel";
import {
  clampSidePanelWidth,
  SIDE_PANEL_MAX_WIDTH,
  SIDE_PANEL_MIN_WIDTH,
  SIDE_PANEL_WIDTH_KEY,
  useSidePanel,
} from "./side-panel-context";

function OpenButton() {
  const { openPanel } = useSidePanel();
  return (
    <button
      type="button"
      onClick={() =>
        openPanel({
          kind: "setup-guide",
          title: "Setup Guide: Box",
          props: { registrySpecifier: "com.pulsemcp.mirror/box" },
        })
      }
    >
      open
    </button>
  );
}

function harness() {
  return render(
    <SidePanelProvider>
      <OpenButton />
      <SidePanelSurface />
    </SidePanelProvider>,
  );
}

function setViewportWidth(width: number) {
  Object.defineProperty(window, "innerWidth", {
    configurable: true,
    value: width,
  });
}

afterEach(cleanup);

beforeEach(() => {
  window.localStorage.clear();
  setViewportWidth(1600);
});

describe("clampSidePanelWidth", () => {
  it("leaves a comfortable viewport untouched", () => {
    expect(clampSidePanelWidth(560, 1600)).toBe(560);
    expect(clampSidePanelWidth(420, 1600)).toBe(420);
  });

  it("caps the panel at its maximum however wide the screen is", () => {
    expect(clampSidePanelWidth(900, 2560)).toBe(SIDE_PANEL_MAX_WIDTH);
  });

  it("makes the panel yield first as the viewport narrows", () => {
    // 1280 - 256 sidebar - 480 page floor = 544 left for the panel.
    expect(clampSidePanelWidth(560, 1280)).toBe(544);
  });

  it("stops yielding at the panel's own minimum", () => {
    // Past this point the page absorbs the squeeze rather than the panel
    // shrinking into uselessness.
    expect(clampSidePanelWidth(560, 900)).toBe(SIDE_PANEL_MIN_WIDTH);
  });
});

describe("SidePanelSurface", () => {
  it("renders nothing until a panel is opened", () => {
    harness();

    expect(screen.queryByRole("complementary")).toBeNull();
  });

  it("opens at its maximum width with the caller's title", () => {
    harness();
    fireEvent.click(screen.getByRole("button", { name: "open" }));

    const panel = screen.getByRole("complementary", {
      name: "Setup Guide: Box",
    });
    expect(panel.style.width).toBe(`${SIDE_PANEL_MAX_WIDTH}px`);
    expect(screen.getByTestId("panel-body").textContent).toBe(
      "rendered setup-guide",
    );
  });

  it("closes from the header button", async () => {
    harness();
    fireEvent.click(screen.getByRole("button", { name: "open" }));
    fireEvent.click(screen.getByRole("button", { name: "Close panel" }));

    // Outlives the click by one collapse: unmounting on the spot would snap
    // the page back to full width while the panel was still on screen.
    const panel = screen.getByRole("complementary");
    expect(panel.hasAttribute("inert")).toBe(true);
    await waitForElementToBeRemoved(panel);
  });

  it("closes on Escape from inside the panel", async () => {
    harness();
    fireEvent.click(screen.getByRole("button", { name: "open" }));

    fireEvent.keyDown(screen.getByTestId("panel-body"), { key: "Escape" });

    await waitForElementToBeRemoved(screen.getByRole("complementary"));
  });

  it("leaves Escape alone outside the panel, so it stays free for dialogs", () => {
    harness();
    fireEvent.click(screen.getByRole("button", { name: "open" }));

    fireEvent.keyDown(screen.getByRole("button", { name: "open" }), {
      key: "Escape",
    });

    expect(screen.queryByRole("complementary")).toBeTruthy();
  });

  it("resizes by keyboard and remembers the width", () => {
    harness();
    fireEvent.click(screen.getByRole("button", { name: "open" }));

    const handle = screen.getByRole("separator", { name: "Resize panel" });
    // Right narrows: the panel grows leftwards, into the page.
    fireEvent.keyDown(handle, { key: "ArrowRight" });

    const narrowed = SIDE_PANEL_MAX_WIDTH - 16;
    expect(
      screen.getByRole("complementary", { name: "Setup Guide: Box" }).style
        .width,
    ).toBe(`${narrowed}px`);
    expect(window.localStorage.getItem(SIDE_PANEL_WIDTH_KEY)).toBe(
      String(narrowed),
    );
  });

  it("resizes by dragging, committing the width only on release", () => {
    harness();
    fireEvent.click(screen.getByRole("button", { name: "open" }));

    const handle = screen.getByRole("separator", { name: "Resize panel" });
    handle.setPointerCapture = () => {};
    handle.releasePointerCapture = () => {};

    // Dispatched back to back, as they can arrive in one task: the handlers
    // must not depend on a re-render having landed between them.
    fireEvent.pointerDown(handle, { clientX: 800, pointerId: 1 });
    fireEvent.pointerMove(handle, { clientX: 900, pointerId: 1 });

    const narrowed = SIDE_PANEL_MAX_WIDTH - 100;
    expect(
      screen.getByRole("complementary", { name: "Setup Guide: Box" }).style
        .width,
    ).toBe(`${narrowed}px`);
    expect(window.localStorage.getItem(SIDE_PANEL_WIDTH_KEY)).toBeNull();

    fireEvent.pointerUp(handle, { clientX: 900, pointerId: 1 });

    expect(window.localStorage.getItem(SIDE_PANEL_WIDTH_KEY)).toBe(
      String(narrowed),
    );
  });

  it("will not let the keyboard push the panel past its bounds", () => {
    harness();
    fireEvent.click(screen.getByRole("button", { name: "open" }));

    const handle = screen.getByRole("separator", { name: "Resize panel" });
    fireEvent.keyDown(handle, { key: "ArrowLeft" });

    expect(
      screen.getByRole("complementary", { name: "Setup Guide: Box" }).style
        .width,
    ).toBe(`${SIDE_PANEL_MAX_WIDTH}px`);
    expect(handle.getAttribute("aria-valuemin")).toBe(
      String(SIDE_PANEL_MIN_WIDTH),
    );
  });
});
