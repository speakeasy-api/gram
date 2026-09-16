import { useQuery } from "@tanstack/react-query";

import type { IdentityProviderReadiness } from "@gram/admin-client/models/components/identityproviderreadiness";
import { organizationGuidedReadinessQuery } from "@/lib/gramAdminClient";

/** The parts of a React Query result the readiness panel draws. */
export interface GuidedReadinessQuery {
  data: IdentityProviderReadiness | undefined;
  isPending: boolean;
  isFetching: boolean;
  error: unknown;
  refetch: () => void;
}

/**
 * The one seam between the admin readiness panel and the server.
 *
 * These pages have no Gram context provider, so the generated `use*` hook
 * cannot be called here: the query is built against this app's own client, the
 * way every other admin read is.
 */
export function useOrganizationGuidedReadiness(
  organizationID: string,
): GuidedReadinessQuery {
  const query = useQuery(organizationGuidedReadinessQuery(organizationID));

  return {
    data: query.data,
    isPending: query.isPending,
    isFetching: query.isFetching,
    error: query.error,
    refetch: () => void query.refetch(),
  };
}
