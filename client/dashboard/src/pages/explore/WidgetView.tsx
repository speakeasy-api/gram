import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { Icon } from "@/components/ui/Icon";
import { Skeleton } from "@/components/ui/Skeleton";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import { useAnalyticsDescribe } from "@gram/client/react-query/analyticsDescribe.js";
import { useMemo, type JSX } from "react";
import { Link } from "react-router";
import {
  completeMeasures,
  findDataset,
  formatMeasureValue,
  measureAlias,
  measureUnit,
  numericCell,
  queryBodyFromSpec,
  specProblem,
  type ChartType,
  type ExploreSpec,
  type ResultRow,
} from "./exploreModel";
import { ResultDrawing } from "./ExploreResults";
import { applyPageContext, type PageContext } from "./pageContext";
import { encodeSpec, QUERY_PARAM, WIDGET_PARAM } from "./exploreUrl";
import { useRunQuery } from "./useRunQuery";
import { specFromStoredWidget, type StoredWidget } from "./widgetSpec";

/** A widget as a page holds it: what the widgets service stores. */
export interface ViewableWidget extends StoredWidget {
  /** Set for a saved widget, so Open in Explore opens it rather than a copy. */
  id?: string | undefined;
  name: string;
  /** Why the server says the widget no longer works, when it does. */
  invalidReason?: string | undefined;
}

/**
 * The body height a chart type is drawn at when the caller does not size
 * the card: a number tile is short, anything with an axis or a list taller.
 */
function widgetBodyHeight(chartType: ChartType): number {
  return chartType === "number" ? 72 : 240;
}

/**
 * One widget drawn on its own, anywhere: a product page or, later, a
 * dashboard. It runs its own query and owns every state the card can be in,
 * so a page places widgets without knowing how any of them is answered. A
 * widget that no longer validates says why in place of its numbers; it never
 * shows stale or empty ones.
 *
 * On a page, the page's window and filters fold into the widget's question
 * (see pageContext.ts), and what runs is what Open in Explore opens.
 */
export function WidgetView({
  widget,
  page,
  height,
  className,
}: {
  widget: ViewableWidget;
  /** The window and filters of the page the widget sits on. */
  page?: PageContext;
  /** The body's height in pixels; the chart type decides when unset. */
  height?: number;
  className?: string;
}): JSX.Element {
  const describe = useAnalyticsDescribe();
  const datasets = describe.data?.datasets;
  const saved = useMemo(() => specFromStoredWidget(widget), [widget]);
  // The page is folded in once the catalog says which of its filters the
  // widget's dataset can take.
  const paged =
    saved && datasets
      ? applyPageContext(saved, findDataset(datasets, saved.dataset), page)
      : null;
  const chartType =
    saved?.chartType ?? (widget.visualization.type as ChartType);
  const bodyHeight = height ?? widgetBodyHeight(chartType);
  // A number tile is only as tall as its figure, so it grows to fit a
  // failure in its place rather than clipping the reason; anything drawn to
  // a height keeps it.
  const bodyStyle =
    height === undefined && chartType === "number"
      ? { minHeight: bodyHeight }
      : { height: bodyHeight };

  let body: JSX.Element;
  if (saved === null) {
    body = (
      <WidgetBroken
        reason={
          widget.invalidReason ||
          "its query uses options the builder doesn't offer"
        }
      />
    );
  } else if (widget.invalidReason) {
    body = <WidgetBroken reason={widget.invalidReason} />;
  } else if (datasets === undefined || paged === null) {
    body = describe.isError ? (
      <WidgetError text="The catalog did not load." />
    ) : (
      <WidgetLoading />
    );
  } else {
    body = (
      <WidgetAnswer
        datasets={datasets}
        spec={paged.spec}
        onRangeSelect={page?.onRangeSelect}
      />
    );
  }

  return (
    <section
      aria-label={widget.name}
      className={cn(
        "border-border bg-card flex min-w-0 flex-col gap-3 border p-4",
        className,
      )}
    >
      <WidgetHeader
        name={widget.name}
        // A widget the server says is broken opens as it was saved, where it
        // is fixed, and only when it was saved. Otherwise the card opens
        // what it ran; a question the page changed is no longer the saved
        // widget, so it opens as a query of its own.
        spec={
          widget.invalidReason
            ? widget.id
              ? saved
              : null
            : (paged?.spec ?? null)
        }
        widgetId={
          widget.invalidReason || !paged?.changed ? widget.id : undefined
        }
      />
      {paged && paged.skipped.length > 0 ? (
        <p className="text-muted-foreground -mt-2 text-xs">
          Not filtered by {paged.skipped.join(", ")}, which {saved?.dataset}{" "}
          cannot filter on.
        </p>
      ) : null}
      <div className="flex min-h-0 flex-col overflow-auto" style={bodyStyle}>
        {body}
      </div>
    </section>
  );
}

