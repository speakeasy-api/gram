import { useOrganization } from "@/contexts/Auth";
import { useIsCurrentOrganization } from "@/hooks/useIsCurrentOrganization";
import { handleAPIError } from "@/lib/errors";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet.js";
import {
  invalidateAllProductFeatures,
  useProductFeatures,
} from "@gram/client/react-query/productFeatures.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

export type JudgeFailureMode = {
  features: ReturnType<typeof useProductFeatures>;
  /**
   * Whether a prompt policy blocks the message when its judge times out or
   * errors. Organization-wide; undefined until the setting loads.
   */
  failClosed: boolean | undefined;
  setFailClosed: (
    failClosed: boolean,
    options?: { onSuccess?: () => void },
  ) => void;
  isPending: boolean;
};

/**
 * The organization's risk failure mode, which decides what every prompt
 * policy does when its judge can't answer. Shared by the editor's card, the
 * review summary, and the guardrail replay so all three read the same value.
 */
export function useJudgeFailureMode(): JudgeFailureMode {
  const organization = useOrganization();
  const isCurrentOrganization = useIsCurrentOrganization(organization.id);
  const queryClient = useQueryClient();
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  const mutation = useFeaturesSetMutation({
    onSuccess: async (_, variables) => {
      if (!isCurrentOrganization()) return;
      await invalidateAllProductFeatures(queryClient);
      if (!isCurrentOrganization()) return;
      toast.success(
        variables.request.setProductFeatureRequestBody.enabled
          ? "Prompt policies now block when the judge can't answer"
          : "Prompt policies now allow when the judge can't answer",
      );
    },
    onError: (error) => {
      if (!isCurrentOrganization()) return;
      handleAPIError(error, "Failed to update the judge failure setting");
    },
  });

  const setFailClosed = (
    failClosed: boolean,
    options?: { onSuccess?: () => void },
  ) =>
    mutation.mutate(
      {
        request: {
          setProductFeatureRequestBody: {
            organizationId: organization.id,
            featureName: FeatureName.RiskEnforcementFailClosed,
            enabled: failClosed,
          },
        },
      },
      options,
    );

  return {
    features,
    failClosed: features.data?.riskEnforcementFailClosed,
    setFailClosed,
    isPending: mutation.isPending,
  };
}
