import type { Dashboard } from "@gram/client/models/components/dashboard.js";
import type { Widget } from "@gram/client/models/components/widget.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DashboardGrid } from "./DashboardGrid";

// Each card's answer is WidgetView's, tested on its own; here a card only
// has to be drawn where its placement says, with its actions.
vi.mock("./WidgetView", () => ({
  WidgetView: ({
    widget,
    actions,
  }: {
    widget: { name: string };
    actions?: React.ReactNode;
  }) => (
    <section aria-label={widget.name}>
      <header>{widget.name}</header>
      {actions}
    </section>
  ),
  WidgetPlaceholder: ({ chartType }: { chartType: string }) => (
    <div data-testid="placeholder" data-chart-type={chartType} />
  ),
}));
// Cards mount straight away: there is no IntersectionObserver here.
vi.mock("./WidgetCards", () => ({
  OnceVisible: ({ children }: { children: React.ReactNode }) => children,
}));

// The grid measures its container; jsdom has nothing to measure, so the
// grid takes its default width.
vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
);

function widget(id: string, name: string, chartType = "line"): Widget {
  return {
    id,
    name,
    dataset: "sessions",
    query: {},
    visualization: { type: chartType },
    dashboards: [],
    projectId: "project",
    organizationId: "org",
    createdAt: new Date(),
    updatedAt: new Date(),
  };
}

function dashboard(widgets: Dashboard["widgets"]): Dashboard {
  return {
    id: "d-1",
    name: "Agent activity",
    projectId: "project",
    organizationId: "org",
    filters: { values: {} },
    widgets,
    createdAt: new Date(),
    updatedAt: new Date(),
  };
}

const widgets = [widget("w-1", "Sessions"), widget("w-2", "Cost", "number")];

function renderGrid({
  canEdit = true,
  placements = [
    { id: "p-1", widgetId: "w-1", x: 0, y: 0, w: 6, h: 3 },
    { id: "p-2", widgetId: "w-2", x: 6, y: 0, w: 3, h: 2 },
  ],
  onRemove = vi.fn<(placementId: string) => void>(),
  onSave = vi.fn<() => void>(),
} = {}) {
  render(
    <DashboardGrid
      dashboard={dashboard(placements)}
      widgets={widgets}
      canEdit={canEdit}
      saving={false}
      onSave={onSave}
      onRemove={onRemove}
      onOpen={() => {}}
    />,
  );
  return { onRemove, onSave };
}

describe("DashboardGrid", () => {
  afterEach(() => cleanup());

  it("draws a card per placement, resizable by someone who may edit", () => {
    renderGrid();

    expect(screen.getByRole("region", { name: "Sessions" })).toBeTruthy();
    expect(screen.getByRole("region", { name: "Cost" })).toBeTruthy();
    expect(document.querySelectorAll(".react-grid-item")).toHaveLength(2);
    // The handles are drawn for everyone and hidden from a reader.
    expect(
      document.querySelectorAll(".react-grid-item.react-resizable-hide"),
    ).toHaveLength(0);
  });

  it("gives a reader the cards without handles or actions", () => {
    renderGrid({ canEdit: false });

    expect(screen.getByRole("region", { name: "Sessions" })).toBeTruthy();
    expect(
      document.querySelectorAll(".react-grid-item.react-resizable-hide"),
    ).toHaveLength(2);
    expect(
      screen.queryByRole("button", { name: "Actions for Sessions" }),
    ).toBeNull();
  });

  it("holds the cards still while a save is in flight", () => {
    render(
      <DashboardGrid
        dashboard={dashboard([
          { id: "p-1", widgetId: "w-1", x: 0, y: 0, w: 6, h: 3 },
        ])}
        widgets={widgets}
        canEdit
        saving
        onSave={() => {}}
        onRemove={() => {}}
        onOpen={() => {}}
      />,
    );
    expect(
      document.querySelectorAll(".react-grid-item.react-resizable-hide"),
    ).toHaveLength(1);
  });

  it("takes a card off through its actions", async () => {
    const user = userEvent.setup();
    const { onRemove } = renderGrid();

    await user.click(screen.getByRole("button", { name: "Actions for Cost" }));
    await user.click(
      screen.getByRole("menuitem", { name: /Remove from dashboard/ }),
    );
    expect(onRemove).toHaveBeenCalledWith("p-2");
  });

  it("holds a card's place until its widget has loaded", () => {
    render(
      <DashboardGrid
        dashboard={dashboard([
          { id: "p-1", widgetId: "w-9", x: 0, y: 0, w: 6, h: 3 },
        ])}
        widgets={[]}
        canEdit={false}
        saving={false}
        onSave={() => {}}
        onRemove={() => {}}
        onOpen={() => {}}
      />,
    );
    expect(screen.getByTestId("placeholder")).toBeTruthy();
    expect(screen.queryByRole("region")).toBeNull();
    fireEvent.mouseDown(screen.getByTestId("placeholder"));
  });
});
