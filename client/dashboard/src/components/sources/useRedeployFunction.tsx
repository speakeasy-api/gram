import { useSdkClient } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import {
  useActiveDeployment,
  useLatestDeployment,
} from "@gram/client/react-query/index.js";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

/**
 * Hook that redeploys a single function source by evolving the active
 * deployment with the function's current asset ID from the latest deployment.
 *
 * Flow:
 * 1. Look up the function in the latest deployment to get its current asset info
 * 2. Identify the active deployment
 * 3. Call evolve on the active deployment, upserting the function with the
 *    asset from the latest deployment
 */
export function useRedeployFunction() {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const { data: latestResult } = useLatestDeployment();
  const { data: activeResult } = useActiveDeployment();

  return useMutation({
    mutationFn: async (functionSlug: string) => {
      const latestDeployment = latestResult?.deployment;
      if (!latestDeployment) {
        throw new Error("No latest deployment found.");
      }

      const fn = latestDeployment.functionsAssets?.find(
        (f) => f.slug === functionSlug,
      );
      if (!fn) {
        throw new Error("Function not found in latest deployment.");
      }

      const activeDeployment = activeResult?.deployment;
      if (!activeDeployment) {
        throw new Error("No active deployment found.");
      }

      return client.deployments.evolveDeployment({
        evolveForm: {
          deploymentId: activeDeployment.id,
          upsertFunctions: [
            {
              assetId: fn.assetId,
              name: fn.name,
              slug: fn.slug,
              runtime: fn.runtime,
            },
          ],
        },
      });
    },
    onMutate: (functionSlug) => {
      toast.loading("Redeploying function...", {
        id: `redeploy-fn-${functionSlug}`,
      });
    },
    onSuccess: (data) => {
      queryClient.invalidateQueries({
        queryKey: ["@gram/client", "deployments"],
      });
      const href = data.deployment?.id
        ? routes.deployments.deployment.href(data.deployment.id)
        : undefined;
      toast.success(() => <RedeployFunctionSuccessToast href={href} />);
    },
    onError: (err) => {
      console.error("Failed to redeploy function:", err);
      toast.error("Failed to redeploy function. Please try again.");
    },
    onSettled: (_data, _err, functionSlug) => {
      toast.dismiss(`redeploy-fn-${functionSlug}`);
    },
  });
}

function RedeployFunctionSuccessToast({ href }: { href: string | undefined }) {
  if (!href) return <p>Function redeployed successfully!</p>;

  return (
    <p>
      Function redeployed successfully!{" "}
      <a href={href} className="underline">
        View deployment
      </a>
      .
    </p>
  );
}
