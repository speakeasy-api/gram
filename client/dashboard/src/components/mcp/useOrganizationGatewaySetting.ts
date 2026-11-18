import { handleAPIError } from "@/lib/errors";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet.js";
import {
  invalidateAllOrganizationGateway,
  useOrganizationGateway,
} from "@gram/client/react-query/organizationGateway.js";
import { invalidateAllProjectGateway } from "@gram/client/react-query/projectGateway.js";
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
 * product feature turns the gateway off, so "on" is its inverse. It also
 * turns every Project Gateway on or off. Both gateway pages use this state;
 * `enabled` is false for a reader without organization access, who sees a
 * Project Gateway but not the setting.
 */
export function useOrganizationGatewaySetting({
  organizationId,
  isCurrentOrganization,
  enabled = true,
}: {
  organizationId: string;
  isCurrentOrganization: () => boolean;
  enabled?: boolean;
}): OrganizationGatewaySetting {
  const queryClient = useQueryClient();
  const features = useProductFeatures({ organizationId }, undefined, {
    throwOnError: false,
    enabled,
  });
  const gateway = useOrganizationGateway(undefined, undefined, {
    throwOnError: false,
    enabled,
  });
  const mutation = useFeaturesSetMutation({
    onSuccess: async (_, variables) => {
      if (!isCurrentOrganization()) return;
      await Promise.all([
        invalidateAllProductFeatures(queryClient),
        invalidateAllOrganizationGateway(queryClient),
        invalidateAllProjectGateway(queryClient),
      ]);
      if (!isCurrentOrganization()) return;
      toast.success(
        variables.request.setProductFeatureRequestBody.enabled
          ? "Gateways turned off"
          : "Gateways turned on",
      );
    },
    onError: (error) => {
      if (!isCurrentOrganization()) return;
      handleAPIError(error, "Failed to update the gateways");
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
