import type { QueryClient } from "@tanstack/react-query";
import type { ServerIdentityImpactServer } from "@gram/client/models/components/serveridentityimpactserver.js";
import type { GetServerIdentityImpactRequest } from "@gram/client/models/operations/getserveridentityimpact.js";
import {
  invalidateAllServerIdentityImpact,
  useServerIdentityImpact,
} from "@gram/client/react-query/serverIdentityImpact.js";

/** What a change does to one server sharing the user session issuer. */
export type SharedIssuerImpact = ServerIdentityImpactServer["impact"];

/** Other servers a client binding change affects, as the server derives it. */
export type SharedIssuerImpactState = {
  servers: ServerIdentityImpactServer[];
  /** Servers on the issuer the caller cannot read, so they are not named. */
  hiddenCount: number;
  pending: boolean;
  failed: boolean;
  /** Why the server refuses this change; the commit would refuse it too. */
  refusal?: string;
};

const NO_IMPACT: SharedIssuerImpactState = {
  servers: [],
  hiddenCount: 0,
  pending: false,
  failed: false,
};

function refusalMessage(error: unknown): string | undefined {
  if (
    error instanceof Error &&
    (error as { statusCode?: unknown }).statusCode === 409
  ) {
    return error.message;
  }
  return undefined;
}

/**
 * Asks the server which other servers, in any project, a client binding
 * change on the user session issuer would re-point, clear or sign out. Pass
 * null when nothing about the bindings changes.
 */
export function useSharedIssuerImpact(
  request: GetServerIdentityImpactRequest | null,
): SharedIssuerImpactState {
  const query = useServerIdentityImpact(
    request ?? { userSessionIssuerId: "", change: "detach" },
    undefined,
    {
      enabled: request !== null,
      throwOnError: false,
      retry: false,
      staleTime: 0,
      refetchOnMount: "always",
    },
  );
  if (!request) return NO_IMPACT;
  const refusal = refusalMessage(query.error);
  return {
    servers: query.data?.servers ?? [],
    hiddenCount: query.data?.hiddenServerCount ?? 0,
    // A cached answer for an older binding state must not arm the confirm.
    pending: query.isFetching,
    failed: query.isError && refusal === undefined,
    refusal,
  };
}

/** Drops cached previews once a binding change lands. */
export function invalidateSharedIssuerImpact(
  queryClient: QueryClient,
): Promise<void> {
  return invalidateAllServerIdentityImpact(queryClient);
}

/** The change affects servers other than the target, or could not be checked. */
export function needsSharedIssuerConfirm(
  impact: SharedIssuerImpactState,
): boolean {
  return (
    impact.failed ||
    impact.refusal !== undefined ||
    impact.servers.length > 0 ||
    impact.hiddenCount > 0
  );
}

/**
 * The change cannot be confirmed here: the server refuses it, or it reaches
 * servers the user cannot see.
 */
export function sharedIssuerChangeBlocked(
  impact: SharedIssuerImpactState,
): boolean {
  return impact.refusal !== undefined || impact.hiddenCount > 0;
}

/** Affected servers grouped by impact, in a stable order. */
export function groupByImpact(
  servers: readonly ServerIdentityImpactServer[],
): Array<[SharedIssuerImpact, ServerIdentityImpactServer[]]> {
  const order: SharedIssuerImpact[] = [
    "repoint",
    "clear",
    "resignin",
    "client_removed",
  ];
  return order
    .map((impact): [SharedIssuerImpact, ServerIdentityImpactServer[]] => [
      impact,
      servers.filter((server) => server.impact === impact),
    ])
    .filter(([, group]) => group.length > 0);
}
