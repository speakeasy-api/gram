import { DetailPage, type DetailSection } from "@/components/page-templates";
import { SourceActivityPanel } from "@/components/sources/SourceActivityPanel";
import { SourceContentViewer } from "@/components/sources/SourceContentViewer";
import { SourceDangerZone } from "@/components/sources/SourceDangerZone";
import {
  SourceDetail as SourceDetailBody,
  SourceDownloadButton,
} from "@/components/sources/SourceDetailPanel";
import { SourceToolsSection } from "@/components/sources/SourceToolsSection";
import { SourceVersionsSection } from "@/components/sources/SourceVersionsSection";
import { sectionIdForHash } from "@/components/sources/sourceDetailSections";
import {
  sourceAssetId,
  useProjectSources,
  type SourceOption,
} from "@/components/sources/source-list";
import { useSourceTools } from "@/components/sources/useSourceQueries";
import { Button } from "@/components/ui/Button";
import { useProject } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { useEffect } from "react";
import { useLocation, useParams } from "react-router";

// Brings the section a link names into view once the page has something to
// scroll to. The sections render after the deployment loads, so a hash the
// browser handled at navigation time pointed at nothing.
function useScrollToSectionHash(ready: boolean): void {
  const location = useLocation();

  useEffect(() => {
    if (!ready) return;
    const targetId = sectionIdForHash(location.hash);
    if (!targetId) return;

    const animationFrame = window.requestAnimationFrame(() => {
      document
        .getElementById(targetId)
        ?.scrollIntoView({ behavior: "smooth", block: "start" });
    });
    return () => window.cancelAnimationFrame(animationFrame);
  }, [ready, location.hash]);
}

function contentLabel(kind: SourceOption["kind"]): string {
  switch (kind) {
    case "openapi":
      return "OpenAPI document";
    case "function":
      return "Function manifest";
  }
}

function kindDescription(kind: SourceOption["kind"]): string {
  switch (kind) {
    case "openapi":
      return "An OpenAPI document in this project's active deployment.";
    case "function":
      return "A function in this project's active deployment.";
  }
}

/**
 * One source at its own URL.
 *
 * A source is read in a sheet where it is being chosen, but it also needs an
 * address: the CLI hands people a link after a push, and a source is the thing
 * worth pointing a colleague at. Both surfaces render the same details body;
 * the page adds the sections that only make sense with room to scroll.
 */
export default function SourceDetailRoute(): JSX.Element {
  const routes = useRoutes();
  const { sourceId } = useParams<{ sourceId: string }>();
  const project = useProject();
  const { sources, isLoading, isError } = useProjectSources();

  // Asset ids are unique across both kinds, so the id alone addresses a
  // source — and the URL carries no segment that isn't a page of its own.
  const source = sources.find(
    (candidate) => sourceAssetId(candidate) === sourceId,
  );
  const kind = source?.kind ?? "openapi";
  const assetId = sourceId ?? "";

  // The page's tools feed three sections, so they are read once here and
  // handed down rather than filtered again in each.
  const {
    tools,
    toolUrns,
    isLoading: isToolsLoading,
  } = useSourceTools(kind, assetId);

  useScrollToSectionHash(!isLoading && source != null);

  // A deployment that failed to load is not a source that isn't there: saying
  // "not found" for a dropped request sends people looking for the wrong
  // problem.
  if (!isLoading && !source) {
    return (
      <DetailPage
        scope="mcp:read"
        resourceId={project.id}
        sections={[]}
        notFound={{
          title: isError ? "Couldn't load this source" : "Source not found",
          description: isError
            ? "The project's active deployment could not be fetched. Reload to try again."
            : "This source is not in the project's active deployment. It may have been replaced by a newer one.",
          backTo: routes.mcp.sources.href(),
        }}
      />
    );
  }

  // Every section is rendered only once the source is known: the viewers key
  // their fetches on the kind, and a wrong guess would request the wrong
  // endpoint.
  const sections: DetailSection[] = source
    ? [
        {
          id: "details",
          label: "Details",
          content: (
            <SourceDetailBody
              sourceKind={kind}
              assetId={assetId}
              variant="page"
            />
          ),
        },
        {
          id: "activity",
          label: "Activity",
          content: (
            <SourceActivityPanel
              sourceKey={assetId}
              toolUrns={toolUrns}
              isToolsLoading={isToolsLoading}
            />
          ),
        },
        {
          id: "tools",
          label: `Tools (${tools.length})`,
          content: (
            <SourceToolsSection
              sourceKind={kind}
              tools={tools}
              isLoading={isToolsLoading}
            />
          ),
        },
        {
          id: "content",
          label: contentLabel(kind),
          content: <SourceContentViewer sourceKind={kind} assetId={assetId} />,
        },
        {
          id: "versions",
          label: "Versions",
          content: <SourceVersionsSection sourceKind={kind} />,
        },
        {
          id: "settings",
          label: "Settings",
          content: (
            <SourceDangerZone
              source={{ kind, assetId, name: source.name }}
              slug={source.slug}
            />
          ),
        },
      ]
    : [];

  return (
    <DetailPage
      scope="mcp:read"
      resourceId={project.id}
      layout="hash-scroll"
      loading={isLoading}
      title={source?.name ?? "Source"}
      description={kindDescription(kind)}
      breadcrumbSubstitutions={{ [assetId]: source?.name }}
      primaryAction={
        <>
          {/* The same action the sheet carries in its header, since the page
              is the other half of how a source is read. */}
          <SourceDownloadButton
            sourceKind={kind}
            assetId={assetId}
            variant="button"
          />
          {/* Arriving from a source, that source is the choice already made. */}
          <Button variant="primary" asChild>
            <routes.mcp.add.fromSource.Link
              queryParams={{ source: `${kind}:${assetId}` }}
            >
              <Button.Text>Build a server</Button.Text>
            </routes.mcp.add.fromSource.Link>
          </Button>
        </>
      }
      sections={sections}
    />
  );
}
