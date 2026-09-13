import {
  deleteSourceCascade,
  fetchLinkedMcpServers,
} from "@/pages/mcp/x/tabs/settings/sections/sourceDelete";
import { invalidateWrapperDeleteAuthViews } from "@/pages/mcp/x/tabs/settings/sections/sourceInvalidation";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import {
  createDefaultMcpEndpoint,
  DEFAULT_ENDPOINT_FAILED_MESSAGE,
} from "@/lib/mcpEndpoints";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import { invalidateAllMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { invalidateAllGetRemoteMcpServer } from "@gram/client/react-query/getRemoteMcpServer.js";
import { invalidateAllRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { invalidateAllRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import { invalidateAllUserSessionIssuers } from "@gram/client/react-query/userSessionIssuers.js";
import {
  useMutation,
  useQueryClient,
  type UseMutationResult,
} from "@tanstack/react-query";
import { toast } from "sonner";
import {
  configureCreatedRemoteMcpIdentity,
  type ConfigureCreatedIdentityResult,
  type RemoteMcpCreationIdentity,
} from "./configureCreatedIdentity";

export type CreateRemoteMcpSourceVariables = {
  name?: string | undefined;
  url: string;
  userSessionIssuerId?: string | undefined;
  organizationOwnedUserSessionIssuer?: boolean | undefined;
  identityMode: RemoteMcpCreationIdentity;
  agentAuthorization?: string;
};

export type CreateRemoteMcpSourceData = {
  remoteMcpServer: RemoteMcpServer;
  mcpServer: McpServer;
  identityConfiguration: ConfigureCreatedIdentityResult;
};

export function useCreateRemoteMcpSource(): UseMutationResult<
  CreateRemoteMcpSourceData,
  Error,
  CreateRemoteMcpSourceVariables
> {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const { orgSlug } = useSlugs();

  return useMutation({
    mutationFn: async ({
      name,
      url,
      identityMode,
      agentAuthorization,
      userSessionIssuerId,
      organizationOwnedUserSessionIssuer,
    }) => {
      const { remoteMcpServer, mcpServer } =
        await client.remoteMcp.createServerAndMcpServer({
          createServerForm: {
            name,
            url,
            transportType: "streamable-http",
            userSessionIssuerId,
          },
        });

      const identityConfiguration = await configureCreatedRemoteMcpIdentity({
        client,
        remoteMcpServer,
        mcpServer,
        identityMode,
        agentAuthorization,
        organizationOwnedUserSessionIssuer,
      });
      const configuredMcpServer =
        identityConfiguration.status === "configured"
          ? identityConfiguration.mcpServer
          : mcpServer;

      // Pre-stage a default endpoint so the user doesn't have to create one
      // before the server can serve. Best-effort: never rolls back the source.
      if (orgSlug) {
        await createDefaultMcpEndpoint(client, configuredMcpServer, orgSlug);
      } else {
        toast.warning(DEFAULT_ENDPOINT_FAILED_MESSAGE);
      }

      return {
        remoteMcpServer,
        mcpServer: configuredMcpServer,
        identityConfiguration,
      };
    },
    onSuccess: async ({ identityConfiguration }) => {
      // refetchType "all" forces the refetch even when there are no active
      // observers — Sources isn't mounted while the create form is, so without
      // this the listServers cache stays stale until the next mount.
      const invalidations = [
        invalidateAllRemoteMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
        // Creation either links an existing issuer or mints a project issuer,
        // so refresh the effective list in both cases.
        invalidateAllUserSessionIssuers(queryClient, { refetchType: "all" }),
      ];
      // The issuer/client caches only change when explicit identity setup ran
      // to completion; an incomplete setup leaves them untouched, so don't
      // force those extra refetches on the common no-OAuth path.
      if (identityConfiguration.userIdentity?.status) {
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

export type DeleteRemoteMcpSourceVariables = {
  remoteMcpServerId: string;
};

export function useDeleteRemoteMcpSource(): UseMutationResult<
  void,
  Error,
  DeleteRemoteMcpSourceVariables
> {
  const client = useSdkClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({ remoteMcpServerId }) => {
      // Soft-delete each linked mcp_server first; the server-side handler
      // cascades to its mcp_endpoints. Only then does the source go.
      await deleteSourceCascade({
        listLinked: () =>
          fetchLinkedMcpServers(client, queryClient, { remoteMcpServerId }),
        deleteMcpServer: (id) => client.mcpServers.delete({ id }),
        deleteSource: () =>
          client.remoteMcp.deleteServer({ id: remoteMcpServerId }),
        sourceLabel: "remote MCP source",
      });
    },
    onSuccess: async () => {
      // Mark stale only (refetchType "none"): the deleted server's own queries
      // are still mounted on its detail page until the caller navigates away,
      // and force-refetching them here would block mutateAsync on requests for
      // a resource that no longer exists. Consumers refetch on their next
      // mount after navigation.
      await Promise.all([
        invalidateAllRemoteMcpServers(queryClient, { refetchType: "none" }),
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
        invalidateAllGetRemoteMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllRemoteMcpServers(queryClient, { refetchType: "all" }),
        invalidateWrapperDeleteAuthViews(queryClient, { refetchType: "all" }),
      ]);
    },
  });
}
