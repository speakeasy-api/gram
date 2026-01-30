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
import {
  DeploymentFunctions,
  OpenAPIv3DeploymentAsset,
} from "@gram/client/models/components";
import {
  buildDeploymentQuery,
  buildListToolsQuery,
} from "@gram/client/react-query";
import { Badge, Stack } from "@speakeasy-api/moonshine";
import { useSuspenseQueries } from "@tanstack/react-query";
import { FileCodeIcon, SquareFunctionIcon } from "lucide-react";
import React from "react";
import { useParams } from "react-router";
import { useDeploymentSearchParams } from "./use-deployment-search-params";

export const AssetsTabContent = () => {
  const routes = useRoutes();
  const { setSearchParams } = useDeploymentSearchParams();

  const deploymentAssets = useDeploymentAssetsSuspense();

  const [okOpenAPI, okFunctions, errAll] = React.useMemo(() => {
    const okOpenAPI: OpenAPIAsset[] = [];
    const okFunctions: FunctionAsset[] = [];
    const err: DeploymentAsset[] = [];
    for (const asset of [
      ...deploymentAssets.data.openapiv3,
      ...deploymentAssets.data.function,
    ]) {
      if (asset.report.toolCount === 0) {
        err.push(asset);
      } else if (asset.type === "function") {
        okFunctions.push(asset);
      } else if (asset.type === "openapiv3") {
        okOpenAPI.push(asset);
      }
    }
    return [okOpenAPI, okFunctions, err];
  }, [deploymentAssets.data]);

  return (
    <Stack gap={12}>
      {errAll.length > 0 && (
        <div>
          <Stack gap={2} className="mb-6">
            <Heading variant="h2">Invalid Assets</Heading>
            <Type variant="small">
              The following assets caused this deployment to fail. Correct these
              errors by managing assets in the{" "}
              <routes.mcp.Link className="text-link hover:cursor-pointer">
                Toolsets page
              </routes.mcp.Link>
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
            {errAll.map((asset) => {
              return (
                <li key={asset.id}>
                  <AssetItem asset={asset} />
                </li>
              );
            })}
          </ul>
        </div>
      )}

      {okFunctions.length > 0 ? (
        <div>
          <Heading variant="h2" className="mb-6">
            Functions
          </Heading>

          <ul className="flex flex-col gap-4 flex-wrap">
            {okFunctions.map((asset) => {
              return (
                <li key={asset.id}>
                  <AssetItem asset={asset} />
                </li>
              );
            })}
          </ul>
        </div>
      ) : null}

      {okOpenAPI.length > 0 ? (
        <div>
          <Heading variant="h2" className="mb-6">
            OpenAPI
          </Heading>
          <ul className="flex flex-col gap-4 flex-wrap">
            {okOpenAPI.map((asset) => {
              return (
                <li key={asset.id}>
                  <AssetItem asset={asset} />
                </li>
              );
            })}
          </ul>
        </div>
      ) : null}
    </Stack>
  );
};

type AssetItemProps = {
  asset: DeploymentAsset;
};

const AssetItem = ({ asset }: AssetItemProps) => {
  const handleDownload = useDownloadAsset(asset.type);
  let icon = <FileCodeIcon strokeWidth={1} className="size-12 min-w-12" />;
  if (asset.type === "function") {
    icon = <SquareFunctionIcon strokeWidth={1} className="size-12 min-w-12" />;
  }
  let type = (
    <span className="text-xs text-muted leading-5">OpenAPI Document</span>
  );
  if (asset.type === "function") {
    type = (
      <span className="text-xs text-muted leading-5 font-mono">
        {asset.runtime}
      </span>
    );
  }

  return (
    <MiniCard className="w-full max-w-full bg-surface-secondary-default border-neutral-softest p-6">
      <MiniCard.Title>
        <div className="flex gap-4 w-full items-center">
          {icon}
          <div className="flex flex-col">
            <span className="text-base leading-7">{asset.name}</span>
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted leading-5">{type}</span>
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
          variant="destructive"
          className="px-0.5 py-1 leading-2 items-center flex text-xs"
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

const useDownloadAsset = (assetType: string) => {
  const { id: projectId } = useProject();
  const path =
    assetType === "openapiv3"
      ? "/rpc/assets.serveOpenAPIv3"
      : "/rpc/assets.serveFunction";

  return (assetId: string, assetName: string) => {
    const downloadURL = new URL(path, getServerURL());
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

interface OpenAPIAsset extends OpenAPIv3DeploymentAsset {
  type: "openapiv3";
  report: { toolCount: number };
}

interface FunctionAsset extends DeploymentFunctions {
  type: "function";
  report: { toolCount: number };
}

type DeploymentAsset = OpenAPIAsset | FunctionAsset;

type DeploymentAssetsResult = {
  status: "success";
  data: {
    openapiv3: OpenAPIAsset[];
    function: FunctionAsset[];
  };
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
      if (deploymentQuery.isError || toolsQuery.isError) {
        const errors = [];
        if (deploymentQuery.isError) {
          errors.push(
            `deployment data (${deploymentQuery.error?.message || "unknown error"})`,
          );
        }
        if (toolsQuery.isError) {
          errors.push(
            `tools data (${toolsQuery.error?.message || "unknown error"})`,
          );
        }
        throw new Error(`Failed to fetch ${errors.join(" and ")}`);
      }

      const oapiAssets = deploymentQuery.data.openapiv3Assets;
      const oapiToolByAssetIdCounts = toolsQuery.data.tools.reduce(
        (acc, tool) => {
          const assetId = tool.httpToolDefinition?.assetId;
          if (!assetId) return acc;
          acc[assetId] = (acc[assetId] || 0) + 1;
          return acc;
        },
        {} as Record<string, number>,
      );

      const oapi: OpenAPIAsset[] = [];
      for (const asset of oapiAssets) {
        oapi.push({
          ...asset,
          type: "openapiv3",
          report: {
            toolCount: oapiToolByAssetIdCounts[asset.assetId] || 0,
          },
        });
      }

      const funcAssets = deploymentQuery.data.functionsAssets ?? [];
      const funcToolByAssetIdCounts = toolsQuery.data.tools.reduce(
        (acc, tool) => {
          const assetId = tool.functionToolDefinition?.assetId;
          if (!assetId) return acc;
          acc[assetId] = (acc[assetId] || 0) + 1;
          return acc;
        },
        {} as Record<string, number>,
      );

      const funcs: FunctionAsset[] = [];
      for (const asset of funcAssets) {
        funcs.push({
          type: "function",
          ...asset,
          report: {
            toolCount: funcToolByAssetIdCounts[asset.assetId] || 0,
          },
        });
      }

      return {
        status: "success",
        data: {
          openapiv3: oapi,
          function: funcs,
        },
      };
    },
  });

  return deploymentAssetsResult;
};
