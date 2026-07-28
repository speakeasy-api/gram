import { RemoveMcpSourceDialogContent } from "@/components/sources/RemoveMcpSourceDialogContent";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useDeleteRemoteMcpSource } from "./hooks";

interface RemoveRemoteMcpDialogContentProps {
  remoteMcpServerId: string;
  url: string;
  linkedMcpServers: McpServer[];
  onClose: () => void;
  onSuccess: () => void;
}

export function RemoveRemoteMcpDialogContent({
  remoteMcpServerId,
  url,
  linkedMcpServers,
  onClose,
  onSuccess,
}: RemoveRemoteMcpDialogContentProps): JSX.Element {
  const deleteSource = useDeleteRemoteMcpSource();

  return (
    <RemoveMcpSourceDialogContent
      title="Delete Remote MCP Server"
      entityDescription="the remote MCP server"
      // Confirmation is keyed on the URL since remote MCP sources don't have
      // a slugified name.
      confirmLabel="the server URL"
      confirmValue={url}
      successMessage="Remote MCP server deleted"
      failureMessage="Failed to delete remote MCP server"
      linkedMcpServers={linkedMcpServers}
      isPending={deleteSource.isPending}
      errorMessage={
        deleteSource.isError ? deleteSource.error.message : undefined
      }
      onClose={onClose}
      onSuccess={onSuccess}
      onConfirm={async () => {
        await deleteSource.mutateAsync({
          remoteMcpServerId,
          mcpServerIds: linkedMcpServers.map((server) => server.id),
        });
      }}
    />
  );
}
