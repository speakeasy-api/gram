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
 */
export function WidgetView({
  widget,
  height,
  className,
}: {
  widget: ViewableWidget;
  /** The body's height in pixels; the chart type decides when unset. */
  height?: number;
  className?: string;
}): JSX.Element {
  const spec = useMemo(() => specFromStoredWidget(widget), [widget]);
  const chartType = spec?.chartType ?? (widget.visualization.type as ChartType);
  const bodyHeight = height ?? widgetBodyHeight(chartType);
  // A number tile is only as tall as its figure, so it grows to fit a
  // failure in its place rather than clipping the reason; anything drawn to
  // a height keeps it.
  const bodyStyle =
    height === undefined && chartType === "number"
      ? { minHeight: bodyHeight }
      : { height: bodyHeight };
  return (
    <section
      aria-label={widget.name}
      className={cn(
        "border-border bg-card flex min-w-0 flex-col gap-3 border p-4",
        className,
      )}
    >
      <WidgetHeader name={widget.name} spec={spec} widgetId={widget.id} />
      <div className="flex min-h-0 flex-col overflow-auto" style={bodyStyle}>
        {spec === null ? (
          <WidgetBroken
            reason={
              widget.invalidReason ||
              "its query uses options the builder doesn't offer"
            }
          />
        ) : widget.invalidReason ? (
          <WidgetBroken reason={widget.invalidReason} />
        ) : (
          <WidgetAnswer spec={spec} />
        )}
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
  // Any chart on any page is a starting point for a question: the builder
  // opens on exactly this query, through the same link a shared query uses.
  const href = useMemo(() => {
    if (spec === null) return null;
    const params = new URLSearchParams({ [QUERY_PARAM]: encodeSpec(spec) });
    if (widgetId) params.set(WIDGET_PARAM, widgetId);
    return `${routes.explore.href()}?${params.toString()}`;
  }, [spec, widgetId, routes.explore]);
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

/** The widget's answer, once the catalog says it can still be asked. */
function WidgetAnswer({ spec }: { spec: ExploreSpec }): JSX.Element {
  const describe = useAnalyticsDescribe();
  const datasets = describe.data?.datasets;
  // The catalog decides whether the question still resolves; the query is
  // not sent until it has, so a broken widget costs no scan.
  const problem = datasets ? specProblem(datasets, spec) : null;
  const body = useMemo(
    () => (problem === "" ? queryBodyFromSpec(spec) : null),
    [problem, spec],
  );
  const result = useRunQuery(body);

  if (datasets === undefined) {
    if (describe.isError) {
      return <WidgetError text="The catalog did not load." />;
    }
    return <WidgetLoading />;
  }
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
    <ResultDrawing dataset={dataset} spec={spec} rows={result.data.rows} />
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
