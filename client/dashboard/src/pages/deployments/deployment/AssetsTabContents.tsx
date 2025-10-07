import { MiniCard } from "@/components/ui/card-mini";
import { Heading } from "@/components/ui/heading";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { OpenAPIv3DeploymentAsset } from "@gram/client/models/components";
import {
  buildDeploymentQuery,
  buildListToolsQuery,
} from "@gram/client/react-query";
import { Badge, Stack } from "@speakeasy-api/moonshine";
import { useSuspenseQueries } from "@tanstack/react-query";
import { FileCodeIcon } from "lucide-react";
import React from "react";
import { useParams } from "react-router";
import { useDeploymentSearchParams } from "./use-deployment-search-params";

export const AssetsTabContents = () => {
  const routes = useRoutes();
  const { setSearchParams } = useDeploymentSearchParams();

  const deploymentAssets = useDeploymentAssetsSuspense();

  const [okAssets, errAssets] = React.useMemo(() => {
    const ok: DeploymentAsset[] = [];
    const err: DeploymentAsset[] = [];
    for (const asset of deploymentAssets.data) {
      if (asset.report.toolCount === 0) {
        err.push(asset);
      } else {
        ok.push(asset);
      }
    }
    return [ok, err];
  }, [deploymentAssets.data]);

  return (
    <Stack gap={12}>
      {errAssets.length > 0 && (
        <div>
          <Stack gap={2} className="mb-6">
            <Heading variant="h2">Invalid Assets</Heading>
            <Type variant="small">
              The following assets caused this deployment to fail. Correct these
              errors by managing assets in the{" "}
              <routes.toolsets.Link className="text-link hover:cursor-pointer">
                Toolsets page
              </routes.toolsets.Link>
              . For details on the specific issues with each document, view the{" "}
              <a
                onClick={() =>
                  setSearchParams({ tab: "logs", grouping: "by_source" })
                }
                className="text-link hover:cursor-pointer"
              >
                deployment logs
              </a>
              .
            </Type>
          </Stack>

          <ul className="flex flex-col gap-4 flex-wrap">
            {errAssets.map((asset) => {
              return (
                <li key={asset.id}>
                  <AssetItem asset={asset} />
                </li>
              );
            })}
          </ul>
        </div>
      )}

      <div>
        <Heading variant="h2" className="mb-6">
          Assets
        </Heading>
        <ul className="flex flex-col gap-4 flex-wrap">
          {okAssets.map((asset) => {
            return (
              <li key={asset.id}>
                <AssetItem asset={asset} />
              </li>
            );
          })}
        </ul>
      </div>
    </Stack>
  );
};

type AssetItemProps = {
  asset: DeploymentAsset;
};

const AssetItem = ({ asset }: AssetItemProps) => {
  const handleDownload = useDownloadAsset();

  return (
    <MiniCard className="w-full max-w-full bg-surface-secondary-default border-neutral-softest p-6">
      <MiniCard.Title>
        <div className="flex gap-4 w-full items-center">
          <FileCodeIcon strokeWidth={1} className="size-12 min-w-12" />
          <div className="flex flex-col">
            <span className="text-base leading-7">{asset.name}</span>
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted leading-5">
                OpenAPI Document
              </span>
              {asset.report.toolCount === 0 && <AssetErrorBadge />}
            </div>
          </div>
        </div>
      </MiniCard.Title>
      <MiniCard.Actions
        actions={[
          {
            label: "Download",
            icon: "download",
            onClick: () => handleDownload(asset.assetId, asset.name),
          },
        ]}
      />
    </MiniCard>
  );
};

const AssetErrorBadge = () => {
  return (
    <Tooltip>
      <TooltipTrigger>
        <Badge
          size="xs"
          variant="destructive"
          className="px-0.5 py-1 leading-2 items-center flex"
        >
          Error
        </Badge>
      </TooltipTrigger>
      <TooltipContent side="bottom">
        This asset is causing the deployment to fail.
      </TooltipContent>
    </Tooltip>
  );
};

const useDownloadAsset = () => {
  const { id: projectId } = useProject();

  return (assetId: string, assetName: string) => {
    const downloadURL = new URL("/rpc/assets.serveOpenAPIv3", getServerURL());
    downloadURL.searchParams.set("id", assetId);
    downloadURL.searchParams.set("project_id", projectId);

    const link = document.createElement("a");
    link.href = downloadURL.toString();
    link.download = assetName;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  };
};

type DeploymentAsset = OpenAPIv3DeploymentAsset & {
  report: { toolCount: number };
};

type DeploymentAssetsResult =
  | {
      status: "pending";
    }
  | {
      status: "error";
      errors: {
        deployment?: Error | null;
        tools?: Error | null;
      };
    }
  | {
      status: "success";
      data: DeploymentAsset[];
    };

const useDeploymentAssetsSuspense = () => {
  const { deploymentId } = useParams();
  const client = useSdkClient();

  const deploymentAssetsResult: DeploymentAssetsResult = useSuspenseQueries({
    queries: [
      buildDeploymentQuery(client, { id: deploymentId! }),
      buildListToolsQuery(client, { deploymentId: deploymentId! }),
    ],
    combine: ([deploymentQuery, toolsQuery]) => {
      const result: DeploymentAsset[] = [];
      if (deploymentQuery.isError || toolsQuery.isError) {
        throw new Error("Failed to fetch deployment and/or tools data");
      }

      const assets = deploymentQuery.data.openapiv3Assets;
      const toolByAssetIdCounts = toolsQuery.data.tools.reduce(
        (acc, tool) => {
          const assetId = tool.httpToolDefinition?.openapiv3DocumentId;
          if (!assetId) return acc;
          acc[assetId] = (acc[assetId] || 0) + 1;
          return acc;
        },
        {} as Record<string, number>,
      );

      for (const asset of assets) {
        result.push({
          ...asset,
          report: {
            toolCount: toolByAssetIdCounts[asset.id] || 0,
          },
        });
      }

      return { status: "success", data: result };
    },
  });

  return deploymentAssetsResult;
};
