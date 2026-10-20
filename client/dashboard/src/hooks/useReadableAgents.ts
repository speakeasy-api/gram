import { hashKey, useQuery, type UseQueryResult } from "@tanstack/react-query";
import { useMemo } from "react";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { collectPageItems } from "@/components/sessions/collectPageItems";

/** Ownership independently permits reads; let the server filter the inventory. */
export function useReadableAgents(
  enabled: boolean,
): UseQueryResult<ManagedAgent[], Error> {
  const organization = useOrganization();
  const sdk = useSdkClient();
  const { user } = useSession();
  return useQuery({
    queryKey: ["managed-agents", organization.id, "list", user.id],
    queryKeyHashFn: hashKey,
    // agents.list pages. Every caller of this hook builds a lookup map or a
    // whole-inventory list, so a first page would silently resolve some agents
    // and leave the rest showing as unknown.
    queryFn: ({ signal }) =>
      collectPageItems(sdk.agents.list(undefined, undefined, { signal })),
    enabled,
    throwOnError: false,
    retry: false,
  });
}

/**
 * The agents this viewer may read, by id. An agent missing from the map is
 * one they cannot read or one that no longer exists, so its name stays hidden.
 */
export function useReadableAgentsById(
  enabled: boolean,
): Map<string, ManagedAgent> {
  const agents = useReadableAgents(enabled);
  return useMemo(
    () =>
      new Map(
        (agents.isError ? [] : (agents.data ?? [])).map((agent) => [
          agent.id,
          agent,
        ]),
      ),
    [agents.data, agents.isError],
  );
}
