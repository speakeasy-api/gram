import { SharedTunnelConfirmDialog } from "@/components/mcp/shared-tunnel-impact";
import { Text } from "@/components/ui/Text";
import { formatTunneledMcpDisplay } from "@/lib/sources";
import { RESOURCE_IDENTIFIER_EXPLAINER } from "@/pages/sources/tunneled-mcp/copy";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { useUpdateTunneledMcpServerMutation } from "@gram/client/react-query/updateTunneledMcpServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { EditableSourceFieldSection } from "./EditableSourceFieldSection";
import { invalidateTunneledMcpSourceViews } from "./sourceInvalidation";

export const MCP_RESOURCE_IDENTIFIER_SECTION_ID = "resource-identifier";

export function ResourceIdentifierSection({
  tunneledMcpServer,
  mcpServerId,
}: {
  tunneledMcpServer: TunneledMcpServer;
  mcpServerId: string;
}): JSX.Element {
  const update = useUpdateTunneledMcpServerMutation();
  const queryClient = useQueryClient();

  return (
    <EditableSourceFieldSection
      id={MCP_RESOURCE_IDENTIFIER_SECTION_ID}
      title="Resource Identifier"
      description={`${RESOURCE_IDENTIFIER_EXPLAINER} Leave blank if the server has no OAuth of its own. The identifier belongs to the tunnel, so it is shared by every MCP server on it.`}
      label="Protected resource identifier"
      placeholder="https://mcp.internal.example.com/mcp"
      stored={tunneledMcpServer.resourceIdentifier ?? ""}
      projectId={tunneledMcpServer.projectId}
      footerHint="Optional. Clearing the field unsets the identifier."
      save={async (value) => {
        // An empty string clears the identifier back to unset.
        const saved = await update.mutateAsync({
          request: {
            updateTunneledMcpServerForm: {
              id: tunneledMcpServer.id,
              resourceIdentifier: value,
            },
          },
        });
        await invalidateTunneledMcpSourceViews(queryClient);
        return saved.resourceIdentifier ?? "";
      }}
      toastMessage={(cleared) =>
        cleared ? "Resource identifier cleared" : "Resource identifier updated"
      }
      fallbackError="Failed to update resource identifier"
      confirmSave={({ value, ...dialog }) => (
        <SharedTunnelConfirmDialog
          {...dialog}
          tunneledMcpServerId={tunneledMcpServer.id}
          tunnelName={formatTunneledMcpDisplay(tunneledMcpServer)}
          currentMcpServerId={mcpServerId}
          publicWarning={tunneledMcpServer.allowPublic}
          title={
            value ? "Change resource identifier?" : "Clear resource identifier?"
          }
          description="The resource identifier is the audience of signed caller assertions and decides which user credentials are routed to the upstream server."
          effect="Changing it changes the audience and credential routing for all of them at once."
          confirmLabel={value ? "Save identifier" : "Clear identifier"}
          pendingLabel="Saving"
        >
          <Text small>
            New value: <span className="font-mono">{value || "(unset)"}</span>
          </Text>
        </SharedTunnelConfirmDialog>
      )}
    />
  );
}
