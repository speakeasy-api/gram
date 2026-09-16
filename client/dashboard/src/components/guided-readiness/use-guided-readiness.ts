import type { GuidedReadiness } from "./types";

/** The parts of a React Query result the readiness panel draws. */
export interface GuidedReadinessQuery {
  data: GuidedReadiness | undefined;
  isPending: boolean;
  isFetching: boolean;
  error: unknown;
  refetch: () => void;
}

/**
 * The one seam between the readiness panel and the server.
 *
 * identityProviders.getGuidedReadiness is not generated yet, so this reports
 * the read as still in flight — which is what the panel already draws for a
 * slow one, and the only honest answer while nothing has been asked. Repoint
 * this body at the generated hook and every surface picks it up; nothing else
 * knows where readiness comes from.
 *
 * `enabled` is false for anyone who cannot see the panel, so the real hook
 * never fires a request the caller would throw away.
 */
export function useGuidedReadiness(enabled: boolean): GuidedReadinessQuery {
  return {
    data: undefined,
    isPending: enabled,
    isFetching: false,
    error: null,
    refetch: () => undefined,
  };
}
