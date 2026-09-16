import type { IdentityProviderReadiness } from "@gram/client/models/components/identityproviderreadiness.js";
import { useGuidedReadiness as useGeneratedGuidedReadiness } from "@gram/client/react-query/guidedReadiness.js";

/** The parts of a React Query result the readiness panel draws. */
export interface GuidedReadinessQuery {
  data: IdentityProviderReadiness | undefined;
  isPending: boolean;
  isFetching: boolean;
  error: unknown;
  refetch: () => void;
}

/**
 * The one seam between the readiness surfaces and the server, so the panel and
 * the provider grid read one answer through one call site.
 *
 * `enabled` is false for anyone who cannot be shown the answer and for a caller
 * that does not need it, so no request goes out to be thrown away.
 */
export function useGuidedReadiness(enabled: boolean): GuidedReadinessQuery {
  const query = useGeneratedGuidedReadiness(undefined, undefined, {
    enabled,
    // Readiness reports; a failed read is a state the surfaces draw rather
    // than an error the page should fall over on.
    throwOnError: false,
  });

  return {
    data: query.data,
    isPending: query.isPending,
    isFetching: query.isFetching,
    error: query.error,
    refetch: () => void query.refetch(),
  };
}
