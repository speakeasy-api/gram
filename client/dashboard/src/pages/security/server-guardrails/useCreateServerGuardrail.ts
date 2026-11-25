import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { useRiskCreatePolicyMutation } from "@gram/client/react-query/riskCreatePolicy.js";
import { invalidateAllRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { invalidateAllRiskListPoliciesForMcpServer } from "@gram/client/react-query/riskListPoliciesForMcpServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { useDetectorMode } from "../use-detector-mode";
import {
  buildServerGuardrailRequest,
  type ServerGuardrailState,
} from "./server-guardrail-policy";

export interface CreateServerGuardrailInput {
  state: ServerGuardrailState;
  mcpServerIds: string[];
  name: string;
}

/** Creates a risk policy scoped to the given MCP servers and refreshes the
 *  policy lists that show it. Rejects with the server's message on failure so
 *  callers can surface it. */
export function useCreateServerGuardrail(): {
  create: (input: CreateServerGuardrailInput) => Promise<RiskPolicy>;
  isPending: boolean;
  error: Error | null;
} {
  const queryClient = useQueryClient();
  const mode = useDetectorMode();
  const mutation = useRiskCreatePolicyMutation({
    onSuccess: () => {
      void invalidateAllRiskListPolicies(queryClient);
      void invalidateAllRiskListPoliciesForMcpServer(queryClient);
    },
  });

  const create = ({ state, mcpServerIds, name }: CreateServerGuardrailInput) =>
    mutation.mutateAsync({
      request: {
        createRiskPolicyRequestBody: buildServerGuardrailRequest(state, {
          mcpServerIds,
          name,
          mode,
        }),
      },
    });

  return { create, isPending: mutation.isPending, error: mutation.error };
}
