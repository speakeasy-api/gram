import { useFetcher } from "@/contexts/Fetcher";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { formatRemoteMcpDisplay } from "@/lib/sources";
import { createDefaultMcpEndpoint } from "@/lib/mcpEndpoints";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import { invalidateAllMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { invalidateAllRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { invalidateAllRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import { invalidateAllUserSessionIssuers } from "@gram/client/react-query/userSessionIssuers.js";
import {
  useMutation,
  useQueryClient,
  type UseMutationResult,
} from "@tanstack/react-query";
import {
  autoConfigureRemoteMcpAuth,
  type AutoConfigureAuthResult,
} from "./autoConfigureAuth";

export type CreateRemoteMcpSourceVariables = {
  name?: string | undefined;
  url: string;
};

export type CreateRemoteMcpSourceData = {
  remoteMcpServer: RemoteMcpServer;
  mcpServer: McpServer;
  authAutoConfig: AutoConfigureAuthResult;
};

export function useCreateRemoteMcpSource(): UseMutationResult<
  CreateRemoteMcpSourceData,
  Error,
  CreateRemoteMcpSourceVariables
> {
  const client = useSdkClient();
  const { fetch: authedFetch } = useFetcher();
  const queryClient = useQueryClient();
  const { orgSlug } = useSlugs();

  return useMutation({
    mutationFn: async ({ name, url }) => {
      const remoteMcpServer = await client.remoteMcp.createServer({
        createServerForm: {
          name,
          url,
          transportType: "streamable-http",
        },
      });

      let mcpServer: McpServer;
      try {
        mcpServer = await client.mcpServers.create({
          createMcpServerForm: {
            // mcp_servers.name is required; reuse the canonical
            // formatRemoteMcpDisplay fallback so the auto-linked row matches
            // what the dashboard shows for the source.
            name: formatRemoteMcpDisplay(remoteMcpServer),
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

      const authAutoConfig = await autoConfigureRemoteMcpAuth({
        client,
        authedFetch,
        remoteMcpServer,
        mcpServer,
      });
      const configuredMcpServer =
        authAutoConfig.status === "configured"
          ? authAutoConfig.mcpServer
          : mcpServer;

      // Pre-stage a default endpoint so the user doesn't have to create one
      // before the server can serve. Best-effort: never rolls back the source.
      await createDefaultMcpEndpoint(client, configuredMcpServer, orgSlug);

      return {
        remoteMcpServer,
        mcpServer: configuredMcpServer,
        authAutoConfig,
      };
    },
    onSuccess: async ({ authAutoConfig }) => {
      // refetchType "all" forces the refetch even when there are no active
      // observers — Sources isn't mounted while the create form is, so without
      // this the listServers cache stays stale until the next mount.
      const invalidations = [
        invalidateAllRemoteMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
        // Every create links a fresh user_session_issuer, so its cache always
        // goes stale regardless of whether auto-config attached a client.
        invalidateAllUserSessionIssuers(queryClient, { refetchType: "all" }),
      ];
      // The issuer/client caches only change when auto-configuration actually
      // ran to completion; a skipped run leaves them untouched, so don't force
      // those extra refetches on the common no-OAuth path.
      if (authAutoConfig.status === "configured") {
        invalidations.push(
          invalidateAllRemoteSessionIssuers(queryClient, {
            refetchType: "all",
          }),
          invalidateAllRemoteSessionClients(queryClient, {
            refetchType: "all",
          }),
        );
      }
      await Promise.all(invalidations);
    },
  });
}

export type LinkMcpServerToRemoteVariables = {
  remoteMcpServer: RemoteMcpServer;
};

// Mirrors the auto-provisioning step in useCreateRemoteMcpSource: create an
// mcp_servers row backed by the given remote MCP server, using the same
// display-name fallback and "disabled" visibility default. Surfaced as its own
// hook so the details page can re-link after a user deletes the auto-created
// server.
export function useLinkMcpServerToRemote(): UseMutationResult<
  void,
  Error,
  LinkMcpServerToRemoteVariables
> {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const { orgSlug } = useSlugs();

  return useMutation({
    mutationFn: async ({ remoteMcpServer }) => {
      // Auto-config of the upstream client is intentionally left to the
      // Authentication tab here.
      const mcpServer = await client.mcpServers.create({
        createMcpServerForm: {
          name: formatRemoteMcpDisplay(remoteMcpServer),
          remoteMcpServerId: remoteMcpServer.id,
          visibility: "disabled",
        },
      });

      // Mirror the create flow: pre-stage a default endpoint. Best-effort.
      await createDefaultMcpEndpoint(client, mcpServer, orgSlug);
    },
    onSuccess: async () => {
      await Promise.all([
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
        // Re-link now mints a fresh user_session_issuer too, so its cache goes
        // stale just as it does on create.
        invalidateAllUserSessionIssuers(queryClient, { refetchType: "all" }),
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
      // cascades to its mcp_endpoints. The deletes are independent, so run
      // them concurrently and surface any failure before touching the source.
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

      await client.remoteMcp.deleteServer({ id: remoteMcpServerId });
    },
    onSuccess: async () => {
      await Promise.all([
        invalidateAllRemoteMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
        invalidateAllUserSessionIssuers(queryClient, { refetchType: "all" }),
      ]);
    },
  });
}
