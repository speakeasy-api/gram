import type { GramCore } from "@gram/client/core.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import type { QueryHookOptions } from "@gram/client/react-query/_types.js";
import {
  buildPlatformMCPOnboardingQuery,
  type PlatformMCPOnboardingQueryData,
  type PlatformMCPOnboardingQueryError,
} from "@gram/client/react-query/platformMCPOnboarding.js";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";

export function buildOrganizationPlatformMCPOnboardingQuery(
  client: GramCore,
  organizationId: string,
  options?: QueryHookOptions<
    PlatformMCPOnboardingQueryData,
    PlatformMCPOnboardingQueryError
  >,
): ReturnType<typeof buildPlatformMCPOnboardingQuery> {
  const query = buildPlatformMCPOnboardingQuery(
    client,
    { gramSession: "" },
    { sessionHeaderGramSession: "" },
    options,
  );

  return {
    ...query,
    queryKey: [...query.queryKey, { organizationId }],
  };
}

export function useOrganizationPlatformMCPOnboarding(
  organizationId: string,
  options?: QueryHookOptions<
    PlatformMCPOnboardingQueryData,
    PlatformMCPOnboardingQueryError
  >,
): UseQueryResult<
  PlatformMCPOnboardingQueryData,
  PlatformMCPOnboardingQueryError
> {
  const client = useGramContext();
  return useQuery({
    ...buildOrganizationPlatformMCPOnboardingQuery(
      client,
      organizationId,
      options,
    ),
    ...options,
  });
}
