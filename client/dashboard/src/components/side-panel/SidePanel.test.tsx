import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitForElementToBeRemoved,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ useIsMobile: vi.fn() }));

vi.mock("./panel-kinds", () => ({
  SidePanelKind: ({ descriptor }: { descriptor: { kind: string } }) => (
    <div data-testid="panel-body">rendered {descriptor.kind}</div>
  ),
}));

vi.mock("@/hooks/use-mobile", () => ({
  useIsMobile: mocks.useIsMobile,
}));

import { SidePanelProvider, SidePanelSurface } from "./SidePanel";
import {
  clampSidePanelWidth,
  SIDE_PANEL_MAX_WIDTH,
  SIDE_PANEL_MIN_WIDTH,
  SIDE_PANEL_WIDTH_KEY,
  useSidePanel,
} from "./side-panel-context";

type PanelChrome = { subtitle?: string; iconUrl?: string; docsUrl?: string };

function OpenButton(chrome: PanelChrome) {
  const { openPanel } = useSidePanel();
  return (
    <button
      type="button"
      onClick={() =>
        openPanel({
          kind: "setup-guide",
          title: "Box",
          ...chrome,
          props: { registrySpecifier: "com.pulsemcp.mirror/box" },
        })
      }
    >
      open
    </button>
  );
}

// null opens a panel without that line, which a bare `undefined` cannot express
// past the default.
function harness(
  subtitle: string | null = "MCP setup guide",
  chrome: Omit<PanelChrome, "subtitle"> = {},
) {
  return render(
    <SidePanelProvider>
      <OpenButton subtitle={subtitle ?? undefined} {...chrome} />
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
  mocks.useIsMobile.mockReturnValue(false);
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
      name: "Box MCP setup guide",
    });
    expect(panel.style.width).toBe(`${SIDE_PANEL_MAX_WIDTH}px`);
    expect(screen.getByTestId("panel-body").textContent).toBe(
      "rendered setup-guide",
    );
  });

  it("heads the panel with its subject, then what it is holding", () => {
    harness();
    fireEvent.click(screen.getByRole("button", { name: "open" }));

    expect(screen.getByText("Box")).toBeTruthy();
    expect(screen.getByText("MCP setup guide")).toBeTruthy();
  });

  it("leaves the second line off when the caller has no subtitle", () => {
    harness(null);
    fireEvent.click(screen.getByRole("button", { name: "open" }));

    expect(screen.getByRole("complementary", { name: "Box" })).toBeTruthy();
    expect(screen.queryByText("MCP setup guide")).toBeNull();
  });

  it("wears the subject's own icon when it has one", () => {
    const { container } = harness("MCP setup guide", {
      iconUrl: "https://cdn.example.com/box.png",
    });
    fireEvent.click(screen.getByRole("button", { name: "open" }));

    const icon = container.querySelector("img");
    expect(icon?.getAttribute("src")).toBe("https://cdn.example.com/box.png");
    // Decorative: the title beside it already names the server.
    expect(icon?.getAttribute("alt")).toBe("");
  });

  it("falls back to a generic mark when the subject has no icon", () => {
    const { container } = harness();
    fireEvent.click(screen.getByRole("button", { name: "open" }));

    expect(container.querySelector("img")).toBeNull();
    expect(container.querySelector("svg.lucide-book-open")).toBeTruthy();
  });

  it("links out to the docs from the header, in a new tab", () => {
    harness("MCP setup guide", {
      docsUrl: "https://www.speakeasy.com/docs/guides/box",
    });
    fireEvent.click(screen.getByRole("button", { name: "open" }));

    const docs = screen.getByRole("link", { name: "Docs" });
    expect(docs.getAttribute("href")).toBe(
      "https://www.speakeasy.com/docs/guides/box",
    );
    expect(docs.getAttribute("target")).toBe("_blank");
    expect(docs.getAttribute("rel")).toBe("noopener noreferrer");
  });

  it("leaves the docs link out when there is no single page to open", () => {
    harness();
    fireEvent.click(screen.getByRole("button", { name: "open" }));

    expect(screen.queryByRole("link", { name: "Docs" })).toBeNull();
    expect(screen.getByRole("button", { name: "Close panel" })).toBeTruthy();
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
      screen.getByRole("complementary", { name: "Box MCP setup guide" }).style
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
      screen.getByRole("complementary", { name: "Box MCP setup guide" }).style
        .width,
    ).toBe(`${narrowed}px`);
    expect(window.localStorage.getItem(SIDE_PANEL_WIDTH_KEY)).toBeNull();

    fireEvent.pointerUp(handle, { clientX: 900, pointerId: 1 });

    expect(window.localStorage.getItem(SIDE_PANEL_WIDTH_KEY)).toBe(
      String(narrowed),
    );
  });

  it("drops a drag the browser takes over, without committing its width", () => {
    harness();
    fireEvent.click(screen.getByRole("button", { name: "open" }));

    const handle = screen.getByRole("separator", { name: "Resize panel" });
    handle.setPointerCapture = () => {};
    handle.releasePointerCapture = () => {};

    fireEvent.pointerDown(handle, { clientX: 800, pointerId: 1 });
    fireEvent.pointerMove(handle, { clientX: 900, pointerId: 1 });
    fireEvent.pointerCancel(handle, { clientX: 900, pointerId: 1 });

    const panelWidth = () =>
      screen.getByRole("complementary", { name: "Box MCP setup guide" }).style
        .width;

    // Back to the stored width, and nothing persisted.
    expect(panelWidth()).toBe(`${SIDE_PANEL_MAX_WIDTH}px`);
    expect(window.localStorage.getItem(SIDE_PANEL_WIDTH_KEY)).toBeNull();

    // The drag is over: a pointer merely passing over the grip afterwards must
    // not carry on resizing from where it left off.
    fireEvent.pointerMove(handle, { clientX: 1000, pointerId: 1 });

    expect(panelWidth()).toBe(`${SIDE_PANEL_MAX_WIDTH}px`);
  });

  it("gives the viewport back on mobile, keeping the panel for when it widens", () => {
    mocks.useIsMobile.mockReturnValue(true);
    harness();
    fireEvent.click(screen.getByRole("button", { name: "open" }));

    // A phone viewport has no room to split: the panel's own minimum would
    // leave the page a sliver.
    expect(screen.queryByRole("complementary")).toBeNull();
  });

  it("will not let the keyboard push the panel past its bounds", () => {
    harness();
    fireEvent.click(screen.getByRole("button", { name: "open" }));

    const handle = screen.getByRole("separator", { name: "Resize panel" });
    fireEvent.keyDown(handle, { key: "ArrowLeft" });

    expect(
      screen.getByRole("complementary", { name: "Box MCP setup guide" }).style
        .width,
    ).toBe(`${SIDE_PANEL_MAX_WIDTH}px`);
    expect(handle.getAttribute("aria-valuemin")).toBe(
      String(SIDE_PANEL_MIN_WIDTH),
    );
  });
});
