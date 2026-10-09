import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { WorkbenchPage } from "@/components/page-templates";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { PageTabsList, PageTabsTrigger, Tabs } from "@/components/ui/Tabs";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import type { Widget } from "@gram/client/models/components/widget.js";
import { useAnalyticsDescribe } from "@gram/client/react-query/analyticsDescribe.js";
import { useDashboards } from "@gram/client/react-query/dashboards.js";
import { useWidgets } from "@gram/client/react-query/widgets.js";
import { useEffect, useMemo, useState, type JSX, type ReactNode } from "react";
import { Link, useLocation, useNavigate, useSearchParams } from "react-router";
import {
  findDataset,
  initialSpec,
  queryBodyFromSpec,
  type ExploreSpec,
} from "./exploreModel";
import { DashboardList } from "./DashboardList";
import { DashboardPage } from "./DashboardPage";
import { DASHBOARD_PARAM, encodeSpec, TAB_PARAM } from "./exploreUrl";
import { ExploreResults, ResultsFrame } from "./ExploreResults";
import { QueryBuilder, ResultsToolbar } from "./QueryBuilder";
import { clearPageFilterParams } from "./usePageFilters";
import { useQueryUrl } from "./useQueryUrl";
import { WidgetBar } from "./WidgetBar";
import { DiscardChangesDialog } from "./WidgetDialogs";
import { WidgetList } from "./WidgetList";
import {
  differsFromWidget,
  specFromStoredWidget,
  type StoredWidget,
} from "./widgetSpec";
import { useRunQuery } from "./useRunQuery";

// Explore: ask questions of this project's agent activity. The page is
// strictly project-scoped — the active project is the only one queried — and
// the builder it shows is generated from the analytics catalog.
export default function Explore(): JSX.Element {
  return (
    <WorkbenchPage scope="project:read">
      {/* WorkbenchPage owns overflow-hidden; the builder and results scroll
          together inside it. */}
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
        <div className="flex w-full flex-col gap-6 p-8 pb-24">
          <ExploreHeader />
          <ExploreBody />
        </div>
      </div>
    </WorkbenchPage>
  );
}

function ExploreHeader(): JSX.Element {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <Page.Eyebrow />
      <div className="flex items-center gap-2">
        <h1 className="text-display-sm font-thin">Explore</h1>
        <ReleaseStageBadge stage="preview" />
      </div>
      <p className="text-muted-foreground text-sm">
        Pick a dataset, choose what to measure, and break it down by the fields
        this project reports.
      </p>
    </div>
  );
}

/**
 * Explore is dogfooded before it ships, so the page is gated as well as the
 * nav entry: hiding the link alone would leave the URL open to anyone who
 * guessed it. The flag is a rollout control, not authorization — the queries
 * behind this page are scoped by project:read whatever it says.
 */
function ExploreBody(): JSX.Element {
  const rollout = useFeatureFlag(FEATURE_FLAGS.explore);

  // PostHog answers after the first paint, so wait rather than telling
  // someone who does have Explore that they do not.
  if (rollout.status === "loading") return <BuilderSkeleton />;
  if (rollout.status !== "enabled") {
    return (
      <InlineEmptyState
        icon="telescope"
        heading="Explore is not available yet"
        description="It is in preview with a few organizations. Ask your Speakeasy contact to turn it on."
      />
    );
  }
  return <ExploreCatalog />;
}

function ExploreCatalog(): JSX.Element {
  const describe = useAnalyticsDescribe();

  if (describe.isPending) return <BuilderSkeleton />;
  // A refetch that fails still leaves the cached catalog usable, so the
  // initial-load error is only the right answer when there is nothing to show.
  if (describe.isError && describe.data === undefined) {
    return (
      <InlineEmptyState
        icon="triangle-alert"
        heading="The catalog did not load"
        description="Explore builds its controls from the catalog, so there is nothing to show until it does."
        action={
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void describe.refetch()}
          >
            Try again
          </Button>
        }
      />
    );
  }

  return <ExploreWorkbench datasets={describe.data.datasets} />;
}

