import { hashKey, useQuery, type UseQueryResult } from "@tanstack/react-query";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";

/** Ownership independently permits reads; let the server filter the inventory. */
export function useReadableAgents(
  enabled: boolean,
  refetchInterval?: number,
): UseQueryResult<ManagedAgent[], Error> {
  const organization = useOrganization();
  const sdk = useSdkClient();
  const { user } = useSession();
  return useQuery({
    queryKey: ["managed-agents", organization.id, "list", user.id],
    queryKeyHashFn: hashKey,
    queryFn: ({ signal }) => sdk.agents.list(undefined, undefined, { signal }),
    enabled,
    throwOnError: false,
    retry: false,
    refetchInterval,
  });
}
