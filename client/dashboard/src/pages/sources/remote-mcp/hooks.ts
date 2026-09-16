import { useFetcher } from "@/contexts/Fetcher";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import {
  createDefaultMcpEndpoint,
  DEFAULT_ENDPOINT_FAILED_MESSAGE,
} from "@/lib/mcpEndpoints";
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
import { toast } from "sonner";
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
      const { remoteMcpServer, mcpServer } =
        await client.remoteMcp.createServerAndMcpServer({
          createServerForm: {
            name,
            url,
            transportType: "streamable-http",
          },
        });

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
      if (orgSlug) {
        await createDefaultMcpEndpoint(client, configuredMcpServer, orgSlug);
      } else {
        toast.warning(DEFAULT_ENDPOINT_FAILED_MESSAGE);
      }

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
