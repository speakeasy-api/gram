import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { useProject } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { Upload } from "lucide-react";
import { newVersionQueryParams } from "./source-list-actions";

/**
 * Sends someone to the upload flow to replace an OpenAPI document with a
 * newer one. Sits in the page header beside "Build a server": replacing the
 * document is the main thing people come to an OpenAPI source to do.
 *
 * Functions have no equivalent here; a CLI push is their new version.
 */
export function SourceUploadVersionButton({
  slug,
}: {
  /** The deployment's slug for the document, which the upload flow keys on. */
  slug: string;
}): JSX.Element {
  const routes = useRoutes();
  const project = useProject();

  return (
    <RequireScope
      scope="project:write"
      resourceId={project.id}
      level="component"
    >
      <Button variant="secondary" asChild>
        <routes.mcp.add.openapi.Link queryParams={newVersionQueryParams(slug)}>
          <Button.LeftIcon>
            <Upload className="size-4" />
          </Button.LeftIcon>
          <Button.Text>Upload new version</Button.Text>
        </routes.mcp.add.openapi.Link>
      </Button>
    </RequireScope>
  );
}
