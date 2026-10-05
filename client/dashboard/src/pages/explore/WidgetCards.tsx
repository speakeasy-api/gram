import { MoreActions, type Action } from "@/components/ui/MoreActions";
import type { Widget } from "@gram/client/models/components/widget.js";
import { Page } from "@/components/page-layout";
import { useAnalyticsDescribe } from "@gram/client/react-query/analyticsDescribe.js";
import { useEffect, useMemo, useRef, useState, type JSX } from "react";
import type { ChartType } from "./exploreModel";
import { usePageFilters, type PageFilterField } from "./usePageFilters";
import {
  WidgetPlaceholder,
  WidgetView,
  type OpenInExplore,
} from "./WidgetView";

// The fields the cards' filter bar may offer, in order: the dimensions most
// questions about agent activity are cut by. The bar shows only those a
// widget on screen can be filtered by.
const CARD_FILTER_FIELDS: readonly PageFilterField[] = [
  { field: "user", label: "User" },
  { field: "surface", label: "Agent" },
  { field: "model", label: "Model" },
  { field: "mcp_server", label: "MCP server" },
  { field: "status", label: "Status" },
];

/**
 * The project's widgets drawn as cards, in the order and under the search
 * and filters the list shows, beneath the dashboard's shared filter bar. A
 * card answers its own saved question over its own window until the bar
 * picks a date range, and narrows by whatever the bar filters on. A card's
 * actions are the list row's, and opening one goes through the same check
 * for edits not yet saved.
 */
export function WidgetCards({
  widgets,
  datasets,
  actionsFor,
  onOpen,
}: {
  widgets: Widget[];
  /** Every dataset the project's widgets ask, for which filters apply. */
  datasets: readonly string[];
  actionsFor: (widget: Widget) => Action[];
  onOpen: OpenInExplore;
}): JSX.Element {
  const catalog = useAnalyticsDescribe().data?.datasets;
  // Settled on the project's widgets rather than the ones the search leaves,
  // so the bar does not change under someone typing.
  const fields = useMemo(
    () =>
      CARD_FILTER_FIELDS.filter(({ field }) =>
        (catalog ?? []).some(
          (dataset) =>
            datasets.includes(dataset.name) &&
            dataset.fields.some(
              (candidate) =>
                candidate.name === field &&
                candidate.role === "dimension" &&
                (candidate.operators ?? []).includes("in"),
            ),
        ),
      ),
    [catalog, datasets],
  );
  const page = usePageFilters({ fields });

  return (
    <div className="flex flex-col gap-4">
      <Page.Toolbar>
        <Page.Toolbar.Filters {...page.toolbar} />
      </Page.Toolbar>
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
                page={page.context}
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
    </div>
  );
}

/**
 * Mounts a card once it first scrolls near the viewport, and keeps it. Each
 * card scans its dataset across its whole window, so a long list asks only
 * the questions someone scrolls to rather than all of them at once. Without
 * an IntersectionObserver, cards mount straight away. Until then it holds
 * the card's place at the size the card will draw at.
 */
function OnceVisible({
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
