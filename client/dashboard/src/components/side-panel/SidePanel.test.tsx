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
  SidePanelKindHeaderAction: () => null,
}));

vi.mock("@/hooks/use-mobile", () => ({
  useIsMobile: mocks.useIsMobile,
}));

import { MemoryRouter, Route, Routes, useNavigate } from "react-router";
import { SidePanelProvider, SidePanelSurface } from "./SidePanel";
import {
  clampSidePanelWidth,
  SIDE_PANEL_MAX_WIDTH,
  SIDE_PANEL_MIN_WIDTH,
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

function NavigateButton() {
  const navigate = useNavigate();
  return (
    <button type="button" onClick={() => void navigate("/two")}>
      navigate
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
    <MemoryRouter initialEntries={["/one"]}>
      <SidePanelProvider>
        <OpenButton subtitle={subtitle ?? undefined} {...chrome} />
        <NavigateButton />
        <Routes>
          <Route path="/one" element={null} />
          <Route path="/two" element={null} />
        </Routes>
        <SidePanelSurface />
      </SidePanelProvider>
    </MemoryRouter>,
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

  it("closes when the page it belongs to goes away", async () => {
    harness();
    fireEvent.click(screen.getByRole("button", { name: "open" }));
    expect(screen.queryByRole("complementary")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "navigate" }));

    await waitForElementToBeRemoved(() => screen.queryByRole("complementary"));
  });

  it("closes on a click outside it, like every other sheet", async () => {
    harness();
    fireEvent.click(screen.getByRole("button", { name: "open" }));

    fireEvent.pointerDown(document.body);

    await waitForElementToBeRemoved(() => screen.queryByRole("complementary"));
  });

  it("stays open for a pointer landing inside it", () => {
    harness();
    fireEvent.click(screen.getByRole("button", { name: "open" }));

    fireEvent.pointerDown(screen.getByTestId("panel-body"));

    expect(screen.queryByRole("complementary")).toBeTruthy();
  });

  it("gives the viewport back on mobile, keeping the panel for when it widens", () => {
    mocks.useIsMobile.mockReturnValue(true);
    harness();
    fireEvent.click(screen.getByRole("button", { name: "open" }));

    // A phone viewport has no room to split: the panel's own minimum would
    // leave the page a sliver.
    expect(screen.queryByRole("complementary")).toBeNull();
  });
});
