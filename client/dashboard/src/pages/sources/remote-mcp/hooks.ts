import { useSdkClient } from "@/contexts/Sdk";
import type { RemoteMcpServer } from "@gram/client/models/components";
import {
  invalidateAllMcpEndpoints,
  invalidateAllMcpServers,
  invalidateAllRemoteMcpServers,
} from "@gram/client/react-query/index.js";
import {
  useMutation,
  useQueryClient,
  type UseMutationResult,
} from "@tanstack/react-query";

export type CreateRemoteMcpSourceVariables = {
  name?: string | undefined;
  url: string;
};

export type CreateRemoteMcpSourceData = { remoteMcpServer: RemoteMcpServer };

export function useCreateRemoteMcpSource(): UseMutationResult<
  CreateRemoteMcpSourceData,
  Error,
  CreateRemoteMcpSourceVariables
> {
  const client = useSdkClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({ name, url }) => {
      const remoteMcpServer = await client.remoteMcp.createServer({
        createServerForm: {
          name,
          url,
          transportType: "streamable-http",
          headers: [],
        },
      });

      try {
        await client.mcpServers.create({
          createMcpServerForm: {
            remoteMcpServerId: remoteMcpServer.id,
            visibility: "disabled",
          },
        });
      } catch (linkError) {
        try {
          await client.remoteMcp.deleteServer({ id: remoteMcpServer.id });
        } catch (rollbackError) {
          const linkMsg =
            linkError instanceof Error ? linkError.message : String(linkError);
          const rollbackMsg =
            rollbackError instanceof Error
              ? rollbackError.message
              : String(rollbackError);
          throw new Error(
            `Created remote MCP server ${remoteMcpServer.id} but failed to link an MCP server, and the rollback also failed. Delete it manually before retrying. Cause: ${linkMsg}. Rollback: ${rollbackMsg}.`,
          );
        }
        throw linkError instanceof Error
          ? linkError
          : new Error(String(linkError));
      }

      return { remoteMcpServer };
    },
    onSuccess: async () => {
      // refetchType "all" forces the refetch even when there are no active
      // observers — Sources isn't mounted while the create form is, so without
      // this the listServers cache stays stale until the next mount.
      await Promise.all([
        invalidateAllRemoteMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
    },
  });
}

export type DeleteRemoteMcpSourceVariables = {
  remoteMcpServerId: string;
  // mcp_servers rows backed by this remote MCP server. Pre-fetched by the
  // confirmation dialog so the same list the user just confirmed is exactly
  // what gets soft-deleted.
  mcpServerIds: string[];
};

export function useDeleteRemoteMcpSource(): UseMutationResult<
  void,
  Error,
  DeleteRemoteMcpSourceVariables
> {
  const client = useSdkClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({ remoteMcpServerId, mcpServerIds }) => {
      // Soft-delete each linked mcp_server first; the server-side handler
      // cascades to its mcp_endpoints. Sequential keeps error surfacing simple
      // — if one fails partway through we want to know which.
      for (const id of mcpServerIds) {
        await client.mcpServers.delete({ id });
      }

      await client.remoteMcp.deleteServer({ id: remoteMcpServerId });
    },
    onSuccess: async () => {
      await Promise.all([
        invalidateAllRemoteMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
      ]);
    },
  });
}
