import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import {
  deleteSourceCascade,
  fetchLinkedMcpServers,
} from "@/pages/mcp/x/tabs/settings/sections/sourceDelete";
import { invalidateWrapperDeleteAuthViews } from "@/pages/mcp/x/tabs/settings/sections/sourceInvalidation";
import { formatTunneledMcpDisplay } from "@/lib/sources";
import {
  createDefaultMcpEndpoint,
  DEFAULT_ENDPOINT_FAILED_MESSAGE,
} from "@/lib/mcpEndpoints";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { invalidateAllGetTunneledMcpServer } from "@gram/client/react-query/getTunneledMcpServer.js";
import { invalidateAllListTunneledMcpServerConnections } from "@gram/client/react-query/listTunneledMcpServerConnections.js";
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
  userSessionIssuerId?: string;
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
    mutationFn: async ({ name, resourceIdentifier, userSessionIssuerId }) => {
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
            userSessionIssuerId,
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
    // The result carries the plaintext key. Drop it from the mutation cache the
    // moment nothing observes it; the section clears its own copy on close.
    gcTime: 0,
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
};

export function useDeleteTunneledMcpSource(): UseMutationResult<
  void,
  Error,
  DeleteTunneledMcpSourceVariables
> {
  const client = useSdkClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({ tunneledMcpServerId }) => {
      await deleteSourceCascade({
        listLinked: () =>
          fetchLinkedMcpServers(client, queryClient, { tunneledMcpServerId }),
        deleteMcpServer: (id) => client.mcpServers.delete({ id }),
        deleteSource: () =>
          client.tunneledMcp.deleteServer({ id: tunneledMcpServerId }),
        sourceLabel: "tunneled MCP source",
      });
    },
    onSuccess: async () => {
      // Mark stale only (refetchType "none"): the deleted source's own queries
      // are still mounted on the detail page until the caller navigates away,
      // and force-refetching them here would block mutateAsync on requests for
      // a resource that no longer exists, leaving the confirm dialog stuck on
      // "Deleting". Consumers refetch on their next mount after navigation.
      await Promise.all([
        invalidateAllTunneledMcpServers(queryClient, { refetchType: "none" }),
        invalidateAllListTunneledMcpServerConnections(queryClient, {
          refetchType: "none",
        }),
        invalidateAllMcpServers(queryClient, { refetchType: "none" }),
        invalidateAllMcpEndpoints(queryClient, { refetchType: "none" }),
        invalidateWrapperDeleteAuthViews(queryClient, { refetchType: "none" }),
      ]);
    },
    onError: async () => {
      // A partial run left some wrappers gone and the source in place. Refetch
      // so the open dialog lists what remains before the user retries, and so
      // the still-mounted Authentication section drops the issuer and client
      // bindings the deleted wrappers took with them.
      await Promise.all([
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
        invalidateAllGetTunneledMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllTunneledMcpServers(queryClient, { refetchType: "all" }),
        invalidateWrapperDeleteAuthViews(queryClient, { refetchType: "all" }),
      ]);
    },
  });
}
