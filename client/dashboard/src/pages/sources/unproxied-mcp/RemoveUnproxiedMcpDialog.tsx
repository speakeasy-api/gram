import { RemoveMcpSourceDialogContent } from "@/components/sources/RemoveMcpSourceDialogContent";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useDeleteUnproxiedMcpSource } from "./hooks";

interface RemoveUnproxiedMcpDialogContentProps {
  unproxiedMcpServerId: string;
  url: string;
  linkedMcpServers: McpServer[];
  onClose: () => void;
  onSuccess: () => void;
}

export function RemoveUnproxiedMcpDialogContent({
  unproxiedMcpServerId,
  url,
  linkedMcpServers,
  onClose,
  onSuccess,
}: RemoveUnproxiedMcpDialogContentProps): JSX.Element {
  const deleteSource = useDeleteUnproxiedMcpSource();

  return (
    <RemoveMcpSourceDialogContent
      title="Delete Unproxied MCP Server"
      entityDescription="the unproxied MCP server"
      // Confirmation is keyed on the URL since unproxied sources don't
      // have a slugified name.
      confirmLabel="the server URL"
      confirmValue={url}
      successMessage="Unproxied MCP server deleted"
      failureMessage="Failed to delete unproxied MCP server"
      linkedMcpServers={linkedMcpServers}
      isPending={deleteSource.isPending}
      errorMessage={
        deleteSource.isError ? deleteSource.error.message : undefined
      }
      onClose={onClose}
      onSuccess={onSuccess}
      onConfirm={async () => {
        await deleteSource.mutateAsync({
          unproxiedMcpServerId,
          mcpServerIds: linkedMcpServers.map((server) => server.id),
        });
      }}
    />
  );
}
