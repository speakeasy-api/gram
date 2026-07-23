import { useRBAC } from "@/hooks/useRBAC";
import type { ProxiedMcpTool } from "@/hooks/useProxiedMcpTools";
import { handleAPIError } from "@/lib/errors";
import { useAddMcpServerToolMetadataBatchMutation } from "@gram/client/react-query/addMcpServerToolMetadataBatch.js";
import { invalidateAllListMcpServerToolMetadata } from "@gram/client/react-query/listMcpServerToolMetadata.js";
import { useSetMcpServerToolMetadataBatchMutation } from "@gram/client/react-query/setMcpServerToolMetadataBatch.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { toast } from "sonner";
import { fullSyncBatch, newToolsBatch } from "./toolMetadataSync";
import type { ToolMetadataByName } from "@/hooks/useToolMetadata";

export interface UseSyncToolMetadataResult {
  /** Make the stored set mirror the session, removing tools it dropped. */
  sync: () => void;
  isSyncing: boolean;
}

/**
 * Keeps Speakeasy's stored tool metadata in step with the live MCP session.
 *
 * The session is only the source the annotations are read FROM — what gets
 * written is persisted against the MCP server for the whole project, so a sync
 * changes what every caller of that server sees, not just this viewer.
 *
 * Tools the session advertises for the first time are recorded automatically —
 * there is no stored value to disagree with, so nothing needs confirming.
 * Everything else (a tool whose advertised hints changed, or one the session
 * stopped advertising and so would be removed) waits for an explicit sync,
 * because those overwrite or remove records an operator may be relying on.
 */
export function useSyncToolMetadata({
  mcpServerId,
  live,
  stored,
  enabled,
}: {
  mcpServerId: string | undefined;
  live: Record<string, ProxiedMcpTool> | undefined;
  stored: ToolMetadataByName;
  /** False until both sides have loaded, and for servers without metadata. */
  enabled: boolean;
}): UseSyncToolMetadataResult {
  const queryClient = useQueryClient();
  const { hasAnyScope } = useRBAC();

  // Writing is gated on mcp:write like any other mutation; a read-only viewer
  // must not have a page visit silently write on their behalf.
  const canWrite = hasAnyScope(["mcp:write"], mcpServerId);

  const refresh = () =>
    invalidateAllListMcpServerToolMetadata(queryClient, {
      refetchType: "all",
    });

  // Guard the automatic pass so a re-render (or the refetch the write itself
  // triggers) can't fire it twice for the same server. Released whenever the
  // write fails, so a transient error doesn't leave the tools unrecorded until
  // the page is remounted — the next refetch gets to try again.
  const autoWrittenFor = useRef<string | null>(null);

  // Records tools with no stored entry. Strictly additive: it rejects the whole
  // batch if any tool already has one, so a 409 means our stored snapshot was
  // stale rather than that anything went wrong. This pass is invisible, so that
  // case just reloads the list instead of surfacing an error.
  const add = useAddMcpServerToolMetadataBatchMutation({
    onSuccess: refresh,
    onError: async (error) => {
      autoWrittenFor.current = null;
      if (error instanceof GramError && error.statusCode === 409) {
        await refresh();
        return;
      }
      handleAPIError(error, "Failed to record new tool metadata");
    },
  });

  // Makes the stored set mirror the session, deleting tools it dropped.
  const set = useSetMcpServerToolMetadataBatchMutation({
    onSuccess: async () => {
      await refresh();
      toast.success("Annotations synced");
    },
    onError: (error) => handleAPIError(error, "Failed to sync tool metadata"),
  });

  useEffect(() => {
    if (!enabled || !canWrite || !mcpServerId || !live) return;
    if (autoWrittenFor.current === mcpServerId) return;

    const tools = newToolsBatch(live, stored);
    autoWrittenFor.current = mcpServerId;
    if (!tools) return;

    add.mutate({
      request: {
        setToolMetadataBatchRequestBody: { mcpServerId, tools },
      },
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, canWrite, mcpServerId, live, stored]);

  return {
    sync: () => {
      if (!live || !mcpServerId) return;
      set.mutate({
        request: {
          setToolMetadataBatchRequestBody: {
            mcpServerId,
            tools: fullSyncBatch(live),
          },
        },
      });
    },
    isSyncing: set.isPending,
  };
}
