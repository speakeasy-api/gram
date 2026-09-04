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
  const { sources, isLoading } = useProjectSources();

  // Asset ids are unique across both kinds, so the id alone addresses a
  // source — and the URL carries no segment that isn't a page of its own.
  const source = sources.find(
    (candidate) => sourceAssetId(candidate) === sourceId,
  );
  const kind = source?.kind ?? "openapi";

  if (!isLoading && !source) {
    return (
      <DetailPage
        scope="mcp:read"
        sections={[]}
        notFound={{
          title: "Source not found",
          description:
            "This source is not in the project's latest deployment. It may have been replaced by a newer one.",
          backTo: routes.mcp.sources.href(),
        }}
      />
    );
  }

  return (
    <DetailPage
      scope="mcp:read"
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
          <routes.mcp.add.fromSource.Link
            queryParams={{ source: `${kind}:${sourceId ?? ""}` }}
          >
            <Button variant="primary">
              <Button.Text>Build a server</Button.Text>
            </Button>
          </routes.mcp.add.fromSource.Link>
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
