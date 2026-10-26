import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { invalidateAllGetMetaMcpServer } from "@gram/client/react-query/getMetaMcpServer.js";
import { invalidateAllMetaMcpServers } from "@gram/client/react-query/metaMcpServers.js";
import { useUpdateMetaMcpServerMutation } from "@gram/client/react-query/updateMetaMcpServer.js";
import { useRBAC } from "@/hooks/useRBAC";
import { useOrganization } from "@/contexts/Auth";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { DiscoveryModeSection } from "./DiscoveryModeSection";

export function GatewayDiscoverySection({
  metaMcpServer,
}: {
  metaMcpServer: MetaMcpServer;
}): JSX.Element {
  const { hasScope } = useRBAC();
  const canWrite = hasScope("mcp:write", metaMcpServer.projectId);
  const organization = useOrganization();
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  // Code Mode is rolled out per organization by Speakeasy. Organizations
  // without it still see it, disabled, so they know to ask for it. Only a
  // successful read says the organization is not enabled; while the read is
  // loading or failed (members without org:read), the gateway's own
  // availability decides.
  const entitlementKnown = features.data !== undefined;
  const notEnabled = features.data?.codeModeEnabled === false;
  // Code Mode is never offered to anonymous callers.
  const requiresSignIn = !metaMcpServer.userSessionIssuerId;
  const codeModeSelectable =
    metaMcpServer.codeModeEnabled === true && !notEnabled;
  const queryClient = useQueryClient();
  const update = useUpdateMetaMcpServerMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllGetMetaMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMetaMcpServers(queryClient, { refetchType: "all" }),
        queryClient.invalidateQueries({ queryKey: ["gatewayInspection"] }),
      ]);
      toast.success("Gateway discovery updated");
    },
  });
  return (
    <DiscoveryModeSection
      savedMode={metaMcpServer.discoveryMode}
      canWrite={canWrite}
      writeScope={{ scope: "mcp:write", resourceId: metaMcpServer.projectId }}
      codeModeSelectable={codeModeSelectable}
      codeModeNotEnabled={notEnabled}
      codeModeUnavailableLabel={
        requiresSignIn
          ? "Code Mode (requires sign-in)"
          : "Code Mode (unavailable)"
      }
      codeModeFallbackReason={
        notEnabled
          ? "Code Mode is not enabled for your organization; contact Speakeasy to turn it on."
          : requiresSignIn
            ? "Code Mode requires sign-in, and this gateway allows anonymous callers."
            : entitlementKnown
              ? "The code runtime is unavailable."
              : "Code Mode is unavailable for this gateway."
      }
      isPending={update.isPending}
      error={update.isError ? update.error : null}
      onSave={(mode) =>
        update.mutate({
          request: {
            updateMetaMcpServerForm: {
              id: metaMcpServer.id,
              name: metaMcpServer.name,
              discoveryMode: mode,
            },
          },
        })
      }
    />
  );
}
