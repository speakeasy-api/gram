import { useIsSpeakeasyStaff } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { formatRemoteMcpDisplay } from "@/lib/sources";
import {
  deleteSourceCascade,
  fetchLinkedMcpServers,
} from "@/pages/mcp/x/tabs/settings/sections/sourceDelete";
import { invalidateWrapperDeleteAuthViews } from "@/pages/mcp/x/tabs/settings/sections/sourceInvalidation";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { UnproxiedMcpServer } from "@gram/client/models/components/unproxiedmcpserver.js";
import { invalidateAllGetUnproxiedMcpServer } from "@gram/client/react-query/getUnproxiedMcpServer.js";
import { invalidateAllMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
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

export type DeleteUnproxiedMcpSourceVariables = {
  unproxiedMcpServerId: string;
};

// Mirrors server/internal/access.RequireStaffForUnproxiedMcp so the wrappers
// are never deleted ahead of a source delete that is bound to be refused.
export const UNPROXIED_DELETE_STAFF_ONLY_MESSAGE =
  "Unproxied MCP servers can only be deleted by Speakeasy staff.";

export function useDeleteUnproxiedMcpSource(): UseMutationResult<
  void,
  Error,
  DeleteUnproxiedMcpSourceVariables
> {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const isSpeakeasyStaff = useIsSpeakeasyStaff();

  return useMutation({
    mutationFn: async ({ unproxiedMcpServerId }) => {
      if (!isSpeakeasyStaff) {
        throw new Error(UNPROXIED_DELETE_STAFF_ONLY_MESSAGE);
      }
      // Soft-delete each linked mcp_server first; the backend's FK is
      // ON DELETE RESTRICT, so the source delete would fail while any wrapper
      // still references it.
      await deleteSourceCascade({
        listLinked: () =>
          fetchLinkedMcpServers(client, queryClient, { unproxiedMcpServerId }),
        deleteMcpServer: (id) => client.mcpServers.delete({ id }),
        deleteSource: () =>
          client.unproxiedMcp.deleteServer({ id: unproxiedMcpServerId }),
        sourceLabel: "unproxied MCP source",
      });
    },
    onSuccess: async () => {
      // Mark stale only: the deleted server's queries are still mounted until
      // the caller navigates away (see useDeleteRemoteMcpSource).
      await Promise.all([
        invalidateAllUnproxiedMcpServers(queryClient, {
          refetchType: "none",
        }),
        invalidateAllMcpServers(queryClient, { refetchType: "none" }),
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
        invalidateAllGetUnproxiedMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllUnproxiedMcpServers(queryClient, { refetchType: "all" }),
        invalidateWrapperDeleteAuthViews(queryClient, { refetchType: "all" }),
      ]);
    },
  });
}
