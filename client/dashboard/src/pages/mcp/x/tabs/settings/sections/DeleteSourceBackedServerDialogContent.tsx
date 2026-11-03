import { RemoveMcpSourceDialogContent } from "@/components/mcp/RemoveMcpSourceDialog";
import { ServerUsageImpactById } from "@/components/mcp-server-usage/ServerUsageConfirmation";
import { useDeleteRemoteMcpSource } from "@/pages/sources/remote-mcp/hooks";
import { useDeleteTunneledMcpSource } from "@/pages/sources/tunneled-mcp/hooks";
import { useDeleteUnproxiedMcpSource } from "@/pages/sources/unproxied-mcp/hooks";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import {
  sourceDeleteSpec,
  type SourceBackedDeleteTarget,
} from "./sourceDelete";

// Deleting a source-backed MCP server deletes the source row too, which
// takes every sibling server and their endpoints with it. This picks the
// delete mutation for the source kind. `linkedMcpServers` is what the dialog
// lists; the mutation refetches the current set before deleting, so a sibling
// created after the page loaded goes too.
export function DeleteSourceBackedServerDialogContent({
  target,
  linkedMcpServers,
  onClose,
  onSuccess,
}: {
  target: SourceBackedDeleteTarget;
  linkedMcpServers: McpServer[];
  onClose: () => void;
  onSuccess: () => void;
}): JSX.Element {
  const deleteRemote = useDeleteRemoteMcpSource();
  const deleteTunneled = useDeleteTunneledMcpSource();
  const deleteUnproxied = useDeleteUnproxiedMcpSource();

  const mutation = {
    remote: deleteRemote,
    tunneled: deleteTunneled,
    unproxied: deleteUnproxied,
  }[target.kind];

  const confirm = async () => {
    switch (target.kind) {
      case "remote":
        await deleteRemote.mutateAsync({
          remoteMcpServerId: target.source.id,
        });
        return;
      case "tunneled":
        await deleteTunneled.mutateAsync({
          tunneledMcpServerId: target.source.id,
        });
        return;
      case "unproxied":
        await deleteUnproxied.mutateAsync({
          unproxiedMcpServerId: target.source.id,
        });
    }
  };

  return (
    <RemoveMcpSourceDialogContent
      {...sourceDeleteSpec(target)}
      linkedMcpServers={linkedMcpServers}
      isPending={mutation.isPending}
      errorMessage={mutation.isError ? mutation.error.message : undefined}
      onClose={onClose}
      onSuccess={onSuccess}
      onConfirm={confirm}
      usage={
        linkedMcpServers.length > 0 && (
          <div className="grid max-h-96 gap-4 overflow-y-auto">
            {linkedMcpServers.map((server) => (
              <ServerUsageImpactById
                key={server.id}
                mcpServerId={server.id}
                heading={
                  linkedMcpServers.length > 1
                    ? `Who uses ${server.name}`
                    : undefined
                }
              />
            ))}
          </div>
        )
      }
    />
  );
}
