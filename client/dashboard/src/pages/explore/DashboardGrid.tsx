import { MoreActions } from "@/components/ui/MoreActions";
import { cn } from "@/lib/utils";
import type { Dashboard } from "@gram/client/models/components/dashboard.js";
import type { PlacementInput } from "@gram/client/models/components/placementinput.js";
import type { Widget } from "@gram/client/models/components/widget.js";
import { useEffect, useMemo, useState, type JSX } from "react";
import { GridLayout, useContainerWidth, type Layout } from "react-grid-layout";
import "react-grid-layout/css/styles.css";
import "./DashboardGrid.css";
import {
  chartTypeOf,
  GRID_COLUMNS,
  GRID_MARGIN,
  layoutFor,
  placementsFor,
  ROW_HEIGHT,
  sameLayout,
} from "./dashboardLayout";
import type { ChartType } from "./exploreModel";
import { OnceVisible } from "./WidgetCards";
import {
  WidgetPlaceholder,
  WidgetView,
  type OpenInExplore,
} from "./WidgetView";

/**
 * A dashboard's cards on its 12-column grid. Someone who may edit the
 * dashboard drags a card by its header and resizes it by its corner, and
 * each move is saved as it lands: there is no edit mode and no Save button,
 * so what one person sees is what everyone sees. Each card is the saved
 * widget drawn by WidgetView, so it answers its own question.
 */
export function DashboardGrid({
  dashboard,
  widgets,
  canEdit,
  saving,
  onSave,
  onRemove,
  onOpen,
}: {
  dashboard: Dashboard;
  /** The project's widgets, which the cards link to. */
  widgets: Widget[];
  canEdit: boolean;
  /** A layout save is in flight, so no card may move until it lands. */
  saving: boolean;
  /** Save the layout after a card is moved or resized. */
  onSave: (placements: PlacementInput[]) => void;
  /** Take a card off the dashboard. */
  onRemove: (placementId: string) => void;
  onOpen: OpenInExplore;
}): JSX.Element {
  const byId = useMemo(
    () => new Map(widgets.map((widget) => [widget.id, widget])),
    [widgets],
  );
  const saved = useMemo(
    () => layoutFor(dashboard.widgets, byId),
    [dashboard.widgets, byId],
  );
  // What the grid shows: the saved layout, or the one just edited until its
  // save lands. Kept apart so the cards stay where they were dropped while
  // the save is in flight; whenever the saved layout changes — the save
  // landing, or someone else moving a card — it wins.
  const [layout, setLayout] = useState<Layout>(saved);
  useEffect(() => setLayout(saved), [saved]);
  const settle = (next: Layout) => {
    setLayout(next);
    if (!sameLayout(next, dashboard.widgets)) {
      onSave(placementsFor(next, dashboard.widgets));
    }
  };

  const { width, containerRef, mounted } = useContainerWidth();

  return (
    <div
      ref={containerRef}
      className={cn("dashboard-grid", canEdit && "dashboard-grid--editable")}
    >
      {mounted ? (
        <GridLayout
          width={width}
          layout={layout}
          gridConfig={{
            cols: GRID_COLUMNS,
            rowHeight: ROW_HEIGHT,
            margin: GRID_MARGIN,
            containerPadding: [0, 0],
          }}
          // The header is the handle, as WidgetView draws it; the controls
          // in it keep their clicks. One save at a time: the next move waits
          // for the last to land, so saves cannot overtake each other.
          dragConfig={{
            enabled: canEdit && !saving,
            handle: "header",
            cancel: "a, button",
          }}
          resizeConfig={{ enabled: canEdit && !saving, handles: ["se"] }}
          onDragStop={settle}
          onResizeStop={settle}
        >
          {dashboard.widgets.map((placement) => {
            const widget = byId.get(placement.widgetId);
            const chartType = chartTypeOf(widget) as ChartType;
            return (
              <div key={placement.id} className="overflow-hidden">
                {widget ? (
                  <OnceVisible chartType={chartType}>
                    <WidgetView
                      widget={widget}
                      height="fill"
                      onOpen={onOpen}
                      actions={
                        canEdit ? (
                          <MoreActions
                            triggerAriaLabel={`Actions for ${widget.name}`}
                            actions={[
                              {
                                label: "Remove from dashboard",
                                icon: "circle-minus",
                                destructive: true,
                                onClick: () => onRemove(placement.id),
                              },
                            ]}
                          />
                        ) : undefined
                      }
                    />
                  </OnceVisible>
                ) : (
                  // The widget list has not answered yet; the card's widget
                  // is live, or the service would not have listed the card.
                  <WidgetPlaceholder chartType={chartType} className="h-full" />
                )}
              </div>
            );
          })}
        </GridLayout>
      ) : null}
    </div>
  );
}
