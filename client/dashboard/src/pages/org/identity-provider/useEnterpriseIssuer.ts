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
// After it is ready the hourly sweep activates the key; check about once a
// minute until a little past the hour, so the page shows the new key.
const ACTIVATION_POLL_MS = 60_000;
const MAX_ACTIVATION_WAIT_MS = 70 * 60_000;

/**
 * Polls while a rotation is pending: quickly until it is ready, then slowly
 * until the sweep's automatic activation would have happened.
 */
export function rotationPollInterval(
  rotationReadyAt: Date | undefined,
  now: number = Date.now(),
): number | false {
  if (!rotationReadyAt) return false;
  const wait = rotationReadyAt.getTime() - now;
  if (Number.isNaN(wait) || wait > MAX_ROTATION_WAIT_MS) return false;
  if (wait > 0) return ROTATION_POLL_MS;
  return -wait <= MAX_ACTIVATION_WAIT_MS ? ACTIVATION_POLL_MS : false;
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
    refetchOnWindowFocus: true,
    refetchInterval: (query) =>
      rotationPollInterval(query.state.data?.rotationReadyAt),
  });
}
