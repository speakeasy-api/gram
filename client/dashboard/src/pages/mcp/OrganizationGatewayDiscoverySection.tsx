import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { OrganizationGateway } from "@gram/client/models/components/organizationgateway.js";
import { invalidateAllOrganizationGateway } from "@gram/client/react-query/organizationGateway.js";
import { useSetOrganizationGatewayDiscoveryModeMutation } from "@gram/client/react-query/setOrganizationGatewayDiscoveryMode.js";
import { DiscoveryModeSection } from "./gateway/DiscoveryModeSection";

/**
 * The Organization Gateway's default tool discovery mode: what connections
 * that keep the gateway default at sign-in get. Organization admins set it.
 */
export function OrganizationGatewayDiscoverySection({
  gateway,
  codeModeEntitled,
  canManage,
  isCurrentOrganization,
}: {
  gateway: OrganizationGateway;
  /** The organization's Code Mode entitlement; undefined while unknown. */
  codeModeEntitled: boolean | undefined;
  canManage: boolean;
  isCurrentOrganization: () => boolean;
}): JSX.Element {
  const queryClient = useQueryClient();
  // Code Mode is rolled out per organization by Speakeasy. Organizations
  // without it still see it, disabled, so they know to ask for it.
  const notEnabled = codeModeEntitled === false;
  const codeModeSelectable = gateway.codeModeEnabled && !notEnabled;
  const update = useSetOrganizationGatewayDiscoveryModeMutation({
    onSuccess: async () => {
      if (!isCurrentOrganization()) return;
      await invalidateAllOrganizationGateway(queryClient, {
        refetchType: "all",
      });
      if (!isCurrentOrganization()) return;
      toast.success("Organization Gateway discovery updated");
    },
  });
  return (
    <DiscoveryModeSection
      savedMode={gateway.discoveryMode}
      canWrite={canManage}
      writeScope={{ scope: "org:admin" }}
      codeModeSelectable={codeModeSelectable}
      codeModeNotEnabled={notEnabled}
      codeModeUnavailableLabel="Code Mode (unavailable)"
      codeModeFallbackReason={
        notEnabled
          ? "Code Mode is not enabled for your organization; contact Speakeasy to turn it on."
          : "The code runtime is unavailable."
      }
      isPending={update.isPending}
      error={update.isError ? update.error : null}
      onSave={(mode) =>
        update.mutate({
          request: {
            setGatewayDiscoveryModeRequestBody: { discoveryMode: mode },
          },
        })
      }
    />
  );
}
