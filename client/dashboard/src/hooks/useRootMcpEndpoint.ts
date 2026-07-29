import { invalidateAllCustomDomainMcpEndpoints } from "@gram/client/react-query/customDomainMcpEndpoints.js";
import { invalidateAllGetDomain } from "@gram/client/react-query/getDomain.js";
import { invalidateAllMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import {
  type SetRootMcpEndpointMutationVariables,
  useSetRootMcpEndpointMutation,
} from "@gram/client/react-query/setRootMcpEndpoint.js";
import { type QueryClient, useQueryClient } from "@tanstack/react-query";
import { useCallback, useRef } from "react";
import { toast } from "sonner";

export async function invalidateRootMcpEndpointQueries(
  queryClient: QueryClient,
): Promise<void> {
  await Promise.all([
    invalidateAllGetDomain(queryClient, { refetchType: "all" }),
    invalidateAllCustomDomainMcpEndpoints(queryClient, {
      refetchType: "all",
    }),
    invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
  ]);
}

export function useRootMcpEndpointMutation(): {
  isPending: boolean;
  setRootMcpEndpoint: (customDomainId: string, mcpEndpointId?: string) => void;
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

  return {
    isPending: mutation.isPending,
    setRootMcpEndpoint,
  };
}
