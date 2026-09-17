import { RESOURCE_IDENTIFIER_EXPLAINER } from "@/pages/sources/tunneled-mcp/copy";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { useUpdateTunneledMcpServerMutation } from "@gram/client/react-query/updateTunneledMcpServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { EditableSourceFieldSection } from "./EditableSourceFieldSection";
import { invalidateTunneledMcpSourceViews } from "./sourceInvalidation";

export const MCP_RESOURCE_IDENTIFIER_SECTION_ID = "resource-identifier";

export function ResourceIdentifierSection({
  tunneledMcpServer,
}: {
  tunneledMcpServer: TunneledMcpServer;
}): JSX.Element {
  const update = useUpdateTunneledMcpServerMutation();
  const queryClient = useQueryClient();

  return (
    <EditableSourceFieldSection
      id={MCP_RESOURCE_IDENTIFIER_SECTION_ID}
      title="Resource Identifier"
      description={`${RESOURCE_IDENTIFIER_EXPLAINER} Leave blank if the server has no OAuth of its own.`}
      label="Protected resource identifier"
      placeholder="https://mcp.internal.example.com/mcp"
      stored={tunneledMcpServer.resourceIdentifier ?? ""}
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
    />
  );
}