function ExploreWorkbench({
  datasets,
}: {
  datasets: AnalyticsDataset[];
}): JSX.Element {
  // Queries run when asked, never as a side effect of editing: a query scans
  // the dataset across its whole window, so the builder waits for Run. The
  // spec the last run answered is kept apart from the one being edited, so
  // the results panel keeps describing the query that produced them.
  const [submitted, setSubmitted] = useState<ExploreSpec | null>(null);

  // The query being edited lives in the URL, so it is a link at every step.
  // A link opened, or a question stepped back to, runs as it arrives.
  const url = useQueryUrl(datasets, setSubmitted);
  const tab = useTab();
  // Without a query in the URL — or with one the catalog can no longer
  // answer — the builder opens on the catalog's first dataset.
  const opening = useMemo(() => initialSpec(datasets), [datasets]);
  // A widget the catalog has since broken stays open for editing, so it can
  // be fixed and saved again; a plain link that broke just opens the
  // default view.
  const broken = url.widgetId !== null ? url.stale : null;
  const spec = url.spec ?? broken?.spec ?? opening;

  const list = useWidgets();
  const widgets = list.data?.widgets ?? [];
  const dashboardList = useDashboards();
  const dashboards = dashboardList.data?.dashboards ?? [];
  const openWidget = url.widgetId
    ? widgets.find((widget) => widget.id === url.widgetId)
    : undefined;
  const problem =
    widgetProblem(spec, openWidget) ??
    (broken ? { unreadable: false, reason: broken.problem } : null);
  // The last answer belongs to the question being left, so it goes; a
  // widget that still runs brings its own as it opens. A card opens the
  // question it ran, which a page may have changed from the saved widget's.
  const openQuery = (next: ExploreSpec, widgetId: string | null) => {
    setSubmitted(null);
    url.open(next, widgetId);
  };
  const open = (widget: Widget) => {
    setSubmitted(null);
    url.open(specFromStoredWidget(widget), widget.id);
  };
  // Leaving an open widget with edits not yet saved asks first; the edits
  // are only in this entry's URL, which is hard to find again.
  const unsaved =
    spec !== null &&
    openWidget !== undefined &&
    specFromStoredWidget(openWidget) !== null &&
    differsFromWidget(spec, openWidget);
  const [leaving, setLeaving] = useState<(() => void) | null>(null);
  const confirmLeave = (proceed: () => void) => {
    if (unsaved) setLeaving(() => proceed);
    else proceed();
  };
  const ran =
    submitted && findDataset(datasets, submitted.dataset) ? submitted : null;
  // One question, one query: a timeseries draws its buckets and nothing
  // else; the whole-window figures are a table chart away.
  const body = useMemo(() => (ran ? queryBodyFromSpec(ran) : null), [ran]);
  const result = useRunQuery(body);

  // The same query text means nothing changed since the last run: Run then
  // asks the same question again rather than serving the cached answer.
  const unchanged =
    ran !== null && spec !== null && encodeSpec(ran) === encodeSpec(spec);

  // A run that returns claims its history entry, so the next edit starts a
  // new one and Back returns to this question.
  const { ran: markRan } = url;
  const answered = result.isSuccess && !result.isPlaceholderData;
  useEffect(() => {
    if (ran && answered) markRan(ran);
  }, [ran, answered, markRan]);
  const run = () => {
    if (!unchanged) {
      setSubmitted(spec);
      return;
    }
    void result.refetch();
  };

  if (!spec) {
    return (
      <InlineEmptyState
        icon="telescope"
        heading="No datasets yet"
        description="Datasets appear here as the catalog grows."
      />
    );
  }
  return (
    <Tabs value={tab.current} className="flex flex-col gap-6">
      <div className="border-b">
        <PageTabsList className="h-auto w-max gap-6 bg-transparent p-0">
          <PageTabsTrigger value="explore" asChild>
            <Link to={tab.href("explore")} state={tab.state}>
              Explore
            </Link>
          </PageTabsTrigger>
          <PageTabsTrigger value="widgets" asChild>
            <Link
              to={tab.href("widgets")}
              state={tab.state}
              className="inline-flex items-center gap-2"
            >
              Widgets
              {list.data ? (
                <span className="text-muted-foreground tabular-nums">
                  {widgets.length}
                </span>
              ) : null}
            </Link>
          </PageTabsTrigger>
          <PageTabsTrigger value="dashboards" asChild>
            <Link
              to={tab.href("dashboards")}
              state={tab.state}
              className="inline-flex items-center gap-2"
            >
              Dashboards
              {dashboardList.data ? (
                <span className="text-muted-foreground tabular-nums">
                  {dashboards.length}
                </span>
              ) : null}
            </Link>
          </PageTabsTrigger>
        </PageTabsList>
      </div>

      {tab.current === "widgets" ? (
        <WidgetList
          widgets={widgets}
          isPending={list.isPending}
          isError={list.isError}
          onOpen={open}
          onOpenQuery={openQuery}
          confirmLeave={confirmLeave}
          onDeleted={(id) => {
            // The deleted widget may be the one the builder has open.
            if (id === url.widgetId) url.setWidgetId(null);
          }}
          onExplore={() => tab.go("explore")}
          onOpenDashboard={(id) => tab.go("dashboards", id)}
          onRetry={() => void list.refetch()}
        />
      ) : null}
      {tab.current === "dashboards" ? (
        tab.dashboardId ? (
          <DashboardPage
            id={tab.dashboardId}
            widgets={widgets}
            widgetsLoaded={list.data !== undefined}
            widgetsFailed={list.isError && list.data === undefined}
            onRetryWidgets={() => void list.refetch()}
            backHref={tab.href("dashboards")}
            backState={tab.state}
            onOpen={(dashboard) => tab.go("dashboards", dashboard.id)}
            onDeleted={() => tab.go("dashboards")}
            onOpenQuery={(spec, widgetId) =>
              confirmLeave(() => openQuery(spec, widgetId ?? null))
            }
          />
        ) : (
          <DashboardList
            dashboards={dashboards}
            isPending={dashboardList.isPending}
            isError={dashboardList.isError}
            onOpen={(dashboard) => tab.go("dashboards", dashboard.id)}
            onRetry={() => void dashboardList.refetch()}
          />
        )
      ) : null}
      {/* The builder stays mounted behind the other tabs, so its last
          answer is still there on the way back. */}
      <div hidden={tab.current !== "explore"} className="flex flex-col gap-6">
        <ExploreTab
          datasets={datasets}
          spec={spec}
          widgetId={url.widgetId}
          widgets={widgets}
          listResolving={list.isPending || list.isFetching}
          problem={problem}
          ran={ran}
          unchanged={unchanged}
          result={result}
          onOpen={open}
          confirmLeave={confirmLeave}
          onWidgetIdChange={url.setWidgetId}
          onOpenDashboard={(id) => tab.go("dashboards", id)}
          onChange={url.edit}
          onRun={run}
        />
      </div>
      <DiscardChangesDialog
        name={openWidget?.name ?? ""}
        open={leaving !== null}
        onCancel={() => setLeaving(null)}
        onConfirm={() => {
          const proceed = leaving;
          setLeaving(null);
          proceed?.();
        }}
      />
    </Tabs>
  );
}

