import { invalidateAllCustomDomainMcpEndpoints } from "@gram/client/react-query/customDomainMcpEndpoints.js";
import { invalidateAllGetDomain } from "@gram/client/react-query/getDomain.js";
import { invalidateAllMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { invalidateAllListDomains } from "@gram/client/react-query/listDomains.js";
import { invalidateAllRootMcpServers } from "@gram/client/react-query/rootMcpServers.js";
import {
  type SetRootMcpEndpointMutationVariables,
  useSetRootMcpEndpointMutation,
} from "@gram/client/react-query/setRootMcpEndpoint.js";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import { type QueryClient, useQueryClient } from "@tanstack/react-query";
import { useCallback, useRef } from "react";
import { toast } from "sonner";

/**
 * Writes an updated endpoint into every cached endpoint list.
 *
 * Invalidation alone would get there, but only after a refetch round-trip,
 * and the detail-page sidebar keeps its own observer of the same list — so
 * the address it shows visibly lags the field the user just saved. Seeding
 * the response notifies every observer synchronously instead.
 */
export function patchMcpEndpointInCache(
  queryClient: QueryClient,
  endpoint: McpEndpoint,
): void {
  queryClient.setQueriesData<{ mcpEndpoints: McpEndpoint[] }>(
    { queryKey: ["@gram/client", "mcpEndpoints", "list"] },
    (current) => {
      if (!current?.mcpEndpoints.some((e) => e.id === endpoint.id)) {
        return current;
      }
      return {
        ...current,
        mcpEndpoints: current.mcpEndpoints.map((e) =>
          e.id === endpoint.id ? endpoint : e,
        ),
      };
    },
  );
}

export async function invalidateRootMcpEndpointQueries(
  queryClient: QueryClient,
): Promise<void> {
  await Promise.all([
    invalidateAllGetDomain(queryClient, { refetchType: "all" }),
    invalidateAllCustomDomainMcpEndpoints(queryClient, {
      refetchType: "all",
    }),
    invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
    invalidateAllRootMcpServers(queryClient, { refetchType: "all" }),
    invalidateAllListDomains(queryClient, { refetchType: "all" }),
  ]);
}

export function useRootMcpEndpointMutation(): {
  isPending: boolean;
  setRootMcpEndpoint: (customDomainId: string, mcpEndpointId?: string) => void;
  setRootMcpServer: (customDomainId: string, mcpServerId?: string) => void;
} {
  const queryClient = useQueryClient();
  const retryMutation = useRef<
    (variables: SetRootMcpEndpointMutationVariables) => void
  >(() => undefined);

  const mutation = useSetRootMcpEndpointMutation({
    onSuccess: async (_, variables) => {
      await invalidateRootMcpEndpointQueries(queryClient);
      const isSet =
        variables.request.setRootMcpEndpointRequestBody.mcpEndpointId !==
          undefined ||
        variables.request.setRootMcpEndpointRequestBody.mcpServerId !==
          undefined;
      toast.success(isSet ? "Domain root updated" : "Domain root cleared");
    },
    onError: async (_, variables) => {
      await invalidateRootMcpEndpointQueries(queryClient);
      toast.error("Domain root routing may not be applied", {
        description:
          "The saved selection has been refreshed. Retry to reconcile routing.",
        action: {
          label: "Retry",
          onClick: () => retryMutation.current(variables),
        },
      });
    },
  });
  retryMutation.current = mutation.mutate;

  const { mutate } = mutation;
  const setRootMcpEndpoint = useCallback(
    (customDomainId: string, mcpEndpointId?: string) => {
      mutate({
        security: { sessionHeaderGramSession: "" },
        request: {
          setRootMcpEndpointRequestBody: {
            customDomainId,
            mcpEndpointId,
          },
        },
      });
    },
    [mutate],
  );

  // Attaches the server to the domain (creating its endpoint when missing)
  // and maps it to the root in one call; legal while the domain is pending.
  const setRootMcpServer = useCallback(
    (customDomainId: string, mcpServerId?: string) => {
      mutate({
        security: { sessionHeaderGramSession: "" },
        request: {
          setRootMcpEndpointRequestBody: {
            customDomainId,
            mcpServerId,
          },
        },
      });
    },
    [mutate],
  );

  return {
    isPending: mutation.isPending,
    setRootMcpEndpoint,
    setRootMcpServer,
  };
}
