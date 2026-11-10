import type { QueryClient } from "@tanstack/react-query";
import type { ServerIdentityDedicatedIssuerImpact } from "@gram/client/models/components/serveridentitydedicatedissuerimpact.js";
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
  /**
   * Set when the refused change can be saved by moving the server to a new
   * user session issuer of its own, with what that move costs.
   */
  dedicatedIssuer?: ServerIdentityDedicatedIssuerImpact;
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
  const refusal = refusalMessage(query.error);
  // A refused replace made from a server may be one the server can still save
  // by leaving the issuer, which only the server can tell.
  const offerRequest =
    request && request.change === "replace" && request.mcpServerId
      ? { ...request, dedicatedIssuer: true }
      : null;
  const offer = useServerIdentityImpact(
    offerRequest ?? { userSessionIssuerId: "", change: "detach" },
    undefined,
    {
      enabled: offerRequest !== null && refusal !== undefined,
      throwOnError: false,
      retry: false,
      staleTime: 0,
      refetchOnMount: "always",
    },
  );
  if (!request) return NO_IMPACT;
  const offered = offerRequest !== null && refusal !== undefined;
  return {
    servers: query.data?.servers ?? [],
    hiddenCount: query.data?.hiddenServerCount ?? 0,
    // A cached answer for an older binding state must not arm the confirm.
    pending: query.isFetching || (offered && offer.isFetching),
    failed: query.isError && refusal === undefined,
    refusal,
    dedicatedIssuer: offered ? offer.data?.dedicatedIssuer : undefined,
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
 * The change cannot be confirmed here: the server refuses it with no way to
 * move this server off the issuer, or it reaches servers the user cannot see.
 */
export function sharedIssuerChangeBlocked(
  impact: SharedIssuerImpactState,
): boolean {
  if (impact.dedicatedIssuer !== undefined) return false;
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
