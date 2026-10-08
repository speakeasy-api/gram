import { useRBAC } from "@/hooks/useRBAC";
import type { ProxiedMcpTool } from "@/hooks/useProxiedMcpTools";
import { handleAPIError } from "@/lib/errors";
import { useAddMcpServerToolMetadataBatchMutation } from "@gram/client/react-query/addMcpServerToolMetadataBatch.js";
import { useDeleteMcpServerToolMetadataMutation } from "@gram/client/react-query/deleteMcpServerToolMetadata.js";
import { invalidateAllListMcpServerToolMetadata } from "@gram/client/react-query/listMcpServerToolMetadata.js";
import { useSetMcpServerToolMetadataMutation } from "@gram/client/react-query/setMcpServerToolMetadata.js";
import { useSetMcpServerToolMetadataBatchMutation } from "@gram/client/react-query/setMcpServerToolMetadataBatch.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { toast } from "sonner";
import {
  advertisedToForm,
  fullSyncBatch,
  newToolsBatch,
} from "./toolMetadataSync";
import type { ToolMetadataByName } from "@/hooks/useToolMetadata";

/**
 * How far one live session's listing can be trusted as the server's inventory.
 *
 * - `mirror`: the listing is taken as the whole inventory, so an explicit sync
 *   may remove stored tools it no longer shows. Remote servers use this.
 * - `additive`: the listing is only what this caller can see — a tunnel's
 *   upstream may answer each user differently — so nothing is ever removed or
 *   replaced on its evidence. Newly seen tools are recorded; changing or
 *   removing a stored tool is an explicit action on that one tool.
 */
export type ToolMetadataSyncMode = "mirror" | "additive";

export interface UseSyncToolMetadataResult {
  /**
   * Make the stored set mirror the session, removing tools it dropped.
   * Undefined in additive mode, where no listing is authoritative.
   */
  sync: (() => void) | undefined;
  isSyncing: boolean;
  /**
   * Per-tool actions for additive mode, undefined in mirror mode. Each writes
   * exactly one tool: record a newly seen tool, replace a stored tool's
   * annotations with what the session advertises, or remove a stored tool.
   */
  toolActions: ToolMetadataActions | undefined;
}

export interface ToolMetadataActions {
  record: (toolName: string) => void;
  apply: (toolName: string) => void;
  remove: (toolName: string) => void;
  /** The tool a write is in flight for, so its controls can be disabled. */
  pendingTool: string | undefined;
}

/**
 * Keeps Speakeasy's stored tool metadata in step with the live MCP session.
 *
 * The session is only the source the annotations are read FROM — what gets
 * written is persisted against the MCP server for the whole project, so a write
 * changes what every caller of that server sees, not just this viewer.
 *
 * Tools the session advertises for the first time are recorded automatically —
 * there is no stored value to disagree with, so nothing needs confirming.
 * Everything else (a tool whose advertised hints changed, or one the session
 * does not show) waits for an explicit action, because those overwrite or
 * remove records an operator may be relying on. `live` must be a listing that
 * succeeded: tools kept from an earlier listing after a failed refetch are not
 * evidence of anything.
 */
