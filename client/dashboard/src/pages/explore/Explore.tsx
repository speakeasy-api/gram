import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { WorkbenchPage } from "@/components/page-templates";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import { useAnalyticsDescribe } from "@gram/client/react-query/analyticsDescribe.js";
import { useMemo, useState, type JSX } from "react";
import {
  findDataset,
  hasChartShape,
  initialSpec,
  queryBodyFromSpec,
  type ExploreSpec,
} from "./exploreModel";
import { ExploreResults } from "./ExploreResults";
import { QueryBuilder } from "./QueryBuilder";
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
  const [draft, setDraft] = useState<ExploreSpec | null>(null);

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

  return (
    <ExploreWorkbench
      datasets={describe.data.datasets}
      draft={draft}
      onChange={setDraft}
    />
  );
}

function ExploreWorkbench({
  datasets,
  draft,
  onChange,
}: {
  datasets: AnalyticsDataset[];
  draft: ExploreSpec | null;
  onChange: (spec: ExploreSpec) => void;
}): JSX.Element {
  // Until the user edits something, the builder opens on the catalog's first
  // dataset. Memoized so a run can tell it apart from an edit by reference.
  const opening = useMemo(() => initialSpec(datasets), [datasets]);
  // A draft naming a dataset the catalog has since dropped is stale, so it
  // opens afresh rather than querying a dataset that is no longer there.
  const live = draft && findDataset(datasets, draft.dataset) ? draft : null;
  const spec = live ?? opening;

  // Queries run when asked, never as a side effect of editing: a query scans
  // the dataset across its whole window, so the builder waits for Run. The
  // spec the last run answered is kept apart from the one being edited, so
  // the results panel keeps describing the query that produced them.
  const [submitted, setSubmitted] = useState<ExploreSpec | null>(null);
  const ran =
    submitted && findDataset(datasets, submitted.dataset) ? submitted : null;
  const chartBody = useMemo(
    () => (ran && hasChartShape(ran) ? queryBodyFromSpec(ran, "chart") : null),
    [ran],
  );
  const summaryBody = useMemo(
    () => (ran ? queryBodyFromSpec(ran, "summary") : null),
    [ran],
  );
  const chart = useRunQuery(chartBody);
  const summary = useRunQuery(summaryBody);

  // Every edit replaces the spec object, so the same reference means nothing
  // changed since the last run: Run then asks the same question again rather
  // than serving the cached answer.
  const unchanged = ran !== null && ran === spec;
  const run = () => {
    if (!unchanged) {
      setSubmitted(spec);
      return;
    }
    if (chartBody) void chart.refetch();
    void summary.refetch();
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
    <>
      <QueryBuilder
        datasets={datasets}
        spec={spec}
        onChange={onChange}
        onRun={run}
        changed={ran !== null && !unchanged}
      />
      {ran ? (
        <ExploreResults
          dataset={findDataset(datasets, ran.dataset)}
          spec={ran}
          chart={chart}
          summary={summary}
        />
      ) : (
        <ResultsPrompt />
      )}
    </>
  );
}

// The results panel before the first run: the same frame, waiting.
function ResultsPrompt(): JSX.Element {
  return (
    <section className="border-border bg-card flex flex-col gap-4 border p-5">
      <span className="text-eyebrow">Results</span>
      <InlineEmptyState
        icon="telescope"
        heading="Nothing has run yet"
        description="Compose a query above and press Run query."
      />
    </section>
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
