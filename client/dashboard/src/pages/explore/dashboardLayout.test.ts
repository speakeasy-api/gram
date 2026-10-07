import type { DashboardPlacement } from "@gram/client/models/components/dashboardplacement.js";
import type { Widget } from "@gram/client/models/components/widget.js";
import { describe, expect, it } from "vitest";
import { layoutFor, placementsFor, sameLayout } from "./dashboardLayout";

function widget(id: string, chartType: string): Widget {
  return {
    id,
    name: id,
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

function placement(
  id: string,
  widgetId: string,
  box: [number, number, number, number],
): DashboardPlacement {
  const [x, y, w, h] = box;
  return { id, widgetId, x, y, w, h };
}

const widgets = new Map([
  ["number", widget("number", "number")],
  ["line", widget("line", "line")],
]);

describe("layoutFor", () => {
  it("keys each card by its placement and holds it to its chart's minimum", () => {
    const layout = layoutFor(
      [
        placement("p-1", "number", [0, 0, 3, 2]),
        placement("p-2", "line", [3, 0, 6, 3]),
        placement("p-3", "number", [9, 0, 3, 2]),
      ],
      widgets,
    );
    expect(layout).toEqual([
      { i: "p-1", x: 0, y: 0, w: 3, h: 2, minW: 2, minH: 2 },
      { i: "p-2", x: 3, y: 0, w: 6, h: 3, minW: 4, minH: 3 },
      { i: "p-3", x: 9, y: 0, w: 3, h: 2, minW: 2, minH: 2 },
    ]);
  });

  it("treats a card whose widget has not loaded as a chart", () => {
    const [item] = layoutFor(
      [placement("p-1", "missing", [0, 0, 6, 3])],
      widgets,
    );
    expect(item).toMatchObject({ minW: 4, minH: 3 });
  });
});

describe("placementsFor", () => {
  const saved = [
    placement("p-1", "number", [0, 0, 3, 2]),
    placement("p-2", "line", [3, 0, 6, 3]),
  ];

  it("saves every card where the grid put it, by its placement", () => {
    expect(
      placementsFor(
        [
          { i: "p-2", x: 0, y: 0, w: 8, h: 4 },
          { i: "p-1", x: 8, y: 0, w: 3, h: 2 },
        ],
        saved,
      ),
    ).toEqual([
      { id: "p-2", widgetId: "line", x: 0, y: 0, w: 8, h: 4 },
      { id: "p-1", widgetId: "number", x: 8, y: 0, w: 3, h: 2 },
    ]);
  });

  it("leaves out a grid item that is not a card", () => {
    expect(
      placementsFor([{ i: "stray", x: 0, y: 0, w: 1, h: 1 }], saved),
    ).toEqual([]);
  });
});

describe("sameLayout", () => {
  const saved = [placement("p-1", "number", [0, 0, 3, 2])];

  it("is true when every card is where it is saved", () => {
    expect(sameLayout([{ i: "p-1", x: 0, y: 0, w: 3, h: 2 }], saved)).toBe(
      true,
    );
  });

  it("is false once a card moves, resizes, or goes", () => {
    expect(sameLayout([{ i: "p-1", x: 1, y: 0, w: 3, h: 2 }], saved)).toBe(
      false,
    );
    expect(sameLayout([{ i: "p-1", x: 0, y: 0, w: 4, h: 2 }], saved)).toBe(
      false,
    );
    expect(sameLayout([], saved)).toBe(false);
  });
});
