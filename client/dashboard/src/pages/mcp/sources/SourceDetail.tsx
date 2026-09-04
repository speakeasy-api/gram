import { DetailPage } from "@/components/page-templates";
import {
  SourceDetail as SourceDetailBody,
  SourceDownloadButton,
} from "@/components/sources/SourceDetailPanel";
import {
  sourceAssetId,
  useProjectSources,
} from "@/components/sources/source-list";
import { Button } from "@/components/ui/Button";
import { useProject } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { useParams } from "react-router";

/**
 * One source at its own URL.
 *
 * A source is read in a sheet where it is being chosen, but it also needs an
 * address: the CLI hands people a link after a push, and a source is the thing
 * worth pointing a colleague at. Both surfaces render the same body.
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
            ? "The project's latest deployment could not be fetched. Reload to try again."
            : "This source is not in the project's latest deployment. It may have been replaced by a newer one.",
          backTo: routes.mcp.sources.href(),
        }}
      />
    );
  }

  return (
    <DetailPage
      scope="mcp:read"
      resourceId={project.id}
      layout="scroll"
      loading={isLoading}
      title={source?.name ?? "Source"}
      description={
        kind === "openapi"
          ? "An OpenAPI document in this project's latest deployment."
          : "A function in this project's latest deployment."
      }
      breadcrumbSubstitutions={{ [sourceId ?? ""]: source?.name }}
      primaryAction={
        <>
          {/* The same action the sheet carries in its header, since the page
              is the other half of how a source is read. */}
          <SourceDownloadButton
            sourceKind={kind}
            assetId={sourceId!}
            variant="button"
          />
          {/* Arriving from a source, that source is the choice already made. */}
          <Button variant="primary" asChild>
            <routes.mcp.add.fromSource.Link
              queryParams={{ source: `${kind}:${sourceId ?? ""}` }}
            >
              <Button.Text>Build a server</Button.Text>
            </routes.mcp.add.fromSource.Link>
          </Button>
        </>
      }
      sections={[
        {
          id: "details",
          label: "Details",
          content: (
            <SourceDetailBody
              sourceKind={kind}
              assetId={sourceId!}
              variant="page"
            />
          ),
        },
      ]}
    />
  );
}
