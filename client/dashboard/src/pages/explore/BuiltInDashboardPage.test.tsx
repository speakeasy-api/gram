import type { BuiltInDashboard } from "@gram/client/models/components/builtindashboard.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { BuiltInDashboardPage } from "./BuiltInDashboardPage";
import type { GridCard } from "./dashboardLayout";

const testState = vi.hoisted(() => ({
  pending: false,
  builtIn: [] as unknown[],
  refetch: vi.fn(),
  duplicateBuiltIn: vi.fn(),
  /** What the page's filter bar holds. */
  context: {} as Record<string, unknown>,
  /** Every whole-bar setting, in call order. */
  applied: [] as unknown[],
}));

vi.mock("@gram/client/react-query/dashboards.js", () => ({
  useDashboards: () => ({
    isPending: testState.pending,
    data: testState.pending
      ? undefined
      : { dashboards: [], builtIn: testState.builtIn },
    refetch: testState.refetch,
  }),
}));
vi.mock("@gram/client/react-query/analyticsDescribe.js", () => ({
  useAnalyticsDescribe: () => ({ data: { datasets: [] } }),
}));
// The bar is the dashboard's shared filter system, tested on its own; here
// it only has to be given the cards' fields and read back.
vi.mock("./usePageFilters", () => ({
  pageFieldsFor: () => [{ field: "mcp_server", label: "MCP server" }],
  usePageFilters: () => ({
    toolbar: { schema: [], values: {} },
    context: testState.context,
    touched: false,
    apply: (values: unknown) => {
      testState.applied.push(values);
    },
  }),
}));
vi.mock("./useDashboardMutations", () => ({
  useDashboardMutations: () => ({
    duplicateBuiltIn: testState.duplicateBuiltIn,
    pending: false,
  }),
}));
// The grid is react-grid-layout's, tested on its own; here the cards only
// have to reach it, where the layout says, read only.
vi.mock("./DashboardGrid", () => ({
  DashboardGrid: ({
    cards,
    canEdit,
    page,
  }: {
    cards: GridCard[];
    canEdit: boolean;
    page?: unknown;
  }) => (
    <ul
      aria-label="Cards"
      data-editable={canEdit}
      data-page={JSON.stringify(page ?? null)}
    >
      {cards.map(({ placement, widget }) => (
        <li
          key={placement.id}
          data-box={`${placement.x},${placement.y},${placement.w},${placement.h}`}
        >
          {widget?.name}
        </li>
      ))}
    </ul>
  ),
}));
vi.mock("@/components/page-layout", () => {
  const Row = ({ children }: { children?: ReactNode }) => <div>{children}</div>;
  return {
    Page: {
      Toolbar: Object.assign(Row, {
        Row,
        Actions: Row,
        Filters: () => <div data-testid="filters" />,
      }),
    },
  };
});

const mcpTools: BuiltInDashboard = {
  slug: "mcp-tools",
  name: "MCP & Tools",
  description: "What the agents call",
  cards: [
    {
      name: "Tool calls",
      dataset: "tool_calls",
      query: { window: "7d" },
      visualization: { type: "number" },
      x: 0,
      y: 0,
      w: 6,
      h: 2,
    },
    {
      name: "Calls over time",
      description: "Day by day",
      dataset: "tool_calls",
      query: { window: "7d" },
      visualization: { type: "stacked_bar" },
      x: 0,
      y: 2,
      w: 12,
      h: 4,
    },
  ],
};

function renderPage(slug = "mcp-tools", onOpen = vi.fn<() => void>()) {
  render(
    <MemoryRouter>
      <BuiltInDashboardPage
        slug={slug}
        backHref="/dashboards"
        onOpen={onOpen}
      />
    </MemoryRouter>,
  );
  return { onOpen };
}

describe("BuiltInDashboardPage", () => {
  beforeEach(() => {
    testState.pending = false;
    testState.builtIn = [mcpTools];
    testState.refetch.mockClear();
    testState.duplicateBuiltIn.mockClear();
    testState.context = { window: { preset: "7d" }, filters: {} };
    testState.applied = [];
  });

  afterEach(() => cleanup());

  it("draws the cards read only where the layout puts them, under the filter bar, with nothing to edit", () => {
    renderPage();

    expect(screen.getByRole("heading", { name: "MCP & Tools" })).toBeTruthy();
    expect(screen.getByText("Speakeasy-built")).toBeTruthy();
    expect(screen.getByText("What the agents call")).toBeTruthy();
    expect(screen.getByTestId("filters")).toBeTruthy();

    const cards = screen.getByRole("list", { name: "Cards" });
    expect(cards.getAttribute("data-editable")).toBe("false");
    expect(cards.getAttribute("data-page")).toBe(
      JSON.stringify(testState.context),
    );
    const boxes = Array.from(cards.querySelectorAll("li")).map((item) => [
      item.textContent,
      item.getAttribute("data-box"),
    ]);
    expect(boxes).toEqual([
      ["Tool calls", "0,0,6,2"],
      ["Calls over time", "0,2,12,4"],
    ]);

    expect(screen.queryByRole("button", { name: "Add widget" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Actions for/ })).toBeNull();
    expect(screen.queryByRole("button", { name: "Save filters" })).toBeNull();
    // The bar is on the defaults, so there is nothing to reset.
    expect(screen.queryByRole("button", { name: "Reset filters" })).toBeNull();
    expect(screen.getByRole("link", { name: "All dashboards" })).toBeTruthy();
  });

  it("offers to reset the bar once it leaves the defaults, back to them", () => {
    testState.context = {
      window: { preset: "30d" },
      filters: { mcp_server: ["docs"] },
    };
    renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Reset filters" }));
    expect(testState.applied).toEqual([
      {
        window: { preset: "7d", customRange: null, customLabel: null },
        filters: { mcp_server: [] },
      },
    ]);
  });

  it("duplicates into the caller's own dashboard and opens it", () => {
    const { onOpen } = renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Duplicate" }));
    expect(testState.duplicateBuiltIn).toHaveBeenCalledWith(
      "mcp-tools",
      onOpen,
    );
  });

  it("says while the list loads, and when the dashboard is not there", () => {
    testState.pending = true;
    renderPage();
    expect(document.querySelector("[aria-busy='true']")).toBeTruthy();
    cleanup();

    testState.pending = false;
    renderPage("retired");
    expect(screen.getByText("This dashboard did not load")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(testState.refetch).toHaveBeenCalled();
  });
});
