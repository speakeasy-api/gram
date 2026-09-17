import type { Action } from "@/components/ui/MoreActions";
import { useRBAC } from "@/hooks/useRBAC";
import { useActiveDeployment } from "@/hooks/toolTypes";
import { useRoutes } from "@/routes";
import { useCallback } from "react";
import { useNavigate } from "react-router";
import type { RemovableSource } from "./RemoveSourceDialog";
import { sourceAssetId, type SourceOption } from "./source-list";
import { useDownloadSource } from "./source-list-download";

/**
 * The query parameter the OpenAPI upload page reads to upload a new version
 * of an existing document instead of adding one. The source detail page links
 * there with it too, so the two agree on the name here.
 */
export const NEW_VERSION_SLUG_PARAM = "slug";

export function newVersionQueryParams(slug: string): Record<string, string> {
  return { [NEW_VERSION_SLUG_PARAM]: slug };
}

const WRITE_SCOPE_HINT = "Requires the project:write scope.";

/**
 * The actions a source offers from its card or row, in the order the old
 * sources page listed them. Read actions are open to everyone; the ones that
 * change the project are listed but disabled without project:write, so the
 * menu reads the same for every viewer and says why an item is off.
 */
export function useSourceListActions({
  onRemove,
}: {
  onRemove: (source: RemovableSource) => void;
}): (source: SourceOption) => Action[] {
  const routes = useRoutes();
  const navigate = useNavigate();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("project:write");
  const download = useDownloadSource();
  const { data: deploymentResult } = useActiveDeployment();
  const deploymentId = deploymentResult?.deployment?.id;

  return useCallback(
    (source: SourceOption): Action[] => {
      const disabledHint = canWrite ? undefined : WRITE_SCOPE_HINT;
      const actions: Action[] = [
        {
          label: "View details",
          icon: "eye",
          onClick: () => routes.mcp.sources.detail.goTo(sourceAssetId(source)),
        },
        {
          label: "Download",
          icon: "download",
          onClick: () => void download(source),
        },
      ];
      if (source.kind === "openapi" && source.slug) {
        const slug = source.slug;
        actions.push({
          label: "Upload new version",
          icon: "upload",
          disabled: !canWrite,
          description: disabledHint,
          // goTo takes path params only, so the query string rides on href.
          onClick: () =>
            void navigate(
              `${routes.mcp.add.openapi.href()}?${new URLSearchParams(
                newVersionQueryParams(slug),
              ).toString()}`,
            ),
        });
      }
      if (deploymentId) {
        actions.push({
          label: "Go to deployment",
          icon: "history",
          onClick: () => routes.deployments.deployment.goTo(deploymentId),
        });
      }
      actions.push({
        label: "Delete",
        icon: "trash",
        destructive: true,
        separatorBefore: true,
        disabled: !canWrite,
        description: disabledHint,
        onClick: () =>
          onRemove({
            kind: source.kind,
            assetId: sourceAssetId(source),
            name: source.name,
          }),
      });
      return actions;
    },
    [canWrite, deploymentId, download, navigate, onRemove, routes],
  );
}
