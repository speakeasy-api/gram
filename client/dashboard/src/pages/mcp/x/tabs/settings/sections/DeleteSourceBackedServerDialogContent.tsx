import { RemoveMcpSourceDialogContent } from "@/components/mcp/RemoveMcpSourceDialog";
import { useDeleteRemoteMcpSource } from "@/pages/sources/remote-mcp/hooks";
import { useDeleteUnproxiedMcpSource } from "@/pages/sources/unproxied-mcp/hooks";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useEffect } from "react";
import { sourceDeleteSpec, type CascadeDeleteTarget } from "./sourceDelete";

// Deleting a remote- or unproxied-backed MCP server deletes the source row
// too, which takes every sibling server and their endpoints with it. This picks the
// delete mutation for the source kind. `linkedMcpServers` is what the dialog
// lists; the mutation refetches the current set before deleting, so a sibling
// created after the page loaded goes too.
export function DeleteSourceBackedServerDialogContent({
  target,
  linkedMcpServers,
  onClose,
  onSuccess,
  onBusyChange,
}: {
  target: CascadeDeleteTarget;
  linkedMcpServers: McpServer[];
  onClose: () => void;
  onSuccess: () => void;
  /** Told while the delete runs, so the dialog can refuse to close. */
  onBusyChange?: (busy: boolean) => void;
}): JSX.Element {
  const deleteRemote = useDeleteRemoteMcpSource();
  const deleteUnproxied = useDeleteUnproxiedMcpSource();

  const mutation = {
    remote: deleteRemote,
    unproxied: deleteUnproxied,
  }[target.kind];
  useEffect(
    () => onBusyChange?.(mutation.isPending),
    [mutation.isPending, onBusyChange],
  );

  const confirm = async () => {
    switch (target.kind) {
      case "remote":
        await deleteRemote.mutateAsync({
          remoteMcpServerId: target.source.id,
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
    />
  );
}