function WidgetHeader({
  name,
  spec,
  widgetId,
}: {
  name: string;
  spec: ExploreSpec | null;
  widgetId: string | undefined;
}): JSX.Element {
  const routes = useRoutes();
  const datasets = useAnalyticsDescribe().data?.datasets;
  // A saved widget opens in the builder even when it no longer works, which
  // is where it is fixed. Anything else that no longer works would open
  // Explore on its default view, so it offers no way in.
  const openable =
    spec !== null &&
    (widgetId !== undefined ||
      (datasets !== undefined && specProblem(datasets, spec) === ""));
  // Any chart on any page is a starting point for a question: the builder
  // opens on exactly this query, through the same link a shared query uses.
  const href = useMemo(() => {
    if (spec === null || !openable) return null;
    const params = new URLSearchParams({ [QUERY_PARAM]: encodeSpec(spec) });
    if (widgetId) params.set(WIDGET_PARAM, widgetId);
    return `${routes.explore.href()}?${params.toString()}`;
  }, [spec, openable, widgetId, routes.explore]);
  return (
    <header className="flex min-w-0 items-center justify-between gap-2">
      <h3 className="text-eyebrow truncate" title={name}>
        {name}
      </h3>
      {href ? (
        <Link
          to={href}
          className="text-muted-foreground hover:text-foreground inline-flex shrink-0 items-center gap-1 text-xs no-underline hover:underline"
        >
          Open in Explore
          <Icon name="arrow-up-right" className="size-3" />
        </Link>
      ) : null}
    </header>
  );
}

/** The widget's answer, once the catalog has loaded. */
function WidgetAnswer({
  datasets,
  spec,
  onRangeSelect,
}: {
  datasets: AnalyticsDataset[];
  spec: ExploreSpec;
  onRangeSelect: ((from: Date, to: Date) => void) | undefined;
}): JSX.Element {
  // The catalog decides whether the question still resolves; the query is
  // not sent until it has, so a broken widget costs no scan.
  const problem = specProblem(datasets, spec);
  const body = useMemo(
    () => (problem === "" ? queryBodyFromSpec(spec) : null),
    [problem, spec],
  );
  const result = useRunQuery(body);

  if (problem) return <WidgetBroken reason={problem} />;
  if (result.isError) {
    return (
      <WidgetError
        text={`The query did not run: ${result.error.message || "something went wrong"}.`}
      />
    );
  }
  if (result.data === undefined) return <WidgetLoading />;
  if (result.data.rows.length === 0) {
    return <WidgetEmptyState message="Nothing in this window" />;
  }
  const dataset = findDataset(datasets, spec.dataset);
  const measures = completeMeasures(spec.measures);
  // The card names the number, so a single figure is drawn bare rather
  // than as a labelled tile inside a labelled card.
  if (spec.chartType === "number" && measures.length === 1) {
    return (
      <WidgetNumber dataset={dataset} spec={spec} row={result.data.rows[0]} />
    );
  }
  return (
    <ResultDrawing
      dataset={dataset}
      spec={spec}
      rows={result.data.rows}
      onRangeSelect={onRangeSelect}
    />
  );
}

function WidgetNumber({
  dataset,
  spec,
  row,
}: {
  dataset: AnalyticsDataset | undefined;
  spec: ExploreSpec;
  row: ResultRow | undefined;
}): JSX.Element {
  const [measure] = completeMeasures(spec.measures);
  const value = measure ? numericCell(row?.[measureAlias(measure)]) : null;
  return (
    <span className="font-serif text-3xl leading-none">
      {value === null || measure === undefined
        ? "—"
        : formatMeasureValue(value, measureUnit(dataset, measure))}
    </span>
  );
}

function WidgetBroken({ reason }: { reason: string }): JSX.Element {
  return <WidgetError text={`This widget no longer works: ${reason}.`} />;
}

function WidgetError({ text }: { text: string }): JSX.Element {
  return (
    <p
      role="alert"
      className="text-muted-foreground flex items-start gap-2 text-sm"
    >
      <Icon
        name="triangle-alert"
        className="text-destructive mt-0.5 size-4 shrink-0"
      />
      <span>{text}</span>
    </p>
  );
}

function WidgetLoading(): JSX.Element {
  return <Skeleton className="h-full min-h-8 w-full" aria-busy="true" />;
}
