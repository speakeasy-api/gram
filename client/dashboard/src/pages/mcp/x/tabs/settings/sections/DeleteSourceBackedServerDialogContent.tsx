import { RemoveMcpSourceDialogContent } from "@/components/mcp/RemoveMcpSourceDialog";
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
// delete mutation for the source kind and hands the cascade dialog the
// exact list of servers it will remove.
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
  const mcpServerIds = linkedMcpServers.map((server) => server.id);

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
          mcpServerIds,
        });
        return;
      case "tunneled":
        await deleteTunneled.mutateAsync({
          tunneledMcpServerId: target.source.id,
          mcpServerIds,
        });
        return;
      case "unproxied":
        await deleteUnproxied.mutateAsync({
          unproxiedMcpServerId: target.source.id,
          mcpServerIds,
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
    />
  );
}
