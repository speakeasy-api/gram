import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import type { InternalSessionIssuer } from "@gram/client/models/components/internalsessionissuer.js";
import { useOrganization } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildInternalSessionIssuerQuery } from "@gram/client/react-query/internalSessionIssuer.core.js";
import { enterpriseIssuerQueryKey } from "@/pages/org/identity-provider/useEnterpriseIssuer";

/** Speakeasy's issuer status for anyone who can read the organization, not only its admins. */
export function useSpeakeasyIssuerStatus(): UseQueryResult<
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
    enabled:
      hasScope("org:read", organization.id) ||
      hasScope("org:admin", organization.id),
    throwOnError: false,
    retry: false,
  });
}
