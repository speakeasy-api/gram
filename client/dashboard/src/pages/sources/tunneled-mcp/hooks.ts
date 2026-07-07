import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { formatTunneledMcpDisplay } from "@/lib/sources";
import { createDefaultMcpEndpoint } from "@/lib/mcpEndpoints";
import type {
  McpServer,
  TunneledMcpServer,
} from "@gram/client/models/components";
import {
  invalidateAllListTunneledMcpServerConnections,
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
        invalidateAllListTunneledMcpServerConnections(queryClient, {
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
      // Linked-server deletes are independent; run them concurrently and
      // surface any failure before touching the source itself.
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

      await client.tunneledMcp.deleteServer({ id: tunneledMcpServerId });
    },
    onSuccess: async () => {
      await Promise.all([
        invalidateAllTunneledMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllListTunneledMcpServerConnections(queryClient, {
          refetchType: "all",
        }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
      ]);
    },
  });
}
