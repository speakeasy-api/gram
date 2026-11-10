import { handleAPIError } from "@/lib/errors";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet.js";
import {
  invalidateAllOrganizationGateway,
  useOrganizationGateway,
} from "@gram/client/react-query/organizationGateway.js";
import {
  invalidateAllProductFeatures,
  useProductFeatures,
} from "@gram/client/react-query/productFeatures.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

export type OrganizationGatewaySetting = {
  features: ReturnType<typeof useProductFeatures>;
  gateway: ReturnType<typeof useOrganizationGateway>;
  /**
   * Whether the gateway is switched on. The stored setting decides; when it
   * can't be read, the gateway's own answer does only if it is definite
   * (served is on, turned off is off). Otherwise undefined.
   */
  on: boolean | undefined;
  setOn: (on: boolean, options?: { onSuccess?: () => void }) => void;
  isPending: boolean;
};

/**
 * The Organization Gateway's on/off state and the switch that changes it. The stored
 * product feature turns the gateway off, so "on" is its inverse. The gateway
 * detail page uses this state.
 */
export function useOrganizationGatewaySetting({
  organizationId,
  isCurrentOrganization,
}: {
  organizationId: string;
  isCurrentOrganization: () => boolean;
}): OrganizationGatewaySetting {
  const queryClient = useQueryClient();
  const features = useProductFeatures({ organizationId }, undefined, {
    throwOnError: false,
  });
  const gateway = useOrganizationGateway(undefined, undefined, {
    throwOnError: false,
  });
  const mutation = useFeaturesSetMutation({
    onSuccess: async (_, variables) => {
      if (!isCurrentOrganization()) return;
      await Promise.all([
        invalidateAllProductFeatures(queryClient),
        invalidateAllOrganizationGateway(queryClient),
      ]);
      if (!isCurrentOrganization()) return;
      toast.success(
        variables.request.setProductFeatureRequestBody.enabled
          ? "Organization Gateway turned off"
          : "Organization Gateway turned on",
      );
    },
    onError: (error) => {
      if (!isCurrentOrganization()) return;
      handleAPIError(error, "Failed to update the Organization Gateway");
    },
  });

  const setOn = (on: boolean, options?: { onSuccess?: () => void }) =>
    mutation.mutate(
      {
        request: {
          setProductFeatureRequestBody: {
            organizationId,
            featureName: FeatureName.OrganizationGatewayDisabled,
            enabled: !on,
          },
        },
      },
      options,
    );

  return {
    features,
    gateway,
    on: features.data
      ? !features.data.organizationGatewayDisabled
      : gateway.data?.available
        ? true
        : gateway.data?.unavailableReason === "turned_off"
          ? false
          : undefined,
    setOn,
    isPending: mutation.isPending,
  };
}
