import { RemoveMcpSourceDialogContent } from "@/components/sources/RemoveMcpSourceDialogContent";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useDeleteTunneledMcpSource } from "./hooks";

interface RemoveTunneledMcpDialogContentProps {
  tunneledMcpServerId: string;
  displayName: string;
  linkedMcpServers: McpServer[];
  onClose: () => void;
  onSuccess: () => void;
}

export function RemoveTunneledMcpDialogContent({
  tunneledMcpServerId,
  displayName,
  linkedMcpServers,
  onClose,
  onSuccess,
}: RemoveTunneledMcpDialogContentProps): JSX.Element {
  const deleteSource = useDeleteTunneledMcpSource();

  return (
    <RemoveMcpSourceDialogContent
      title="Delete Tunneled MCP Server"
      entityDescription="the tunneled MCP source"
      confirmLabel="the display name"
      confirmValue={displayName}
      successMessage="Tunneled MCP server deleted"
      failureMessage="Failed to delete tunneled MCP server"
      linkedMcpServers={linkedMcpServers}
      isPending={deleteSource.isPending}
      errorMessage={
        deleteSource.isError ? deleteSource.error.message : undefined
      }
      onClose={onClose}
      onSuccess={onSuccess}
      onConfirm={async () => {
        await deleteSource.mutateAsync({
          tunneledMcpServerId,
          mcpServerIds: linkedMcpServers.map((server) => server.id),
        });
      }}
    />
  );
}