function ExploreTab({
  datasets,
  spec,
  widgetId,
  widgets,
  listResolving,
  problem,
  ran,
  unchanged,
  result,
  onOpen,
  confirmLeave,
  onWidgetIdChange,
  onOpenDashboard,
  onChange,
  onRun,
}: {
  datasets: AnalyticsDataset[];
  spec: ExploreSpec;
  widgetId: string | null;
  widgets: Widget[];
  listResolving: boolean;
  problem: WidgetProblem | null;
  ran: ExploreSpec | null;
  unchanged: boolean;
  result: ReturnType<typeof useRunQuery>;
  onOpen: (widget: Widget) => void;
  confirmLeave: (proceed: () => void) => void;
  onWidgetIdChange: (widgetId: string | null) => void;
  onOpenDashboard: (dashboardId: string) => void;
  onChange: (spec: ExploreSpec) => void;
  onRun: () => void;
}): JSX.Element {
  // How and over when the question is answered sit on the results panel's
  // header, whichever panel is showing.
  const toolbar = (
    <ResultsToolbar
      spec={spec}
      onChange={onChange}
      onRun={onRun}
      changed={ran !== null && !unchanged}
    />
  );
  return (
    <>
      {problem?.unreadable ? (
        <Alert variant="error" dismissible={false}>
          This widget can't be opened in the builder: {problem.reason}. Save the
          builder as a new widget, or delete it.
        </Alert>
      ) : problem ? (
        <Alert variant="error" dismissible={false}>
          This widget no longer works: {problem.reason}. Edit it below, then
          save it again.
        </Alert>
      ) : null}
      <QueryBuilder
        datasets={datasets}
        spec={spec}
        onChange={onChange}
        actions={
          <WidgetBar
            spec={spec}
            widgetId={widgetId}
            widgets={widgets}
            listResolving={listResolving}
            onOpen={onOpen}
            confirmLeave={confirmLeave}
            onWidgetIdChange={onWidgetIdChange}
            onOpenDashboard={onOpenDashboard}
          />
        }
      />
      {ran ? (
        <ExploreResults
          dataset={findDataset(datasets, ran.dataset)}
          spec={ran}
          result={result}
          toolbar={toolbar}
        />
      ) : (
        <ResultsPrompt toolbar={toolbar} />
      )}
    </>
  );
}

