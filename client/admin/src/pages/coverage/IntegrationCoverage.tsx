import type { JSX } from "react";
import { ExternalLink, Grid2X2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useQuery } from "@tanstack/react-query";
import { errorMessage } from "@/lib/gramAdminApi";
import { supportMatrixQuery } from "./api";
import { CatalogContext } from "./catalogContext";
import { MatrixExplorer } from "./MatrixExplorer";
import { matrixSourceURL, type Snapshot } from "./model";

export function IntegrationCoverage(): JSX.Element {
  const query = useQuery(supportMatrixQuery);
  if (query.isPending) return <p role="status">Loading support matrix…</p>;
  if (!query.data)
    return (
      <div className="space-y-3">
        <h1 className="text-2xl font-semibold">Support matrix</h1>
        <p role="alert">{errorMessage(query.error)}</p>
        <Button onClick={() => void query.refetch()}>Retry</Button>
      </div>
    );
  return (
    <CatalogContext.Provider value={query.data}>
      <SupportMatrixPage snapshot={query.data} />
    </CatalogContext.Provider>
  );
}

/**
 * The matrix is code: this page shows the file the server was built with.
 * Changing it is a pull request against that file, which the tests check.
 */
function SupportMatrixPage({ snapshot }: { snapshot: Snapshot }): JSX.Element {
  const { methods, platforms, capabilities, revision } = snapshot;
  const assessed = methods.reduce(
    (count, method) =>
      count +
      method.platforms.filter((support) => support.applicability !== "unknown")
        .length,
    0,
  );
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="mb-1 flex items-center gap-2">
            <Grid2X2 className="size-5" />
          </div>
          <h1 className="text-2xl font-semibold tracking-tight">
            Support matrix
          </h1>
          <p className="text-muted-foreground mt-1 max-w-2xl text-sm">
            What each integration method delivers on each platform. The matrix
            is code: this is the file the server was built with, and a change is
            a pull request.
          </p>
        </div>
        <div className="text-muted-foreground text-right text-xs leading-5">
          <p>
            {methods.length} methods · {platforms.length} platforms ·{" "}
            {capabilities.length} capabilities
          </p>
          <p>
            {assessed} / {methods.length * platforms.length} platform mappings
            assessed
          </p>
          <p>
            Revision <span className="font-mono">{revision.slice(0, 12)}</span>
          </p>
          <a
            href={matrixSourceURL}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-1 underline underline-offset-4 hover:no-underline"
          >
            Edit on GitHub
            <ExternalLink className="size-3" aria-hidden="true" />
          </a>
        </div>
      </header>
      <MatrixExplorer />
    </div>
  );
}
