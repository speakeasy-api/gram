import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { invalidateAllRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { invalidateAllRiskOverview } from "@gram/client/react-query/riskOverview.js";
import { invalidateAllRiskPoliciesGet } from "@gram/client/react-query/riskPoliciesGet.js";
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

/** What turning a policy on or off changes: an approval policy holds tool
 *  calls, every other policy scans messages. */
function toggledMessage(policy: RiskPolicy): string {
  if (policy.action === "require_approval") {
    return policy.enabled
      ? "Policy enabled. Calls to its tools wait for approval."
      : "Policy disabled. Calls to its tools are no longer held.";
  }
  return policy.enabled
    ? "Policy enabled. New messages will be scanned."
    : "Policy disabled. New messages will not be scanned.";
}

export function useTogglePolicyEnabled(): ReturnType<
  typeof useRiskPoliciesUpdateMutation
> {
  const queryClient = useQueryClient();
  return useRiskPoliciesUpdateMutation({
    onSuccess: (policy) => {
      void invalidateAllRiskListPolicies(queryClient);
      void invalidateAllRiskPoliciesGet(queryClient);
      void invalidateAllRiskOverview(queryClient);
      toast.success(toggledMessage(policy));
    },
    onError: (err) =>
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : "Failed to update policy.",
      ),
  });
}
