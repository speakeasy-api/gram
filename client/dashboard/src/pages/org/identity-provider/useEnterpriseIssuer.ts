import { useQuery } from "@tanstack/react-query";
import { useOrganization } from "@/contexts/Auth";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildInternalSessionIssuerQuery } from "@gram/client/react-query/internalSessionIssuer.core.js";

export const enterpriseIssuerQueryKey = (organizationId: string) =>
  [
    "@gram/client",
    "internalSessionIssuers",
    "get",
    { organizationId },
  ] as const;

/** Keep public issuer metadata isolated when the active organization changes. */
export function useEnterpriseIssuer() {
  const organization = useOrganization();
  const client = useGramContext();
  return useQuery({
    ...buildInternalSessionIssuerQuery(client),
    queryKey: enterpriseIssuerQueryKey(organization.id),
    throwOnError: false,
    retry: false,
    refetchInterval: (query) =>
      query.state.data?.rotationReadyAt ? 5_000 : false,
  });
}
