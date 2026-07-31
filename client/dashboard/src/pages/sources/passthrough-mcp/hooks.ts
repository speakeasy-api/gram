import { useSdkClient } from "@/contexts/Sdk";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { PassthroughMcpServer } from "@gram/client/models/components/passthroughmcpserver.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { invalidateAllPassthroughMcpServers } from "@gram/client/react-query/passthroughMcpServers.js";
import {
  useMutation,
  useQueryClient,
  type UseMutationResult,
} from "@tanstack/react-query";

export type CreatePassthroughMcpSourceVariables = {
  name?: string | undefined;
  url: string;
  description?: string | undefined;
};

export type CreatePassthroughMcpSourceData = {
  passthroughMcpServer: PassthroughMcpServer;
  mcpServer: McpServer;
};

export function useCreatePassthroughMcpSource(): UseMutationResult<
  CreatePassthroughMcpSourceData,
  Error,
  CreatePassthroughMcpSourceVariables
> {
  const client = useSdkClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({ name, url, description }) => {
      const passthroughMcpServer = await client.passthroughMcp.createServer({
        createPassthroughMcpServerForm: { name, url, description },
      });

      let mcpServer: McpServer;
      try {
        mcpServer = await client.mcpServers.create({
          createMcpServerForm: {
            // mcp_servers.name is required; reuse the display name the
            // server just computed so the wrapping row matches what the
            // dashboard shows for the source.
            name: passthroughMcpServer.name || passthroughMcpServer.url,
            passthroughMcpServerId: passthroughMcpServer.id,
            // No further configuration step exists for a pass-through
            // server (no OAuth, no endpoint to stage), so it can go live
            // immediately rather than parking disabled.
            visibility: "private",
          },
        });
      } catch (linkError) {
        try {
          await client.passthroughMcp.deleteServer({
            id: passthroughMcpServer.id,
          });
        } catch (rollbackError) {
          const linkMsg =
            linkError instanceof Error ? linkError.message : String(linkError);
          const rollbackMsg =
            rollbackError instanceof Error
              ? rollbackError.message
              : String(rollbackError);
          throw new Error(
            `Created pass-through MCP server ${passthroughMcpServer.id} but failed to link an MCP server, and the rollback also failed. Delete it manually before retrying. Cause: ${linkMsg}. Rollback: ${rollbackMsg}.`,
          );
        }
        throw linkError instanceof Error
          ? linkError
          : new Error(String(linkError));
      }

      return { passthroughMcpServer, mcpServer };
    },
    onSuccess: async () => {
      // refetchType "all" forces the refetch even when there are no active
      // observers — Sources isn't mounted while the create form is, so
      // without this the listServers cache stays stale until the next mount.
      await Promise.all([
        invalidateAllPassthroughMcpServers(queryClient, {
          refetchType: "all",
        }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
    },
  });
}

export type DeletePassthroughMcpSourceVariables = {
  passthroughMcpServerId: string;
  // mcp_servers rows backed by this pass-through MCP server. Pre-fetched by
  // the confirmation dialog so the same list the user just confirmed is
  // exactly what gets soft-deleted.
  mcpServerIds: string[];
};

export function useDeletePassthroughMcpSource(): UseMutationResult<
  void,
  Error,
  DeletePassthroughMcpSourceVariables
> {
  const client = useSdkClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({ passthroughMcpServerId, mcpServerIds }) => {
      // Soft-delete each linked mcp_server first; the backend's FK is
      // ON DELETE RESTRICT, so the source delete below would fail while any
      // wrapper still references it.
      const results = await Promise.allSettled(
        mcpServerIds.map((id) => client.mcpServers.delete({ id })),
      );
      const failed = results.find(
        (result): result is PromiseRejectedResult =>
          result.status === "rejected",
      );
      if (failed) {
        throw failed.reason instanceof Error
          ? failed.reason
          : new Error(String(failed.reason));
      }

      await client.passthroughMcp.deleteServer({ id: passthroughMcpServerId });
    },
    onSuccess: async () => {
      await Promise.all([
        invalidateAllPassthroughMcpServers(queryClient, {
          refetchType: "all",
        }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
    },
  });
}