/** What is wrong with the open widget, and whether the builder can show it. */
interface WidgetProblem {
  /**
   * The builder cannot read the widget, so it is not what the builder shows
   * and saving over it would replace it with something unrelated.
   */
  unreadable: boolean;
  reason: string;
}

/**
 * Why the open widget does not work: the builder cannot read it, or the
 * server said it no longer works when it last read it — while the builder
 * still holds what was saved, so a fix in progress is not reported as
 * broken.
 */
function widgetProblem(
  spec: ExploreSpec | null,
  widget: (StoredWidget & { invalidReason?: string | undefined }) | undefined,
): WidgetProblem | null {
  if (!spec || !widget) return null;
  if (specFromStoredWidget(widget) === null) {
    return {
      unreadable: true,
      reason:
        widget.invalidReason ||
        "its query uses options the builder doesn't offer",
    };
  }
  if (widget.invalidReason && !differsFromWidget(spec, widget)) {
    return { unreadable: false, reason: widget.invalidReason };
  }
  return null;
}

type ExploreTabName = "explore" | "widgets" | "dashboards";

/**
 * The page's tab, kept in the URL beside the query so a link can open
 * straight onto the widget list or a dashboard. Switching tabs keeps the
 * query and the history entry's state, so coming back does not rerun or
 * forget anything. An open dashboard is left behind on switching, so the
 * Dashboards tab opens on its list.
 */
function useTab(): {
  current: ExploreTabName;
  /** The dashboard the Dashboards tab has open, when it is the tab. */
  dashboardId: string | null;
  href: (to: ExploreTabName, dashboardId?: string) => string;
  state: unknown;
  go: (to: ExploreTabName, dashboardId?: string) => void;
} {
  const [params] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const named = params.get(TAB_PARAM);
  const current: ExploreTabName =
    named === "widgets" || named === "dashboards" ? named : "explore";
  const dashboardId =
    current === "dashboards" ? params.get(DASHBOARD_PARAM) : null;
  const href = (to: ExploreTabName, dashboard?: string) => {
    const out = new URLSearchParams(params);
    if (to === "explore") out.delete(TAB_PARAM);
    else out.set(TAB_PARAM, to);
    if (to === "dashboards" && dashboard) out.set(DASHBOARD_PARAM, dashboard);
    else out.delete(DASHBOARD_PARAM);
    // A dashboard's filter bar is its own: it opens on its saved filters,
    // and what was picked on it stays behind.
    if (to === "dashboards" || current === "dashboards") {
      clearPageFilterParams(out);
    }
    const search = out.toString();
    return search === "" ? location.pathname : `?${search}`;
  };
  return {
    current,
    dashboardId,
    href,
    state: location.state,
    go: (to, dashboard) =>
      void navigate(href(to, dashboard), { state: location.state }),
  };
}

// The results panel before the first run: the same frame, waiting.
function ResultsPrompt({ toolbar }: { toolbar: ReactNode }): JSX.Element {
  return (
    <ResultsFrame toolbar={toolbar}>
      <InlineEmptyState
        icon="telescope"
        heading="Nothing has run yet"
        description="Compose a query above and press Run query."
      />
    </ResultsFrame>
  );
}

function BuilderSkeleton(): JSX.Element {
  return (
    <div
      className="border-border bg-card flex flex-col gap-5 border p-5"
      aria-busy="true"
      aria-label="Loading the catalog"
    >
      <Skeleton className="h-10 w-64" />
      <Skeleton className="h-10 w-96" />
      <Skeleton className="h-10 w-48" />
      <Skeleton className="h-10 w-full max-w-3xl" />
    </div>
  );
}
