import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { Key } from "@gram/client/models/components/key.js";
import type { ListKeysResult } from "@gram/client/models/components/listkeysresult.js";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import {
  useListAPIKeys,
  type ListAPIKeysQueryError,
} from "@gram/client/react-query/listAPIKeys";

import { permissionsMissingFromKey } from "./agent-key-permissions";

const security = { sessionHeaderGramSession: "" };

/**
 * The agent's configured permissions. Shared with the permission editor, so
 * saving there refreshes every view of them, including which keys predate
 * the change. Reading the policy needs write access to the agent.
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

/** The agent's keys. Listing them needs authority to manage its credentials. */
export function useAgentKeys(
  agent: ManagedAgent,
): UseQueryResult<ListKeysResult, ListAPIKeysQueryError> {
  const organization = useOrganization();
  return useListAPIKeys({ agentId: agent.id }, security, {
    queryKeyHashFn: (key) => JSON.stringify([organization.id, key]),
    enabled: agent.permissions.authorize,
    retry: false,
    throwOnError: false,
  });
}

/**
 * Keys issued before a permission was added to the agent, which they cannot
 * use until reissued. Empty while either read is unavailable, so a missing
 * read never claims a key is out of date.
 */
export function useKeysMissingPermissions(agent: ManagedAgent): Key[] {
  const policy = useAgentPolicyGrants(agent);
  const keys = useAgentKeys(agent);
  if (
    !agent.permissions.write ||
    !agent.permissions.authorize ||
    !policy.data ||
    policy.isError ||
    !keys.data ||
    keys.isError
  )
    return [];
  return keys.data.keys.filter(
    (key) => permissionsMissingFromKey(key, policy.data).length > 0,
  );
}
