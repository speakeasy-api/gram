import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";

/**
 * The agent's policy grants, read once for every panel that shows them: the
 * Permissions tab, the permission editor (which invalidates this key on save)
 * and the Overview's "Acting on behalf of". The endpoint requires setup/write
 * access, independently of agent:read, so it is not asked without it.
 */
export function useAgentPolicyGrants(
  agent: ManagedAgent,
): UseQueryResult<AgentPolicyGrant[]> {
  const organization = useOrganization();
  const sdk = useSdkClient();
  return useQuery({
    queryKey: ["agent-policy-grants", organization.id, agent.id],
    queryFn: ({ signal }) =>
      sdk.agents.listPolicyGrants({ agentId: agent.id }, undefined, { signal }),
    enabled: agent.permissions.write,
    retry: false,
    throwOnError: false,
  });
}
