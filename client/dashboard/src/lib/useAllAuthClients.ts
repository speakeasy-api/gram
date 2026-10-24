import { collectPageItems } from "@/components/sessions/collectPageItems";
import { useSdkClient } from "@/contexts/Sdk";
import type { AuthClient } from "@gram/client/models/components/authclient.js";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";

/**
 * Every auth client in the organization, across all pages. Keyed under the
 * SDK's list key so its invalidation refreshes this too.
 */
export function useAllAuthClients({
  enabled = true,
}: { enabled?: boolean } = {}): UseQueryResult<AuthClient[]> {
  const sdk = useSdkClient();
  return useQuery({
    queryKey: ["@gram/client", "authClients", "list", { all: true }],
    queryFn: ({ signal }) =>
      collectPageItems(
        sdk.authClients.list({ limit: 100 }, undefined, { signal }),
      ),
    enabled,
    throwOnError: false,
  });
}
