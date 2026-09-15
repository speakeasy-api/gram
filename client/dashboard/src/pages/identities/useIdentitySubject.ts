import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import type { IdentityModel } from "@gram/client/models/components/identitymodel.js";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { useIdentity } from "@gram/client/react-query/identity.js";
import { hashKey, useQuery, type UseQueryResult } from "@tanstack/react-query";

/** Agent ownership does not attribute the owner's human activity to the agent. */
export function agentIdentitySubject(agent: ManagedAgent): IdentityModel {
  return {
    canonicalUrn: `agent:${agent.id}`,
    kind: "agent",
    displayName: agent.name,
    directory: { groups: [] },
    emails: [],
    externalUserIds: [],
    userIds: [],
  };
}

/** The identity resolver handles people; registered agents use their management API. */
export function useIdentitySubject(
  urn: string,
): UseQueryResult<IdentityModel, Error> {
  const organization = useOrganization();
  const sdk = useSdkClient();
  const isAgent = urn.startsWith("agent:");
  const agentId = isAgent ? urn.slice("agent:".length) : "";
  const identity = useIdentity({ urn }, undefined, {
    throwOnError: false,
    enabled: !!urn && !isAgent,
  });
  const agent = useQuery({
    queryKey: ["managed-agents", organization.id, "detail", agentId],
    queryKeyHashFn: hashKey,
    queryFn: ({ signal }) =>
      sdk.agents.get({ id: agentId }, undefined, { signal }),
    select: agentIdentitySubject,
    enabled: isAgent,
    throwOnError: false,
    retry: false,
  });
  return isAgent ? agent : identity;
}
