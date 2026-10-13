import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import type { InternalSessionIssuer } from "@gram/client/models/components/internalsessionissuer.js";
import { useOrganization } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildInternalSessionIssuerQuery } from "@gram/client/react-query/internalSessionIssuer.core.js";

export const enterpriseIssuerQueryKey = (organizationId: string) =>
  [
    "@gram/client",
    "internalSessionIssuers",
    "get",
    { organizationId },
  ] as const;

const ROTATION_POLL_MS = 5_000;
const MAX_ROTATION_WAIT_MS = 5 * 60_000;

/** Polls while a pending rotation waits to become ready, for a few minutes at most. */
export function rotationPollInterval(
  rotationReadyAt: Date | undefined,
  now: number = Date.now(),
): number | false {
  if (!rotationReadyAt) return false;
  const wait = rotationReadyAt.getTime() - now;
  return wait > 0 && wait <= MAX_ROTATION_WAIT_MS ? ROTATION_POLL_MS : false;
}

/** Keep public issuer metadata isolated when the active organization changes. Only org admins read it. */
export function useEnterpriseIssuer(): UseQueryResult<
  InternalSessionIssuer,
  Error
> {
  const organization = useOrganization();
  const { hasScope } = useRBAC();
  const client = useGramContext();
  return useQuery({
    ...buildInternalSessionIssuerQuery(client, undefined, {
      sessionHeaderGramSession: "",
    }),
    queryKey: enterpriseIssuerQueryKey(organization.id),
    enabled: hasScope("org:admin", organization.id),
    throwOnError: false,
    retry: false,
    refetchInterval: (query) =>
      rotationPollInterval(query.state.data?.rotationReadyAt),
  });
}
