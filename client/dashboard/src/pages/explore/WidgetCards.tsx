import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { Skeleton } from "@/components/ui/Skeleton";
import type { Widget } from "@gram/client/models/components/widget.js";
import { useEffect, useRef, useState, type JSX } from "react";
import { WidgetView } from "./WidgetView";

/**
 * The project's widgets drawn as cards, each answering its own saved
 * question over its own window, in the order and under the filters the list
 * shows. A card's actions are the list row's, and opening one goes through
 * the same check for edits not yet saved.
 */
export function WidgetCards({
  widgets,
  actionsFor,
  onOpen,
}: {
  widgets: Widget[];
  actionsFor: (widget: Widget) => Action[];
  onOpen: (widget: Widget) => void;
}): JSX.Element {
  if (widgets.length === 0) {
    return (
      <p className="text-muted-foreground py-10 text-center text-sm">
        No widgets match these filters.
      </p>
    );
  }
  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2 xl:grid-cols-3">
      {widgets.map((widget) => (
        <OnceVisible key={widget.id}>
          <WidgetView
            widget={widget}
            className="h-full"
            onOpen={() => onOpen(widget)}
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
  );
}

/**
 * Mounts a card once it first scrolls near the viewport, and keeps it. Each
 * card scans its dataset across its whole window, so a long list asks only
 * the questions someone scrolls to rather than all of them at once. Without
 * an IntersectionObserver, cards mount straight away.
 */
function OnceVisible({ children }: { children: JSX.Element }): JSX.Element {
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
    <div ref={ref} aria-busy="true">
      <Skeleton className="h-64 w-full" />
    </div>
  );
}
