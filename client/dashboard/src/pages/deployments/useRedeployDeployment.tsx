import {
  RedeployDeploymentMutationData,
  RedeployDeploymentMutationVariables,
  useRedeployDeploymentMutation,
} from "@gram/client/react-query/redeployDeployment.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useRoutes } from "@/routes.js";
import { MutationHookOptions } from "@gram/client/react-query";
import { useParams } from "react-router";

/**
 * A wrapper around the useRedeployDeploymentMutation hook that adds UI
 * feedback and query invalidation.
 */
export const useRedeployDeployment = (
  options: MutationHookOptions<
    RedeployDeploymentMutationData,
    Error,
    RedeployDeploymentMutationVariables
  > = {},
) => {
  const queryClient = useQueryClient();
  const routes = useRoutes();

  const redeployMutation = useRedeployDeploymentMutation({
    ...options,
    onMutate: (vars) => {
      toast.loading("Redeploying...", {
        id: vars.request.redeployRequestBody.deploymentId,
      });
      options?.onMutate?.(vars);
    },
    onSuccess: (data, vars, ctx) => {
      // Invalidate and refetch deployments list
      queryClient.invalidateQueries({
        queryKey: ["@gram/client", "deployments", "list"],
      });
      const href = data.deployment?.id
        ? routes.deployments.deployment.href(data.deployment.id)
        : undefined;
      toast.success(() => <RedeploySuccessToast href={href} />);
      options?.onSuccess?.(data, vars, ctx);
    },
    onError: (err, vars, ctx) => {
      console.error("Failed to redeploy:", err);
      toast.error(`Failed to redeploy. Please try again.`);
      options?.onError?.(err, vars, ctx);
    },
    onSettled: (data, err, vars, ctx) => {
      toast.dismiss(vars.request.redeployRequestBody.deploymentId);
      options?.onSettled?.(data, err, vars, ctx);
    },
  });

  return redeployMutation;
};

const RedeploySuccessToast = ({ href }: { href: string | undefined }) => {
  if (!href) return <p>Successfully redeployed!</p>;

  return (
    <p>
      Successfully redeployed!{" "}
      <a href={href} className="underline">
        View deployment
      </a>
      .
    </p>
  );
};
