import type { IdentityModel } from "@gram/client/models/components/identitymodel.js";
import type { useOrgRoutes, useRoutes } from "@/routes";

/**
 * The "Open in …" targets, pre-filtered to the person.
 *
 * Each subsystem filters on the identifier it records activity under, and each
 * page names that filter differently in the URL, so the mapping lives here
 * rather than being spelled out at every panel.
 */
export function identityHandoffs(
  identity: IdentityModel,
  routes: ReturnType<typeof useRoutes>,
  orgRoutes: ReturnType<typeof useOrgRoutes>,
  /** The member's principal URN, when the subject has a directory row. */
  principalUrn: string | undefined,
): {
  auditLogs: string;
  agentSessions: string;
  toolLogs: string;
  costs: string;
  riskEvents: string;
  challenges: string;
  shadowMcp: string;
  deviceAgent: string;
} {
  const userId = identity.userIds[0];
  const email = identity.emails[0];
  const externalUserId = identity.externalUserIds[0];
  const query = (base: string, params: Record<string, string | undefined>) => {
    const search = new URLSearchParams();
    for (const [key, value] of Object.entries(params)) {
      if (value) search.set(key, value);
    }
    const encoded = search.toString();
    return encoded ? `${base}?${encoded}` : base;
  };

  return {
    // Audit logs key on the Gram user id, which is what its actor facet holds.
    auditLogs: query(orgRoutes.auditLogs.href(), { actor: userId }),
    // Agent sessions has no user dimension of its own; its search covers the
    // chat's user id and name, so the address is the closest honest filter.
    agentSessions: query(routes.agentSessions.href(), {
      search: email ?? externalUserId,
    }),
    toolLogs: query(routes.logs.href(), { user: email }),
    // Costs filters by drilling rather than by query param: the email
    // dimension is a path segment on the explorer.
    costs: email
      ? `${routes.costs.href()}/email~${encodeURIComponent(email)}`
      : routes.costs.href(),
    riskEvents: query(routes.riskEvents.href(), {
      user_id: externalUserId ?? email,
    }),
    challenges: query(`${orgRoutes.access.href()}/challenges`, {
      identity: principalUrn,
    }),
    // Neither of these has a per-person filter to carry: the shadow inventory
    // is keyed by server and the device list by device.
    shadowMcp: routes.shadowMCP.href(),
    deviceAgent: orgRoutes.deviceAgent.href(),
  };
}
