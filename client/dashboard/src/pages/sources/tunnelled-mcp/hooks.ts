import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { formatTunnelledMcpDisplay } from "@/lib/sources";
import { randomSlugSuffix } from "@/lib/slug";
import type {
  McpServer,
  TunnelledMcpServer,
} from "@gram/client/models/components";
import {
  invalidateAllMcpEndpoints,
  invalidateAllMcpServers,
  invalidateAllTunnelledMcpServers,
} from "@gram/client/react-query/index.js";
import {
  useMutation,
  useQueryClient,
  type UseMutationResult,
} from "@tanstack/react-query";
import { toast } from "sonner";

type SdkClient = ReturnType<typeof useSdkClient>;

const DEFAULT_ENDPOINT_FAILED_MESSAGE =
  "MCP server created, but the default endpoint failed. Add one from the server page.";

async function createDefaultMcpEndpoint(
  client: SdkClient,
  mcpServer: McpServer,
  orgSlug: string | undefined,
): Promise<void> {
  if (!orgSlug) {
    toast.warning(DEFAULT_ENDPOINT_FAILED_MESSAGE);
    return;
  }

  try {
    await client.mcpEndpoints.create({
      createMcpEndpointForm: {
        mcpServerId: mcpServer.id,
        slug: `${orgSlug}-${randomSlugSuffix()}`,
      },
    });
  } catch {
    toast.warning(DEFAULT_ENDPOINT_FAILED_MESSAGE);
  }
}

export type CreateTunnelledMcpSourceVariables = {
  name: string;
};

export type CreateTunnelledMcpSourceData = {
  tunnelledMcpServer: TunnelledMcpServer;
  tunnelKey: string;
  mcpServer: McpServer;
};

export function useCreateTunnelledMcpSource(): UseMutationResult<
  CreateTunnelledMcpSourceData,
  Error,
  CreateTunnelledMcpSourceVariables
> {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const { orgSlug } = useSlugs();

  return useMutation({
    mutationFn: async ({ name }) => {
      const result = await client.tunnelledMcp.createServer({
        createTunnelledMcpServerForm: { name },
      });
      const tunnelledMcpServer = result.server;

      let mcpServer: McpServer;
      try {
        mcpServer = await client.mcpServers.create({
          createMcpServerForm: {
            name: formatTunnelledMcpDisplay(tunnelledMcpServer),
            tunnelledMcpServerId: tunnelledMcpServer.id,
            visibility: "disabled",
          },
        });
      } catch (linkError) {
        try {
          await client.tunnelledMcp.deleteServer({
            id: tunnelledMcpServer.id,
          });
        } catch (rollbackError) {
          const linkMsg =
            linkError instanceof Error ? linkError.message : String(linkError);
          const rollbackMsg =
            rollbackError instanceof Error
              ? rollbackError.message
              : String(rollbackError);
          throw new Error(
            `Created tunnelled MCP server ${tunnelledMcpServer.id} but failed to link an MCP server, and the rollback also failed. Delete it manually before retrying. Cause: ${linkMsg}. Rollback: ${rollbackMsg}.`,
          );
        }
        throw linkError instanceof Error
          ? linkError
          : new Error(String(linkError));
      }

      await createDefaultMcpEndpoint(client, mcpServer, orgSlug);

      return {
        tunnelledMcpServer,
        tunnelKey: result.tunnelKey,
        mcpServer,
      };
    },
    onSuccess: async () => {
      await Promise.all([
        invalidateAllTunnelledMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
      ]);
    },
  });
}

export type LinkMcpServerToTunnelledVariables = {
  tunnelledMcpServer: TunnelledMcpServer;
};

export function useLinkMcpServerToTunnelled(): UseMutationResult<
  void,
  Error,
  LinkMcpServerToTunnelledVariables
> {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const { orgSlug } = useSlugs();

  return useMutation({
    mutationFn: async ({ tunnelledMcpServer }) => {
      const mcpServer = await client.mcpServers.create({
        createMcpServerForm: {
          name: formatTunnelledMcpDisplay(tunnelledMcpServer),
          tunnelledMcpServerId: tunnelledMcpServer.id,
          visibility: "disabled",
        },
      });

      await createDefaultMcpEndpoint(client, mcpServer, orgSlug);
    },
    onSuccess: async () => {
      await Promise.all([
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
      ]);
    },
  });
}

export type DeleteTunnelledMcpSourceVariables = {
  tunnelledMcpServerId: string;
  mcpServerIds: string[];
};

export function useDeleteTunnelledMcpSource(): UseMutationResult<
  void,
  Error,
  DeleteTunnelledMcpSourceVariables
> {
  const client = useSdkClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({ tunnelledMcpServerId, mcpServerIds }) => {
      for (const id of mcpServerIds) {
        await client.mcpServers.delete({ id });
      }

      await client.tunnelledMcp.deleteServer({ id: tunnelledMcpServerId });
    },
    onSuccess: async () => {
      await Promise.all([
        invalidateAllTunnelledMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
      ]);
    },
  });
}
