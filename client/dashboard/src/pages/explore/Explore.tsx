import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { WorkbenchPage } from "@/components/page-templates";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import { useAnalyticsDescribe } from "@gram/client/react-query/analyticsDescribe.js";
import { useState, type JSX } from "react";
import { initialSpec, type ExploreSpec } from "./exploreModel";
import { ExploreResults } from "./ExploreResults";
import { QueryBuilder } from "./QueryBuilder";

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

function ExploreBody(): JSX.Element {
  const describe = useAnalyticsDescribe();
  const [draft, setDraft] = useState<ExploreSpec | null>(null);

  if (describe.isPending) return <BuilderSkeleton />;
  if (describe.isError) {
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
  // Derived during render: until the user edits something, the builder opens
  // on the catalog's first dataset.
  const spec = draft ?? initialSpec(datasets);
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
      <QueryBuilder datasets={datasets} spec={spec} onChange={onChange} />
      <ExploreResults dataset={spec.dataset} />
    </>
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
