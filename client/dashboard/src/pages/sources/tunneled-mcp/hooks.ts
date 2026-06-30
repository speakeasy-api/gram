import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { formatTunneledMcpDisplay } from "@/lib/sources";
import { randomSlugSuffix } from "@/lib/slug";
import type {
  McpServer,
  TunneledMcpServer,
} from "@gram/client/models/components";
import {
  invalidateAllGetTunneledMcpServer,
  invalidateAllMcpEndpoints,
  invalidateAllMcpServers,
  invalidateAllTunneledMcpServers,
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

export type CreateTunneledMcpSourceVariables = {
  name: string;
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
    mutationFn: async ({ name }) => {
      const result = await client.tunneledMcp.createServer({
        createTunneledMcpServerForm: { name },
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

      await createDefaultMcpEndpoint(client, mcpServer, orgSlug);

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
      ]);
    },
  });
}

export type LinkMcpServerToTunneledVariables = {
  tunneledMcpServer: TunneledMcpServer;
};

export function useLinkMcpServerToTunneled(): UseMutationResult<
  void,
  Error,
  LinkMcpServerToTunneledVariables
> {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const { orgSlug } = useSlugs();

  return useMutation({
    mutationFn: async ({ tunneledMcpServer }) => {
      const mcpServer = await client.mcpServers.create({
        createMcpServerForm: {
          name: formatTunneledMcpDisplay(tunneledMcpServer),
          tunneledMcpServerId: tunneledMcpServer.id,
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

export type RotateTunneledMcpServerKeyVariables = {
  tunneledMcpServerId: string;
};

export type RotateTunneledMcpServerKeyData = {
  tunneledMcpServer: TunneledMcpServer;
  tunnelKey: string;
};

export function useRotateTunneledMcpServerKey(): UseMutationResult<
  RotateTunneledMcpServerKeyData,
  Error,
  RotateTunneledMcpServerKeyVariables
> {
  const client = useSdkClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({ tunneledMcpServerId }) => {
      const result = await client.tunneledMcp.rotateServerKey({
        rotateTunneledMcpServerKeyForm: {
          id: tunneledMcpServerId,
        },
      });
      return {
        tunneledMcpServer: result.server,
        tunnelKey: result.tunnelKey,
      };
    },
    onSuccess: async () => {
      await Promise.all([
        invalidateAllGetTunneledMcpServer(queryClient, {
          refetchType: "all",
        }),
        invalidateAllTunneledMcpServers(queryClient, { refetchType: "all" }),
      ]);
    },
  });
}

export type DeleteTunneledMcpSourceVariables = {
  tunneledMcpServerId: string;
  mcpServerIds: string[];
};

export function useDeleteTunneledMcpSource(): UseMutationResult<
  void,
  Error,
  DeleteTunneledMcpSourceVariables
> {
  const client = useSdkClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({ tunneledMcpServerId, mcpServerIds }) => {
      for (const id of mcpServerIds) {
        await client.mcpServers.delete({ id });
      }

      await client.tunneledMcp.deleteServer({ id: tunneledMcpServerId });
    },
    onSuccess: async () => {
      await Promise.all([
        invalidateAllTunneledMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
      ]);
    },
  });
}
