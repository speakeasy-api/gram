import { useSdkClient } from "@/contexts/Sdk";
import { formatRemoteMcpDisplay } from "@/lib/sources";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { UnproxiedMcpServer } from "@gram/client/models/components/unproxiedmcpserver.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { invalidateAllUnproxiedMcpServers } from "@gram/client/react-query/unproxiedMcpServers.js";
import {
  useMutation,
  useQueryClient,
  type UseMutationResult,
} from "@tanstack/react-query";

export type CreateUnproxiedMcpSourceVariables = {
  name?: string | undefined;
  url: string;
  description?: string | undefined;
};

export type CreateUnproxiedMcpSourceData = {
  unproxiedMcpServer: UnproxiedMcpServer;
  mcpServer: McpServer;
};

export function useCreateUnproxiedMcpSource(): UseMutationResult<
  CreateUnproxiedMcpSourceData,
  Error,
  CreateUnproxiedMcpSourceVariables
> {
  const client = useSdkClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({ name, url, description }) => {
      const unproxiedMcpServer = await client.unproxiedMcp.createServer({
        createUnproxiedMcpServerForm: { name, url, description },
      });

      let mcpServer: McpServer;
      try {
        mcpServer = await client.mcpServers.create({
          createMcpServerForm: {
            // mcp_servers.name is required; reuse the display name the
            // server just computed so the wrapping row matches what the
            // dashboard shows for the source.
            name: formatRemoteMcpDisplay(unproxiedMcpServer),
            unproxiedMcpServerId: unproxiedMcpServer.id,
            // Unproxied servers have no Gram-hosted endpoint, so
            // disabled/private/public gates nothing Gram actually serves —
            // the vendor's own server is reachable regardless. "public" is
            // the only value that isn't misleading.
            visibility: "public",
          },
        });
      } catch (linkError) {
        try {
          await client.unproxiedMcp.deleteServer({
            id: unproxiedMcpServer.id,
          });
        } catch (rollbackError) {
          const linkMsg =
            linkError instanceof Error ? linkError.message : String(linkError);
          const rollbackMsg =
            rollbackError instanceof Error
              ? rollbackError.message
              : String(rollbackError);
          throw new Error(
            `Created unproxied MCP server ${unproxiedMcpServer.id} but failed to link an MCP server, and the rollback also failed. Delete it manually before retrying. Cause: ${linkMsg}. Rollback: ${rollbackMsg}.`,
          );
        }
        throw linkError instanceof Error
          ? linkError
          : new Error(String(linkError));
      }

      return { unproxiedMcpServer, mcpServer };
    },
    onSuccess: async () => {
      // refetchType "all" forces the refetch even when there are no active
      // observers — Sources isn't mounted while the create form is, so
      // without this the listServers cache stays stale until the next mount.
      await Promise.all([
        invalidateAllUnproxiedMcpServers(queryClient, {
          refetchType: "all",
        }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
    },
  });
}
