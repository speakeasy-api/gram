import { useRoutes } from "@/routes.js";
import { MutationHookOptions } from "@gram/client/react-query";
import {
  RedeployDeploymentMutationData,
  RedeployDeploymentMutationVariables,
  useRedeployDeploymentMutation,
} from "@gram/client/react-query/redeployDeployment.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "@/lib/toast";

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
    onMutate: (vars, ctx) => {
      toast.loading("Redeploying...", {
        id: vars.request.redeployRequestBody.deploymentId,
      });
      options?.onMutate?.(vars, ctx);
    },
    onSuccess: (data, vars, onMutateResult, ctx) => {
      // Invalidate and refetch deployments list
      queryClient.invalidateQueries({
        queryKey: ["@gram/client", "deployments", "list"],
      });
      const href = data.deployment?.id
        ? routes.deployments.deployment.href(data.deployment.id)
        : undefined;
      toast.success(() => <RedeploySuccessToast href={href} />);
      options?.onSuccess?.(data, vars, onMutateResult, ctx);
    },
    onError: (err, vars, onMutateResult, ctx) => {
      console.error("Failed to redeploy:", err);
      toast.error(`Failed to redeploy. Please try again.`, { persist: true });
      options?.onError?.(err, vars, onMutateResult, ctx);
    },
    onSettled: (data, err, vars, onMutateResult, ctx) => {
      toast.dismiss(vars.request.redeployRequestBody.deploymentId);
      options?.onSettled?.(data, err, vars, onMutateResult, ctx);
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
