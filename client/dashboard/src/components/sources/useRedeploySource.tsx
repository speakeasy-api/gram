import { useSdkClient } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { EvolveForm } from "@gram/client/models/components";
import { useActiveDeployment } from "@gram/client/react-query/index.js";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

type RedeploySourceParams = {
  /** The deployment ID to source the asset from. */
  deploymentId: string;
  slug: string;
  type: "openapi" | "function" | "externalmcp";
};

/**
 * Hook that redeploys a source by evolving the active deployment with the
 * asset version from a specific (possibly older) deployment.
 *
 * Flow:
 * 1. Fetch the full target deployment to get the source's asset info
 * 2. Identify the active deployment
 * 3. Call evolve on the active deployment, upserting the source with the
 *    asset from the target deployment
 */
export function useRedeploySource() {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const { data: activeResult } = useActiveDeployment();

  return useMutation({
    mutationFn: async ({ deploymentId, slug, type }: RedeploySourceParams) => {
      const activeDeployment = activeResult?.deployment;
      if (!activeDeployment) {
        throw new Error("No active deployment found.");
      }

      // Fetch the full deployment to get asset details
      const targetDeployment = await client.deployments.getById({
        id: deploymentId,
      });

      const evolveForm: EvolveForm = {
        deploymentId: activeDeployment.id,
      };

      switch (type) {
        case "openapi": {
          const asset = targetDeployment.openapiv3Assets.find(
            (a) => a.slug === slug,
          );
          if (!asset) {
            throw new Error("OpenAPI source not found in target deployment.");
          }
          evolveForm.upsertOpenapiv3Assets = [
            { assetId: asset.assetId, name: asset.name, slug: asset.slug },
          ];
          break;
        }
        case "function": {
          const fn = targetDeployment.functionsAssets?.find(
            (f) => f.slug === slug,
          );
          if (!fn) {
            throw new Error("Function source not found in target deployment.");
          }
          evolveForm.upsertFunctions = [
            {
              assetId: fn.assetId,
              name: fn.name,
              slug: fn.slug,
              runtime: fn.runtime,
            },
          ];
          break;
        }
        case "externalmcp": {
          const mcp = targetDeployment.externalMcps?.find(
            (m) => m.slug === slug,
          );
          if (!mcp) {
            throw new Error(
              "External MCP source not found in target deployment.",
            );
          }
          evolveForm.upsertExternalMcps = [
            {
              registryId: mcp.registryId,
              name: mcp.name,
              slug: mcp.slug,
              registryServerSpecifier: mcp.registryServerSpecifier,
            },
          ];
          break;
        }
      }

      return client.deployments.evolveDeployment({ evolveForm });
    },
    onMutate: ({ slug }) => {
      toast.loading("Redeploying source...", {
        id: `redeploy-${slug}`,
      });
    },
    onSuccess: (data) => {
      queryClient.invalidateQueries({
        queryKey: ["@gram/client", "deployments"],
      });
      const href = data.deployment?.id
        ? routes.deployments.deployment.href(data.deployment.id)
        : undefined;
      toast.success(() => <RedeploySuccessToast href={href} />);
    },
    onError: (err) => {
      console.error("Failed to redeploy source:", err);
      toast.error("Failed to redeploy source. Please try again.");
    },
    onSettled: (_data, _err, { slug }) => {
      toast.dismiss(`redeploy-${slug}`);
    },
  });
}

function RedeploySuccessToast({ href }: { href: string | undefined }) {
  if (!href) return <p>Source redeployed successfully!</p>;

  return (
    <p>
      Source redeployed successfully!{" "}
      <a href={href} className="underline">
        View deployment
      </a>
      .
    </p>
  );
}
