import { RemoveMcpSourceDialogContent } from "@/components/sources/RemoveMcpSourceDialogContent";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useDeletePassthroughMcpSource } from "./hooks";

interface RemovePassthroughMcpDialogContentProps {
  passthroughMcpServerId: string;
  url: string;
  linkedMcpServers: McpServer[];
  onClose: () => void;
  onSuccess: () => void;
}

export function RemovePassthroughMcpDialogContent({
  passthroughMcpServerId,
  url,
  linkedMcpServers,
  onClose,
  onSuccess,
}: RemovePassthroughMcpDialogContentProps): JSX.Element {
  const deleteSource = useDeletePassthroughMcpSource();

  return (
    <RemoveMcpSourceDialogContent
      title="Delete Pass-through MCP Server"
      entityDescription="the pass-through MCP server"
      // Confirmation is keyed on the URL since pass-through sources don't
      // have a slugified name.
      confirmLabel="the server URL"
      confirmValue={url}
      successMessage="Pass-through MCP server deleted"
      failureMessage="Failed to delete pass-through MCP server"
      linkedMcpServers={linkedMcpServers}
      isPending={deleteSource.isPending}
      errorMessage={
        deleteSource.isError ? deleteSource.error.message : undefined
      }
      onClose={onClose}
      onSuccess={onSuccess}
      onConfirm={async () => {
        await deleteSource.mutateAsync({
          passthroughMcpServerId,
          mcpServerIds: linkedMcpServers.map((server) => server.id),
        });
      }}
    />
  );
}
