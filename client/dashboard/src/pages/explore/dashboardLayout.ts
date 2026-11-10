import type { DashboardPlacement } from "@gram/client/models/components/dashboardplacement.js";
import type { PlacementInput } from "@gram/client/models/components/placementinput.js";
import type { Layout, LayoutItem } from "react-grid-layout";
import type { StoredWidget } from "./widgetSpec";
import type { ViewableWidget } from "./WidgetView";

// How a dashboard's cards sit on its grid, as the dashboards service lays
// them out: the numbers here match the service's, which is what accepts or
// refuses a saved layout.

/** The grid's width in columns. */
export const GRID_COLUMNS = 12;

/** A row's height in pixels. */
export const ROW_HEIGHT = 80;

/** The gap between cards, across and down, in pixels. */
export const GRID_MARGIN: readonly [number, number] = [16, 16];

/**
 * One card as the grid draws it: where it sits, and the widget it shows. A
 * project dashboard's card links to a saved widget, which is undefined until
 * the widget list has answered; a Speakeasy-built dashboard's card carries
 * its widget with it and links to nothing.
 */
export interface GridCard {
  placement: DashboardPlacement;
  widget: ViewableWidget | undefined;
}

/** The smallest a card may be, by chart type: a number tile, or a chart. */
function cardMinimum(chartType: string): { w: number; h: number } {
  return chartType === "number" ? { w: 2, h: 2 } : { w: 4, h: 3 };
}

/** The chart type a widget is drawn as, or "" when it cannot be read. */
export function chartTypeOf(
  widget: Pick<StoredWidget, "visualization"> | undefined,
): string {
  const type = widget?.visualization.type;
  return typeof type === "string" ? type : "";
}

/**
 * The grid's items for a dashboard's cards, each keyed by its placement so
 * a card keeps its identity as it moves, and each held to the smallest its
 * widget's chart may be drawn at.
 */
export function layoutFor(cards: readonly GridCard[]): LayoutItem[] {
  return cards.map(({ placement, widget }) => {
    const minimum = cardMinimum(chartTypeOf(widget));
    return {
      i: placement.id,
      x: placement.x,
      y: placement.y,
      w: placement.w,
      h: placement.h,
      minW: minimum.w,
      minH: minimum.h,
    };
  });
}

/**
 * The grid's layout as the service saves it: every card by its placement,
 * where the grid now has it. A grid item with no placement is not a card
 * and is left out.
 */
export function placementsFor(
  layout: Layout,
  placements: DashboardPlacement[],
): PlacementInput[] {
  const byId = new Map(
    placements.map((placement) => [placement.id, placement]),
  );
  const out: PlacementInput[] = [];
  for (const item of layout) {
    const placement = byId.get(item.i);
    if (!placement) continue;
    out.push({
      id: placement.id,
      widgetId: placement.widgetId,
      x: item.x,
      y: item.y,
      w: item.w,
      h: item.h,
    });
  }
  return out;
}

/** Whether the grid has every card where it is saved, so nothing needs saving. */
export function sameLayout(
  layout: Layout,
  placements: DashboardPlacement[],
): boolean {
  if (layout.length !== placements.length) return false;
  const byId = new Map(
    placements.map((placement) => [placement.id, placement]),
  );
  return layout.every((item) => {
    const placement = byId.get(item.i);
    return (
      placement !== undefined &&
      placement.x === item.x &&
      placement.y === item.y &&
      placement.w === item.w &&
      placement.h === item.h
    );
  });
}