export function useSyncToolMetadata({
  mcpServerId,
  live,
  listedAt = 0,
  stored,
  enabled,
  mode,
  project,
}: {
  mcpServerId: string | undefined;
  live: Record<string, ProxiedMcpTool> | undefined;
  /**
   * When the listing in `live` succeeded. It advances on every successful
   * refetch, even one returning the same (referentially shared) tools.
   */
  listedAt?: number;
  stored: ToolMetadataByName;
  /** False until both sides have loaded, and for servers without metadata. */
  enabled: boolean;
  mode: ToolMetadataSyncMode;
  /** The server's project, for org-level pages with no ambient project. */
  project?: { id: string; slug: string };
}): UseSyncToolMetadataResult {
  const queryClient = useQueryClient();
  const { hasAnyScope } = useRBAC();

  // Writing is gated on mcp:write like any other mutation; a read-only viewer
  // must not have a page visit silently write on their behalf.
  const canWrite = hasAnyScope(["mcp:write"], mcpServerId, project?.id);

  const refresh = () =>
    invalidateAllListMcpServerToolMetadata(queryClient, {
      refetchType: "all",
    });

  // What the automatic pass last wrote (or is writing) per server and project:
  // in additive mode the names of the batch, in mirror mode just that it ran.
  // A re-render, or the refetch the write itself triggers, never sends the
  // same batch twice, while a later listing showing other unrecorded tools
  // writes those. A failed write forgets only its own entry, so the next
  // successful listing — even one returning the same tools — tries again, and
  // a persistent failure is retried once per listing rather than in a loop.
  const autoWritten = useRef(new Map<string, string>());

  // Records tools with no stored entry. Strictly additive: it rejects the whole
  // batch if any tool already has one, so a 409 means our stored snapshot was
  // stale rather than that anything went wrong. This pass is invisible, so that
  // case just reloads the list instead of surfacing an error.
  const add = useAddMcpServerToolMetadataBatchMutation({ onSuccess: refresh });

  // Makes the stored set mirror the session, deleting tools it dropped.
  const set = useSetMcpServerToolMetadataBatchMutation({
    onSuccess: async () => {
      await refresh();
      toast.success("Annotations synced");
    },
    onError: (error) => handleAPIError(error, "Failed to sync tool metadata"),
  });

  const recordOne = useAddMcpServerToolMetadataBatchMutation({
    onSuccess: refresh,
    onError: async (error) => {
      await refresh();
      handleAPIError(error, "Failed to record tool metadata");
    },
  });
  const applyOne = useSetMcpServerToolMetadataMutation({
    onSuccess: refresh,
    onError: (error) => handleAPIError(error, "Failed to update tool metadata"),
  });
  const removeOne = useDeleteMcpServerToolMetadataMutation({
    onSuccess: refresh,
    onError: (error) => handleAPIError(error, "Failed to remove tool metadata"),
  });

  useEffect(() => {
    if (!enabled || !canWrite || !mcpServerId || !live) return;

    const tools = newToolsBatch(live, stored);
    const serverKey = `${project?.slug ?? ""}:${mcpServerId}`;
    const batchKey =
      mode === "additive"
        ? (tools ?? []).map((tool) => tool.toolName).join("\n")
        : "";
    if (autoWritten.current.get(serverKey) === batchKey) return;
    autoWritten.current.set(serverKey, batchKey);
    if (!tools) return;

    add
      .mutateAsync({
        request: {
          gramProject: project?.slug,
          setToolMetadataBatchRequestBody: { mcpServerId, tools },
        },
      })
      .catch(async (error: unknown) => {
        // Only this request's entry: a newer batch, or another server's, keeps
        // its own guard.
        if (autoWritten.current.get(serverKey) === batchKey) {
          autoWritten.current.delete(serverKey);
        }
        if (error instanceof GramError && error.statusCode === 409) {
          await refresh();
          return;
        }
        handleAPIError(error, "Failed to record new tool metadata");
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    enabled,
    canWrite,
    mcpServerId,
    live,
    listedAt,
    stored,
    mode,
    project?.slug,
  ]);

  const pendingTool =
    (recordOne.isPending &&
      recordOne.variables?.request.setToolMetadataBatchRequestBody.tools[0]
        ?.toolName) ||
    (applyOne.isPending &&
      applyOne.variables?.request.setToolMetadataRequestBody.toolName) ||
    (removeOne.isPending && removeOne.variables?.request.toolName) ||
    undefined;

  const toolActions: ToolMetadataActions | undefined =
    mode === "additive" && mcpServerId
      ? {
          record: (toolName) => {
            const tool = live?.[toolName];
            if (!tool || !Object.hasOwn(live, toolName) || pendingTool) return;
            recordOne.mutate({
              request: {
                gramProject: project?.slug,
                setToolMetadataBatchRequestBody: {
                  mcpServerId,
                  tools: [advertisedToForm(toolName, tool)],
                },
              },
            });
          },
          apply: (toolName) => {
            const tool = live?.[toolName];
            if (!tool || !Object.hasOwn(live, toolName) || pendingTool) return;
            applyOne.mutate({
              request: {
                gramProject: project?.slug,
                setToolMetadataRequestBody: {
                  mcpServerId,
                  ...advertisedToForm(toolName, tool),
                },
              },
            });
          },
          remove: (toolName) => {
            if (pendingTool) return;
            removeOne.mutate({
              request: { gramProject: project?.slug, mcpServerId, toolName },
            });
          },
          pendingTool,
        }
      : undefined;

  return {
    sync:
      mode === "mirror"
        ? () => {
            if (!live || !mcpServerId) return;
            set.mutate({
              request: {
                gramProject: project?.slug,
                setToolMetadataBatchRequestBody: {
                  mcpServerId,
                  tools: fullSyncBatch(live),
                },
              },
            });
          }
        : undefined,
    isSyncing: set.isPending,
    toolActions,
  };
}
