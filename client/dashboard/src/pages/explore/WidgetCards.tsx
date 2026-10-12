import { MoreActions, type Action } from "@/components/ui/MoreActions";
import type { Widget } from "@gram/client/models/components/widget.js";
import { useEffect, useRef, useState, type JSX } from "react";
import type { ChartType } from "./exploreModel";
import type { PageContext } from "./pageContext";
import {
  WidgetPlaceholder,
  WidgetView,
  type OpenInExplore,
} from "./WidgetView";

/**
 * The project's widgets drawn as cards, in the order and under the search
 * and filters the list shows. A card answers its own saved question over
 * its own window until the toolbar's filter bar picks a date range, and
 * narrows by whatever that bar filters on. A card's actions are the list
 * row's, and opening one goes through the same check for edits not yet
 * saved.
 */
export function WidgetCards({
  widgets,
  page,
  actionsFor,
  onOpen,
}: {
  widgets: Widget[];
  /** What the Widgets tab's filter bar holds. */
  page: PageContext;
  actionsFor: (widget: Widget) => Action[];
  onOpen: OpenInExplore;
}): JSX.Element {
  return (
    <>
      {widgets.length === 0 ? (
        <p className="text-muted-foreground py-10 text-center text-sm">
          No widgets match these filters.
        </p>
      ) : (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2 xl:grid-cols-3">
          {widgets.map((widget) => (
            <OnceVisible
              key={widget.id}
              chartType={widget.visualization.type as ChartType}
            >
              <WidgetView
                widget={widget}
                page={page}
                className="h-full"
                onOpen={onOpen}
                actions={
                  <MoreActions
                    triggerAriaLabel={`Actions for ${widget.name}`}
                    actions={actionsFor(widget)}
                  />
                }
              />
            </OnceVisible>
          ))}
        </div>
      )}
    </>
  );
}

/**
 * Mounts a card once it first scrolls near the viewport, and keeps it. Each
 * card scans its dataset across its whole window, so a long list or a tall
 * dashboard asks only the questions someone scrolls to rather than all of
 * them at once. Without an IntersectionObserver, cards mount straight away.
 * Until then it holds the card's place at the size the card will draw at.
 */
export function OnceVisible({
  chartType,
  children,
}: {
  chartType: ChartType;
  children: JSX.Element;
}): JSX.Element {
  const ref = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(
    () => typeof IntersectionObserver === "undefined",
  );
  useEffect(() => {
    if (visible || !ref.current) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) setVisible(true);
      },
      { rootMargin: "200px" },
    );
    observer.observe(ref.current);
    return () => observer.disconnect();
  }, [visible]);
  if (visible) return children;
  return (
    <div ref={ref} className="h-full">
      <WidgetPlaceholder chartType={chartType} className="h-full" />
    </div>
  );
}
