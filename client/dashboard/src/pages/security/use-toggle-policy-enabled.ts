import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { invalidateAllRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { invalidateAllRiskOverview } from "@gram/client/react-query/riskOverview.js";
import { invalidateAllRiskPoliciesGet } from "@gram/client/react-query/riskPoliciesGet.js";
import { invalidateAllRiskPoliciesStatus } from "@gram/client/react-query/riskPoliciesStatus.js";
import {
  useRiskPoliciesUpdateMutation,
  type RiskPoliciesUpdateMutationVariables,
} from "@gram/client/react-query/riskPoliciesUpdate.js";

export function togglePolicyEnabledVariables(
  id: string,
  name: string,
  enabled: boolean,
): RiskPoliciesUpdateMutationVariables {
  return {
    request: {
      updateRiskPolicyRequestBody: { id, name, enabled },
    },
  };
}

export function useTogglePolicyEnabled(): ReturnType<
  typeof useRiskPoliciesUpdateMutation
> {
  const queryClient = useQueryClient();
  return useRiskPoliciesUpdateMutation({
    onSuccess: (policy) => {
      void invalidateAllRiskListPolicies(queryClient);
      void invalidateAllRiskPoliciesGet(queryClient);
      void invalidateAllRiskPoliciesStatus(queryClient);
      void invalidateAllRiskOverview(queryClient);
      toast.success(
        policy.enabled
          ? "Policy enabled. New messages will be scanned."
          : "Policy disabled. New messages will not be scanned.",
      );
    },
    onError: (err) =>
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : "Failed to update policy.",
      ),
  });
}
