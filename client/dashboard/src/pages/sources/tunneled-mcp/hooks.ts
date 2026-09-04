import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { formatTunneledMcpDisplay } from "@/lib/sources";
import {
  createDefaultMcpEndpoint,
  DEFAULT_ENDPOINT_FAILED_MESSAGE,
} from "@/lib/mcpEndpoints";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { invalidateAllMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { invalidateAllTunneledMcpServers } from "@gram/client/react-query/tunneledMcpServers.js";
import { invalidateAllUserSessionIssuers } from "@gram/client/react-query/userSessionIssuers.js";
import {
  useMutation,
  useQueryClient,
  type UseMutationResult,
} from "@tanstack/react-query";
import { toast } from "sonner";

export type CreateTunneledMcpSourceVariables = {
  name: string;
  resourceIdentifier?: string;
};

export type CreateTunneledMcpSourceData = {
  tunneledMcpServer: TunneledMcpServer;
  tunnelKey: string;
  mcpServer: McpServer;
};

export function useCreateTunneledMcpSource(): UseMutationResult<
  CreateTunneledMcpSourceData,
  Error,
  CreateTunneledMcpSourceVariables
> {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const { orgSlug } = useSlugs();

  return useMutation({
    mutationFn: async ({ name, resourceIdentifier }) => {
      const result = await client.tunneledMcp.createServer({
        createTunneledMcpServerForm: { name, resourceIdentifier },
      });
      const tunneledMcpServer = result.server;

      let mcpServer: McpServer;
      try {
        mcpServer = await client.mcpServers.create({
          createMcpServerForm: {
            name: formatTunneledMcpDisplay(tunneledMcpServer),
            tunneledMcpServerId: tunneledMcpServer.id,
            visibility: "disabled",
          },
        });
      } catch (linkError) {
        try {
          await client.tunneledMcp.deleteServer({
            id: tunneledMcpServer.id,
          });
        } catch (rollbackError) {
          const linkMsg =
            linkError instanceof Error ? linkError.message : String(linkError);
          const rollbackMsg =
            rollbackError instanceof Error
              ? rollbackError.message
              : String(rollbackError);
          throw new Error(
            `Created tunneled MCP server ${tunneledMcpServer.id} but failed to link an MCP server, and the rollback also failed. Delete it manually before retrying. Cause: ${linkMsg}. Rollback: ${rollbackMsg}.`,
          );
        }
        throw linkError instanceof Error
          ? linkError
          : new Error(String(linkError));
      }

      if (orgSlug) {
        await createDefaultMcpEndpoint(client, mcpServer, orgSlug);
      } else {
        toast.warning(DEFAULT_ENDPOINT_FAILED_MESSAGE);
      }

      return {
        tunneledMcpServer,
        tunnelKey: result.tunnelKey,
        mcpServer,
      };
    },
    onSuccess: async () => {
      await Promise.all([
        invalidateAllTunneledMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
        invalidateAllUserSessionIssuers(queryClient, { refetchType: "all" }),
      ]);
    },
  });
}
