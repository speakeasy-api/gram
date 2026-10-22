import type { DashboardPlacement } from "@gram/client/models/components/dashboardplacement.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DashboardGrid } from "./DashboardGrid";
import type { GridCard } from "./dashboardLayout";
import type { ViewableWidget } from "./WidgetView";

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

function widget(id: string, name: string, chartType = "line"): ViewableWidget {
  return {
    id,
    name,
    dataset: "sessions",
    query: {},
    visualization: { type: chartType },
  };
}

const widgets = new Map(
  [widget("w-1", "Sessions"), widget("w-2", "Cost", "number")].map((one) => [
    one.id,
    one,
  ]),
);

/** Cards for placements, each linked to its widget when the list has it. */
function cards(placements: DashboardPlacement[]): GridCard[] {
  return placements.map((placement) => ({
    placement,
    widget: widgets.get(placement.widgetId),
  }));
}

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
      cards={cards(placements)}
      canEdit={canEdit}
      saving={false}
      onSave={onSave}
      onRemove={onRemove}
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
        cards={cards([{ id: "p-1", widgetId: "w-1", x: 0, y: 0, w: 6, h: 3 }])}
        canEdit
        saving
        onSave={() => {}}
        onRemove={() => {}}
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
        cards={cards([{ id: "p-1", widgetId: "w-9", x: 0, y: 0, w: 6, h: 3 }])}
        canEdit={false}
        saving={false}
        onSave={() => {}}
        onRemove={() => {}}
      />,
    );
    expect(screen.getByTestId("placeholder")).toBeTruthy();
    expect(screen.queryByRole("region")).toBeNull();
    fireEvent.mouseDown(screen.getByTestId("placeholder"));
  });

  it("draws a card that carries its own widget, linked to no saved one", () => {
    render(
      <DashboardGrid
        cards={[
          {
            placement: { id: "built:0", widgetId: "", x: 0, y: 0, w: 6, h: 2 },
            widget: {
              name: "Tool calls",
              dataset: "tool_calls",
              query: {},
              visualization: { type: "number" },
            },
          },
        ]}
        canEdit={false}
        saving={false}
        onSave={() => {}}
        onRemove={() => {}}
      />,
    );
    expect(screen.getByRole("region", { name: "Tool calls" })).toBeTruthy();
    expect(screen.queryByTestId("placeholder")).toBeNull();
  });
});
